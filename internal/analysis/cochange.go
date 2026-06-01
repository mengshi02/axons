// Package analysis provides code analysis utilities.
package analysis

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/pkg/types"
	"go.uber.org/zap"
)

// CoChangeAnalyzer analyzes git history for co-change patterns.
type CoChangeAnalyzer struct {
	repo    *repository.Repository
	rootDir string
}

// NewCoChangeAnalyzer creates a new co-change analyzer.
func NewCoChangeAnalyzer(repo *repository.Repository, rootDir string) *CoChangeAnalyzer {
	return &CoChangeAnalyzer{
		repo:    repo,
		rootDir: rootDir,
	}
}

// CommitInfo represents information about a git commit.
type CommitInfo struct {
	SHA   string
	Epoch int64
	Files []string
}

// CoChangeOptions contains options for co-change analysis.
type CoChangeOptions struct {
	Since      string
	AfterSHA   string
	MinSupport int
	MaxFiles   int
	KnownFiles map[string]bool
}

// Analyze performs co-change analysis on the git history.
func (a *CoChangeAnalyzer) Analyze(opts *CoChangeOptions) error {
	if opts == nil {
		opts = &CoChangeOptions{}
	}
	if opts.MinSupport == 0 {
		opts.MinSupport = 3
	}
	if opts.MaxFiles == 0 {
		opts.MaxFiles = 50
	}

	logger.Info("Starting co-change analysis",
		zap.String("root", a.rootDir),
		zap.String("since", opts.Since),
	)

	commits, err := a.scanGitHistory(opts)
	if err != nil {
		return fmt.Errorf("failed to scan git history: %w", err)
	}

	logger.Info("Scanned commits", zap.Int("count", len(commits)))

	coChanges, fileCommitCounts := a.computeCoChanges(commits, opts)

	logger.Info("Computed co-change pairs", zap.Int("count", len(coChanges)))

	if err := a.repo.SaveCoChanges(coChanges); err != nil {
		return fmt.Errorf("failed to save co-changes: %w", err)
	}

	for file, count := range fileCommitCounts {
		if err := a.repo.SaveFileCommitCount(&types.FileCommitCount{
			File:        file,
			CommitCount: count,
		}); err != nil {
			logger.Warn("Failed to save file commit count", zap.String("file", file), zap.Error(err))
		}
	}

	if err := a.repo.SaveCoChangeMeta("last_analysis", time.Now().Format(time.RFC3339)); err != nil {
		logger.Warn("Failed to save co-change metadata", zap.Error(err))
	}

	return nil
}

// scanGitHistory scans git log and returns commit information.
func (a *CoChangeAnalyzer) scanGitHistory(opts *CoChangeOptions) ([]CommitInfo, error) {
	args := []string{
		"log",
		"--name-only",
		"--pretty=format:%H%n%at",
		"--no-merges",
		"--diff-filter=AMRC",
	}

	if opts.Since != "" {
		args = append(args, fmt.Sprintf("--since=%s", opts.Since))
	}
	if opts.AfterSHA != "" {
		args = append(args, fmt.Sprintf("%s..HEAD", opts.AfterSHA))
	}
	args = append(args, "--", ".")

	cmd := exec.Command("git", args...)
	cmd.Dir = a.rootDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	return a.parseGitLogOutput(output, opts), nil
}

// parseGitLogOutput parses the output of git log.
func (a *CoChangeAnalyzer) parseGitLogOutput(output []byte, opts *CoChangeOptions) []CommitInfo {
	var commits []CommitInfo

	blocks := bytes.Split(bytes.TrimSpace(output), []byte("\n\n"))
	for _, block := range blocks {
		lines := bytes.Split(block, []byte("\n"))
		if len(lines) < 2 {
			continue
		}

		sha := string(lines[0])
		epoch, err := strconv.ParseInt(string(lines[1]), 10, 64)
		if err != nil {
			continue
		}

		var files []string
		for _, line := range lines[2:] {
			file := a.normalizePath(string(line))
			if file == "" {
				continue
			}
			if opts.KnownFiles != nil && !opts.KnownFiles[file] {
				continue
			}
			files = append(files, file)
		}

		if len(files) > 0 {
			commits = append(commits, CommitInfo{
				SHA:   sha,
				Epoch: epoch,
				Files: files,
			})
		}
	}

	return commits
}

// computeCoChanges computes co-change pairs from commit history.
func (a *CoChangeAnalyzer) computeCoChanges(commits []CommitInfo, opts *CoChangeOptions) ([]*types.CoChange, map[string]int) {
	fileCommitCounts := make(map[string]int)
	pairCounts := make(map[string]int)
	pairLastEpoch := make(map[string]int64)

	for _, commit := range commits {
		files := commit.Files

		if len(files) > opts.MaxFiles {
			continue
		}

		for _, f := range files {
			fileCommitCounts[f]++
		}

		for i := 0; i < len(files); i++ {
			for j := i + 1; j < len(files); j++ {
				fileA, fileB := files[i], files[j]
				if fileA > fileB {
					fileA, fileB = fileB, fileA
				}
				key := fileA + "|" + fileB
				pairCounts[key]++
				if commit.Epoch > pairLastEpoch[key] {
					pairLastEpoch[key] = commit.Epoch
				}
			}
		}
	}

	var coChanges []*types.CoChange
	for key, count := range pairCounts {
		if count < opts.MinSupport {
			continue
		}

		parts := strings.Split(key, "|")
		if len(parts) != 2 {
			continue
		}
		fileA, fileB := parts[0], parts[1]

		countA := fileCommitCounts[fileA]
		countB := fileCommitCounts[fileB]

		union := countA + countB - count
		var jaccard float64
		if union > 0 {
			jaccard = float64(count) / float64(union)
		}

		coChanges = append(coChanges, &types.CoChange{
			FileA:          fileA,
			FileB:          fileB,
			CommitCount:    count,
			Jaccard:        jaccard,
			LastCommitTime: pairLastEpoch[key],
		})
	}

	return coChanges, fileCommitCounts
}

// normalizePath normalizes a file path.
func (a *CoChangeAnalyzer) normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(a.rootDir, path)
		if err == nil {
			path = rel
		}
	}
	path = filepath.ToSlash(path)
	return path
}

// GetCoChangesForFile retrieves co-change relationships for a file.
func (a *CoChangeAnalyzer) GetCoChangesForFile(file string, limit int) ([]*types.CoChange, error) {
	return a.repo.FindCoChangesByFile(file, limit)
}

// ClearCoChangeData clears all co-change analysis data.
func (a *CoChangeAnalyzer) ClearCoChangeData() error {
	return a.repo.ClearCoChanges()
}
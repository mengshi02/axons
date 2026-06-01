package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxReadBytes caps the content returned by read_file to avoid overwhelming the context window.
const maxReadBytes = 512 * 1024 // 512 KB

// maxCommandTimeout is the upper limit for run_command timeout.
const maxCommandTimeout = 120

// handleReadFile reads a file from the filesystem.
// Path traversal outside the project root is blocked.
func (s *MCPServer) handleReadFile(ctx context.Context, req *mcp.CallToolRequest, args ReadFileArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Path == "" {
		return nil, nil, fmt.Errorf("path is required")
	}

	absPath, err := s.ResolveSafePath(args.Path)
	if err != nil {
		return nil, nil, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Trim to size limit before line filtering
	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	start := 1
	end := totalLines
	if args.StartLine > 0 {
		start = args.StartLine
	}
	if args.EndLine > 0 && args.EndLine < end {
		end = args.EndLine
	}
	// Clamp
	if start < 1 {
		start = 1
	}
	if end > totalLines {
		end = totalLines
	}

	selected := lines[start-1 : end]
	content := strings.Join(selected, "\n")

	return nil, map[string]interface{}{
		"path":        absPath,
		"content":     content,
		"start_line":  start,
		"end_line":    end,
		"total_lines": totalLines,
		"truncated":   len(data) >= maxReadBytes,
	}, nil
}

// handleWriteFile writes content to a file, creating parent directories as needed.
// Path traversal outside the project root is blocked.
func (s *MCPServer) handleWriteFile(ctx context.Context, req *mcp.CallToolRequest, args WriteFileArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Path == "" {
		return nil, nil, fmt.Errorf("path is required")
	}

	absPath, err := s.ResolveSafePath(args.Path)
	if err != nil {
		return nil, nil, err
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create directories: %w", err)
	}

	if err := os.WriteFile(absPath, []byte(args.Content), 0644); err != nil {
		return nil, nil, fmt.Errorf("failed to write file: %w", err)
	}

	return nil, map[string]interface{}{
		"path":  absPath,
		"bytes": len(args.Content),
		"ok":    true,
	}, nil
}

// handleRunCommand runs a shell command and returns stdout/stderr.
// Only an allowlist of safe commands is permitted.
func (s *MCPServer) handleRunCommand(ctx context.Context, req *mcp.CallToolRequest, args RunCommandArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Command == "" {
		return nil, nil, fmt.Errorf("command is required")
	}

	if !isAllowedCommand(args.Command) {
		return nil, nil, fmt.Errorf("command %q is not allowed; permitted commands: %s", args.Command, strings.Join(allowedCommands, ", "))
	}

	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > maxCommandTimeout {
		timeout = maxCommandTimeout
	}

	cwd := args.Cwd
	if cwd == "" {
		cwd = s.RootDir()
	} else {
		var err error
		cwd, err = s.ResolveSafePath(cwd)
		if err != nil {
			return nil, nil, err
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, args.Command, args.Args...)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return nil, map[string]interface{}{
		"command":   args.Command,
		"args":      args.Args,
		"cwd":       cwd,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"exit_code": exitCode,
		"ok":        exitCode == 0,
	}, nil
}

// allowedCommands is the allowlist for run_command.
var allowedCommands = []string{
	"go", "python", "python3", "node", "npm", "npx",
	"cargo", "rustc", "javac", "java", "mvn", "gradle",
	"git", "make", "sh", "bash",
}

func isAllowedCommand(cmd string) bool {
	// 提取命令的第一个单词(真正的命令名),兼容两种调用格式:
	// 1. "go" - 只有命令名
	// 2. "go test ./..." - 命令名带参数
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	base := filepath.Base(parts[0])
	for _, a := range allowedCommands {
		if base == a {
			return true
		}
	}
	return false
}

// ResolveSafePath resolves a path relative to the project root and ensures
// it does not escape outside the root directory.
func (s *MCPServer) ResolveSafePath(path string) (string, error) {
	root := s.RootDir()

	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(root, path))
	}

	if root != "" && !strings.HasPrefix(abs, root+string(filepath.Separator)) && abs != root {
		return "", fmt.Errorf("path %q is outside the project root %q", path, root)
	}
	return abs, nil
}

// RootDir returns the project root directory.
// Priority: rootPath > dbPath parent > current working directory
func (s *MCPServer) RootDir() string {
	if s.rootPath != "" {
		return s.rootPath
	}
	if s.dbPath != "" {
		return filepath.Dir(s.dbPath)
	}
	wd, _ := os.Getwd()
	return wd
}

// handleSmartRead intelligently reads a file based on its size.
// For small files (<500 lines): reads entire content.
// For medium files (500-2000 lines): reads with smart truncation (head + tail).
// For large files (>2000 lines): returns outline and suggests using search tools.
func (s *MCPServer) handleSmartRead(ctx context.Context, req *mcp.CallToolRequest, args SmartReadArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Path == "" {
		return nil, nil, fmt.Errorf("path is required")
	}

	absPath, err := s.ResolveSafePath(args.Path)
	if err != nil {
		return nil, nil, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)
	totalBytes := len(data)

	// Determine mode
	mode := args.Mode
	if mode == "" {
		mode = "auto"
	}

	// File size thresholds
	const smallFileLines = 500
	const mediumFileLines = 2000
	const maxResultBytes = 200 * 1024 // 200KB max result

	// Auto mode: select strategy based on file size
	if mode == "auto" {
		if totalLines <= smallFileLines {
			mode = "full"
		} else if totalLines <= mediumFileLines {
			mode = "truncated"
		} else {
			mode = "outline"
		}
	}

	switch mode {
	case "full":
		// Read entire file (for small files)
		content := string(data)
		truncated := false
		if len(content) > maxResultBytes {
			content = content[:maxResultBytes]
			truncated = true
		}
		return nil, map[string]interface{}{
			"mode":        "full",
			"path":        absPath,
			"content":     content,
			"total_lines": totalLines,
			"total_bytes": totalBytes,
			"truncated":   truncated,
		}, nil

	case "truncated":
		// Smart truncation: preserve head (imports, declarations) and tail
		headLines := smallFileLines * 3 / 5 // 60% of small file threshold
		tailLines := smallFileLines * 2 / 5 // 40% of small file threshold

		head := strings.Join(lines[:min(headLines, totalLines)], "\n")
		tail := ""
		if totalLines > tailLines {
			tail = strings.Join(lines[totalLines-tailLines:], "\n")
		}

		truncatedContent := head + fmt.Sprintf("\n\n... [TRUNCATED: file has %d lines, showing lines 1-%d and %d-%d] ...\n\n",
			totalLines, headLines, totalLines-tailLines+1, totalLines) + tail

		return nil, map[string]interface{}{
			"mode":        "truncated",
			"path":        absPath,
			"content":     truncatedContent,
			"total_lines": totalLines,
			"total_bytes": totalBytes,
			"head_lines":  headLines,
			"tail_lines":  tailLines,
			"truncated":   true,
		}, nil

	case "outline":
		// For very large files: return structure outline and suggest search tools
		// Extract first N lines as outline
		outlineLines := 100
		if totalLines < outlineLines {
			outlineLines = totalLines
		}
		outline := strings.Join(lines[:outlineLines], "\n")

		suggestion := fmt.Sprintf(`
⚠️ LARGE FILE DETECTED: %d lines (%d bytes)

This file is too large to read entirely. Recommended approaches:

1. **Use search tools to find specific code**:
   - keyword_search: Search for function/class names
   - hybrid_search: Semantic search for code concepts
   - get_node_by_file: List all symbols in this file

2. **Read specific line ranges**:
   - read_file with start_line and end_line parameters

3. **Use symbol-level operations**:
   - get_source_code with symbol IDs to read specific functions

First %d lines shown below as structure preview:
`, totalLines, totalBytes, outlineLines)

		return nil, map[string]interface{}{
			"mode":        "outline",
			"path":        absPath,
			"content":     suggestion + outline,
			"total_lines": totalLines,
			"total_bytes": totalBytes,
			"outline_lines": outlineLines,
			"truncated":   true,
			"suggestion":  "Use search tools or read_file with line ranges for large files",
		}, nil

	case "symbols":
		// Return list of symbols in the file (requires code graph)
		// This is a placeholder - actual implementation would query the code graph
		return nil, map[string]interface{}{
			"mode":        "symbols",
			"path":        absPath,
			"content":     "Symbol mode requires code graph. Use get_node_by_file tool instead.",
			"total_lines": totalLines,
			"total_bytes": totalBytes,
			"suggestion":  "Use get_node_by_file tool to list symbols in this file",
		}, nil

	default:
		return nil, nil, fmt.Errorf("invalid mode: %s (valid modes: auto, full, truncated, outline, symbols)", mode)
	}
}

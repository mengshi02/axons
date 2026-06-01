// Package graph provides process detection for execution flow materialization.
package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/utils"
	"github.com/mengshi02/axons/pkg/types"
)

// ProcessDetectorConfig controls detection behavior.
type ProcessDetectorConfig struct {
	MaxTraceDepth int // max BFS depth (default 10)
	MaxBranching  int // max callees to follow per node (default 4)
	MaxProcesses  int // max processes to detect (default 75)
	MinSteps      int // min steps for a valid process (default 3)
}

var defaultProcessConfig = ProcessDetectorConfig{
	MaxTraceDepth: 10,
	MaxBranching:  4,
	MaxProcesses:  75,
	MinSteps:      3,
}

// ProcessNode represents a detected execution flow.
type ProcessNode struct {
	ID           string   // "proc_handleBuild_12345"
	Label        string   // "handleBuild → BuildEdges → Finalize"
	ProcessType  string   // "intra_community" | "cross_community" | "unknown"
	StepCount    int
	EntryPointID int64
	TerminalID   int64
	CommunityIDs []int64
	Trace        []int64 // ordered node IDs
}

// ProcessDetector detects and materializes execution flows.
type ProcessDetector struct {
	repo   *repository.Repository
	db     *sql.DB
	config ProcessDetectorConfig
}

// NewProcessDetector creates a new ProcessDetector.
func NewProcessDetector(repo *repository.Repository, cfg ...ProcessDetectorConfig) *ProcessDetector {
	config := defaultProcessConfig
	if len(cfg) > 0 {
		config = cfg[0]
	}
	return &ProcessDetector{
		repo:   repo,
		db:     repo.DB(),
		config: config,
	}
}

// entryPointPatterns are universal function name patterns indicating entry points.
// These apply to all languages regardless of file extension.
var entryPointPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(main|init|bootstrap|start|run|setup|configure)$`),
	regexp.MustCompile(`^[Hh]andle[A-Z_]`),
	regexp.MustCompile(`^[Oo]n[A-Z_]`),
	regexp.MustCompile(`[Hh]andler$`),
	regexp.MustCompile(`[Cc]ontroller$`),
	regexp.MustCompile(`^[Pp]rocess[A-Z_]`),
	regexp.MustCompile(`^[Ee]xecute[A-Z_]`),
	regexp.MustCompile(`^[Pp]erform[A-Z_]`),
	regexp.MustCompile(`^[Dd]ispatch[A-Z_]`),
	regexp.MustCompile(`^[Ss]erve[A-Z_]`),
}

// langEntryPatterns maps file extension to language-specific entry point patterns.
var langEntryPatterns = map[string][]*regexp.Regexp{
	// Python: snake_case conventions
	".py": {
		regexp.MustCompile(`^(handle|on|view|endpoint|dispatch|process|execute|perform|serve|run|get|post|put|delete|patch)_`),
		regexp.MustCompile(`_(handler|view|endpoint|controller)$`),
		regexp.MustCompile(`^do_(get|post|put|delete|patch|request|handle)`),
		regexp.MustCompile(`^(wsgi_app|asgi_app|application)$`),
	},
	// Java: doXxx, servlet patterns, Spring MVC
	".java": {
		regexp.MustCompile(`^do(Get|Post|Put|Delete|Patch|Head|Options|Filter|Forward|Handle|Process|Execute|Run)`),
		regexp.MustCompile(`^(handle|process|execute|perform|dispatch|serve|run)[A-Z]`),
		regexp.MustCompile(`(Controller|Handler|Servlet|Endpoint|Action|Service)$`),
		regexp.MustCompile(`^(main|init|destroy|service)$`),
	},
	// Rust: fn main, fn run, fn handle, async fn serve
	".rs": {
		regexp.MustCompile(`^(main|run|start|serve|handle|execute|process|dispatch)$`),
		regexp.MustCompile(`^(handle|process|execute|dispatch|serve|run)_`),
		regexp.MustCompile(`_(handler|controller|service|endpoint)$`),
	},
	// Ruby: snake_case, Rails CRUD actions, Sidekiq perform
	".rb": {
		regexp.MustCompile(`^(handle|on|process|execute|perform|dispatch|serve|call|run)_`),
		regexp.MustCompile(`_(handler|controller|action)$`),
		regexp.MustCompile(`^(index|show|create|update|destroy|new|edit|call)$`),
		regexp.MustCompile(`^(handle|perform|execute|process|run)$`),
	},
	// PHP: handle, __invoke, Laravel resource actions
	".php": {
		regexp.MustCompile(`^(handle|process|execute|dispatch|invoke|run|call)$`),
		regexp.MustCompile(`^(handle|process|execute|dispatch|on)[A-Z_]`),
		regexp.MustCompile(`^__invoke$`),
		regexp.MustCompile(`(Controller|Handler|Action|Middleware)$`),
		regexp.MustCompile(`^(index|show|store|update|destroy|create|edit)$`),
	},
	// C: main, callback/hook patterns
	".c": {
		regexp.MustCompile(`^main$`),
		regexp.MustCompile(`^(handle|on|process|execute|dispatch|run|serve)_`),
		regexp.MustCompile(`_(handler|callback|hook|listener)$`),
	},
	// C++: main, PascalCase and snake_case patterns
	".cpp": {
		regexp.MustCompile(`^main$`),
		regexp.MustCompile(`^(handle|on|process|execute|dispatch|run|serve)_`),
		regexp.MustCompile(`_(handler|callback|hook|listener)$`),
		regexp.MustCompile(`^(Handle|Process|Execute|Dispatch|Run|Serve)[A-Z]`),
	},
	".cc": {
		regexp.MustCompile(`^main$`),
		regexp.MustCompile(`^(handle|on|process|execute|dispatch|run|serve)_`),
		regexp.MustCompile(`_(handler|callback|hook|listener)$`),
	},
	".cxx": {
		regexp.MustCompile(`^main$`),
		regexp.MustCompile(`^(handle|on|process|execute|dispatch|run|serve)_`),
		regexp.MustCompile(`_(handler|callback|hook|listener)$`),
	},
	// C#: async patterns, MVC controller actions, event handlers
	".cs": {
		regexp.MustCompile(`^(Handle|Process|Execute|Dispatch|Run|Serve|Perform)[A-Z]`),
		regexp.MustCompile(`^On[A-Z]`),
		regexp.MustCompile(`(Controller|Handler|Middleware|Action|Command)$`),
		regexp.MustCompile(`^(Main|Run|Start|Execute|Invoke|Handle)$`),
		regexp.MustCompile(`^(Get|Post|Put|Delete|Patch)[A-Za-z]`),
	},
	// JavaScript: handler/middleware/route patterns, Express
	".js": {
		regexp.MustCompile(`^(handle|on)[A-Z_]`),
		regexp.MustCompile(`_(handler|middleware|controller|route|action)$`),
		regexp.MustCompile(`^(handleRequest|middleware|handler|route|action|controller)$`),
		regexp.MustCompile(`^(get|post|put|delete|patch)[A-Z]`),
	},
	// TypeScript: same as JS plus NestJS decorators naming
	".ts": {
		regexp.MustCompile(`^(handle|on)[A-Z_]`),
		regexp.MustCompile(`_(handler|middleware|controller|route|action)$`),
		regexp.MustCompile(`^(handleRequest|middleware|handler|route|action|controller)$`),
		regexp.MustCompile(`^(get|post|put|delete|patch)[A-Z]`),
		regexp.MustCompile(`(Controller|Handler|Middleware|Resolver|Guard)$`),
	},
	".jsx": {
		regexp.MustCompile(`^(handle|on)[A-Z_]`),
		regexp.MustCompile(`_(handler|action)$`),
	},
	".tsx": {
		regexp.MustCompile(`^(handle|on)[A-Z_]`),
		regexp.MustCompile(`_(handler|action)$`),
	},
}

// fileExtension extracts the file extension from a file path (e.g. ".go", ".py").
// Delegates to utils.FileExtension.
func fileExtension(filePath string) string {
	return utils.FileExtension(filePath)
}

// scoreLangEntryPoint returns an additional score bonus if the node matches
// language-specific entry point patterns derived from its file extension.
func scoreLangEntryPoint(node *types.Node) float64 {
	ext := fileExtension(node.File)
	patterns, ok := langEntryPatterns[ext]
	if !ok {
		return 0
	}
	for _, pat := range patterns {
		if pat.MatchString(node.Name) {
			return 1.5
		}
	}
	return 0
}

// scoreEntryPoint calculates how likely this node is a process entry point.
// Higher = more likely.
func scoreEntryPoint(node *types.Node, callerCount, calleeCount int) float64 {
	if calleeCount == 0 {
		return 0 // leaf node, not an entry point
	}

	score := float64(calleeCount) / float64(callerCount+1) // call ratio

	if node.Exported {
		score += 1.0
	}

	for _, pat := range entryPointPatterns {
		if pat.MatchString(node.Name) {
			score += 2.0
			break
		}
	}

	// Language-specific bonus
	score += scoreLangEntryPoint(node)

	// Penalize test functions
	if strings.Contains(node.File, "_test.") ||
		strings.Contains(node.File, ".test.") ||
		strings.Contains(node.File, ".spec.") {
		score -= 5.0
	}

	return score
}

// DetectAndSave runs process detection and writes results to the DB.
func (pd *ProcessDetector) DetectAndSave() error {
	// Clear old processes
	if err := pd.clearOldProcesses(); err != nil {
		return fmt.Errorf("failed to clear old processes: %w", err)
	}

	// Load all function/method nodes
	functions, err := pd.loadFunctions()
	if err != nil {
		return fmt.Errorf("failed to load functions: %w", err)
	}
	if len(functions) == 0 {
		return nil
	}

	// Build caller count and callee map from DB
	callerCounts := make(map[int64]int)
	calleesMap := make(map[int64][]int64)

	rows, err := pd.db.Query(`SELECT source_id, target_id FROM edges WHERE kind = 'calls'`)
	if err != nil {
		return fmt.Errorf("failed to load call edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var src, tgt int64
		if err := rows.Scan(&src, &tgt); err != nil {
			continue
		}
		callerCounts[tgt]++
		calleesMap[src] = append(calleesMap[src], tgt)
	}
	rows.Close()

	// Score and sort entry points
	type scoredNode struct {
		node  *types.Node
		score float64
	}
	var candidates []scoredNode
	for _, fn := range functions {
		callers := callerCounts[fn.ID]
		callees := len(calleesMap[fn.ID])
		s := scoreEntryPoint(fn, callers, callees)
		if s > 0 {
			candidates = append(candidates, scoredNode{fn, s})
		}
	}
	// Sort descending by score (simple insertion sort - usually small N)
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].score > candidates[j-1].score; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}

	nodeMap := make(map[int64]*types.Node, len(functions))
	for _, fn := range functions {
		nodeMap[fn.ID] = fn
	}

	// BFS trace from each candidate
	seen := make(map[int64]bool) // entry points already used
	detected := make([]*ProcessNode, 0, pd.config.MaxProcesses)

	for _, c := range candidates {
		if len(detected) >= pd.config.MaxProcesses {
			break
		}
		if seen[c.node.ID] {
			continue
		}
		seen[c.node.ID] = true

		trace := pd.bfsTrace(c.node.ID, calleesMap, nodeMap)
		if len(trace) < pd.config.MinSteps {
			continue
		}

		proc := pd.buildProcessNode(c.node, trace, nodeMap)
		detected = append(detected, proc)
	}

	// Persist
	return pd.saveProcesses(detected)
}

// bfsTrace performs BFS forward from entryID along CALLS edges.
func (pd *ProcessDetector) bfsTrace(entryID int64, calleesMap map[int64][]int64, nodeMap map[int64]*types.Node) []int64 {
	visited := make(map[int64]bool)
	visited[entryID] = true
	trace := []int64{entryID}

	type bfsItem struct {
		id    int64
		depth int
	}
	queue := []bfsItem{{entryID, 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth >= pd.config.MaxTraceDepth {
			continue
		}

		callees := calleesMap[cur.id]
		count := 0
		for _, cid := range callees {
			if count >= pd.config.MaxBranching {
				break
			}
			if visited[cid] {
				continue
			}
			if _, ok := nodeMap[cid]; !ok {
				continue
			}
			visited[cid] = true
			trace = append(trace, cid)
			queue = append(queue, bfsItem{cid, cur.depth + 1})
			count++
		}
	}
	return trace
}

// buildProcessNode constructs a ProcessNode from trace.
func (pd *ProcessDetector) buildProcessNode(entry *types.Node, trace []int64, nodeMap map[int64]*types.Node) *ProcessNode {
	// Build label from first 3 nodes
	labelParts := make([]string, 0, 3)
	for i, id := range trace {
		if i >= 3 {
			break
		}
		if n, ok := nodeMap[id]; ok {
			labelParts = append(labelParts, n.Name)
		}
	}
	label := strings.Join(labelParts, " → ")
	if len(trace) > 3 {
		label += fmt.Sprintf(" (+%d)", len(trace)-3)
	}

	terminalID := trace[len(trace)-1]

	id := fmt.Sprintf("proc_%s_%d", sanitizeID(entry.Name), entry.ID)

	return &ProcessNode{
		ID:           id,
		Label:        label,
		ProcessType:  "unknown", // community info not available without Louvain run
		StepCount:    len(trace),
		EntryPointID: entry.ID,
		TerminalID:   terminalID,
		CommunityIDs: []int64{},
		Trace:        trace,
	}
}

func sanitizeID(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	s := re.ReplaceAllString(name, "_")
	if len(s) > 30 {
		s = s[:30]
	}
	return s
}

func (pd *ProcessDetector) clearOldProcesses() error {
	_, err := pd.db.Exec(`DELETE FROM processes`)
	return err
}

func (pd *ProcessDetector) loadFunctions() ([]*types.Node, error) {
	rows, err := pd.db.Query(`
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes WHERE kind IN ('function','method')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*types.Node
	for rows.Next() {
		n := &types.Node{}
		if err := rows.Scan(&n.ID, &n.Name, &n.Kind, &n.File, &n.Line, &n.EndLine,
			&n.ParentID, &n.Exported, &n.QualifiedName, &n.Scope, &n.Visibility, &n.Role, &n.FileHash); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func (pd *ProcessDetector) saveProcesses(processes []*ProcessNode) error {
	tx, err := pd.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmtProc, err := tx.Prepare(`
		INSERT OR REPLACE INTO processes (id, label, process_type, step_count, entry_point_id, terminal_id, community_ids)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmtProc.Close()

	stmtStep, err := tx.Prepare(`
		INSERT OR REPLACE INTO process_steps (process_id, node_id, step) VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmtStep.Close()

	for _, proc := range processes {
		communityJSON, _ := json.Marshal(proc.CommunityIDs)
		if _, err := stmtProc.Exec(
			proc.ID, proc.Label, proc.ProcessType, proc.StepCount,
			proc.EntryPointID, proc.TerminalID, string(communityJSON),
		); err != nil {
			return fmt.Errorf("failed to insert process %s: %w", proc.ID, err)
		}

		for step, nodeID := range proc.Trace {
			if _, err := stmtStep.Exec(proc.ID, nodeID, step+1); err != nil {
				return fmt.Errorf("failed to insert step %d for process %s: %w", step, proc.ID, err)
			}
		}
	}

	return tx.Commit()
}
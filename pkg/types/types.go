// Package types defines the core data structures for axons.
package types

import "time"

// SymbolKind represents the type of a symbol.
type SymbolKind string

const (
	SymbolKindFunction   SymbolKind = "function"
	SymbolKindMethod     SymbolKind = "method"
	SymbolKindClass      SymbolKind = "class"
	SymbolKindInterface  SymbolKind = "interface"
	SymbolKindType       SymbolKind = "type"
	SymbolKindStruct     SymbolKind = "struct"
	SymbolKindEnum       SymbolKind = "enum"
	SymbolKindTrait      SymbolKind = "trait"
	SymbolKindRecord     SymbolKind = "record"
	SymbolKindModule     SymbolKind = "module"
	SymbolKindParameter  SymbolKind = "parameter"
	SymbolKindProperty   SymbolKind = "property"
	SymbolKindConstant   SymbolKind = "constant"
	SymbolKindVariable   SymbolKind = "variable"
	SymbolKindImport     SymbolKind = "import"
	SymbolKindField      SymbolKind = "field"
)

// EdgeKind represents the type of an edge.
type EdgeKind string

const (
	EdgeKindCalls         EdgeKind = "calls"
	EdgeKindImports       EdgeKind = "imports"
	EdgeKindImportsType   EdgeKind = "imports-type"
	EdgeKindDynamicImport EdgeKind = "dynamic-imports"
	EdgeKindReexports     EdgeKind = "reexports"
	EdgeKindExtends       EdgeKind = "extends"
	EdgeKindImplements    EdgeKind = "implements"
	EdgeKindContains      EdgeKind = "contains"
	EdgeKindParameterOf   EdgeKind = "parameter_of"
	EdgeKindReceiver      EdgeKind = "receiver"
	EdgeKindFlowsTo       EdgeKind = "flows_to"
	EdgeKindReturns       EdgeKind = "returns"
	EdgeKindMutates       EdgeKind = "mutates"
)

// Role represents the role of a node in the graph.
type Role string

const (
	RoleEntry    Role = "entry"
	RoleCore     Role = "core"
	RoleUtility  Role = "utility"
	RoleAdapter  Role = "adapter"
	RoleDead     Role = "dead"
	RoleTestOnly Role = "test-only"
	RoleLeaf     Role = "leaf"
)

// Visibility represents the visibility of a symbol.
type Visibility string

const (
	VisibilityPublic             Visibility = "public"
	VisibilityPrivate            Visibility = "private"
	VisibilityProtected          Visibility = "protected"
	VisibilityInternal           Visibility = "internal"
	VisibilityProtectedInternal  Visibility = "protected internal"
)

// Project represents a code project/workspace.
type Project struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	RootPath      string    `json:"root_path"`
	
	// Source tracking (local vs remote)
	Source        string    `json:"source"`         // "local" or "remote"
	Provider      string    `json:"provider"`       // "github", "gitlab", etc.
	RemoteURL     string    `json:"remote_url"`     // Original remote URL
	CloneMode     string    `json:"clone_mode"`     // "managed" or "custom"
	Managed       bool      `json:"managed"`        // Whether managed by axons
	Branch        string    `json:"branch"`         // Cloned branch
	ClonedAt      time.Time `json:"cloned_at"`      // Clone timestamp
	
	// Existing fields
	WatchEnabled  bool      `json:"watch_enabled"`
	WatchStatus   string    `json:"watch_status"`
	LanguageStack []string  `json:"language_stack"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Node represents a symbol node in the graph.
type Node struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	File          string     `json:"file"`
	Line          int        `json:"line"`
	EndLine       int        `json:"end_line,omitempty"`
	ParentID      *int64     `json:"parent_id,omitempty"`
	Exported      bool       `json:"exported"`
	QualifiedName string     `json:"qualified_name,omitempty"`
	Scope         string     `json:"scope,omitempty"`
	Visibility    Visibility `json:"visibility,omitempty"`
	Role          Role       `json:"role,omitempty"`
	FileHash      string     `json:"file_hash,omitempty"`
	CommunityID   *int64     `json:"community_id,omitempty"` // Louvain community ID (set by RunAnalyses)
}

// Edge represents a relationship between two nodes.
type Edge struct {
	ID         int64    `json:"id"`
	SourceID   int64    `json:"source_id"`
	TargetID   int64    `json:"target_id"`
	Kind       EdgeKind `json:"kind"`
	Confidence float64  `json:"confidence,omitempty"`
	Dynamic    bool     `json:"dynamic,omitempty"`
}

// Definition represents a symbol definition extracted from source code.
type Definition struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Line          int        `json:"line"`
	EndLine       int        `json:"end_line,omitempty"`
	Parent        string     `json:"parent,omitempty"`
	ParentKind    SymbolKind `json:"parent_kind,omitempty"`
	Exported      bool       `json:"exported"`
	Scope         string     `json:"scope,omitempty"`
	Visibility    Visibility `json:"visibility,omitempty"`
	QualifiedName string     `json:"qualified_name,omitempty"`
	// Enhanced metadata
	Decorators    []Decorator `json:"decorators,omitempty"`
	Documentation string      `json:"documentation,omitempty"`
	TypeSignature string      `json:"type_signature,omitempty"`
	IsAsync       bool        `json:"is_async,omitempty"`
}

// Decorator represents a decorator/annotation on a definition.
type Decorator struct {
	Name   string            `json:"name"`
	Args   string            `json:"args,omitempty"`
	Line   int               `json:"line"`
	Params map[string]string `json:"params,omitempty"`
}

// Call represents a function/method call site.
type Call struct {
	Name      string `json:"name"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	Caller    string `json:"caller,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	IsMethod  bool   `json:"is_method,omitempty"`
	IsDynamic bool   `json:"is_dynamic,omitempty"`
}

// Import represents an import statement.
type Import struct {
	Source    string   `json:"source"`
	Line      int      `json:"line"`
	IsDefault bool     `json:"is_default,omitempty"`
	IsNamed   bool     `json:"is_named,omitempty"`
	IsType    bool     `json:"is_type,omitempty"`
	IsDynamic bool     `json:"is_dynamic,omitempty"`
	Alias     string   `json:"alias,omitempty"`
	Symbols   []string `json:"symbols,omitempty"` // Imported symbol names
}

// ClassRelation represents class inheritance/implementation.
type ClassRelation struct {
	ClassName  string   `json:"class_name"`
	Extends    string   `json:"extends,omitempty"`
	Implements []string `json:"implements,omitempty"`
	Line       int      `json:"line"`
}

// Export represents an export statement.
type Export struct {
	Name       string `json:"name"`
	Line       int    `json:"line"`
	IsDefault  bool   `json:"is_default,omitempty"`
	IsReexport bool   `json:"is_reexport,omitempty"`
	Source     string `json:"source,omitempty"`
}

// TypeMapEntry represents type inference information.
type TypeMapEntry struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Line       int     `json:"line"`
	Confidence float64 `json:"confidence,omitempty"`
}

// ExtractorOutput represents the output of a language extractor.
type ExtractorOutput struct {
	Definitions []Definition            `json:"definitions"`
	Calls       []Call                  `json:"calls"`
	Imports     []Import                `json:"imports"`
	Classes     []ClassRelation         `json:"classes"`
	Exports     []Export                `json:"exports"`
	TypeMap     map[string]TypeMapEntry `json:"type_map,omitempty"`
	Dataflow    *DataflowResult         `json:"dataflow,omitempty"`
	Cfg         map[string]*CfgData     `json:"cfg,omitempty"`        // function name -> CFG
	AstNodes    []AstNode               `json:"ast_nodes,omitempty"`
	LineCount   int                     `json:"line_count,omitempty"`
}

// PathAlias represents a path alias configuration.
type PathAlias struct {
	BaseURL string        `json:"base_url,omitempty"`
	Paths   []PathMapping `json:"paths"`
}

// PathMapping represents a single path alias mapping.
type PathMapping struct {
	Alias string   `json:"alias"`
	Paths []string `json:"paths"`
}

// BuildOptions represents options for building the graph.
type BuildOptions struct {
	RootDir         string     `json:"root_dir"`
	DBPath          string     `json:"db_path"`
	Engine          string     `json:"engine"`
	IncludeDataflow bool       `json:"include_dataflow"`
	IncludeAST      bool       `json:"include_ast"`
	FullBuild       bool       `json:"full_build"`
	ExcludePatterns []string   `json:"exclude_patterns"`
	Aliases         PathAlias  `json:"aliases"`
	ProjectID       string     `json:"project_id,omitempty"`
	ProjectName     string     `json:"project_name,omitempty"`
	MaxFileSize     int64      `json:"max_file_size,omitempty"` // max file size in bytes, default 1MB
	WorkerPoolSize  int        `json:"worker_pool_size,omitempty"`  // goroutine pool size for parallel parsing, default NumCPU
	ChunkByteBudget int64      `json:"chunk_byte_budget,omitempty"` // byte budget per parse chunk, default 2MB
}

// BuildResult represents the result of a graph build.
type BuildResult struct {
	FilesParsed             int           `json:"files_parsed"`
	NodesCreated            int           `json:"nodes_created"`
	EdgesCreated            int           `json:"edges_created"`
	Duration                time.Duration `json:"duration"`
	IsFullBuild             bool          `json:"is_full_build"`
	ChangedFiles            []string      `json:"changed_files,omitempty"`
	RemovedFiles            []string      `json:"removed_files,omitempty"`
	ChangedFileOldNodeIDs   []int64       `json:"changed_file_old_node_ids,omitempty"`
	ChangedFileOldEdgeIDs   []int64       `json:"changed_file_old_edge_ids,omitempty"`
	LanguageStack           []string      `json:"language_stack,omitempty"`
}

// QueryOptions represents options for querying the graph.
type QueryOptions struct {
	DBPath  string     `json:"db_path"`
	Name    string     `json:"name,omitempty"`
	Kind    SymbolKind `json:"kind,omitempty"`
	File    string     `json:"file,omitempty"`
	NoTests bool       `json:"no_tests"`
	Limit   int        `json:"limit,omitempty"`
}

// QueryResult represents the result of a symbol query.
type QueryResult struct {
	Node        Node               `json:"node"`
	Callers     []Node             `json:"callers,omitempty"`
	Callees     []Node             `json:"callees,omitempty"`
	Complexity  *ComplexityMetrics `json:"complexity,omitempty"`
}

// ComplexityMetrics represents code complexity metrics.
type ComplexityMetrics struct {
	Cyclomatic         int     `json:"cyclomatic"`
	Cognitive          int     `json:"cognitive"`
	Nesting            int     `json:"nesting"`
	LinesOfCode        int     `json:"lines_of_code"`
	HalsteadVolume     float64 `json:"halstead_volume,omitempty"`
	HalsteadDifficulty float64 `json:"halstead_difficulty,omitempty"`
}

// ImpactResult represents the result of an impact analysis.
type ImpactResult struct {
	Root         Node   `json:"root"`
	ImpactRadius int    `json:"impact_radius"`
	Callers      []Node `json:"callers"`
	TotalAffected int   `json:"total_affected"`
}

// SearchResult represents a search result.
type SearchResult struct {
	Node      Node    `json:"node"`
	Score     float64 `json:"score"`
	MatchType string  `json:"match_type"`
}

// Metadata represents database metadata.
type Metadata struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LanguageInfo represents information about a supported language.
type LanguageInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// DataflowKind represents the type of data flow relationship.
type DataflowKind string

const (
	DataflowKindParameter   DataflowKind = "parameter"
	DataflowKindReturn      DataflowKind = "return"
	DataflowKindAssignment  DataflowKind = "assignment"
	DataflowKindArgFlow     DataflowKind = "arg_flow"
	DataflowKindMutation    DataflowKind = "mutation"
)

// DataflowEdge represents a data flow relationship between nodes.
type DataflowEdge struct {
	ID           int64        `json:"id"`
	SourceID     int64        `json:"source_id"`
	TargetID     int64        `json:"target_id"`
	Kind         DataflowKind `json:"kind"`
	ParamIndex   *int         `json:"param_index,omitempty"`
	Expression   string       `json:"expression,omitempty"`
	Line         int          `json:"line,omitempty"`
	Confidence   float64      `json:"confidence,omitempty"`
}

// DataflowParam represents a parameter dataflow.
type DataflowParam struct {
	FuncName   string `json:"func_name"`
	ParamName  string `json:"param_name"`
	ParamIndex int    `json:"param_index"`
	Line       int    `json:"line"`
}

// DataflowReturn represents a return value dataflow.
type DataflowReturn struct {
	FuncName        string   `json:"func_name"`
	Expression      string   `json:"expression"`
	ReferencedNames []string `json:"referenced_names,omitempty"`
	Line            int      `json:"line"`
}

// DataflowAssignment represents an assignment dataflow.
type DataflowAssignment struct {
	VarName        string `json:"var_name"`
	CallerFunc     string `json:"caller_func,omitempty"`
	SourceCallName string `json:"source_call_name"`
	Expression     string `json:"expression"`
	Line           int    `json:"line"`
}

// DataflowArgFlow represents an argument flow dataflow.
type DataflowArgFlow struct {
	CallerFunc  string  `json:"caller_func,omitempty"`
	CalleeName  string  `json:"callee_name"`
	ArgIndex    int     `json:"arg_index"`
	ArgName     string  `json:"arg_name,omitempty"`
	BindingType string  `json:"binding_type,omitempty"`
	Confidence  float64 `json:"confidence"`
	Expression  string  `json:"expression"`
	Line        int     `json:"line"`
}

// DataflowMutation represents a mutation dataflow.
type DataflowMutation struct {
	FuncName     string `json:"func_name,omitempty"`
	ReceiverName string `json:"receiver_name"`
	BindingType  string `json:"binding_type,omitempty"`
	MutatingExpr string `json:"mutating_expr"`
	Line         int    `json:"line"`
}

// DataflowResult represents the complete dataflow analysis result.
type DataflowResult struct {
	Parameters  []DataflowParam      `json:"parameters,omitempty"`
	Returns     []DataflowReturn     `json:"returns,omitempty"`
	Assignments []DataflowAssignment `json:"assignments,omitempty"`
	ArgFlows    []DataflowArgFlow    `json:"arg_flows,omitempty"`
	Mutations   []DataflowMutation   `json:"mutations,omitempty"`
}

// CfgBlock represents a control flow graph block.
type CfgBlock struct {
	Index     int    `json:"index"`
	Type      string `json:"type"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Label     string `json:"label,omitempty"`
}

// CfgEdge represents an edge in the control flow graph.
type CfgEdge struct {
	SourceIndex int    `json:"source_index"`
	TargetIndex int    `json:"target_index"`
	Kind        string `json:"kind"`
}

// CfgData represents the complete control flow graph for a function.
type CfgData struct {
	Blocks []CfgBlock `json:"blocks"`
	Edges  []CfgEdge  `json:"edges"`
}

// AstNode represents an AST node for storage.
type AstNode struct {
	ID           int64  `json:"id,omitempty"`
	File         string `json:"file"`
	Line         int    `json:"line"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Text         string `json:"text,omitempty"`
	Receiver     string `json:"receiver,omitempty"`
	ParentNodeID *int64 `json:"parent_node_id,omitempty"`
}

// CoChange represents a co-change relationship between files.
type CoChange struct {
	ID             int64   `json:"id"`
	FileA          string  `json:"file_a"`
	FileB          string  `json:"file_b"`
	CommitCount    int     `json:"commit_count"`
	Jaccard        float64 `json:"jaccard"`
	LastCommitTime int64   `json:"last_commit_time,omitempty"`
}

// NodeMetrics represents metrics for a node.
type NodeMetrics struct {
	NodeID      int64   `json:"node_id"`
	LineCount   int     `json:"line_count,omitempty"`
	SymbolCount int     `json:"symbol_count,omitempty"`
	ImportCount int     `json:"import_count,omitempty"`
	ExportCount int     `json:"export_count,omitempty"`
	FanIn       int     `json:"fan_in,omitempty"`
	FanOut      int     `json:"fan_out,omitempty"`
	Cohesion    float64 `json:"cohesion,omitempty"`
	FileCount   int     `json:"file_count,omitempty"`
}

// FileCommitCount represents commit count for a file.
type FileCommitCount struct {
	File        string `json:"file"`
	CommitCount int    `json:"commit_count"`
}
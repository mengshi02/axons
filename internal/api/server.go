// Package api provides HTTP API server for axons daemon.
package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/agent"
	"github.com/mengshi02/axons/internal/agent/llm"
	"github.com/mengshi02/axons/internal/config"
	"github.com/mengshi02/axons/internal/db"
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/notification"
	"github.com/mengshi02/axons/internal/plugin"
	"github.com/mengshi02/axons/internal/service"
	"github.com/mengshi02/axons/internal/task"
	"github.com/mengshi02/axons/internal/terminal"
	"github.com/mengshi02/axons/internal/version"
)

// Server represents the API server.
type Server struct {
	config       *config.Config
	router       *httprouter.Router
	taskMgr      *task.Manager
	db           *sql.DB
	repo         *repository.Repository
	globalRepo   *repository.GlobalRepository
	dbMgr        *db.Manager
	mcpServer    *MCPServer
	watchMgr     *WatchManager
	eventBroker  *EventBroker
	embeddingSvc *service.EmbeddingService
	backupService *service.BackupService

	// Agent service
	agentService agent.Agent
	agentMemory  agent.Memory

	// Plugin system
	pluginManager *plugin.Manager

	// Notification system
	notificationService *notification.Service
}

// WatchManager manages file watchers.
type WatchManager struct {
	mu       sync.RWMutex
	watchers map[string]*WatchInfo // key: root directory
}

// WatchInfo holds information about an active watcher.
type WatchInfo struct {
	RootDir   string
	Watcher   *graph.Watcher
	Cancel    context.CancelFunc
	StartTime time.Time
	Status    string // "running", "stopped", "error"
	ProjectID string // Associated project ID
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, taskMgr *task.Manager, mgr *db.Manager) *Server {
	s := &Server{
		config:      cfg,
		router:      httprouter.New(),
		taskMgr:     taskMgr,
		dbMgr:       mgr,
		db:          mgr.MainDB(),
		repo:        repository.New(mgr.MainDB()),
		globalRepo:  repository.NewGlobal(mgr.MainDB()),
		watchMgr:    &WatchManager{watchers: make(map[string]*WatchInfo)},
		eventBroker: NewEventBroker(),
	}

	// Initialize MCP server
	s.mcpServer = NewMCPServer(s.repo)

	// Initialize plugin manager (port will be set later when TCP listener is ready)
	s.pluginManager = plugin.NewManager(0) // default port, updated later
	s.pluginManager.SetEventHandler(func(eventType string, data map[string]interface{}) {
		s.eventBroker.Broadcast(Event{
			Type:      EventType(eventType),
			Timestamp: time.Now(),
			Data:      data,
		})
		// Also broadcast to the Go EventBus so iframe plugins receive lifecycle events
		pluginID, _ := data["pluginId"].(string)
		plugin.GetGlobalBus().Emit(plugin.Event{
			PluginID: pluginID,
			Type:     eventType,
			Payload:  data,
		})
	})

	// Initialize embedding service (will be configured later via settings)
	s.embeddingSvc = service.NewEmbeddingService(s.repo, nil)

	// Initialize backup service
	s.backupService = service.NewBackupService(mgr.MainDB())

	// Initialize notification service
	notificationRepo := notification.NewRepository(mgr.MainDB())
	s.notificationService = notification.NewService(notificationRepo, s.eventBroker, s.pluginManager)

	// Set backup service on MCP server
	s.mcpServer.SetBackupService(s.backupService, s.globalRepo)

	// Initialize terminal manager if enabled
	if cfg.Terminal.Enabled {
		InitTerminalManager(terminal.ManagerConfig{
			MaxSessions: cfg.Terminal.MaxSessions,
		})
	}

	s.registerRoutes()
	s.registerMiddleware()

	return s
}

// SetAgent sets the Agent service.
func (s *Server) SetAgent(ag agent.Agent, memory agent.Memory) {
	s.agentService = ag
	s.agentMemory = memory
}

// PluginManager returns the plugin manager instance.
func (s *Server) PluginManager() *plugin.Manager {
	return s.pluginManager
}

// ReinitAgentFromDB reads LLM configuration from the database and re-initializes the agent service.
// It is called automatically when LLM settings are updated via the API.
func (s *Server) ReinitAgentFromDB() error {
	cfg, err := s.repo.GetLLMConfig()
	if err != nil {
		return fmt.Errorf("failed to read LLM config from DB: %w", err)
	}

	// llm_enabled must be "true"
	if cfg["llm_enabled"] != "true" {
		// Disable agent - LLM not enabled
		s.agentService = nil
		s.agentMemory = nil
		return fmt.Errorf("LLM not enabled (llm_enabled=%q)", cfg["llm_enabled"])
	}

	apiKey := cfg["llm_api_key"]
	provider := cfg["llm_provider"]
	model := cfg["llm_model"]
	baseURL := cfg["llm_base_url"]

	if apiKey == "" && provider != "ollama" {
		return fmt.Errorf("llm_api_key is required for provider %s", provider)
	}

	// Build LLM client
	var client llm.Client
	switch provider {
	case "openai", "":
		if baseURL != "" {
			client = llm.NewOpenAIClientWithBaseURL(apiKey, model, baseURL)
		} else {
			client = llm.NewOpenAIClient(apiKey, model)
		}
	case "anthropic":
		if baseURL != "" {
			client = llm.NewAnthropicClientWithBaseURL(apiKey, model, baseURL)
		} else {
			client = llm.NewAnthropicClient(apiKey, model)
		}
	case "custom":
		if baseURL == "" {
			return fmt.Errorf("llm_base_url is required for custom provider")
		}
		client = llm.NewOpenAIClientWithBaseURL(apiKey, model, baseURL)
	default:
		return fmt.Errorf("unsupported LLM provider: %s", provider)
	}

	// Reuse existing memory or create new one
	var memory agent.Memory
	if s.agentMemory != nil {
		memory = s.agentMemory
	} else {
		memory, err = agent.NewSQLiteMemory(s.db)
		if err != nil {
			return fmt.Errorf("failed to create agent memory: %w", err)
		}
	}

	tools := agent.WrapTools(s.mcpServer)

	maxRounds := 30
	if v := cfg["llm_max_rounds"]; v != "" {
		if n, parseErr := fmt.Sscanf(v, "%d", &maxRounds); n == 0 || parseErr != nil {
			maxRounds = 30
		}
	}

	ag := agent.NewReActAgent(&agent.AgentOptions{
		LLM:       client,
		Memory:    memory,
		Tools:     tools,
		MaxRounds: maxRounds,
	})

	s.agentService = ag
	s.agentMemory = memory
	return nil
}

// GetMCPServer 获取 MCP Server（用于 Agent 工具封装）
func (s *Server) GetMCPServer() *MCPServer {
	return s.mcpServer
}

// RestoreWatches restores all watches that were enabled before server shutdown.
// This should be called after the server is created and ready to accept requests.
func (s *Server) RestoreWatches() error {
	projects, err := s.globalRepo.GetProjectsWithWatchEnabled()
	if err != nil {
		return fmt.Errorf("failed to get projects with watch enabled: %w", err)
	}

	for _, project := range projects {
		// Check if directory still exists
		if _, err := os.Stat(project.RootPath); os.IsNotExist(err) {
			continue
		}

		// Start watcher for this project
		if err := s.startProjectWatch(project.ID, project.RootPath); err != nil {
			fmt.Printf("Failed to restore watch for project %s: %v\n", project.ID, err)
			continue
		}

		fmt.Printf("Restored watch for project %s (ID: %s)\n", project.Name, project.ID)
	}

	return nil
}

// Router returns the HTTP router.
func (s *Server) Router() *httprouter.Router {
	return s.router
}

// projectRepo returns a repository.Repository connected to the project-specific database.
// The project DB is lazily opened and migrated on first access.
func (s *Server) projectRepo(projectID string) (*repository.Repository, error) {
	pdb, err := s.dbMgr.ProjectDB(projectID)
	if err != nil {
		return nil, err
	}
	return repository.New(pdb), nil
}

// registerRoutes registers all API routes.
func (s *Server) registerRoutes() {
	// Graph operations
	s.router.POST("/v1/build", s.handleBuild)
	s.router.POST("/v1/query", s.handleQuery)
	s.router.POST("/v1/search", s.handleSearch)
	s.router.GET("/v1/stats", s.handleStats)
	s.router.GET("/v1/files", s.handleListFiles)
	// Also register under /api/v1 for client compatibility
	s.router.POST("/api/v1/search", s.handleSearch)

	// Symbol operations
	s.router.GET("/v1/symbols/:id", s.handleGetSymbol)
	s.router.GET("/v1/symbols/:id/callers", s.handleGetCallers)
	s.router.GET("/v1/symbols/:id/callees", s.handleGetCallees)

	// Embedding operations
	s.router.POST("/v1/embed", s.handleEmbed)
	s.router.GET("/v1/embed/status", s.handleEmbedStatus)
	s.router.POST("/v1/embed/cancel", s.handleEmbedCancel)
	s.router.POST("/v1/embed/test", s.handleEmbedTest)

	// Semantic search
	s.router.POST("/v1/semantic-search", s.handleSemanticSearch)

	// Audit and analysis
	s.router.POST("/v1/audit", s.handleAudit)
	s.router.POST("/v1/check", s.handleCheck)
	s.router.POST("/v1/complexity", s.handleComplexity)
	s.router.POST("/v1/path", s.handlePath)
	s.router.POST("/v1/sequence", s.handleSequence)
	s.router.POST("/v1/export", s.handleExport)
	s.router.POST("/v1/dataflow", s.handleDataflow)
	s.router.POST("/v1/diff-impact", s.handleDiffImpact)
	s.router.POST("/v1/owners", s.handleOwners)
	s.router.POST("/v1/triage", s.handleTriage)
	s.router.POST("/v1/cochange", s.handleCoChange)
	s.router.POST("/v1/snapshot/:action", s.handleSnapshot)
	s.router.POST("/v1/branch-compare", s.handleBranchCompare)

	// Project operations
	s.router.GET("/v1/projects", s.handleListProjects)
	s.router.POST("/v1/projects", s.handleCreateProject)
	s.router.POST("/v1/projects-new", s.handleNewProject) // Separate path to avoid httprouter conflict with :id
	s.router.GET("/v1/projects/:id", s.handleGetProject)
	s.router.DELETE("/v1/projects/:id", s.handleDeleteProject)
	s.router.GET("/v1/projects/:id/stats", s.handleProjectStats)
	s.router.GET("/v1/projects/:id/build-status", s.handleProjectBuildStatus)

	// App state (atomic projects + active project id, avoids frontend race condition)
	s.router.GET("/v1/app/state", s.handleGetAppState)
	s.router.PUT("/v1/app/state/active-project", s.handleSetActiveProject)
	s.router.GET("/v1/app/state/file-tree", s.handleGetFileTreeState)
	s.router.PUT("/v1/app/state/file-tree", s.handleSetFileTreeState)

	// Registry operations
	s.router.GET("/v1/repos", s.handleListRepos)
	s.router.POST("/v1/repos", s.handleRegisterRepo)
	s.router.GET("/v1/repos/:name", s.handleGetRepo)
	s.router.DELETE("/v1/repos/:name", s.handleUnregisterRepo)
	s.router.POST("/v1/repos/prune", s.handlePruneRepos)

	// CFG operations
	s.router.POST("/v1/cfg", s.handleCFG)

	// Watch operations
	s.router.POST("/v1/watch/start", s.handleWatchStart)
	s.router.POST("/v1/watch/stop", s.handleWatchStop)
	s.router.GET("/v1/watch/status", s.handleWatchStatus)
	s.router.GET("/v1/watch/list", s.handleWatchList)

	// SSE events endpoint
	s.router.GET("/v1/events", s.handleEvents)

	// Project-level watch operations
	s.router.POST("/v1/projects/:id/watch/start", s.handleProjectWatchStart)
	s.router.POST("/v1/projects/:id/watch/stop", s.handleProjectWatchStop)
	s.router.GET("/v1/projects/:id/watch/status", s.handleProjectWatchStatus)
	s.router.POST("/v1/watch/restore", s.handleRestoreWatches)

	// Task management
	s.router.GET("/v1/tasks", s.handleListTasks)
	s.router.GET("/v1/tasks/:id", s.handleGetTask)
	s.router.POST("/v1/tasks/:id/cancel", s.handleCancelTask)
	// Also register under /api/v1 for client compatibility
	s.router.GET("/api/v1/tasks", s.handleListTasks)
	s.router.GET("/api/v1/tasks/:id", s.handleGetTask)
	s.router.POST("/api/v1/tasks/:id/cancel", s.handleCancelTask)

	// Settings management - separated paths to avoid httprouter conflicts
	// Static routes under /v1/settings
	s.router.GET("/v1/settings", s.handleGetSettings)
	s.router.PUT("/v1/settings", s.handleUpdateSettings)
	s.router.GET("/v1/settings/check", s.handleCheckEmbeddingConfig)
	s.router.POST("/v1/settings/test-connection", s.handleTestConnection)
	// Dynamic routes under /v1/config
	s.router.GET("/v1/config/:category", s.handleGetSettingsByCategory)
	s.router.PUT("/v1/config/:key", s.handleSetSetting)
	s.router.DELETE("/v1/config/:key", s.handleDeleteSetting)

	// Health checks (both paths for compatibility)
	s.router.GET("/health", s.handleHealth)
	s.router.GET("/ready", s.handleReady)
	s.router.GET("/api/v1/health", s.handleHealth)
	s.router.GET("/api/v1/ready", s.handleReady)

	// Daemon management
	s.router.GET("/api/v1/status", s.handleStatus)
	s.router.POST("/api/v1/shutdown", s.handleShutdown)

	// Web UI compatibility routes (axons web frontend)
	s.router.GET("/api/repos", s.handleWebRepos)
	s.router.GET("/api/repo", s.handleWebRepo)
	s.router.GET("/api/graph", s.handleWebGraph)
	s.router.POST("/api/graph/delta", s.handleWebGraphDelta)
	s.router.GET("/api/file", s.handleWebFile)
	s.router.POST("/api/file", s.handleWebFile)
	s.router.DELETE("/api/file", s.handleWebFile)

	// File tree operations (handlers_filetree.go)
	s.router.GET("/api/filetree", s.handleFileTreeList)
	s.router.GET("/api/filetree/stat", s.handleFileTreeStat)
	s.router.POST("/api/filetree/file", s.handleFileTreeCreateFile)
	s.router.DELETE("/api/filetree/file", s.handleFileTreeDeleteFile)
	s.router.POST("/api/filetree/folder", s.handleFileTreeCreateFolder)
	s.router.DELETE("/api/filetree/folder", s.handleFileTreeDeleteFolder)
	s.router.POST("/api/filetree/rename", s.handleFileRename)
	s.router.POST("/api/filetree/copy", s.handleFileTreeCopy)
	// Legacy compat routes
	s.router.POST("/api/file/rename", s.handleFileRename)
	s.router.POST("/api/folder", s.handleFolderCreate)
	s.router.POST("/api/search", s.handleWebSearch)
	s.router.POST("/api/impact", s.handleWebImpact)
	s.router.POST("/api/index", s.handleWebIndex)
	s.router.POST("/api/clone", s.handleClone)
	s.router.GET("/api/nodes/:id/neighbors", s.handleWebNodeNeighbors)
	s.router.GET("/api/graph/drilldown", s.handleWebGraphDrilldown)

	// MCP endpoints - register via MCPServer
	s.mcpServer.RegisterRoutes(s.router)

	// LLM Models management
	s.router.GET("/api/llm-models", s.handleGetLLMModels)
	s.router.POST("/api/llm-models", s.handleCreateLLMModel)
	s.router.PUT("/api/llm-models/:id", s.handleUpdateLLMModel)
	s.router.DELETE("/api/llm-models/:id", s.handleDeleteLLMModel)

	// Chat API (Agent)
	s.router.POST("/api/chat", s.handleChat)
	s.router.POST("/api/chat/stream", s.handleChatStream)
	s.router.POST("/api/chat/clear", s.handleChatClear)
	s.router.GET("/api/chat/sessions", s.handleListSessions)
	s.router.GET("/api/chat/sessions/:id/history", s.handleGetSessionHistory)

	// Agent profiles API
	s.router.GET("/api/agent-tools", s.handleListAgentTools)
	s.router.GET("/api/agents", s.handleListAgents)
	s.router.POST("/api/agents", s.handleCreateAgent)
	s.router.GET("/api/agents/:id", s.handleGetAgent)
	s.router.PUT("/api/agents/:id", s.handleUpdateAgent)
	s.router.DELETE("/api/agents/:id", s.handleDeleteAgent)

	// File changes API (for AI modifications)
	s.router.GET("/api/changes", s.handleListChanges)
	s.router.GET("/api/changes/diff", s.handleGetDiff)
	s.router.POST("/api/changes/revert", s.handleRevert)
	s.router.POST("/api/changes/revert-all", s.handleRevertAll)
	s.router.DELETE("/api/changes", s.handleClearChanges)

	// Analysis routes (P0: Hotspots & Dead Code)
	s.router.GET("/v1/analysis/hotspots", s.handleHotspots)
	s.router.GET("/v1/analysis/deadcode", s.handleDeadCode)
	s.router.GET("/v1/analysis/cochange", s.handleCoChangeQuery)

	// CCE (Cognitive Context Engine) routes
	s.router.POST("/v1/cce/context", s.handleCCEGetContext)
	s.router.POST("/v1/cce/embed", s.handleCCEEmbed)
	s.router.GET("/v1/cce/status", s.handleCCEStatus)
	s.router.GET("/v1/cce/templates", s.handleCCETemplates)

	// Graph algorithm routes (P1)
	s.router.GET("/v1/graph/metrics", s.handleGraphMetrics)
	s.router.GET("/v1/graph/communities", s.handleGraphCommunities)
	s.router.GET("/v1/graph/pagerank", s.handleGraphPageRank)
	s.router.GET("/v1/graph/cycles", s.handleGraphCycles)

	// Impact & call chain routes (P1)
	s.router.GET("/v1/symbols/:id/impact", s.handleSymbolImpact)
	s.router.POST("/v1/callchain", s.handleCallChain)

	// Per-symbol CFG (P2)
	s.router.GET("/v1/symbols/:id/cfg", s.handleCFGDetail)

	// Architecture rules engine (P2)
	s.router.GET("/v1/arch/rules", s.handleListArchRules)
	s.router.POST("/v1/arch/rules", s.handleCreateArchRule)
	s.router.DELETE("/v1/arch/rules/:id", s.handleDeleteArchRule)
	s.router.POST("/v1/arch/validate", s.handleValidateArchRules)

	// Process execution flow (P1)
	s.router.GET("/v1/processes", s.handleListProcesses)
	s.router.GET("/v1/processes/:id", s.handleGetProcess)
	s.router.POST("/v1/processes/detect", s.handleDetectProcesses)

	// Terminal routes
	s.router.POST("/api/terminal/sessions", s.handleTerminalCreate)
	s.router.GET("/api/terminal/sessions/:id", s.handleTerminalGet)
	s.router.GET("/api/terminal/sessions/:id/ws", s.handleTerminalWS)
	s.router.DELETE("/api/terminal/sessions/:id", s.handleTerminalKill)
	s.router.GET("/api/terminal/sessions", s.handleTerminalList)
	s.router.POST("/api/terminal/sessions/:id/resize", s.handleTerminalResize)
	s.router.DELETE("/api/terminal/sessions", s.handleTerminalKillAll) // Kill all sessions

	// Plugin system routes
	if s.pluginManager != nil {
		s.pluginManager.RegisterRoutes(s.router)
	}

	// Notification routes
	s.router.GET("/v1/notifications", s.handleGetNotifications)
	s.router.POST("/v1/notifications", s.handleCreateNotification)
	s.router.GET("/v1/notifications/unread-count", s.handleGetUnreadCount)
	s.router.PUT("/v1/notifications/:path", s.handleNotificationDispatch)
	s.router.DELETE("/v1/notifications/:path", s.handleNotificationDispatch)

	// Frontend routes (must be registered last)
	s.RegisterFrontendRoutes()
}

// registerMiddleware registers middleware.
// Note: httprouter.Router implements http.Handler directly, so we don't need
// to wrap it. The ServeHTTP method provides middleware when Server is used as Handler.
func (s *Server) registerMiddleware() {
	// No-op: routes are already registered on s.router
	// Middleware is applied via ServeHTTP when Server is used as http.Handler
}

// ServeHTTP implements http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Apply middleware: CORS → recovery → logging → router
	handler := s.corsMiddleware(s.recoveryMiddleware(s.loggingMiddleware(s.router)))
	handler.ServeHTTP(w, r)
}

// corsMiddleware adds CORS headers for sandboxed plugin iframes.
// Sandbox iframes without allow-same-origin have opaque origin "null"
// and cannot make same-origin requests — all fetch calls become cross-origin.
// Since daemon is a local app with no authentication, allowing all origins is safe.
// WebSocket upgrade requests are passed through without modification to avoid
// interfering with the HTTP 101 Switching Protocols handshake.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CORS for WebSocket upgrade requests — they don't need CORS
		// and modifying response headers before Upgrade() can break the handshake.
		if isWebSocketUpgrade(r) {
			next.ServeHTTP(w, r)
			return
		}

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Set CORS header before handler — static file handlers and ES module
		// script requests from sandboxed iframes (origin "null") require this.
		w.Header().Set("Access-Control-Allow-Origin", "*")

		next.ServeHTTP(w, r)
	})
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade handshake.
func isWebSocketUpgrade(r *http.Request) bool {
	for _, v := range r.Header["Upgrade"] {
		if strings.EqualFold(v, "websocket") {
			return true
		}
	}
	return false
}

// loggingMiddleware logs HTTP requests.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		fmt.Printf("[%s] %s %s %v\n",
			time.Now().Format("2006-01-02 15:04:05"),
			r.Method,
			r.URL.Path,
			time.Since(start),
		)
	})
}

// recoveryMiddleware recovers from panics.
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriterTracker{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				fmt.Fprintf(os.Stderr, "Panic recovered: %v\n%s\n", rec, debug.Stack())
				// Only write error response if headers haven't been sent yet.
				// This prevents "superfluous response.WriteHeader call" warnings.
				if !rw.wroteHeader {
					writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.connectionFailed"))
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// responseWriterTracker wraps http.ResponseWriter to track whether headers have been written.
// It also forwards http.Hijacker and http.Flusher so that WebSocket upgraders and SSE
// streaming work correctly through the recovery middleware.
type responseWriterTracker struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *responseWriterTracker) WriteHeader(code int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher to support SSE streaming through the recovery middleware.
func (w *responseWriterTracker) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker so WebSocket upgraders can take over the TCP connection.
// Without this, gorilla/websocket's Upgrade() would fail because the wrapped ResponseWriter
// doesn't implement Hijacker (Go only promotes methods from the embedded interface type,
// not from other interfaces the concrete value might satisfy).
func (w *responseWriterTracker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.(http.Hijacker).Hijack()
}

// handleHealth handles health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// handleReady handles readiness check requests.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Check database connection
	if err := s.db.Ping(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", i18n.T("api.error.databaseNotAvailable"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ready",
	})
}

// handleListTasks handles task listing requests.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	tasks := s.taskMgr.ListTasks()
	writeJSON(w, http.StatusOK, task.Items{
		Tasks: tasks,
		Count: len(tasks),
	})
}

// handleGetTask handles task detail requests.
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	taskID := ps.ByName("id")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.taskIdRequired"))
		return
	}

	task, exists := s.taskMgr.GetTask(taskID)
	if !exists {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.taskNotFound"))
		return
	}

	writeJSON(w, http.StatusOK, task)
}

// handleCancelTask handles task cancellation requests.
func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	taskID := ps.ByName("id")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.taskIdRequired"))
		return
	}

	if err := s.taskMgr.CancelTask(taskID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "canceled",
	})
}

// handleStatus handles daemon status requests.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	tasks := s.taskMgr.ListTasks()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "running",
		"version":    version.Version,
		"uptime":     "0s", // TODO: track actual uptime
		"task_count": len(tasks),
		"tasks":      tasks,
	})
}

// handleShutdown handles daemon shutdown requests.
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "shutting down",
	})
	// Shutdown in a goroutine to allow response to be sent
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}

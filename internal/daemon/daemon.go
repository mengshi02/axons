// Package daemon provides the daemon process management for axons.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/agent"
	"github.com/mengshi02/axons/internal/agent/llm"
	"github.com/mengshi02/axons/internal/api"
	"github.com/mengshi02/axons/internal/config"
	"github.com/mengshi02/axons/internal/db"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/internal/task"
	"go.uber.org/zap"
)

// Daemon represents the axons daemon process.
type Daemon struct {
	config      *config.Config
	server      *http.Server
	tcpServer   *http.Server
	api         *api.Server
	taskMgr     *task.Manager
	dbMgr       *db.Manager
	listener    net.Listener
	tcpListener net.Listener
	socketPath  string
	pidFile     string
	tcpAddr     string
	mu          sync.RWMutex
	running     bool
	desktopMode bool // true when running inside the Wails desktop app
}

// New creates a new Daemon instance.
func New(cfg *config.Config) (*Daemon, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if err := cfg.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	return &Daemon{
		config:     cfg,
		socketPath: cfg.SocketPath(),
		pidFile:    cfg.Daemon.PIDFile,
		taskMgr:    task.NewManager(5 * time.Minute),
		tcpAddr:    cfg.API.TCP,
	}, nil
}

// SetTCPAddr sets the TCP address for web UI.
func (d *Daemon) SetTCPAddr(addr string) {
	d.tcpAddr = addr
}

// SetTCPListener sets a pre-created TCP listener for web UI.
// This is useful for desktop apps that need to use a random port.
// The actual address can be retrieved via GetTCPAddr().
func (d *Daemon) SetTCPListener(listener net.Listener) {
	d.tcpListener = listener
	d.tcpAddr = listener.Addr().String()
}

// SetDesktopMode marks the daemon as running inside the Wails desktop app.
func (d *Daemon) SetDesktopMode(desktop bool) {
	d.desktopMode = desktop
	// Propagate to plugin manager for CSP/runtime decisions
	if d.api != nil {
		if pm := d.api.PluginManager(); pm != nil {
			if desktop {
				pm.SetRuntimeMode("desktop")
			} else {
				pm.SetRuntimeMode("web")
			}
		}
	}
}

// IsDesktopMode returns true when running inside the Wails desktop app.
func (d *Daemon) IsDesktopMode() bool {
	return d.desktopMode
}

// GetTCPAddr returns the actual TCP address the daemon is listening on.
// Returns empty string if TCP is not enabled.
func (d *Daemon) GetTCPAddr() string {
	return d.tcpAddr
}

// Start starts the daemon process.
func (d *Daemon) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if already running
	if d.running {
		return fmt.Errorf("daemon already running")
	}

	// Check if another daemon is running
	if running, _ := IsRunning(d.pidFile); running {
		return fmt.Errorf("another daemon is already running")
	}

	logger.Info("Starting daemon",
		zap.String("socket", d.socketPath),
		zap.String("database", d.config.Database.Path),
	)

	// Write PID file
	if err := d.writePID(); err != nil {
		logger.Error("Failed to write PID file", zap.Error(err))
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Open database
	// Open main database and run main migrations
	mgr, err := db.NewManager(d.config.Database.Path)
	if err != nil {
		d.removePID()
		logger.Error("Failed to open database", zap.Error(err), zap.String("path", d.config.Database.Path))
		return fmt.Errorf("failed to open database: %w", err)
	}
	d.dbMgr = mgr
	logger.Debug("Database opened", zap.String("path", d.config.Database.Path))

	// Create API server
	d.api = api.NewServer(d.config, d.taskMgr, mgr)
	logger.Debug("API server created")

	// Propagate desktopMode to plugin manager (if SetDesktopMode was called before Run)
	if d.desktopMode && d.api != nil {
		if pm := d.api.PluginManager(); pm != nil {
			pm.SetRuntimeMode("desktop")
		}
	}

	// Initialize Agent service:
	// 1. Try from static config (legacy / CLI flags)
	if d.config.Agent.Enabled && d.config.Agent.APIKey != "" {
		if err := d.initAgent(); err != nil {
			logger.Warn("Failed to initialize agent service from config", zap.Error(err))
		} else {
			logger.Debug("Agent service initialized from config")
		}
	}

	// 2. Try from DB settings (user-configured via SettingsPanel)
	if err := d.api.ReinitAgentFromDB(); err != nil {
		logger.Warn("Agent service not initialized from DB", zap.Error(err))
	} else {
		logger.Info("Agent service initialized from DB settings")
	}

	// Restore watches for projects that had watch enabled
	if err := d.api.RestoreWatches(); err != nil {
		logger.Warn("Failed to restore watches", zap.Error(err))
	}

	// Create Unix socket listener
	listener, err := d.createListener()
	if err != nil {
		d.dbMgr.Close()
		d.removePID()
		logger.Error("Failed to create listener", zap.Error(err))
		return fmt.Errorf("failed to create listener: %w", err)
	}
	d.listener = listener
	logger.Debug("Unix socket listener created", zap.String("path", d.socketPath))

	// Create HTTP server for Unix socket
	d.server = &http.Server{
		Handler:      d.api,
		ReadTimeout:  time.Duration(d.config.API.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(d.config.API.WriteTimeout) * time.Second,
	}

	// Create TCP listener for web UI if configured
	if d.tcpAddr != "" && d.tcpListener == nil {
		tcpListener, err := net.Listen("tcp", d.tcpAddr)
		if err != nil {
			d.listener.Close()
			d.dbMgr.Close()
			d.removePID()
			logger.Error("Failed to listen on TCP", zap.Error(err), zap.String("addr", d.tcpAddr))
			return fmt.Errorf("failed to listen on TCP %s: %w", d.tcpAddr, err)
		}
		d.tcpListener = tcpListener
	}

	// Create HTTP server for TCP if listener is available
	if d.tcpListener != nil {
		d.tcpServer = &http.Server{
			Handler:      d.api,
			ReadTimeout:  time.Duration(d.config.API.ReadTimeout) * time.Second,
			WriteTimeout: time.Duration(d.config.API.WriteTimeout) * time.Second,
		}

		logger.Info("Web UI available", zap.String("url", fmt.Sprintf("http://%s", d.tcpListener.Addr().String())))
		fmt.Printf("Web UI available at http://%s\n", d.tcpListener.Addr().String())
	}

	d.running = true

	// Initialize plugin system: set the axons API port and start auto-start plugins
	if pm := d.api.PluginManager(); pm != nil {
		if d.tcpListener != nil {
			addr := d.tcpListener.Addr().String()
			// Parse port from addr (format: "127.0.0.1:9090" or ":9090")
			if _, portStr, err := net.SplitHostPort(addr); err == nil {
				if port, err := strconv.Atoi(portStr); err == nil {
					pm.SetAxonsPort(port)
				}
			}
		}
		go pm.StartAutoStartPlugins()
	}

	logger.Info("Daemon started successfully")
	return nil
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run() error {
	if err := d.Start(); err != nil {
		return err
	}
	defer d.Stop()

	errCh := make(chan error, 2)

	// Start serving on Unix socket
	go func() {
		logger.Info("Unix socket server started", zap.String("path", d.socketPath))
		if err := d.server.Serve(d.listener); err != nil && err != http.ErrServerClosed {
			logger.Error("Unix socket server error", zap.Error(err))
			errCh <- fmt.Errorf("unix socket server: %w", err)
		}
	}()

	// Start serving on TCP if configured
	if d.tcpServer != nil && d.tcpListener != nil {
		go func() {
			logger.Info("TCP server started", zap.String("addr", d.tcpAddr))
			if err := d.tcpServer.Serve(d.tcpListener); err != nil && err != http.ErrServerClosed {
				logger.Error("TCP server error", zap.Error(err))
				errCh <- fmt.Errorf("tcp server: %w", err)
			}
		}()
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		fmt.Printf("Received signal %v, shutting down...\n", sig)
	case err := <-errCh:
		logger.Error("Server error", zap.Error(err))
		return err
	}

	return nil
}

// Stop stops the daemon.
func (d *Daemon) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}

	logger.Info("Stopping daemon")

	// Shutdown HTTP servers
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if d.server != nil {
		logger.Debug("Shutting down Unix socket server")
		d.server.Shutdown(ctx)
	}

	if d.tcpServer != nil {
		logger.Debug("Shutting down TCP server")
		d.tcpServer.Shutdown(ctx)
	}

	// Close listeners
	if d.listener != nil {
		d.listener.Close()
	}

	if d.tcpListener != nil {
		d.tcpListener.Close()
	}

	// Remove socket file
	if d.socketPath != "" {
		os.Remove(d.socketPath)
	}

	// Close database
	if d.dbMgr != nil {
		logger.Debug("Closing database")
		d.dbMgr.Close()
	}

	// Remove PID file
	d.removePID()

	d.running = false
	logger.Info("Daemon stopped")
	return nil
}

// IsRunning checks if the daemon is running.
func (d *Daemon) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// Ready checks if the daemon is ready to accept connections.
func (d *Daemon) Ready() bool {
	if !d.IsRunning() {
		return false
	}

	// Try to connect
	conn, err := net.DialTimeout("unix", d.socketPath, time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// createListener creates the network listener based on config.
func (d *Daemon) createListener() (net.Listener, error) {
	// Remove existing socket file
	if _, err := os.Stat(d.socketPath); err == nil {
		os.Remove(d.socketPath)
	}

	// Ensure socket directory exists
	if err := os.MkdirAll(filepath.Dir(d.socketPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on unix socket: %w", err)
	}

	// Set socket permissions
	if err := os.Chmod(d.socketPath, 0660); err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to set socket permissions: %w", err)
	}

	return listener, nil
}

// writePID writes the current process ID to the PID file.
func (d *Daemon) writePID() error {
	if d.pidFile == "" {
		return nil
	}

	pid := os.Getpid()
	data := []byte(fmt.Sprintf("%d\n", pid))
	return os.WriteFile(d.pidFile, data, 0644)
}

// removePID removes the PID file.
func (d *Daemon) removePID() {
	if d.pidFile != "" {
		os.Remove(d.pidFile)
	}
}

// IsRunning checks if a daemon is running by checking the PID file.
func IsRunning(pidFile string) (bool, int) {
	if pidFile == "" {
		return false, 0
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false, 0
	}

	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return false, 0
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, 0
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false, 0
	}

	return true, pid
}

// initAgent initializes the Agent service.
func (d *Daemon) initAgent() error {
	cfg := d.config.Agent

	// Create LLM client
	var client llm.Client
	switch cfg.Provider {
	case "openai":
		if cfg.BaseURL != "" {
			client = llm.NewOpenAIClientWithBaseURL(cfg.APIKey, cfg.Model, cfg.BaseURL)
		} else {
			client = llm.NewOpenAIClient(cfg.APIKey, cfg.Model)
		}
	default:
		return fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
	}

	// Create memory
	memory, err := agent.NewSQLiteMemory(d.dbMgr.MainDB())
	if err != nil {
		return fmt.Errorf("failed to create agent memory: %w", err)
	}

	// Create tools from MCP server
	tools := agent.WrapTools(d.api.GetMCPServer())

	// Create agent
	ag := agent.NewReActAgent(&agent.AgentOptions{
		LLM:          client,
		Memory:       memory,
		Tools:        tools,
		MaxRounds:    cfg.MaxRounds,
		SystemPrompt: cfg.SystemPrompt,
	})

	// Set agent on API server
	d.api.SetAgent(ag, memory)

	return nil
}

// RecoverPanic recovers from panics and logs the stack trace.
func RecoverPanic() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "Panic recovered: %v\n%s\n", r, debug.Stack())
	}
}

// Router returns the API router for the daemon.
func (d *Daemon) Router() *httprouter.Router {
	return d.api.Router()
}

// StatusResponse represents daemon status for CLI.
type StatusResponse struct {
	Status    string         `json:"status"`
	Version   string         `json:"version"`
	Uptime    string         `json:"uptime"`
	TaskCount int            `json:"task_count"`
	Tasks     []task.TaskStatus `json:"tasks,omitempty"`
}

// GetStatus connects to a running daemon and gets its status.
func GetStatus(socketPath string) (*StatusResponse, error) {
	// Try to connect to daemon
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	// Use HTTP client over unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://unix/api/v1/status")
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status: %w", err)
	}

	return &status, nil
}

// Stop sends a stop signal to a running daemon via socket.
func Stop(socketPath string) error {
	// Check if running first
	if !IsRunningBySocket(socketPath) {
		return nil
	}

	// Use HTTP client over unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("POST", "http://unix/api/v1/shutdown", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// IsRunningBySocket checks if daemon is running by trying to connect to socket.
func IsRunningBySocket(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
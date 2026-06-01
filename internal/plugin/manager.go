package plugin

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mengshi02/axons/internal/i18n"
)

// PluginStatus represents the runtime status of a plugin.
type PluginStatus string

const (
	StatusImported  PluginStatus = "imported"
	StatusInstalled PluginStatus = "installed"
	StatusStarting  PluginStatus = "starting"
	StatusRunning   PluginStatus = "running"
	StatusStopped   PluginStatus = "stopped"
	StatusCrashed   PluginStatus = "crashed"
)

// PluginInstance represents a running plugin instance.
type PluginInstance struct {
	Manifest  *PluginManifest
	Port      int
	Cmd       *exec.Cmd
	Status    PluginStatus
	Restarts  int
	Token     string
	StartedAt time.Time
	Endpoint  string // http://127.0.0.1:PORT

	stdinPipe io.WriteCloser
	listener  io.Closer // held listener from PortAllocator
}

// InstalledPlugin represents a plugin entry in the installed.json registry.
type InstalledPlugin struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Description   string          `json:"description"`
	Author        string          `json:"author"`
	Icon          string          `json:"icon"`
	Category      string          `json:"category"`
	Status        PluginStatus    `json:"status"`
	Dir           string          `json:"dir"`
	Port          int             `json:"port"`
	InstalledAt   time.Time       `json:"installedAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
	ManifestHash  string          `json:"manifestHash"`
	Backend       *BackendDef     `json:"backend"`
	Frontend      *FrontendDef    `json:"frontend"`
}

// InstalledRegistry represents the installed.json file.
type InstalledRegistry struct {
	Version int                        `json:"version"`
	Plugins map[string]*InstalledPlugin `json:"plugins"`
}

// Manager manages the lifecycle of all plugins.
type Manager struct {
	mu           sync.RWMutex
	instances    map[string]*PluginInstance // pluginId → instance
	registry     *PluginRegistry
	portAlloc    *PortAllocator
	sharedState  *SharedState
	pluginsDir   string     // ~/.axons/plugins
	axonsPort    int        // axons API port
	eventHandler func(eventType string, data map[string]interface{})
	runtimeMode  string     // "desktop" or "web" — set by daemon at startup

	// Async install tracking
	installMu  sync.Mutex
	installing map[string]bool // pluginId → true if currently installing

	// Locale tracking
	availableLocales []LocaleInfo // list of available locales from localization plugins
}

// LocaleInfo describes an available locale from a localization plugin.
type LocaleInfo struct {
	Code        string `json:"code"`        // BCP 47 tag, e.g. "zh-CN"
	NativeName  string `json:"nativeName"`  // e.g. "简体中文"
	EnglishName string `json:"englishName"` // e.g. "Chinese (Simplified)"
	PluginID    string `json:"pluginId"`    // e.g. "com.axons.locale-zh-cn"
	IconPath    string `json:"iconPath"`    // relative icon path from plugin dir, e.g. "icon.svg"
}

// NewManager creates a new plugin Manager.
func NewManager(axonsPort int) *Manager {
	homeDir, _ := os.UserHomeDir()
	pluginsDir := filepath.Join(homeDir, ".axons", "plugins")

	m := &Manager{
		instances:   make(map[string]*PluginInstance),
		registry:    NewPluginRegistry(),
		portAlloc:   NewPortAllocator(),
		sharedState: NewSharedState(pluginsDir),
		pluginsDir:  pluginsDir,
		axonsPort:   axonsPort,
		installing:  make(map[string]bool),
	}

	return m
}

// PluginDataDir returns the data directory for a plugin.
// Path: ~/.axons/plugins/data/{pluginId}
func (m *Manager) PluginDataDir(pluginID string) string {
	return filepath.Join(m.pluginsDir, "data", pluginID)
}

// SetEventHandler sets the callback for plugin lifecycle events (SSE broadcast).
func (m *Manager) SetEventHandler(handler func(eventType string, data map[string]interface{})) {
	m.eventHandler = handler
}

// SetAxonsPort updates the axons API port (called after TCP listener is ready).
func (m *Manager) SetAxonsPort(port int) {
	m.axonsPort = port
}

// SetRuntimeMode sets the runtime mode ("desktop" or "web").
// Called by daemon at startup — desktop mode via Wails, web mode otherwise.
func (m *Manager) SetRuntimeMode(mode string) {
	m.runtimeMode = mode
}

// GetRuntimeMode returns the current runtime mode.
func (m *Manager) GetRuntimeMode() string {
	if m.runtimeMode == "" {
		return "web"
	}
	return m.runtimeMode
}

// emitEvent sends a plugin lifecycle event via the configured handler.
func (m *Manager) emitEvent(eventType string, data map[string]interface{}) {
	if m.eventHandler != nil {
		m.eventHandler(eventType, data)
	}
}

// FindPluginByToken finds a running plugin by its authentication token.
// Returns the plugin ID and true if found, empty string and false otherwise.
// Tokens are runtime-only — stopped plugins have no valid tokens.
func (m *Manager) FindPluginByToken(token string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for pluginID, inst := range m.instances {
		if inst.Token == token {
			return pluginID, true
		}
	}
	return "", false
}

// HasPermission checks whether a plugin has the specified permission declared in its manifest.
func (m *Manager) HasPermission(pluginID, permission string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.instances[pluginID]
	if !ok || inst.Manifest == nil {
		return false
	}
	for _, p := range inst.Manifest.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// Registry returns the plugin registry.
func (m *Manager) Registry() *PluginRegistry {
	return m.registry
}

// PluginsDir returns the plugins directory path.
func (m *Manager) PluginsDir() string {
	return m.pluginsDir
}

// ScanPlugins scans the plugins directory and loads installed.json + manifests.
// It returns the list of discovered plugins.
func (m *Manager) ScanPlugins() ([]*InstalledPlugin, error) {
	// Ensure plugins directory exists
	if err := os.MkdirAll(m.pluginsDir, 0755); err != nil {
		return nil, fmt.Errorf("create plugins dir: %w", err)
	}

	var plugins []*InstalledPlugin

	// Read installed.json if it exists
	installedPath := filepath.Join(m.pluginsDir, "installed.json")
	data, err := os.ReadFile(installedPath)
	if err == nil {
		var reg InstalledRegistry
		if err := json.Unmarshal(data, &reg); err == nil {
			for _, p := range reg.Plugins {
				plugins = append(plugins, p)
			}
		}
	}

	// Scan for unregistered plugin directories
	entries, err := os.ReadDir(m.pluginsDir)
	if err != nil {
		return plugins, nil // Return what we have
	}

	registered := make(map[string]bool)
	for _, p := range plugins {
		registered[p.ID] = true
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(m.pluginsDir, entry.Name())
		manifestPath := filepath.Join(dir, "manifest.json")
		if _, err := os.Stat(manifestPath); err != nil {
			continue // No manifest, skip
		}

		manifest, err := LoadManifest(dir)
		if err != nil {
			fmt.Printf("[plugin-manager] WARN: Failed to load manifest from %s: %v\n", dir, err)
			continue
		}

		if !registered[manifest.ID] {
			// Auto-register as installed
			ip := &InstalledPlugin{
				ID:          manifest.ID,
				Name:        manifest.Name,
				Version:     manifest.Version,
				Description: manifest.Description,
				Author:      manifest.Author,
				Icon:        manifest.Icon,
				Category:    manifest.Category,
				Status:      StatusInstalled,
				Dir:         dir,
				Port:        0,
				InstalledAt: time.Now(),
				UpdatedAt:   time.Now(),
				Backend:     manifest.Backend,
				Frontend:    manifest.Frontend,
			}
			plugins = append(plugins, ip)
		}
	}

	return plugins, nil
}

// StartPlugin starts a plugin's backend process and registers its contributions.
func (m *Manager) StartPlugin(pluginID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if inst, ok := m.instances[pluginID]; ok {
		if inst.Status == StatusRunning || inst.Status == StatusStarting {
			return fmt.Errorf("plugin %s is already %s", pluginID, inst.Status)
		}
	}

	// Check if plugin needs installation first
	reg := m.loadInstalledRegistry()
	if entry, exists := reg.Plugins[pluginID]; exists {
		if entry.Status == StatusImported {
			return fmt.Errorf("plugin %s must be installed before starting (current status: imported)", pluginID)
		}
	}

	// Find the plugin
	plugins, _ := m.ScanPlugins()
	var target *InstalledPlugin
	for _, p := range plugins {
		if p.ID == pluginID {
			target = p
			break
		}
	}
	if target == nil {
		return fmt.Errorf("plugin %s not found", pluginID)
	}

	// Load manifest
	manifest, err := LoadManifest(target.Dir)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	// Create instance
	inst := &PluginInstance{
		Manifest: manifest,
		Status:   StatusStarting,
		Token:    generateToken(),
	}

	// If plugin has backend, start the process
	if manifest.HasBackend() {
		endpoint, err := m.startProcess(inst)
		if err != nil {
			return fmt.Errorf("start process: %w", err)
		}
		inst.Endpoint = endpoint
	} else {
		// Pure frontend plugin — no backend process
		inst.Endpoint = ""
		inst.Status = StatusRunning
	}

	inst.StartedAt = time.Now()
	m.instances[pluginID] = inst

	// Register frontend contributions
	m.registry.RegisterFromManifest(manifest, inst.Endpoint)

	// Update installed.json
	m.updateInstalledStatus(pluginID, StatusRunning, inst.Port)

	// Emit event
	m.emitEvent("plugin.started", map[string]interface{}{
		"pluginId": pluginID,
		"endpoint": inst.Endpoint,
	})

	fmt.Printf("[plugin-manager] Plugin %s started (port=%d, endpoint=%s)\n", pluginID, inst.Port, inst.Endpoint)
	return nil
}

// startProcess starts the plugin backend process.
// Returns the endpoint URL (http://127.0.0.1:PORT).
func (m *Manager) startProcess(inst *PluginInstance) (string, error) {
	manifest := inst.Manifest

	// Allocate port
	var port int
	var listener io.Closer
	if manifest.Backend.Port > 0 {
		var err error
		p, ln, err := m.portAlloc.AllocateFixed(manifest.ID, manifest.Backend.Port)
		if err != nil {
			return "", err
		}
		port = p
		listener = ln
	} else {
		p, ln, err := m.portAlloc.Allocate(manifest.ID)
		if err != nil {
			return "", err
		}
		port = p
		listener = ln
	}

	inst.Port = port
	inst.listener = listener

	// Build command
	cmd := exec.Command(manifest.Backend.Command[0], manifest.Backend.Command[1:]...)
	cmd.Dir = manifest.Dir

	// Ensure plugin data directory exists
	if err := os.MkdirAll(m.PluginDataDir(manifest.ID), 0755); err != nil {
		m.portAlloc.Release(manifest.ID)
		return "", fmt.Errorf("create data dir: %w", err)
	}

	// Environment variables
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("AXONS_API_URL=http://127.0.0.1:%d", m.axonsPort),
		fmt.Sprintf("AXONS_PLUGIN_PORT=%d", port),
		fmt.Sprintf("AXONS_PLUGIN_TOKEN=%s", inst.Token),
		fmt.Sprintf("AXONS_PLUGIN_ID=%s", manifest.ID),
		fmt.Sprintf("AXONS_PLUGIN_DATA_DIR=%s", m.PluginDataDir(manifest.ID)),
	)

	// Additional env vars from manifest
	for k, v := range manifest.Backend.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create stdin pipe for port injection
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		m.portAlloc.Release(manifest.ID)
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	inst.stdinPipe = stdinPipe

	// Capture stdout/stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the process
	if err := cmd.Start(); err != nil {
		m.portAlloc.Release(manifest.ID)
		return "", fmt.Errorf("start command: %w", err)
	}

	inst.Cmd = cmd

	// Release the held listener so the plugin can bind to the port
	if inst.listener != nil {
		inst.listener.Close()
		inst.listener = nil
	}

	// Inject port via stdin
	fmt.Fprintf(stdinPipe, "PORT:%d\n", port)

	// Health check
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := m.healthCheck(endpoint, manifest.Backend.HealthCheck, manifest.Backend.ReadyTimeout); err != nil {
		// Health check failed — kill process
		cmd.Process.Kill()
		m.portAlloc.Release(manifest.ID)
		return "", fmt.Errorf("health check: %w", err)
	}

	inst.Status = StatusRunning

	// Monitor for crashes in background
	go m.watchProcess(inst)

	return endpoint, nil
}

// healthCheck polls the plugin's health endpoint until ready or timeout.
func (m *Manager) healthCheck(endpoint, healthPath, timeoutStr string) error {
	if healthPath == "" {
		healthPath = "/health"
	}
	if timeoutStr == "" {
		timeoutStr = "10s"
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = 10 * time.Second
	}

	url := endpoint + healthPath
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("plugin not ready after %s (url=%s)", timeout, url)
}

// watchProcess monitors a plugin process for unexpected exits.
func (m *Manager) watchProcess(inst *PluginInstance) {
	if inst.Cmd == nil {
		return
	}

	err := inst.Cmd.Wait()
	if err != nil {
		fmt.Printf("[plugin-manager] Plugin %s exited with error: %v\n", inst.Manifest.ID, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Only handle if still in running state (not stopped by user)
	if inst.Status != StatusRunning {
		return
	}

	inst.Restarts++
	if inst.Restarts <= 3 {
		// Auto-restart with exponential backoff
		delay := time.Duration(1<<inst.Restarts) * time.Second
		fmt.Printf("[plugin-manager] Auto-restarting plugin %s (attempt %d, delay %v)\n",
			inst.Manifest.ID, inst.Restarts, delay)

		time.Sleep(delay)

		// Release old port
		m.portAlloc.Release(inst.Manifest.ID)
		inst.listener = nil

		// Restart
		endpoint, err := m.startProcess(inst)
		if err != nil {
			fmt.Printf("[plugin-manager] Failed to restart plugin %s: %v\n", inst.Manifest.ID, err)
			inst.Status = StatusCrashed
			m.registry.UpdateStatus(inst.Manifest.ID, "stopped")
			m.updateInstalledStatus(inst.Manifest.ID, StatusCrashed, 0)
			m.emitEvent("plugin.crashed", map[string]interface{}{
				"pluginId":   inst.Manifest.ID,
				"restarts":   inst.Restarts,
				"lastError":  err.Error(),
			})
		} else {
			inst.Endpoint = endpoint
			m.registry.UpdateEndpoint(inst.Manifest.ID, endpoint)
			m.registry.UpdateStatus(inst.Manifest.ID, "running")
			m.updateInstalledStatus(inst.Manifest.ID, StatusRunning, inst.Port)
			m.emitEvent("plugin.started", map[string]interface{}{
				"pluginId": inst.Manifest.ID,
				"endpoint": endpoint,
			})
			// Watch again
			go m.watchProcess(inst)
		}
	} else {
		// Exceeded max restarts
		fmt.Printf("[plugin-manager] Plugin %s exceeded max restarts (3), marking as crashed\n", inst.Manifest.ID)
		inst.Status = StatusCrashed
		m.portAlloc.Release(inst.Manifest.ID)
		m.registry.UpdateStatus(inst.Manifest.ID, "stopped")
		m.updateInstalledStatus(inst.Manifest.ID, StatusCrashed, 0)
		m.emitEvent("plugin.crashed", map[string]interface{}{
			"pluginId":   inst.Manifest.ID,
			"restarts":   inst.Restarts,
			"lastError":  fmt.Sprintf("exceeded max restarts (%d)", inst.Restarts),
		})
	}
}

// StopPlugin stops a running plugin.
func (m *Manager) StopPlugin(pluginID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, ok := m.instances[pluginID]
	if !ok {
		return fmt.Errorf("plugin %s not found", pluginID)
	}
	if inst.Status != StatusRunning && inst.Status != StatusStarting {
		return fmt.Errorf("plugin %s is not running (status=%s)", pluginID, inst.Status)
	}

	// Call cleanup endpoint (optional, 5s timeout)
	if inst.Endpoint != "" {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Post(inst.Endpoint+"/cleanup", "application/json", strings.NewReader(""))
		if err == nil {
			resp.Body.Close()
		}
		// 404 means plugin doesn't implement /cleanup — that's fine
	}

	// Stop the process
	if inst.Cmd != nil && inst.Cmd.Process != nil {
		inst.Cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() {
			done <- inst.Cmd.Wait()
		}()
		select {
		case <-done:
			// Process exited gracefully
		case <-time.After(5 * time.Second):
			// Force kill
			inst.Cmd.Process.Kill()
		}
	}

	// Release port
	if inst.listener != nil {
		m.portAlloc.Release(pluginID)
		inst.listener = nil
	}

	inst.Status = StatusStopped
	inst.Port = 0
	inst.Endpoint = ""

	// Unregister from registry
	m.registry.UnregisterPlugin(pluginID)

	// Update installed.json
	m.updateInstalledStatus(pluginID, StatusStopped, 0)

	// Emit event
	m.emitEvent("plugin.stopped", map[string]interface{}{
		"pluginId": pluginID,
	})

	fmt.Printf("[plugin-manager] Plugin %s stopped\n", pluginID)
	return nil
}

// GetInstance returns a plugin instance by ID.
func (m *Manager) GetInstance(pluginID string) (*PluginInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inst, ok := m.instances[pluginID]
	if !ok {
		return nil, false
	}
	return inst, true
}

// GetPluginByToken looks up a plugin by its auth token.
func (m *Manager) GetPluginByToken(token string) (*PluginManifest, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, inst := range m.instances {
		if inst.Token == token {
			return inst.Manifest, true
		}
	}
	return nil, false
}

// ListPlugins returns the list of all installed plugins.
func (m *Manager) ListPlugins() ([]*InstalledPlugin, error) {
	return m.ScanPlugins()
}

// ImportPlugin imports a plugin from a tar.gz archive.
func (m *Manager) ImportPlugin(archivePath string) error {
	// Step 1: Create temp directory for extraction
	tmpDir, err := os.MkdirTemp("", "axons-import-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir) // cleanup on failure

	// Step 2: Extract tar.gz to temp directory
	if err := extractTarGz(archivePath, tmpDir); err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}

	// Step 3: Find and validate manifest.json
	// The manifest may be at the root of the archive or one level deep
	manifestDir, err := findManifestDir(tmpDir)
	if err != nil {
		return fmt.Errorf("find manifest: %w", err)
	}

	manifest, err := LoadManifest(manifestDir)
	if err != nil {
		return fmt.Errorf("validate manifest: %w", err)
	}

	// Step 4: Check if plugin is already installed
	reg := m.loadInstalledRegistry()
	if _, exists := reg.Plugins[manifest.ID]; exists {
		return fmt.Errorf("plugin %s is already installed", manifest.ID)
	}

	// Step 5: Move from temp to final location
	targetDir := filepath.Join(m.pluginsDir, manifest.ID)
	if err := os.Rename(manifestDir, targetDir); err != nil {
		// Rename may fail across filesystems, fall back to copy
		if err := copyDir(manifestDir, targetDir); err != nil {
			return fmt.Errorf("move plugin directory: %w", err)
		}
	}

	// Step 6: Write to installed.json (status = "imported")
	ip := &InstalledPlugin{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Author:      manifest.Author,
		Icon:        manifest.Icon,
		Category:    manifest.Category,
		Status:      StatusImported,
		Dir:         targetDir,
		Port:        0,
		InstalledAt: time.Now(),
		UpdatedAt:   time.Now(),
		Backend:     manifest.Backend,
		Frontend:    manifest.Frontend,
	}
	reg.Plugins[manifest.ID] = ip
	if err := m.saveInstalledRegistry(reg); err != nil {
		return fmt.Errorf("save installed registry: %w", err)
	}

	// Step 7: Emit event
	m.emitEvent("plugin.imported", map[string]interface{}{
		"pluginId": manifest.ID,
		"name":     manifest.Name,
		"version":  manifest.Version,
	})

	// Step 8: If localization plugin, load locale resources + broadcast SSE
	if manifest.Category == "localization" && manifest.Frontend != nil && manifest.Frontend.Locale != nil {
		m.loadSingleLocalePlugin(manifest)
	}

	fmt.Printf("[plugin-manager] Plugin %s imported to %s\n", manifest.ID, targetDir)
	return nil
}

// InstallPlugin runs the install script for a plugin.
func (m *Manager) InstallPlugin(pluginID string) error {
	plugins, _ := m.ScanPlugins()
	var target *InstalledPlugin
	for _, p := range plugins {
		if p.ID == pluginID {
			target = p
			break
		}
	}
	if target == nil {
		return fmt.Errorf("plugin %s not found", pluginID)
	}

	if target.Backend == nil || target.Backend.Install == nil {
		// No install script needed
		m.updateInstalledStatus(pluginID, StatusInstalled, 0)
		m.emitEvent("plugin.installed", map[string]interface{}{
			"pluginId": pluginID,
			"name":     target.Name,
			"version":  target.Version,
		})
		return nil
	}

	// Ensure data directory exists before running install script
	dataDir := m.PluginDataDir(pluginID)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin data directory %s: %w", dataDir, err)
	}

	// Run install script
	cmd := exec.Command(target.Backend.Install.Command[0], target.Backend.Install.Command[1:]...)
	cmd.Dir = target.Dir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("AXONS_PLUGIN_DATA_DIR=%s", dataDir),
	)

	// Capture stdout and stderr, emit each line as a plugin.installProgress event
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		m.emitEvent("plugin.installFailed", map[string]interface{}{
			"pluginId": pluginID,
			"error":    fmt.Sprintf("failed to create stdout pipe: %v", err),
		})
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		stdoutPipe.Close()
		m.emitEvent("plugin.installFailed", map[string]interface{}{
			"pluginId": pluginID,
			"error":    fmt.Sprintf("failed to create stderr pipe: %v", err),
		})
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Stream lines from a reader, prefixing each line with the given stream name
	streamLines := func(reader io.Reader, stream string) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			m.emitEvent("plugin.installProgress", map[string]interface{}{
				"pluginId": pluginID,
				"stream":   stream,
				"line":     line,
			})
		}
	}

	if err := cmd.Start(); err != nil {
		m.emitEvent("plugin.installFailed", map[string]interface{}{
			"pluginId": pluginID,
			"error":    fmt.Sprintf("failed to start install script: %v", err),
		})
		return fmt.Errorf("install script failed to start: %w", err)
	}

	// Set up timeout timer — must be before we start reading pipes
	timeout := 180 * time.Second
	if target.Backend.Install.Timeout != "" {
		if d, err := time.ParseDuration(target.Backend.Install.Timeout); err == nil {
			timeout = d
		}
	}

	timer := time.AfterFunc(timeout, func() {
		cmd.Process.Kill()
	})
	defer timer.Stop()

	// Read stdout and stderr concurrently.
	// Pipes will deliver EOF after the process exits (or is killed by timer).
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); streamLines(stdoutPipe, "stdout") }()
	go func() { defer wg.Done(); streamLines(stderrPipe, "stderr") }()

	// Wait for the process to finish, then wait for pipe readers to complete
	waitErr := cmd.Wait()
	wg.Wait()

	if waitErr != nil {
		// Install failed — rollback
		m.emitEvent("plugin.installFailed", map[string]interface{}{
			"pluginId": pluginID,
			"error":    waitErr.Error(),
		})
		return fmt.Errorf("install script failed: %w", waitErr)
	}

	m.updateInstalledStatus(pluginID, StatusInstalled, 0)
	m.emitEvent("plugin.installed", map[string]interface{}{
		"pluginId": pluginID,
		"name":     target.Name,
		"version":  target.Version,
	})

	return nil
}

// UninstallPlugin removes a plugin.
// argValues maps arg names (e.g. "purge_data") to user-chosen values,
// derived from the manifest's UninstallDef.Args declarations.
func (m *Manager) UninstallPlugin(pluginID string, argValues map[string]bool) error {
	// 1. Stop if running
	if _, ok := m.instances[pluginID]; ok {
		if err := m.StopPlugin(pluginID); err != nil {
			fmt.Printf("[plugin-manager] WARN: failed to stop plugin %s during uninstall: %v\n", pluginID, err)
		}
	}

	// 2. Load manifest before directory removal (needed for localization cleanup)
	plugins, _ := m.ScanPlugins()
	var manifest *PluginManifest
	for _, p := range plugins {
		if p.ID == pluginID {
			if mf, err := LoadManifest(p.Dir); err == nil {
				manifest = mf
			}
			break
		}
	}

	// 3. Run uninstall script with declarative args, if provided
	for _, p := range plugins {
		if p.ID == pluginID && p.Backend != nil && p.Backend.Uninstall != nil {
			args := make([]string, len(p.Backend.Uninstall.Command))
			copy(args, p.Backend.Uninstall.Command)

			// Build command-line flags from manifest-declared args and user-chosen values
			for _, arg := range p.Backend.Uninstall.Args {
				if val, ok := argValues[arg.Name]; ok && val {
					// snake_case → --kebab-case
					flag := "--" + strings.ReplaceAll(arg.Name, "_", "-")
					args = append(args, flag)
				}
			}

			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = p.Dir
			cmd.Env = append(os.Environ(),
				fmt.Sprintf("AXONS_PLUGIN_DATA_DIR=%s", m.PluginDataDir(pluginID)),
			)

			// Set timeout to prevent script from hanging
			timer := time.AfterFunc(30*time.Second, func() {
				cmd.Process.Kill()
			})
			if err := cmd.Run(); err != nil {
				fmt.Printf("[plugin-manager] WARN: uninstall script failed for %s: %v\n", pluginID, err)
			}
			timer.Stop()
		}
	}

	// 4. If localization plugin, unload locale resources + broadcast SSE
	if manifest != nil && manifest.Category == "localization" {
		m.unloadSingleLocalePlugin(pluginID, manifest)
	}

	// 5. Remove from instances
	m.mu.Lock()
	delete(m.instances, pluginID)
	m.mu.Unlock()

	// 6. Remove from registry
	m.registry.UnregisterPlugin(pluginID)

	// 7. Remove plugin code directory (always)
	for _, p := range plugins {
		if p.ID == pluginID {
			os.RemoveAll(p.Dir)
			break
		}
	}

	// Note: Data directory cleanup is handled by the plugin uninstall script (step 3).
	// The host never directly removes the data directory to avoid racing with the script.

	// 8. Update installed.json
	m.removeInstalledPlugin(pluginID)

	// 9. Emit event
	m.emitEvent("plugin.uninstalled", map[string]interface{}{
		"pluginId":  pluginID,
		"argValues": argValues,
	})

	fmt.Printf("[plugin-manager] Plugin %s uninstalled (argValues=%v)\n", pluginID, argValues)
	return nil
}

// StartAutoStartPlugins starts all plugins with "onStartup" activation event
// and loads locale resources for all installed localization plugins.
func (m *Manager) StartAutoStartPlugins() {
	plugins, err := m.ScanPlugins()
	if err != nil {
		fmt.Printf("[plugin-manager] WARN: failed to scan plugins for auto-start: %v\n", err)
		return
	}

	for _, p := range plugins {
		manifest, err := LoadManifest(p.Dir)
		if err != nil {
			continue
		}

		// Load locale resources for localization plugins (they don't need
		// to be "started" — just their resources loaded into i18n + availableLocales)
		if manifest.Category == "localization" && manifest.Frontend != nil && manifest.Frontend.Locale != nil {
			m.loadSingleLocalePlugin(manifest)
		}

		// Check activation events for auto-start
		for _, event := range manifest.ActivationEvents {
			if event == "onStartup" {
				if err := m.StartPlugin(p.ID); err != nil {
					fmt.Printf("[plugin-manager] WARN: failed to auto-start plugin %s: %v\n", p.ID, err)
				}
				break
			}
		}
	}
}

// --- installed.json management ---

func (m *Manager) loadInstalledRegistry() *InstalledRegistry {
	path := filepath.Join(m.pluginsDir, "installed.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &InstalledRegistry{
			Version: 1,
			Plugins: make(map[string]*InstalledPlugin),
		}
	}
	var reg InstalledRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return &InstalledRegistry{
			Version: 1,
			Plugins: make(map[string]*InstalledPlugin),
		}
	}
	return &reg
}

func (m *Manager) saveInstalledRegistry(reg *InstalledRegistry) error {
	if err := os.MkdirAll(m.pluginsDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.pluginsDir, "installed.json"), data, 0644)
}

func (m *Manager) updateInstalledStatus(pluginID string, status PluginStatus, port int) {
	reg := m.loadInstalledRegistry()
	if p, ok := reg.Plugins[pluginID]; ok {
		p.Status = status
		p.Port = port
		p.UpdatedAt = time.Now()
	} else {
		// Find plugin info from scan
		plugins, _ := m.ScanPlugins()
		for _, p := range plugins {
			if p.ID == pluginID {
				p.Status = status
				p.Port = port
				p.UpdatedAt = time.Now()
				reg.Plugins[pluginID] = p
				break
			}
		}
	}
	if err := m.saveInstalledRegistry(reg); err != nil {
		fmt.Printf("[plugin-manager] WARN: failed to save installed.json: %v\n", err)
	}
}

func (m *Manager) removeInstalledPlugin(pluginID string) {
	reg := m.loadInstalledRegistry()
	delete(reg.Plugins, pluginID)
	if err := m.saveInstalledRegistry(reg); err != nil {
		fmt.Printf("[plugin-manager] WARN: failed to save installed.json: %v\n", err)
	}
}

// --- Helpers ---

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "axons_plg_" + hex.EncodeToString(b)
}

// FormatPortForDisplay returns a human-readable port string.
func FormatPortForDisplay(port int) string {
	if port == 0 {
		return "N/A"
	}
	return strconv.Itoa(port)
}

// extractTarGz extracts a .tar.gz archive to the target directory.
func extractTarGz(src string, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Security: prevent path traversal
		target := filepath.Join(dst, hdr.Name)
		cleanTarget := filepath.Clean(target)
		cleanDst := filepath.Clean(dst)
		if cleanTarget != cleanDst && !strings.HasPrefix(cleanTarget, cleanDst+string(os.PathSeparator)) {
			return fmt.Errorf("path traversal detected: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("create dir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", target, err)
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.CopyN(outFile, tr, hdr.Size); err != nil {
				outFile.Close()
				return fmt.Errorf("write file %s: %w", target, err)
			}
			outFile.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent dir for symlink %s: %w", target, err)
			}
			os.Remove(target) // remove if exists
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("create symlink %s: %w", target, err)
			}
		}
	}

	return nil
}

// findManifestDir locates the directory containing manifest.json.
// It checks the root first, then one level deep (handles common tar structure).
func findManifestDir(root string) (string, error) {
	// Check root
	if _, err := os.Stat(filepath.Join(root, "manifest.json")); err == nil {
		return root, nil
	}

	// Check one level deep
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("read directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(sub, "manifest.json")); err == nil {
			return sub, nil
		}
	}

	return "", fmt.Errorf("manifest.json not found in archive")
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, si.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy file
			srcFile, err := os.Open(srcPath)
			if err != nil {
				return err
			}
			dstFile, err := os.Create(dstPath)
			if err != nil {
				srcFile.Close()
				return err
			}
			if _, err := io.Copy(dstFile, srcFile); err != nil {
				srcFile.Close()
				dstFile.Close()
				return err
			}
			srcFile.Close()
			dstFile.Close()

			// Preserve mode
			srcInfo, err := os.Stat(srcPath)
			if err != nil {
				return err
			}
			os.Chmod(dstPath, srcInfo.Mode())
		}
	}
	return nil
}

// --- Locale plugin management ---

// loadSingleLocalePlugin loads i18n resources for a localization plugin and broadcasts SSE event.
func (m *Manager) loadSingleLocalePlugin(manifest *PluginManifest) {
	locale := manifest.Frontend.Locale
	if locale == nil {
		return
	}

	// 1. Load backend Go i18n resources into memory
	for _, res := range locale.BackendResources {
		path := filepath.Join(manifest.Dir, res)
		if err := i18n.LoadBundle(locale.Language, filepath.Dir(path)); err != nil {
			fmt.Printf("[plugin-manager] WARN: failed to load backend locale %s: %v\n", locale.Language, err)
		}
	}

	// 2. Append to available locales list
	// Derive relative icon path from manifest.Icon (already resolved to absolute path).
	var iconPath string
	if manifest.Icon != "" {
		rel, err := filepath.Rel(manifest.Dir, manifest.Icon)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			iconPath = rel
		}
	}
	m.mu.Lock()
	m.availableLocales = append(m.availableLocales, LocaleInfo{
		Code:        locale.Language,
		NativeName:  locale.DisplayName.Native,
		EnglishName: locale.DisplayName.English,
		PluginID:    manifest.ID,
		IconPath:    iconPath,
	})
	m.mu.Unlock()

	// 3. SSE broadcast locale.available event
	m.emitEvent("locale.available", map[string]interface{}{
		"locale":      locale.Language,
		"pluginId":    manifest.ID,
		"nativeName":  locale.DisplayName.Native,
		"englishName": locale.DisplayName.English,
	})

	fmt.Printf("[plugin-manager] Locale plugin loaded: %s (%s)\n", locale.Language, manifest.ID)
}

// unloadSingleLocalePlugin unloads i18n resources for a localization plugin and broadcasts SSE event.
func (m *Manager) unloadSingleLocalePlugin(pluginID string, manifest *PluginManifest) {
	locale := manifest.Frontend.Locale
	if locale == nil {
		return
	}

	// 1. Unload backend Go i18n resources from memory
	i18n.UnloadBundle(locale.Language)

	// 2. Remove from available locales list
	m.mu.Lock()
	filtered := make([]LocaleInfo, 0, len(m.availableLocales))
	for _, l := range m.availableLocales {
		if l.PluginID != pluginID {
			filtered = append(filtered, l)
		}
	}
	m.availableLocales = filtered
	m.mu.Unlock()

	// 3. If currently using this locale, fallback to en
	if i18n.GetLocale() == locale.Language {
		i18n.SetLocale("en")
	}

	// 4. SSE broadcast locale.unavailable event
	m.emitEvent("locale.unavailable", map[string]interface{}{
		"locale":   locale.Language,
		"pluginId": pluginID,
		"fallback": "en",
	})

	fmt.Printf("[plugin-manager] Locale plugin unloaded: %s (%s)\n", locale.Language, pluginID)
}

// GetAvailableLocales returns the list of available locales from localization plugins.
func (m *Manager) GetAvailableLocales() []LocaleInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]LocaleInfo, len(m.availableLocales))
	copy(result, m.availableLocales)
	return result
}
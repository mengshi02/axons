package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/i18n"
)

// RegisterRoutes registers all plugin API routes on the given router.
//
// httprouter restriction: static and param segments cannot coexist at the same level.
// e.g. /v1/plugins/registry/:type and /v1/plugins/:id/install conflict because
// "registry" (static) and ":id" (param) are at the same position.
//
// Solution: register a catch-all handler for /v1/plugins/*path that dispatches
// internally based on the path structure, avoiding httprouter tree conflicts.
func (m *Manager) RegisterRoutes(router *httprouter.Router) {
	// Top-level plugin list (no conflict — no :id at this level)
	router.GET("/v1/plugins", m.handleListPlugins)

	// Catch-all dispatcher for /v1/plugins/*subpath
	// All sub-routes are dispatched inside handlePluginDispatch
	router.GET("/v1/plugins/*subpath", m.handlePluginDispatch)
	router.POST("/v1/plugins/*subpath", m.handlePluginDispatch)
	router.PUT("/v1/plugins/*subpath", m.handlePluginDispatch)
	router.DELETE("/v1/plugins/*subpath", m.handlePluginDispatch)
	router.PATCH("/v1/plugins/*subpath", m.handlePluginDispatch)
	router.OPTIONS("/v1/plugins/*subpath", m.handlePluginDispatch)

	// Plugin UI static files (separate prefix, no conflict)
	router.GET("/plugins/:id/*filepath", m.handlePluginStaticRoute)
}

// --- Handlers ---

// handlePluginDispatch is the central router for /v1/plugins/*subpath.
// It parses the subpath and dispatches to the appropriate handler,
// avoiding httprouter's static-vs-param conflict at the /v1/plugins/ level.
func (m *Manager) handlePluginDispatch(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	subpath := ps.ByName("subpath")
	// httprouter returns "/scan" for /v1/plugins/scan
	// Strip leading slash for cleaner matching
	path := strings.TrimPrefix(subpath, "/")

	// Static routes (no :id segment)
	switch {
	case path == "scan" && r.Method == http.MethodPost:
		m.handleScanPlugins(w, r, ps)
		return
	case path == "import" && r.Method == http.MethodPost:
		m.handleImportPlugin(w, r, ps)
		return
	case path == "locales" && r.Method == http.MethodGet:
		m.handleGetLocales(w, r, ps)
		return
	case path == "system-state" && r.Method == http.MethodGet:
		m.handleGetSystemState(w, r, ps)
		return
	case strings.HasPrefix(path, "state/"):
		key := strings.TrimPrefix(path, "state/")
		// Inject key into ps by creating a new params slice
		newPs := httprouter.Params{{Key: "key", Value: key}}
		switch r.Method {
		case http.MethodGet:
			m.handleGetPluginState(w, r, newPs)
		case http.MethodPut:
			m.handleSetPluginState(w, r, newPs)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		}
		return
	case strings.HasPrefix(path, "registry/"):
		rest := strings.TrimPrefix(path, "registry/")
		if rest == "sync" && r.Method == http.MethodPost {
			m.handleSyncPluginEntries(w, r, ps)
			return
		}
		// registry/:type
		newPs := httprouter.Params{{Key: "type", Value: rest}}
		m.handleGetPluginEntries(w, r, newPs)
		return
	// EventBus routes (front-thin-back-thick: daemon is the broadcast center)
	case path == "event" && r.Method == http.MethodPost:
		GetGlobalBus().HandlePostEvent(w, r)
		return
	case path == "events/stream" && r.Method == http.MethodGet:
		GetGlobalBus().HandleEventStream(w, r)
		return
	}

	// Dynamic routes with :id segment
	// path format: {id}/... or {id} (just the id, for DELETE)
	parts := strings.SplitN(path, "/", 3)
	if len(parts) == 0 || parts[0] == "" {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "plugin route not found")
		return
	}

	pluginID := parts[0]
	newPs := httprouter.Params{{Key: "id", Value: pluginID}}

	// Determine sub-action
	subAction := ""
	if len(parts) >= 2 {
		subAction = parts[1]
	}

	switch {
	case subAction == "" && r.Method == http.MethodDelete:
		m.handleUninstallPlugin(w, r, newPs)
	case subAction == "install" && r.Method == http.MethodPost:
		m.handleInstallPlugin(w, r, newPs)
	case subAction == "start" && r.Method == http.MethodPost:
		m.handleStartPlugin(w, r, newPs)
	case subAction == "stop" && r.Method == http.MethodPost:
		m.handleStopPlugin(w, r, newPs)
	case subAction == "data" && r.Method == http.MethodDelete:
		m.handleCleanupPlugin(w, r, newPs)
	case subAction == "proxy":
		// Proxy: /v1/plugins/:id/proxy/*path
		m.HandlePluginProxy(w, r, pluginID)
	case subAction == "iframe-host" && r.Method == http.MethodGet:
		// Iframe container: /v1/plugins/:id/iframe-host
		m.HandlePluginIframeHost(w, r, pluginID)
	default:
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "plugin route not found: "+path)
	}
}

func (m *Manager) handleListPlugins(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	plugins, err := m.ListPlugins()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "LIST_PLUGINS_ERROR", err.Error())
		return
	}

	// Enrich with runtime status
	type PluginCard struct {
		ID          string       `json:"id"`
		Name        string       `json:"name"`
		Version     string       `json:"version"`
		Description string       `json:"description"`
		Author      string       `json:"author"`
		Icon        string       `json:"icon"`
		Category    string       `json:"category"`
		Status      PluginStatus `json:"status"`
		Endpoint    string       `json:"endpoint"`
		Port        int          `json:"port"`
		Dir         string       `json:"dir"`
		Frontend    *FrontendDef `json:"frontend"`
		Backend     *BackendDef  `json:"backend"`
	}

	cards := make([]PluginCard, 0, len(plugins))
	for _, p := range plugins {
		card := PluginCard{
			ID:          p.ID,
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Author:      p.Author,
			Icon:        p.Icon,
			Category:    p.Category,
			Status:      p.Status,
			Dir:         p.Dir,
			Frontend:    p.Frontend,
			Backend:     p.Backend,
		}
		// Add runtime info if running
		if inst, ok := m.GetInstance(p.ID); ok {
			card.Endpoint = inst.Endpoint
			card.Port = inst.Port
			card.Status = inst.Status
		}
		cards = append(cards, card)
	}

	writeJSON(w, http.StatusOK, cards)
}

func (m *Manager) handleScanPlugins(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	plugins, err := m.ScanPlugins()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "SCAN_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":   len(plugins),
		"plugins": plugins,
	})
}

func (m *Manager) handleImportPlugin(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Limit upload size to 100MB
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_UPLOAD", fmt.Sprintf("failed to parse upload: %v", err))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FILE", "no 'file' field in multipart form")
		return
	}
	defer file.Close()

	// Validate file extension
	if ext := filepath.Ext(header.Filename); ext != ".gz" && ext != ".tgz" {
		if !strings.HasSuffix(header.Filename, ".tar.gz") {
			writeJSONError(w, http.StatusBadRequest, "INVALID_FORMAT", "only .tar.gz archives are supported")
			return
		}
	}

	// Save to temp file (ImportPlugin needs a path, not a reader)
	tmpFile, err := os.CreateTemp("", "axons-upload-*.tar.gz")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "TEMP_ERROR", "failed to create temp file")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()
		writeJSONError(w, http.StatusInternalServerError, "SAVE_ERROR", fmt.Sprintf("failed to save upload: %v", err))
		return
	}
	tmpFile.Close()

	// Import the plugin
	if err := m.ImportPlugin(tmpPath); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "IMPORT_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "imported"})
}

func (m *Manager) handleInstallPlugin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pluginID := ps.ByName("id")

	// Validate plugin exists before starting async install
	plugins, _ := m.ScanPlugins()
	found := false
	for _, p := range plugins {
		if p.ID == pluginID {
			found = true
			break
		}
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("plugin %s not found", pluginID))
		return
	}

	// Check if already installing (prevent duplicate installs)
	m.installMu.Lock()
	if m.installing[pluginID] {
		m.installMu.Unlock()
		writeJSONError(w, http.StatusConflict, "ALREADY_INSTALLING", fmt.Sprintf("plugin %s is already being installed", pluginID))
		return
	}
	m.installing[pluginID] = true
	m.installMu.Unlock()

	// Run install asynchronously — return 202 Accepted immediately
	go func() {
		defer func() {
			m.installMu.Lock()
			delete(m.installing, pluginID)
			m.installMu.Unlock()
		}()
		if err := m.InstallPlugin(pluginID); err != nil {
			// InstallPlugin already emits plugin.installFailed event
			return
		}
		// InstallPlugin already emits plugin.installed event on success
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":   "installing",
		"pluginId": pluginID,
	})
}

func (m *Manager) handleStartPlugin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pluginID := ps.ByName("id")
	if err := m.StartPlugin(pluginID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "START_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "running"})
}

func (m *Manager) handleStopPlugin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pluginID := ps.ByName("id")
	if err := m.StopPlugin(pluginID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "STOP_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (m *Manager) handleUninstallPlugin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pluginID := ps.ByName("id")

	// Extract args.{name} format query parameters
	argValues := make(map[string]bool)
	for key, values := range r.URL.Query() {
		if strings.HasPrefix(key, "args.") && len(values) > 0 {
			argName := strings.TrimPrefix(key, "args.")
			argValues[argName] = values[0] == "true"
		}
	}

	if err := m.UninstallPlugin(pluginID, argValues); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "UNINSTALL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "uninstalled",
		"argValues": argValues,
	})
}

func (m *Manager) handleCleanupPlugin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pluginID := ps.ByName("id")
	// Remove shared state for this plugin
	count := m.sharedState.DeleteByPlugin(pluginID)
	// Remove installed registry entry
	m.removeInstalledPlugin(pluginID)
	m.emitEvent("plugin.cleaned", map[string]interface{}{
		"pluginId":    pluginID,
		"stateEntries": count,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "cleaned",
		"stateRemoved": count,
	})
}

func (m *Manager) handleGetPluginEntries(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	entryType := ps.ByName("type")
	entries := m.Registry().GetByType(entryType)
	writeJSON(w, http.StatusOK, entries)
}

func (m *Manager) handleSyncPluginEntries(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Plugins can POST here to sync their runtime state
	var entries []PluginEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}

	for _, entry := range entries {
		if err := m.Registry().Register(entry); err != nil {
			// Log but don't fail on individual entry errors
			println("[plugin-sync] WARN:", err.Error())
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "synced"})
}

func (m *Manager) handleGetSystemState(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	all := m.sharedState.All()

	// Build response with system-wide state (keys starting with "system/")
	systemState := make(map[string]json.RawMessage)
	for k, v := range all {
		systemState[k] = v
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"state":     systemState,
		"timestamp": time.Now().UnixMilli(),
	})
}

func (m *Manager) handleGetPluginState(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	key := ps.ByName("key")

	// Query parameter "prefix" enables prefix-based listing
	if prefix := r.URL.Query().Get("prefix"); prefix != "" {
		entries := m.sharedState.GetByPrefix(prefix)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"entries": entries,
		})
		return
	}

	value, ok := m.sharedState.Get(key)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"key":   key,
			"value": nil,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"key":   key,
		"value": value,
	})
}

func (m *Manager) handleSetPluginState(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	key := ps.ByName("key")

	var body struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be JSON with a 'value' field")
		return
	}

	if len(body.Value) == 0 {
		writeJSONError(w, http.StatusBadRequest, "MISSING_VALUE", "request body must include a 'value' field")
		return
	}

	if err := m.sharedState.Set(key, body.Value); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "STATE_ERROR", "failed to persist state")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"key":   key,
		"value": body.Value,
	})
}

func (m *Manager) handlePluginStaticRoute(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pluginID := ps.ByName("id")
	filePath := ps.ByName("filepath")
	m.HandlePluginStaticFiles(w, r, pluginID, filePath)
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}

// matchPermission maps an API route to a plugin permission.
// Used for permission check logging (Phase 1: warn only).
func matchPermission(method, path string) string {
	if strings.HasPrefix(path, "/v1/graph") || strings.HasPrefix(path, "/v1/search") || strings.HasPrefix(path, "/v1/stats") {
		return "graph:read"
	}
	if strings.HasPrefix(path, "/v1/projects") || strings.HasPrefix(path, "/v1/repos") {
		return "project:read"
	}
	if strings.HasPrefix(path, "/api/llm-models") {
		return "model:register"
	}
	if strings.HasPrefix(path, "/v1/plugins/state") {
		if method == "GET" {
			return "state:read"
		}
		return "state:write"
	}
	return ""
}

// handleGetLocales returns the list of available locales from installed localization plugins.
// GET /v1/plugins/locales
// Response: { "locales": { "zh-CN": { "pluginId": "...", "nativeName": "...", "englishName": "...", "iconPath": "..." } } }
func (m *Manager) handleGetLocales(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	m.mu.RLock()
	locales := make(map[string]struct {
		PluginID    string `json:"pluginId"`
		NativeName  string `json:"nativeName"`
		EnglishName string `json:"englishName"`
		IconPath    string `json:"iconPath"`
	})
	for _, l := range m.availableLocales {
		locales[l.Code] = struct {
			PluginID    string `json:"pluginId"`
			NativeName  string `json:"nativeName"`
			EnglishName string `json:"englishName"`
			IconPath    string `json:"iconPath"`
		}{
			PluginID:    l.PluginID,
			NativeName:  l.NativeName,
			EnglishName: l.EnglishName,
			IconPath:    l.IconPath,
		}
	}
	m.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{"locales": locales})
}
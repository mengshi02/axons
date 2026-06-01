# Exec Command Bridge Design

> Version: v1.0 | Date: 2026-05-25 | Status: In Design

## 1. Background

The axons frontend [`readSystemClipboardFiles()`](../ui/src/components/FileTreePanel.tsx:56) calls `/api/clipboard/files` to read file paths from the system clipboard (files copied by the user from Finder/Explorer/Dolphin), but the backend endpoint is not implemented.

This feature depends on OS-level command-line tools:
- macOS: `osascript` (pre-installed)
- Windows: `PowerShell` (pre-installed)
- Linux: `xclip` / `xsel` / `wl-paste` (**not pre-installed**)

Hard-coding such OS-specific logic into the axons daemon is unreasonable — every new similar scenario (git bridge, shell bridge, etc.) would require modifying daemon code.

**Core idea**: Plugins declare "what tools to install" and "how to execute them", while the daemon provides a generic exec engine for execution. exec becomes a daemon capability, and future similar scenarios only need a manifest.json declaration + scripts, with zero Go code changes.

## 2. Overall Architecture

```
┌─────────── axons daemon ───────────────────────┐
│                                                  │
│  ExecEngine (Generic Command Execution Engine)   │
│  ├── ExecCommand(pluginId, commandId) → result   │
│  ├── OS fork: select command by runtime.GOOS     │
│  ├── fallback chain: auto-retry on failure       │
│  └── parse strategy: built-in stdout parsers     │
│                                                  │
│  RouteMapper (Virtual Endpoint Registration)     │
│  ├── Plugin declares toolExec[].route            │
│  ├── On start: register HTTP handler → ExecEngine│
│  └── On stop: unregister routes                  │
│                                                  │
│  ToolInstaller (Tool Installation)               │
│  ├── Plugin declares toolExec[].install          │
│  ├── check passes → skip installation            │
│  └── check fails → exec install.command          │
│                                                  │
└──────────────────────────────────────────────────┘
       ↕ manifest.json + script files
┌─────────── Plugin Side ────────────────────────┐
│  ~/.axons/plugins/com.axons.clipboard-bridge/   │
│  ├── manifest.json        ← declare command     │
│  │                           templates          │
│  ├── install.sh           ← Linux: install xclip│
│  └── uninstall.sh         ← cleanup             │
└──────────────────────────────────────────────────┘
```

### Data Flow

```
User paste → frontend fetch('/api/clipboard/files')
           → daemon RouteMapper matches virtual endpoint
           → ExecEngine reads command template (manifest.toolExec)
           → select command by OS → exec.Command execution
           → parse strategy parses stdout
           → JSON response returned to frontend
```

## 3. manifest.json Extension

### 3.1 New `toolExec` Field

Add a `toolExec` array to `PluginManifest`, declaring the "tool execution commands" provided by the plugin. Each entry defines a command that can be triggered via an HTTP endpoint.

```jsonc
{
  "id": "com.axons.clipboard-bridge",
  "name": "Clipboard Bridge",
  "version": "1.0.0",
  "description": "Bridge system clipboard file references into axons",
  "author": "axons-community",
  "category": "productivity",
  "minAxonsVersion": "0.8.0",
  "permissions": ["clipboard:read"],

  // No backend process! No backend field needed
  "backend": null,
  "frontend": null,

  // New: tool execution commands
  "toolExec": [
    {
      "id": "clipboard.read-files",
      "description": "Read file paths from system clipboard",

      // HTTP endpoint — daemon auto-registers virtual route
      "route": "GET /api/clipboard/files",
      "timeout": "3s",

      // Command template: OS fork
      "exec": {
        "darwin": {
          "command": ["osascript", "-e", "tell application \"Finder\" to get the clipboard as «class furl»"],
          "parse": "osascript-file-url"
        },
        "linux": {
          "command": ["xclip", "-selection", "clipboard", "-t", "text/uri-list", "-o"],
          "fallback": {
            "command": ["xsel", "--clipboard", "--output"],
            "parse": "uri-list"
          },
          "parse": "uri-list"
        },
        "windows": {
          "command": ["powershell", "-Command", "Get-Clipboard -Format FileDropList | ForEach-Object { $_.FullName }"],
          "parse": "line-list"
        }
      },

      // Tool installation: OS fork
      "install": {
        "darwin": null,
        "linux": {
          "check": ["which", "xclip"],
          "command": ["bash", "{pluginDir}/install.sh"],
          "timeout": "60s"
        },
        "windows": null
      }
    }
  ],

  "activationEvents": ["onStartup"]
}
```

### 3.2 Field Definitions

#### ToolExecDef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique command identifier, unique within the plugin |
| `description` | string | Yes | Command description, used in logs and UI |
| `route` | string | Yes | HTTP route declaration, format `METHOD PATH` |
| `timeout` | string | No | Execution timeout, default `5s` |
| `exec` | map[string]ExecDef | Yes | OS-forked command definitions, key is `darwin`/`linux`/`windows` |
| `install` | map[string]InstallDef | No | OS-forked installation definitions |

#### ExecDef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | []string | Yes | Execution command, first element is the executable |
| `parse` | string | Yes | stdout parse strategy name |
| `fallback` | ExecDef | No | Fallback command (tried when primary command fails) |

#### InstallDef (toolExec level)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `check` | []string | No | Check command, exit code 0 means already installed |
| `command` | []string | No | Install command |
| `timeout` | string | No | Install timeout, default `60s` |

#### Template Variables

Strings in `command` and `check` arrays support the following template variables:

| Variable | Replaced with | Example |
|----------|---------------|---------|
| `{pluginDir}` | Plugin installation directory absolute path | `~/.axons/plugins/com.axons.clipboard-bridge` |
| `{dataDir}` | Plugin data directory absolute path | `~/.axons/plugins/data/com.axons.clipboard-bridge` |

### 3.3 Parse Strategies

The daemon has the following built-in stdout parse strategies:

| Strategy | Purpose | Input Example | Parse Logic |
|----------|---------|---------------|-------------|
| `line-list` | Line-by-line text, one path per line | `/foo/bar.ts\n/baz/qux.go\n` | Split by newline, remove empty lines, trim whitespace |
| `uri-list` | URI list (text/uri-list format) | `file:///foo/bar.ts\r\nfile:///baz/qux.go\r\n` | Split by `\r\n` or `\n`, strip `file://` prefix, URL decode |
| `osascript-file-url` | macOS osascript output | `file:///Users/x/foo.ts\nfile:///Users/x/bar.go\n` | Same as `uri-list`, with additional handling for `«class furl»` POSIX path encoding |
| `json` | JSON output | `{"files":["/a.ts","/b.go"]}` | JSON unmarshal, extract top-level `result` field |
| `raw` | Raw text, no parsing | Any | Return stdout directly as `result` string |

**All strategies produce unified output**:

```json
{
  "result": ["path1", "path2"]
}
```

`line-list` / `uri-list` / `osascript-file-url` return a path array. `json` returns the parsed array. `raw` returns `["entire stdout text"]`.

### 3.4 Relationship with Existing Manifest Fields

| Scenario | backend | frontend | toolExec | Plugin Type |
|----------|---------|----------|----------|-------------|
| Has backend process + has panel | ✓ | ✓ | — | Existing: full-featured plugin |
| No backend + has panel | null | ✓ | — | Existing: frontend-only plugin |
| Has backend + no panel | ✓ | null | — | Existing: backend-only plugin |
| **No backend + no panel + has tool commands** | null | null | ✓ | **New: tool-exec plugin** |
| Hybrid: has backend + has panel + has tool commands | ✓ | ✓ | ✓ | Optional: full-featured + tool commands |

A **tool-exec plugin** is a new plugin type: no process, no panel, only declares command templates, executed by the daemon on the plugin's behalf.

### 3.5 Validation Rules

Add to `ValidateManifest`:

```
toolExec validation:
  - id is required, must be unique within the plugin
  - route is required, format must be "METHOD PATH" (METHOD ∈ {GET,POST,PUT,DELETE})
  - exec is required, must include an entry for the current OS
  - exec.{os}.command is required, at least one element
  - exec.{os}.parse is required, must be one of the built-in strategy names
  - When install.{os}.command exists, check should also exist (otherwise it reinstalls every startup)
```

When both `backend` and `frontend` are null, `toolExec` must be non-empty (otherwise the plugin contributes nothing).

## 4. Host-side (daemon) Implementation

### 4.1 Directory Structure

```
internal/plugin/
├── manager.go           # New: toolExec start/stop logic
├── manifest.go          # New: ToolExecDef struct + validation
├── exec_engine.go       # New: ExecEngine — command execution engine
├── route_mapper.go      # New: RouteMapper — virtual endpoint registration/unregistration
├── parse_strategy.go    # New: parse strategy implementation
├── handlers.go          # Existing: unchanged
├── registry.go          # Existing: unchanged
└── ...
```

### 4.2 ExecEngine

```go
// internal/plugin/exec_engine.go

// ExecResult is the unified output of a command execution.
type ExecResult struct {
    Result []string `json:"result"`
    Error  string   `json:"error,omitempty"`
}

// ExecEngine executes tool commands declared in plugin manifests.
type ExecEngine struct {
    mu       sync.RWMutex
    commands map[string]map[string]*resolvedCommand  // pluginId → commandId → resolved
}

// resolvedCommand is a manifest ToolExecDef resolved for the current OS.
type resolvedCommand struct {
    Command  []string
    Parse    string
    Fallback *resolvedCommand
    Timeout  time.Duration
}

// ExecCommand runs a tool command for the given plugin and command ID.
func (e *ExecEngine) ExecCommand(pluginId, commandId string) (*ExecResult, error) {
    // 1. Lookup resolved command
    // 2. Execute via exec.Command
    // 3. If exit code != 0 and fallback exists → execute fallback
    // 4. Parse stdout using parse strategy
    // 5. Return ExecResult
}
```

Core execution logic:

```go
func (e *ExecEngine) execResolved(rc *resolvedCommand) (*ExecResult, error) {
    ctx, cancel := context.WithTimeout(context.Background(), rc.Timeout)
    defer cancel()

    cmd := exec.CommandContext(ctx, rc.Command[0], rc.Command[1:]...)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    if err := cmd.Run(); err != nil {
        // Command failed, try fallback
        if rc.Fallback != nil {
            return e.execResolved(rc.Fallback)
        }
        return nil, fmt.Errorf("exec failed: %w\nstderr: %s", err, stderr.String())
    }

    // Parse stdout
    parsed, err := ParseOutput(rc.Parse, stdout.String())
    if err != nil {
        return nil, fmt.Errorf("parse failed: %w", err)
    }

    return &ExecResult{Result: parsed}, nil
}
```

### 4.3 Parse Strategy

```go
// internal/plugin/parse_strategy.go

var strategies = map[string]ParseFunc{
    "line-list":           parseLineList,
    "uri-list":            parseURIList,
    "osascript-file-url":  parseOsascriptFileURL,
    "json":                parseJSON,
    "raw":                 parseRaw,
}

type ParseFunc func(stdout string) ([]string, error)

func ParseOutput(strategy string, stdout string) ([]string, error) {
    fn, ok := strategies[strategy]
    if !ok {
        return nil, fmt.Errorf("unknown parse strategy: %s", strategy)
    }
    return fn(stdout)
}

func parseLineList(stdout string) ([]string, error) {
    var result []string
    for _, line := range strings.Split(stdout, "\n") {
        line = strings.TrimSpace(line)
        if line != "" {
            result = append(result, line)
        }
    }
    return result, nil
}

func parseURIList(stdout string) ([]string, error) {
    var result []string
    for _, line := range strings.Split(stdout, "\n") {
        line = strings.TrimRight(line, "\r")
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        if strings.HasPrefix(line, "file:///") {
            u, err := url.Parse(line)
            if err != nil {
                continue
            }
            result = append(result, u.Path)  // URL decode happens automatically
        } else {
            result = append(result, line)
        }
    }
    return result, nil
}

func parseOsascriptFileURL(stdout string) ([]string, error) {
    // macOS «class furl» output format:
    //   file:///Users/x/foo.ts
    //   file:///Users/x/bar.go
    // Reuse uri-list parsing
    return parseURIList(stdout)
}

func parseJSON(stdout string) ([]string, error) {
    var data struct {
        Result []string `json:"result"`
    }
    if err := json.Unmarshal([]byte(stdout), &data); err != nil {
        return nil, err
    }
    return data.Result, nil
}

func parseRaw(stdout string) ([]string, error) {
    return []string{stdout}, nil
}
```

### 4.4 RouteMapper

RouteMapper is responsible for dynamically registering/unregistering virtual endpoints on the daemon HTTP router.

```go
// internal/plugin/route_mapper.go

// RouteMapper maps plugin toolExec routes to ExecEngine invocations.
type RouteMapper struct {
    mu     sync.RWMutex
    engine *ExecEngine
    routes map[string]routeEntry  // "GET /api/clipboard/files" → entry
}

type routeEntry struct {
    PluginID  string
    CommandID string
}

// RegisterAll scans all started plugins and registers their toolExec routes.
// Called after plugin startup.
func (rm *RouteMapper) RegisterAll(manager *Manager, router *httprouter.Router) {
    rm.mu.Lock()
    defer rm.mu.Unlock()

    for _, inst := range manager.GetAllInstances() {
        manifest := inst.Manifest
        if manifest.ToolExec == nil {
            continue
        }
        for _, te := range manifest.ToolExec {
            method, path := parseRoute(te.Route)
            key := method + " " + path
            rm.routes[key] = routeEntry{
                PluginID:  manifest.ID,
                CommandID: te.ID,
            }
            // Register handler — all virtual endpoints point to the same dispatch handler
            router.Handle(method, path, rm.handleToolExec)
        }
    }
}

// handleToolExec is the unified handler for all virtual tool-exec routes.
func (rm *RouteMapper) handleToolExec(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
    key := r.Method + " " + r.URL.Path
    rm.mu.RLock()
    entry, ok := rm.routes[key]
    rm.mu.RUnlock()

    if !ok {
        http.NotFound(w, r)
        return
    }

    result, err := rm.engine.ExecCommand(entry.PluginID, entry.CommandID)
    if err != nil {
        // Tool not installed etc. → return empty result instead of error
        json.NewEncoder(w).Encode(map[string]any{"result": []string{}})
        return
    }

    json.NewEncoder(w).Encode(result)
}
```

**Route conflict handling**: If multiple plugins declare the same route, the first one registered takes effect (consistent with PanelDef ID conflict strategy).

### 4.5 ToolInstaller

During plugin startup, check if `toolExec[].install` needs to be executed:

```go
// In StartPlugin flow, when backend == null && toolExec != null:

func (m *Manager) startToolExecPlugin(inst *PluginInstance) {
    manifest := inst.Manifest

    for _, te := range manifest.ToolExec {
        osKey := runtime.GOOS
        installDef, ok := te.Install[osKey]
        if !ok || installDef == nil || len(installDef.Command) == 0 {
            continue  // No installation needed for current OS
        }

        // check command: exit code 0 means already installed
        if len(installDef.Check) > 0 {
            checkCmd := resolveTemplateVars(installDef.Check, manifest.Dir)
            cmd := exec.Command(checkCmd[0], checkCmd[1:]...)
            if cmd.Run() == nil {
                continue  // Already installed, skip
            }
        }

        // Execute installation
        installCmd := resolveTemplateVars(installDef.Command, manifest.Dir)
        // Reuse existing InstallPlugin execution logic (stdout/stderr streaming)
        m.runToolInstall(inst.Manifest.ID, installCmd, installDef.Timeout)
    }
}
```

### 4.6 Startup Flow Adjustment

```go
// Add toolExec branch in StartPlugin:

func (m *Manager) StartPlugin(pluginID string) error {
    // ... existing logic ...

    if manifest.HasBackend() {
        // Existing flow: start backend process
        endpoint, err := m.startProcess(inst)
        // ...
    } else if len(manifest.ToolExec) > 0 {
        // New flow: tool-exec plugin
        // 1. Check and install tools
        m.startToolExecPlugin(inst)
        // 2. Resolve command templates (for current OS)
        m.execEngine.RegisterCommands(pluginID, manifest.ToolExec)
        // 3. Register virtual routes
        m.routeMapper.Register(pluginID, manifest.ToolExec, s.router)
        // 4. Mark as running
        inst.Status = StatusRunning
    } else {
        // Frontend-only plugin — existing flow
        inst.Status = StatusRunning
    }

    // ... register frontend contributions, update installed.json ...
}
```

### 4.7 Shutdown Flow Adjustment

```go
func (m *Manager) StopPlugin(pluginID string) error {
    inst, ok := m.instances[pluginID]
    // ...

    // Unregister toolExec routes
    if len(inst.Manifest.ToolExec) > 0 {
        m.execEngine.UnregisterCommands(pluginID)
        m.routeMapper.Unregister(pluginID, s.router)
    }

    // ... existing shutdown logic ...
}
```

### 4.8 manifest.go Changes

```go
// Add field to PluginManifest
type PluginManifest struct {
    // ... existing fields ...
    ToolExec []ToolExecDef `json:"toolExec,omitempty"`
}

// ToolExecDef defines a tool execution command contributed by a plugin.
type ToolExecDef struct {
    ID          string                        `json:"id"`
    Description string                        `json:"description"`
    Route       string                        `json:"route"`
    Timeout     string                        `json:"timeout,omitempty"`
    Exec        map[string]*ExecDef           `json:"exec"`
    Install     map[string]*ToolInstallDef    `json:"install,omitempty"`
}

// ExecDef defines a command to execute and how to parse its output.
type ExecDef struct {
    Command  []string  `json:"command"`
    Parse    string    `json:"parse"`
    Fallback *ExecDef  `json:"fallback,omitempty"`
}

// ToolInstallDef defines how to install a tool for a specific OS.
type ToolInstallDef struct {
    Check   []string `json:"check,omitempty"`
    Command []string `json:"command,omitempty"`
    Timeout string   `json:"timeout,omitempty"`
}
```

### 4.9 Permission Additions

```go
// Add to ValidPermissions
"clipboard:read": true,
"tool:exec":      true,  // Generic tool execution permission
"file:trash":     true,  // Move files to system trash / restore
```

## 5. Plugin-side Implementation

### 5.1 clipboard-bridge Plugin Directory Structure

```
com.axons.clipboard-bridge/
├── manifest.json       ← declare command templates
├── install.sh          ← Linux: install xclip/xsel
└── uninstall.sh        ← cleanup
```

### 5.2 manifest.json

```json
{
  "id": "com.axons.clipboard-bridge",
  "name": "Clipboard Bridge",
  "version": "1.0.0",
  "description": "Bridge system clipboard file references into axons (Finder/Explorer/Dolphin)",
  "author": "axons-community",
  "category": "productivity",
  "minAxonsVersion": "0.8.0",
  "permissions": ["clipboard:read"],
  "backend": null,
  "frontend": null,
  "toolExec": [
    {
      "id": "clipboard.read-files",
      "description": "Read file paths from system clipboard",
      "route": "GET /api/clipboard/files",
      "timeout": "3s",
      "exec": {
        "darwin": {
          "command": ["osascript", "-e", "tell application \"Finder\" to get the clipboard as «class furl»"],
          "parse": "osascript-file-url"
        },
        "linux": {
          "command": ["xclip", "-selection", "clipboard", "-t", "text/uri-list", "-o"],
          "fallback": {
            "command": ["xsel", "--clipboard", "--output"],
            "parse": "uri-list"
          },
          "parse": "uri-list"
        },
        "windows": {
          "command": ["powershell", "-Command", "Get-Clipboard -Format FileDropList | ForEach-Object { $_.FullName }"],
          "parse": "line-list"
        }
      },
      "install": {
        "linux": {
          "check": ["which", "xclip"],
          "command": ["bash", "{pluginDir}/install.sh"],
          "timeout": "60s"
        }
      }
    }
  ],
  "activationEvents": ["onStartup"]
}
```

### 5.3 install.sh (Linux Installation Script)

```bash
#!/bin/bash
# install.sh — clipboard-bridge Linux dependency installation
set -e

echo "[clipboard-bridge] Installing clipboard tools for Linux..."

# Detect package manager and install xclip
if command -v apt-get &>/dev/null; then
    echo "Detected apt (Debian/Ubuntu)..."
    sudo apt-get update -qq
    sudo apt-get install -y -qq xclip
elif command -v dnf &>/dev/null; then
    echo "Detected dnf (Fedora)..."
    sudo dnf install -y xclip
elif command -v pacman &>/dev/null; then
    echo "Detected pacman (Arch)..."
    sudo pacman -S --noconfirm xclip
elif command -v zypper &>/dev/null; then
    echo "Detected zypper (openSUSE)..."
    sudo zypper install -y xclip
else
    echo "WARN: Unsupported package manager. Please install xclip manually."
    echo "  Ubuntu/Debian: sudo apt install xclip"
    echo "  Fedora: sudo dnf install xclip"
    echo "  Arch: sudo pacman -S xclip"
    exit 1
fi

# Verify installation
if command -v xclip &>/dev/null; then
    echo "[clipboard-bridge] xclip installed successfully."
else
    echo "[clipboard-bridge] WARN: xclip not found after install."
    exit 1
fi
```

### 5.4 uninstall.sh (Linux Cleanup Script)

```bash
#!/bin/bash
# uninstall.sh — clipboard-bridge cleanup
# Note: does not uninstall xclip — it may be a system tool depended on by other programs
echo "[clipboard-bridge] Uninstalled. (xclip left installed as system tool)"
```

### 5.5 Behavior When Not Installed

When the plugin is not installed, the frontend `readSystemClipboardFiles()` behavior:

```typescript
async function readSystemClipboardFiles(): Promise<string[]> {
  try {
    const res = await fetch('/api/clipboard/files');
    if (!res.ok) return [];  // 404 → plugin not installed → empty array
    const data = await res.json();
    return data.result ?? [];  // Note: field changed from files to result (unified parse output)
  } catch {
    return [];
  }
}
```

| Scenario | Frontend Behavior |
|----------|-------------------|
| Plugin not installed | `/api/clipboard/files` returns 404 → `[]` → internal copy/paste only |
| Plugin installed, tool available | Returns file path array → external copy/paste works |
| Plugin installed, tool unavailable (Linux without xclip) | exec fails → returns `{"result":[]}` → graceful degradation |

## 6. Frontend Changes

### 6.1 FileTreePanel.tsx

Change the return field of `readSystemClipboardFiles()` from `data.files` to `data.result`:

```typescript
async function readSystemClipboardFiles(): Promise<string[]> {
  try {
    const res = await fetch('/api/clipboard/files');
    if (!res.ok) return [];
    const data = await res.json();
    return data.result ?? [];  // Unified to result (ExecEngine output format)
  } catch {
    return [];
  }
}
```

No other code changes — `handlePaste` Priority 1 logic is fully reused.

## 7. Future Extension Examples

Once exec becomes a generic capability, similar scenarios only need manifest.json + scripts:

### 7.1 git-bridge (Read Git Repository Info)

```jsonc
{
  "id": "com.axons.git-bridge",
  "toolExec": [
    {
      "id": "git.current-branch",
      "description": "Get current git branch name",
      "route": "GET /api/git/branch",
      "timeout": "2s",
      "exec": {
        "darwin":  { "command": ["git", "rev-parse", "--abbrev-ref", "HEAD"], "parse": "raw" },
        "linux":   { "command": ["git", "rev-parse", "--abbrev-ref", "HEAD"], "parse": "raw" },
        "windows": { "command": ["git", "rev-parse", "--abbrev-ref", "HEAD"], "parse": "raw" }
      }
    }
  ]
}
```

### 7.2 shell-bridge (Execute System Commands)

```jsonc
{
  "id": "com.axons.shell-bridge",
  "toolExec": [
    {
      "id": "shell.exec",
      "description": "Execute a shell command and return output",
      "route": "POST /api/shell/exec",
      "timeout": "10s",
      "exec": {
        "darwin":  { "command": ["bash", "-c", "{cmd}"], "parse": "raw" },
        "linux":   { "command": ["bash", "-c", "{cmd}"], "parse": "raw" },
        "windows": { "command": ["cmd", "/c", "{cmd}"], "parse": "raw" }
      }
    }
  ]
}
```

> Note: `{cmd}` and other request-body parameter injection requires the daemon to perform parameter substitution before exec, which is a phase-2 enhancement.

### 7.3 trash-bridge (System Trash / Recycle Bin)

Move files to the OS-native trash instead of permanent deletion. Used by FileTree Undo/Redo (see `filetree-undo-redo-design.md` Phase 2).

```jsonc
{
  "id": "com.axons.trash-bridge",
  "name": "Trash Bridge",
  "version": "1.0.0",
  "description": "Bridge OS trash/recycle bin into axons — move files to system trash and restore them",
  "author": "axons-community",
  "category": "productivity",
  "minAxonsVersion": "0.8.0",
  "permissions": ["file:trash"],
  "backend": null,
  "frontend": null,
  "toolExec": [
    {
      "id": "trash.move",
      "description": "Move a file or folder to system trash",
      "route": "POST /api/filetree/trash",
      "timeout": "5s",
      "exec": {
        "darwin": {
          "command": ["osascript", "-e", "tell application \"Finder\" to delete POSIX file \"{path}\""],
          "parse": "raw"
        },
        "linux": {
          "command": ["gio", "trash", "{path}"],
          "fallback": {
            "command": ["trash-put", "{path}"],
            "parse": "raw"
          },
          "parse": "raw"
        },
        "windows": {
          "command": ["powershell", "-Command", "(New-Object -ComObject Shell.Application).Namespace(10).MoveHere(\"{path}\")"],
          "parse": "raw"
        }
      },
      "install": {
        "linux": {
          "check": ["which", "gio"],
          "command": ["bash", "{pluginDir}/install.sh"],
          "timeout": "60s"
        }
      }
    },
    {
      "id": "trash.restore",
      "description": "Restore a file or folder from system trash to its original path",
      "route": "POST /api/filetree/trash/restore",
      "timeout": "5s",
      "exec": {
        "darwin": {
          "command": ["osascript", "-e", "tell application \"Finder\" to move POSIX file \"{path}\" from trash to POSIX file \"{dest}\""],
          "parse": "raw"
        },
        "linux": {
          "command": ["trash-restore", "{path}"],
          "parse": "raw"
        },
        "windows": {
          "command": ["powershell", "-Command", "(New-Object -ComObject Shell.Application).Namespace(10).Items().Item(\"{name}\").InvokeVerb()"],
          "parse": "raw"
        }
      }
    }
  ],
  "activationEvents": ["onStartup"]
}
```

**Frontend integration** (in FileTreePanel):

```typescript
/** Check if trash-bridge plugin is available */
async function isTrashBridgeAvailable(): Promise<boolean> {
  try {
    const res = await fetch('/api/filetree/trash', { method: 'OPTIONS' });
    return res.ok;
  } catch {
    return false;
  }
}

/** Delete via system trash (undo-able) or permanent delete (not undo-able) */
const handleDeleteConfirm = async () => {
  const trashAvailable = await isTrashBridgeAvailable();
  if (trashAvailable) {
    // Move to system trash → can undo
    const undoOps: FileOperation[] = [];
    try {
      for (const target of deleteTargets) {
        const { trash_id } = await trashEntry(target.path, projectId);
        removeEntryFromState(target.path);
        undoOps.push({ type: 'delete', path: target.path, isDir: target.is_dir, trashId: trash_id });
      }
      executeAndTrack({ type: 'compound', ops: undoOps });
    } catch {
      // Trash failed → permanent delete, not undo-able
      await permanentDelete(deleteTargets);
    }
  } else {
    // No trash plugin → permanent delete, not undo-able
    await permanentDelete(deleteTargets);
  }
};
```

**Behavior matrix**:

| Scenario | Frontend Behavior |
|----------|-------------------|
| Plugin not installed | `/api/filetree/trash` returns 404 → permanent delete, not undo-able |
| Plugin installed, trash succeeds | File moved to system trash → entry in undo stack |
| Plugin installed, trash fails (Docker/SSH) | Fallback to permanent delete, not undo-able |
| Undo delete, file still in trash | Restore from system trash |
| Undo delete, file already emptied from trash | Restore fails → error toast, stack pop/push |

## 8. Implementation Steps

| Step | Content | Module |
|------|---------|--------|
| 1 | Add `ToolExecDef` / `ExecDef` / `ToolInstallDef` structs + validation to `manifest.go` | `internal/plugin/manifest.go` |
| 2 | Implement `parse_strategy.go` — 5 built-in parse strategies | `internal/plugin/parse_strategy.go` |
| 3 | Implement `exec_engine.go` — command execution + fallback chain | `internal/plugin/exec_engine.go` |
| 4 | Implement `route_mapper.go` — virtual endpoint registration/unregistration | `internal/plugin/route_mapper.go` |
| 5 | Add toolExec branch to `manager.go` StartPlugin/StopPlugin | `internal/plugin/manager.go` |
| 6 | Change frontend `readSystemClipboardFiles()` return field to `data.result` | `ui/src/components/FileTreePanel.tsx` |
| 7 | Develop clipboard-bridge plugin (manifest.json + install.sh) | Separate directory |
| 8 | Package and test | — |

## 9. Risk Assessment

| Risk | Mitigation |
|------|------------|
| Command injection (malicious plugin declares dangerous commands) | Permission declaration `tool:exec`, phase-1 warn log, phase-2 runtime interception |
| Route conflict (two plugins declare same route) | First-registered wins + warn log |
| exec blocking (command doesn't return for a long time) | timeout field, default 5s, context.WithTimeout forced cancellation |
| install.sh requires sudo | Prompt user to install manually; install script exit 1 does not block plugin startup |
| httprouter doesn't support dynamic route unregistration | Use ServeHTTP interception + internal map dispatch, bypass httprouter limitation |
```
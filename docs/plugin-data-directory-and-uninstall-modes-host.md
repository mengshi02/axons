# Plugin Data Directory Separation & Uninstall Modes Design

## 1 Background and Problem

### 1.1 Current State

Currently, plugin code and plugin data are mixed in the same directory:

```
~/.axons/plugins/
├── chat.axons.huggingface/          ← Plugin code + data mixed
│   ├── manifest.json                ← Code
│   ├── server.py                    ← Code
│   ├── .venv/                       ← Code (installation artifact)
│   ├── ui/                          ← Code
│   ├── models/                      ← Data (downloaded GGUF models)
│   ├── bin/                         ← Data (llama-server binary)
│   └── models.json                  ← Data (model metadata)
├── com.axons.locale-zh-cn/          ← Pure code plugin, no runtime data
└── installed.json                   ← Host registry
```

The host-side [`UninstallPlugin()`](internal/plugin/manager.go:797) executes `os.RemoveAll(p.Dir)` to delete the entire directory.

### 1.2 Problem

The plugin upgrade flow is "uninstall first, then import the new version." During uninstall, `os.RemoveAll` deletes already-downloaded model files (potentially several GB) along with everything else, requiring a re-download after upgrade — a very costly operation.

### 1.3 Goals

1. **Separate plugin code from data**: Code can be deleted on uninstall, while data is preserved in upgrade scenarios
2. **Distinguish uninstall modes**: Support "code-only uninstall" (upgrade scenario) and "full uninstall" (no longer using the plugin)
3. **Host UI adaptation**: The uninstall confirmation dialog dynamically renders checkboxes based on manifest declarations, letting users choose uninstall options
4. **Backward compatibility**: New and old versions of host and plugins can be used in combination

---

## 2 Directory Specification

### 2.1 New Directory Structure

```
~/.axons/plugins/
├── chat.axons.huggingface/          ← Plugin code directory (always deleted on uninstall)
│   ├── manifest.json
│   ├── server.py
│   ├── .venv/
│   ├── ui/
│   ├── install.sh
│   └── uninstall.sh
├── data/                            ← Plugin data directory (only deleted by plugin script on full uninstall)
│   └── chat.axons.huggingface/
│       ├── models/                  ← Downloaded GGUF models
│       ├── bin/                     ← llama-server binary
│       └── models.json              ← Model metadata
├── com.axons.locale-zh-cn/          ← Plugin without runtime data, no data subdirectory
└── installed.json
```

### 2.2 Directory Responsibilities

| Path | Purpose | Content Examples | Uninstall Strategy |
|------|---------|------------------|-------------------|
| `~/.axons/plugins/{id}/` | Plugin code | manifest.json, server.py, .venv, ui/ | Always deleted (host responsible) |
| `~/.axons/plugins/data/{id}/` | Plugin runtime data | models/, bin/, models.json | Only deleted on full uninstall (plugin uninstall script responsible) |

### 2.3 Data Directory Discovery Mechanism

The host injects the environment variable `AXONS_PLUGIN_DATA_DIR` when starting the plugin process. The plugin reads this variable to obtain the data directory path.

```
AXONS_PLUGIN_DATA_DIR=~/.axons/plugins/data/{pluginId}
```

If the environment variable is not present (old host version), the plugin falls back to `~/.axons/plugins/data/{pluginId}` as the default value.

---

## 3 Host-Side Changes

> Repository: `/Users/mengshi3/go/src/github.com/mengshi02/axons`

### 3.1 Manifest Structure Extension

**File**: `internal/plugin/manifest.go`

`UninstallDef` adds an `Args` field to declaratively describe the parameters supported by the uninstall script:

```go
// UninstallDef defines the uninstall script configuration.
type UninstallDef struct {
    Command []string       `json:"command"`
    Args    []UninstallArg `json:"args,omitempty"`
}

// UninstallArg describes a parameter accepted by the uninstall script.
type UninstallArg struct {
    Name        string `json:"name"`                  // Parameter name, e.g. "purge_data"
    Type        string `json:"type"`                  // Type: "boolean"
    Default     bool   `json:"default,omitempty"`     // Default value
    Description string `json:"description,omitempty"` // Parameter description (frontend checkbox label)
}
```

`Name` → command-line argument mapping rule: `snake_case` → `--kebab-case` (e.g. `purge_data` → `--purge-data`).

`ValidateManifest()` adds validation for `Args`:

```go
if m.Backend.Uninstall != nil && len(m.Backend.Uninstall.Args) > 0 {
    seen := make(map[string]bool)
    for _, arg := range m.Backend.Uninstall.Args {
        if arg.Name == "" {
            return fmt.Errorf("manifest: backend.uninstall.args[].name is required")
        }
        if arg.Type != "boolean" {
            return fmt.Errorf("manifest: backend.uninstall.args[].type must be \"boolean\", got %q", arg.Type)
        }
        if seen[arg.Name] {
            return fmt.Errorf("manifest: backend.uninstall.args[].name %q is duplicated", arg.Name)
        }
        seen[arg.Name] = true
    }
}
```

### 3.2 Manager Refactoring

**File**: `internal/plugin/manager.go`

#### 3.2.1 New PluginDataDir Method

```go
// PluginDataDir returns the data directory for a plugin.
// Path: ~/.axons/plugins/data/{pluginId}
func (m *Manager) PluginDataDir(pluginID string) string {
    return filepath.Join(m.pluginsDir, "data", pluginID)
}
```

#### 3.2.2 Inject Data Directory Environment Variable on Startup

**File**: `internal/plugin/manager.go` → `startProcess()` function (around line 352)

Add one line at the existing environment variable injection point:

```go
cmd.Env = append(os.Environ(),
    fmt.Sprintf("AXONS_API_URL=http://127.0.0.1:%d", m.axonsPort),
    fmt.Sprintf("AXONS_PLUGIN_PORT=%d", port),
    fmt.Sprintf("AXONS_PLUGIN_TOKEN=%s", inst.Token),
    fmt.Sprintf("AXONS_PLUGIN_ID=%s", manifest.ID),
    fmt.Sprintf("AXONS_PLUGIN_DATA_DIR=%s", m.PluginDataDir(manifest.ID)),  // New
)
```

The install script and uninstall script also need this environment variable injected when executed:

- In `InstallPlugin()`, when executing the install script, `cmd.Env` needs to be added (currently missing):

```go
cmd.Env = append(os.Environ(),
    fmt.Sprintf("AXONS_PLUGIN_DATA_DIR=%s", m.PluginDataDir(pluginID)),
)
```

- In `UninstallPlugin()`, when executing the uninstall script, append `AXONS_PLUGIN_DATA_DIR` (see 3.2.3)

#### 3.2.3 Modify UninstallPlugin Signature

**File**: `internal/plugin/manager.go` (around line 797)

Core design: **The host is only responsible for decision-making and parameter passing; it does not directly delete the data directory.** Data directory cleanup is entirely the responsibility of the plugin uninstall script — the plugin knows best what data it has produced and how to safely clean it up.

```go
// UninstallPlugin removes a plugin.
// argValues maps arg names (e.g. "purge_data") to user-chosen values,
// derived from the manifest's UninstallDef.Args declarations.
func (m *Manager) UninstallPlugin(pluginID string, argValues map[string]bool) error {
    // 1. Stop running plugin instance
    if _, ok := m.instances[pluginID]; ok {
        if err := m.StopPlugin(pluginID); err != nil {
            fmt.Printf("[plugin-manager] WARN: failed to stop plugin %s during uninstall: %v\n", pluginID, err)
        }
    }

    // 2. Load manifest
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

    // 3. Execute uninstall script, dynamically building command-line arguments based on declarative Args
    for _, p := range plugins {
        if p.ID == pluginID && p.Backend != nil && p.Backend.Uninstall != nil {
            args := make([]string, len(p.Backend.Uninstall.Command))
            copy(args, p.Backend.Uninstall.Command)

            // Iterate over manifest-declared args, convert user-chosen values to command-line flags
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

    // 4. Localization plugin cleanup
    if manifest != nil && manifest.Category == "localization" {
        m.unloadSingleLocalePlugin(pluginID, manifest)
    }

    // 5. Remove from runtime registry
    m.mu.Lock()
    delete(m.instances, pluginID)
    m.mu.Unlock()
    m.registry.UnregisterPlugin(pluginID)

    // 6. Delete plugin code directory (always executed)
    for _, p := range plugins {
        if p.ID == pluginID {
            os.RemoveAll(p.Dir)
            break
        }
    }

    // Note: Data directory deletion is the responsibility of the plugin uninstall script (step 3).
    // The host does not directly operate on the data directory to avoid competing with the script.

    // 7. Update installed.json
    m.removeInstalledPlugin(pluginID)

    // 8. Emit event
    m.emitEvent("plugin.uninstalled", map[string]interface{}{
        "pluginId":  pluginID,
        "argValues": argValues,
    })

    fmt.Printf("[plugin-manager] Plugin %s uninstalled (argValues=%v)\n", pluginID, argValues)
    return nil
}
```

### 3.3 API Layer Changes

**File**: `internal/plugin/handlers.go` (around line 316)

`DELETE /v1/plugins/:id` uses the `args.{name}` format to pass declarative parameters:

```
DELETE /v1/plugins/{id}?args.purge_data=true    ← Full uninstall
DELETE /v1/plugins/{id}                          ← Code-only uninstall
```

```go
func (m *Manager) handleUninstallPlugin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
    pluginID := ps.ByName("id")

    // Extract args.{name} format parameters from query params
    argValues := make(map[string]bool)
    for key, values := range r.URL.Query() {
        if strings.HasPrefix(key, "args.") {
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
```

### 3.4 Frontend API Layer Changes

**File**: `ui/src/services/api.ts` (around line 920)

```typescript
/** Uninstall a plugin */
export async function uninstallPlugin(pluginId: string, argValues: Record<string, boolean> = {}): Promise<void> {
    const params = new URLSearchParams();
    for (const [name, value] of Object.entries(argValues)) {
        if (value) {
            params.set(`args.${name}`, 'true');
        }
    }
    const qs = params.toString();
    const url = `${getBaseURL()}/v1/plugins/${pluginId}${qs ? '?' + qs : ''}`;
    const response = await fetch(url, { method: 'DELETE' });
    if (!response.ok) {
        const err = await response.json().catch(() => ({}));
        throw new Error(err.message || 'Failed to uninstall plugin');
    }
}
```

### 3.5 ConfirmDialog Component Extension

**File**: `ui/src/components/ConfirmDialog.tsx`

Add an optional `checkboxes` property to support dynamically rendering multiple checkboxes (driven by the manifest's `UninstallDef.Args` declarations):

```typescript
interface ConfirmDialogCheckbox {
    id: string;                          // Corresponds to arg name
    label: string;                       // From arg's description
    defaultChecked?: boolean;            // From arg's default
}

interface ConfirmDialogProps {
    isOpen: boolean;
    title: string;
    message: string;
    confirmLabel?: string;
    cancelLabel?: string;
    variant?: 'danger' | 'warning' | 'default';
    checkboxes?: ConfirmDialogCheckbox[];                            // New
    onConfirm: (checkboxValues?: Record<string, boolean>) => void;   // Signature change
    onCancel: () => void;
}
```

Key implementation details inside the component:

```tsx
export function ConfirmDialog({
    isOpen, title, message, confirmLabel, cancelLabel,
    variant = 'danger', checkboxes, onConfirm, onCancel,
}: ConfirmDialogProps) {
    const [values, setValues] = useState<Record<string, boolean>>(() => {
        if (!checkboxes) return {};
        const init: Record<string, boolean> = {};
        for (const cb of checkboxes) {
            init[cb.id] = cb.defaultChecked ?? false;
        }
        return init;
    });

    // ... existing focus/keydown logic unchanged ...

    return (
        <div className="fixed inset-0 ..." onClick={onCancel}>
            <div className="bg-surface ..." onClick={e => e.stopPropagation()}>
                <div className="px-6 py-5">
                    <div className="flex items-start gap-3">
                        {/* Existing icon unchanged */}
                        <div className="flex-1 min-w-0">
                            <h3 className="...">{title}</h3>
                            <p className="...">{message}</p>
                        </div>
                    </div>
                    {/* Dynamically render checkbox list */}
                    {checkboxes && checkboxes.length > 0 && (
                        <div className="mt-3 space-y-2">
                            {checkboxes.map(cb => (
                                <label key={cb.id} className="flex items-center gap-2 text-sm text-text-secondary cursor-pointer select-none">
                                    <input
                                        type="checkbox"
                                        id={cb.id}
                                        checked={values[cb.id] ?? false}
                                        onChange={e => setValues(prev => ({ ...prev, [cb.id]: e.target.checked }))}
                                        className="rounded border-border-subtle"
                                    />
                                    {cb.label}
                                </label>
                            ))}
                        </div>
                    )}
                </div>
                <div className="flex items-center justify-end gap-2 ...">
                    <button onClick={onCancel} className="...">{_cancelLabel}</button>
                    <button onClick={() => onConfirm(checkboxes ? values : undefined)} className="...">{_confirmLabel}</button>
                </div>
            </div>
        </div>
    );
}
```

When `checkboxes` is `undefined` (plugins without `Args` declarations), the dialog does not display any checkboxes, and the behavior is consistent with the existing ConfirmDialog.

### 3.6 ExtensionsPanel Uninstall Confirmation Dialog Refactoring

**File**: `ui/src/components/ExtensionsPanel.tsx`

#### 3.6.1 handleUninstall Function Signature Change

```typescript
const handleUninstall = async (id: string, argValues: Record<string, boolean> = {}) => {
    setActionLoading(id);
    setConfirmDeleteId(null);
    try {
        await uninstallPlugin(id, argValues);
        refresh();
    } catch (err) {
        console.error('Failed to uninstall plugin:', err);
    } finally {
        setActionLoading(null);
    }
};
```

#### 3.6.2 ConfirmDialog Call Refactoring

Dynamically render checkboxes from the manifest's `backend.uninstall.args`. Plugins without `args` declarations do not display any checkboxes.

> Prerequisite: The plugin list API (`/v1/plugins`) response needs to include `backend.uninstall.args` information.

```tsx
<ConfirmDialog
    isOpen={confirmDeleteId !== null}
    title={t('extensions:uninstallTitle')}
    message={t('extensions:uninstallMessage')}
    confirmLabel={t('extensions:uninstallConfirm')}
    variant="danger"
    checkboxes={confirmDeleteId !== null
        ? getUninstallArgs(confirmDeleteId)
            ?.filter(a => a.type === 'boolean')
            .map(a => ({
                id: a.name,
                label: a.description,
                defaultChecked: a.default,
            }))
        : undefined
    }
    onConfirm={(argValues) => {
        if (confirmDeleteId !== null) handleUninstall(confirmDeleteId, argValues);
    }}
    onCancel={() => setConfirmDeleteId(null)}
/>
```

### 3.7 i18n Copy

**File**: `ui/src/i18n/en/extensions.json`

```json
{
    "uninstallTitle": "Uninstall Plugin",
    "uninstallMessage": "Are you sure you want to uninstall this plugin?",
    "uninstallConfirm": "Uninstall"
}
```

> The hardcoded `uninstallPurgeData` key from the original design has been removed. Uninstall checkbox labels are dynamically provided by the manifest's `UninstallArg.Description`, so no i18n key is needed.

> Chinese i18n is provided by the `chat.axons.locale-zh-cn` language pack and is not listed here.

### 3.8 SSE Event Extension

**File**: `ui/src/hooks/useEventStream.ts`

The `plugin.uninstalled` event payload adds an `argValues` field:

```typescript
interface PluginUninstalledEvent {
    pluginId: string;
    argValues: Record<string, boolean>;  // New
}
```

Existing consumers do not need changes — `argValues` is a new field that does not affect existing logic.

---

## 4 Plugin-Side Changes

> Repository: `/Users/mengshi3/go/src/github.com/mengshi02/axons-extension-packages`

### 4.1 Data Directory Migration

**File**: `huggingface/chat.axons.huggingface/server.py` (lines 46-50)

```python
# Old:
PLUGIN_DATA_DIR = Path.home() / ".axons" / "plugins" / "chat.axons.huggingface"

# New: Prefer the environment variable injected by the host
PLUGIN_DATA_DIR = Path(os.environ.get(
    "AXONS_PLUGIN_DATA_DIR",
    str(Path.home() / ".axons" / "plugins" / "data" / "chat.axons.huggingface")
))
MODELS_DIR = PLUGIN_DATA_DIR / "models"
BIN_DIR = PLUGIN_DATA_DIR / "bin"
METADATA_FILE = PLUGIN_DATA_DIR / "models.json"
```

> The plugin already has `_ensure_data_dirs()` that runs `mkdir -p` on startup, so the host does not need to create the directory.

### 4.2 Install Script Refactoring

**File**: `huggingface/chat.axons.huggingface/install.sh` (lines 48-51)

```bash
# Old:
DATA_DIR="$HOME/.axons/plugins/chat.axons.huggingface"

# New: Prefer the environment variable injected by the host
DATA_DIR="${AXONS_PLUGIN_DATA_DIR:-$HOME/.axons/plugins/data/chat.axons.huggingface}"
```

The rest of install.sh logic is unchanged; `BIN_DIR` and `BIN_PATH` are still derived from `DATA_DIR`.

**File**: `huggingface/chat.axons.huggingface/install.ps1` (line 60)

```powershell
# Old:
$DataDir = Join-Path $env:USERPROFILE ".axons\plugins\chat.axons.huggingface"

# New: Prefer the environment variable injected by the host
$DataDir = if ($env:AXONS_PLUGIN_DATA_DIR) { $env:AXONS_PLUGIN_DATA_DIR } else { Join-Path $env:USERPROFILE ".axons\plugins\data\chat.axons.huggingface" }
```

The rest of install.ps1 logic is unchanged; `$BinDir` and `$BinPath` are still derived from `$DataDir`.

### 4.3 Uninstall Script Refactoring (macOS/Linux)

**File**: `huggingface/chat.axons.huggingface/uninstall.sh`

Key changes:
- Support `--purge-data` parameter
- Data directory now reads `AXONS_PLUGIN_DATA_DIR`
- Remove `.venv` deletion (host's `os.RemoveAll(p.Dir)` deletes the entire code directory)
- Retain the existing `pkill` method for stopping processes (in practice there is only one llama-server instance, which is sufficient)

```bash
#!/bin/bash
echo "=== Uninstall Axons HuggingFace ==="

PURGE_DATA=false

# Parse command-line arguments
for arg in "$@"; do
    case $arg in
        --purge-data) PURGE_DATA=true ;;
    esac
done

# Data directory
DATA_DIR="${AXONS_PLUGIN_DATA_DIR:-$HOME/.axons/plugins/data/chat.axons.huggingface}"

# 1. Stop running llama-server process
echo "Stopping running llama-server process..."
if command -v pkill &>/dev/null; then
    pkill -f "llama-server.*$DATA_DIR" 2>/dev/null || true
fi

# 2. Decide whether to clean up data directory based on parameter
if [ "$PURGE_DATA" = true ]; then
    echo "Cleaning plugin data (models, llama-server, metadata)..."
    if [ -d "$DATA_DIR" ]; then
        rm -rf "$DATA_DIR"
    fi
    echo "Full uninstall complete."
else
    echo "Plugin uninstalled, data preserved at: $DATA_DIR"
    echo "To clean up completely, manually delete: $DATA_DIR"
fi
```

### 4.4 Uninstall Script Refactoring (Windows)

**File**: `huggingface/chat.axons.huggingface/uninstall.ps1`

Same changes as above: support `--purge-data`, remove `.venv` deletion.

```powershell
# Axons HuggingFace - Windows Uninstall Script
# Usage: powershell -ExecutionPolicy Bypass -File uninstall.ps1 [--purge-data]

Write-Host "=== Uninstall Axons HuggingFace ===" -ForegroundColor Cyan

# Parse arguments
$PurgeData = $args -contains "--purge-data"

# Data directory
$DataDir = if ($env:AXONS_PLUGIN_DATA_DIR) { $env:AXONS_PLUGIN_DATA_DIR } else { Join-Path $env:USERPROFILE ".axons\plugins\data\chat.axons.huggingface" }

# 1. Stop llama-server process
Write-Host "Stopping running llama-server process..."
Get-Process -Name "llama-server" -ErrorAction SilentlyContinue | Stop-Process -Force

# 2. Decide whether to clean up data directory based on parameter
if ($PurgeData) {
    Write-Host "Cleaning plugin data (models, llama-server, metadata)..."
    if (Test-Path $DataDir) {
        Remove-Item -Recurse -Force $DataDir
    }
    Write-Host "Full uninstall complete." -ForegroundColor Green
} else {
    Write-Host "Uninstall complete." -ForegroundColor Green
    Write-Host "To clean up plugin data, manually delete: $DataDir"
}
```

### 4.5 manifest.json Update

**File**: `huggingface/chat.axons.huggingface/manifest.json`

The `uninstall` node adds `args` declarations:

```json
{
    "backend": {
        "uninstall": {
            "command": ["bash", "uninstall.sh"],
            "args": [{
                "name": "purge_data",
                "type": "boolean",
                "default": false,
                "description": "Delete all plugin data (downloaded models, binaries, metadata)"
            }]
        },
        "platforms": {
            "windows": {
                "uninstall": {
                    "command": [
                        "powershell", "-ExecutionPolicy", "Bypass", "-File", "uninstall.ps1"
                    ],
                    "args": [{
                        "name": "purge_data",
                        "type": "boolean",
                        "default": false,
                        "description": "Delete all plugin data (downloaded models, binaries, metadata)"
                    }]
                }
            }
        }
    }
}
```

---

## 5 Compatibility Matrix

| Scenario | Behavior |
|----------|----------|
| **New host + New plugin** | Host injects `AXONS_PLUGIN_DATA_DIR`, plugin reads it, data stored in `data/{id}/`. On uninstall, host dynamically builds arguments based on manifest `Args` declarations; plugin uninstall script handles data cleanup. ✅ Full functionality |
| **Old host + New plugin** | Host does not inject `AXONS_PLUGIN_DATA_DIR`, plugin falls back to default path `~/.axons/plugins/data/{id}/`, functions normally. On uninstall, old host does not pass declarative parameters; plugin only stops processes, does not clean data. ⚠️ Old host still does `os.RemoveAll` on the entire code directory, but data is in `data/{id}/` and is unaffected |
| **New host + Old plugin** | Host injects `AXONS_PLUGIN_DATA_DIR`, old plugin ignores it. Old plugin has no `args` declaration, frontend does not render checkboxes, uninstall only deletes code directory. ⚠️ Old plugin data is under `plugins/{id}/` and gets deleted along with the code directory, cannot be preserved |

> The third scenario is a transitional period issue. The software has not yet been released, so there are no existing old plugins that need compatibility support.

---

## 6 Change File List

### Host Side (axons repository)

| # | File | Change Description |
|---|------|-------------------|
| 1 | `internal/plugin/manifest.go` | `UninstallDef` adds `Args` field, adds `UninstallArg` struct, `ValidateManifest()` adds `Args` validation |
| 2 | `internal/plugin/manager.go` | Add `PluginDataDir()`; `startProcess()` injects `AXONS_PLUGIN_DATA_DIR`; `InstallPlugin()` adds `cmd.Env` + injects `AXONS_PLUGIN_DATA_DIR`; `UninstallPlugin()` signature changed to `argValues map[string]bool` + declarative dynamic argument building + 30s timeout, remove host-side data directory deletion |
| 3 | `internal/plugin/handlers.go` | `handleUninstallPlugin` changed to parse `args.{name}` format query parameters |
| 4 | `ui/src/components/ConfirmDialog.tsx` | Add `checkboxes` list property, `onConfirm` signature changed to `(checkboxValues?: Record<string, boolean>) => void` |
| 5 | `ui/src/components/ExtensionsPanel.tsx` | Uninstall dialog dynamically renders checkboxes from manifest `args` |
| 6 | `ui/src/services/api.ts` | `uninstallPlugin` changed to accept `argValues: Record<string, boolean>` |
| 7 | `ui/src/i18n/en/extensions.json` | Add `uninstallConfirm` (checkbox labels are dynamically provided by manifest `description`) |
| 8 | `ui/src/hooks/useEventStream.ts` | `plugin.uninstalled` event adds `argValues` field |

### Plugin Side (axons-extension-packages repository)

| # | File | Change Description |
|---|------|-------------------|
| 1 | `huggingface/chat.axons.huggingface/server.py` | `PLUGIN_DATA_DIR` changed to read `AXONS_PLUGIN_DATA_DIR` environment variable |
| 2 | `huggingface/chat.axons.huggingface/install.sh` | `DATA_DIR` changed to read `AXONS_PLUGIN_DATA_DIR` environment variable |
| 3 | `huggingface/chat.axons.huggingface/install.ps1` | `DATA_DIR` changed to read `AXONS_PLUGIN_DATA_DIR` environment variable (Windows version) |
| 4 | `huggingface/chat.axons.huggingface/uninstall.sh` | Support `--purge-data` parameter; data directory changed to read `AXONS_PLUGIN_DATA_DIR`; remove redundant `.venv` deletion |
| 5 | `huggingface/chat.axons.huggingface/uninstall.ps1` | Same as above, Windows version synchronized; remove redundant `.venv` deletion |
| 6 | `huggingface/chat.axons.huggingface/manifest.json` | `uninstall` node adds `args` declarations |

---

## 7 User Interaction Flow

### 7.1 Uninstall Plugin

**Plugin with `args` declarations (e.g. HuggingFace, has runtime data):**

```
User clicks the "Uninstall" button on the plugin card
        │
        ▼
┌─────────────────────────────────────┐
│  Uninstall Plugin                    │
│                                      │
│  Are you sure you want to uninstall  │
│  this plugin?                        │
│                                      │
│  ☐ Delete all plugin data            │
│    (downloaded models, binaries,     │
│     metadata)                        │
│                                      │
│           [Cancel]  [Uninstall]      │
└─────────────────────────────────────┘
        │
        ├── Checkbox unchecked (default)    ├── Checkbox checked
        │                                    │
        ▼                                    ▼
  Uninstall plugin code only          Uninstall plugin code + delete data
  Preserve downloaded models etc.     All data completely deleted
  (For upgrade scenarios)             (For no-longer-using scenarios)
```

**Plugin without `args` declarations (e.g. locale-zh-cn, pure code plugin):**

```
User clicks the "Uninstall" button on the plugin card
        │
        ▼
┌─────────────────────────────────────┐
│  Uninstall Plugin                    │
│                                      │
│  Are you sure you want to uninstall  │
│  this plugin?                        │
│                                      │
│           [Cancel]  [Uninstall]      │
└─────────────────────────────────────┘
        │
        ▼
  Uninstall plugin code (no data directory to preserve or clean up)
```

### 7.2 Plugin Upgrade Flow (After Changes)

```
1. User uninstalls old version (checkbox unchecked) → Only code deleted, models preserved in data/{id}/
2. User imports new version .tar.gz           → Code installed to plugins/{id}/
3. New version starts                         → Reads AXONS_PLUGIN_DATA_DIR, finds existing model data, ready to use
```

Compared to the old flow:

```
1. User uninstalls old version → Code + models all deleted
2. User imports new version    → Code installed
3. New version starts          → Needs to re-download models (time-consuming, bandwidth-consuming)
```
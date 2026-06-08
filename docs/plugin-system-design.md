# Axons Plugin System Design

> Version: v1.1 | Date: 2026-05-14 | Status: Implemented

## 1. Background & Motivation

### 1.1 Current Pain Points

Axons analysis panels (CodeHealth / ImpactAnalysis / CfgDataflow / Sequence / ArchRules / GraphAnalytics) are all hard-coded in the following files. Adding a new feature requires modifying 5+ files:

- `ui/src/App.tsx` — Panel mounting and layout
- `ui/src/components/Footer.tsx` — Footer function buttons
- `ui/src/components/ActivityBar.tsx` — Activity bar icons
- `ui/src/hooks/useAppState.ts` — Panel state management
- New Panel component file

Tight coupling, high cost of extension.

### 1.2 Existing Foundation

- `internal/registry/` — Project registry mechanism
- `internal/mcp/` — MCP tool registration mechanism
- `skills/` — SKILL.md specification directory
- `internal/api/` — Complete HTTP API layer

### 1.2 Is It Worth Doing

**Yes, recommended to implement in two phases**:

| Dimension | Assessment |
|------|------|
| Current pain points | Hard-coded panels are tightly coupled, high extension cost |
| Existing foundation | Registry, MCP, Skills mechanisms can naturally extend |
| Competitor trends | VSCode / JetBrains / Cursor all follow plugin extension paths |
| Risk | Small user base, low ROI for cloud marketplace, defer to Phase 2 |

---

## 2. Architecture Design

### 2.1 Design Principle: Thin Frontend, Thick Backend

During implementation, we follow the **thin frontend, thick backend** principle:

- **Frontend should be as thin as possible**: The frontend is only responsible for UI rendering and interaction, no business logic computation, no complex state holding
- **Capabilities should be implemented in the backend as much as possible**: Data processing, business logic, algorithm computation, etc. are all pushed down to the backend, leveraging the high-performance advantages of backend (Go/Python, etc.)

| Dimension | Frontend (Thin) | Backend (Thick) |
|------|-----------|-----------|
| Responsibility | UI rendering, user interaction, EventBus event subscription/forwarding | Business logic, data processing, algorithm computation, state management |
| Data | Display only, no processing | Raw data acquisition, cleaning, aggregation, computation |
| Communication | `pluginApi.fetch()` desktop direct/Web proxy | Call axons API to get graph data, compute and return to frontend |
| State | Minimal local UI state | Shared state API (`/v1/plugins/state`) persistence |

**Why thin frontend, thick backend**:
1. Backend (Go/Python) processes large-scale code graph data with performance far exceeding browser JS runtime
2. Frontend stays lightweight, plugin UI loads faster, lower memory footprint
3. Backend capabilities can be independently tested and reused, not dependent on UI layer

### 2.2 Core Decisions

| Decision | Choice | Reason |
|--------|------|------|
| Plugin protocol | `manifest.json` (custom) | Follows industry convention (Chrome/PWA/Firefox), designed for axons semantics |
| Runtime | No embedded goja, plugin backend as independent process | Multi-language support, OS-level process isolation, simple debugging |
| Communication | Hybrid: desktop direct + Web proxy | Zero proxy overhead on desktop; Web uses axons proxy for CORS and remote reachability (see Section 2.6) |
| Cross-panel communication | EventBus (frontend) + Shared state API (backend) | Layered solution for different granularity interaction needs |

### 2.3 Overall Architecture

```
┌──────────────── axons daemon ──────────────────────┐
│                                                      │
│  PluginManager                                       │
│  ├── Scan ~/.axons/plugins/*/manifest.json           │
│  ├── Allocate port (stdin+env var), exec.Command start plugin backend process │
│  ├── Health check (poll healthCheck endpoint)        │
│  ├── Crash monitoring (cmd.Wait) + auto restart (max 3 times) │
│  └── Read frontend.panels/commands → register to PluginRegistry │
│                                                      │
│  PluginRegistry (unified registry)                   │
│  ├── In-memory map[type][]PluginEntry                │
│  ├── Supports manifest.json static declaration (frontend.panels/commands) │
│  └── Supports plugin runtime dynamic sync status reporting │
│                                                      │
│  API Routes                                          │
│  ├── GET  /v1/plugins             Plugin list         │
│  ├── GET  /v1/plugins/registry/:type  Query registry by type │
│  ├── POST /v1/plugins/registry/sync   Plugin report status │
│  ├── GET  /v1/plugins/system-state    System state mirror │
│  ├── GET  /v1/plugins/state/:key      Shared state read │
│  ├── PUT  /v1/plugins/state/:key      Shared state write │
│  ├── POST /v1/plugins/import          Offline import  │
│  └── /v1/plugins/:id/proxy/*path     Plugin proxy (Web) │
│                                                      │
└──────────────────────────────────────────────────────┘
      │ Inject env vars                 │ SSE: plugin.crashed
      ▼                                ▼
┌─── Plugin Backend (any language) ───┐  ┌─── Frontend React ─────────────┐
│  Read AXONS_API_URL                 │  │  usePluginRegistry()            │
│  Read AXONS_PLUGIN_PORT             │  │  → Fetch registry, merge to UI  │
│  Read AXONS_PLUGIN_TOKEN            │  │  import() dynamic component load│
│  Bind 127.0.0.1:PORT                │  │  pluginApi.fetch():            │
│  Add CORS headers (required desktop)│  │    Desktop → direct plugin backend │
│  Expose /health + business API      │  │    Web    → axons proxy forward │
└─────────────────────────────────────┘  │  EventBus cross-panel comm     │
                                         │  Listen SSE plugin crash notice │
                                         └─────────────────────────────────┘
```

### 2.4 Data Flow

```
1. axons starts → scan ~/.axons/plugins/ → start each backend process
2. Plugin backend ready → call POST /v1/plugins/registry/sync to report dynamic status
3. Frontend requests GET /v1/plugins → get plugin list (with endpoint + frontend component path)
4. Frontend import() loads plugin UI component, injects pluginApi
5. Plugin UI component uses pluginApi.fetch to request plugin backend:
   - Desktop: pluginApi.fetch('/api/models') → direct http://127.0.0.1:{pluginPort}/api/models
   - Web:     pluginApi.fetch('/api/models') → fetch /v1/plugins/:id/proxy/api/models
              → axons proxy forwards to http://127.0.0.1:{pluginPort}/api/models
6. Plugin backend needs axons data → use AXONS_API_URL to call /v1/* API (Go HTTP, no CORS)
```

### 2.5 Cross-Origin & Hybrid Communication

#### Problem

Axons supports both desktop (Electron) and Web (browser), and the plugin frontend accessing the plugin backend has cross-origin issues:

| Scenario | Frontend origin | Plugin backend origin | Cross-origin? | Reason |
|------|------------|----------------|-------|------|
| Desktop + plugin | `http://127.0.0.1:{axonsPort}` | `http://127.0.0.1:{pluginPort}` | Yes | Different port = different origin |
| Web + plugin | `http://{host}:9090` | `http://127.0.0.1:{pluginPort}` | Yes | Different port + possibly different host |
| Web (remote deploy) | `https://axons.example.com` | `http://127.0.0.1:{pluginPort}` | Unreachable | Plugin backend binds 127.0.0.1, remote browser cannot access |

#### Solution: Hybrid Communication (Phase 1)

**Desktop: Frontend directly connects to plugin backend (zero proxy overhead, best performance)**
**Web: Frontend accesses plugin backend via axons reverse proxy (same origin, no CORS, solves remote reachability)**

```
Desktop (Electron):
  Frontend ──direct──→ Plugin backend http://127.0.0.1:{pluginPort}/api/*
         ──direct──→ axons API   http://127.0.0.1:{axonsPort}/v1/*
         (Plugin backend adds CORS headers, frontend same-machine direct, zero latency)

Web (Browser):
  Frontend ──proxy──→ axons /v1/plugins/:id/proxy/* ──forward──→ Plugin backend
         ──direct──→ axons API   http(s)://{host}/v1/*
         (Frontend same-origin via proxy, solves CORS + remote reachability)
```

#### Runtime Environment Detection

Electron injects the `window.electronAPI` object via preload script's contextBridge, available immediately after page load. Detection logic:

```typescript
// lib/config.ts — extended
let runtimeMode: 'desktop' | 'web' = 'web';  // Default web

export function getRuntimeMode(): 'desktop' | 'web' {
  return runtimeMode;
}

export async function initConfig(): Promise<void> {
  const isLocalhost = window.location.hostname === '127.0.0.1'
                    || window.location.hostname === 'localhost';
  const hasElectron = !!window.electronAPI?.isElectron;

  // Electron's preload runs before page load, no delay retry needed
  if (isLocalhost && !hasElectron) {
    await new Promise(r => setTimeout(r, 100));
  }
  runtimeMode = isLocalhost && hasElectron ? 'desktop' : 'web';
}
```

Detection logic: Desktop WebView always loads from `127.0.0.1` (`isLocalhost` is reliable), `electronAPI` precisely confirms.

#### pluginApi URL Branching

```typescript
// lib/pluginApi.ts
const resolveUrl = (path: string): string => {
  if (getRuntimeMode() === 'desktop') {
    return config.endpoint + path;  // Direct plugin backend
  }
  return `${getBaseURL()}/v1/plugins/${config.pluginId}/proxy${path}`;  // Via axons proxy
};
```

Plugin developers call `pluginApi.fetch('/api/models')` without caring about the runtime environment. `resolveUrl` is the single branching point.

#### Desktop CORS Requirement

Desktop directly connects to plugin backend, the plugin backend **must** return CORS headers:

```python
# Python template — add CORS to every response
class Handler(BaseHTTPRequestHandler):
    def _cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Authorization, Content-Type")

    def do_OPTIONS(self):
        self.send_response(204)
        self._cors()
        self.end_headers()
```

Web mode uses the proxy, so the plugin backend does not need CORS headers (axons proxy returns same-origin responses).

#### axons Proxy Endpoint (Web Only)

Route registration:

```go
// server.go registerRoutes() — new additions
s.router.GET("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.POST("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.PUT("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.DELETE("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.PATCH("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.OPTIONS("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
```

The proxy handler uses the standard library `net/http/httputil.ReverseProxy` (performance is fully sufficient, see below):

```go
// internal/plugin/proxy.go
func (m *Manager) HandlePluginProxy(w http.ResponseWriter, r *http.Request) {
    pluginID := r.PathValue("id")
    path := r.PathValue("path")

    instance, ok := m.GetInstance(pluginID)
    if !ok {
        http.Error(w, "plugin not running", http.StatusNotFound)
        return
    }

    target, _ := url.Parse(instance.Endpoint)
    proxy := httputil.NewSingleHostReverseProxy(target)
    proxy.FlushInterval = -1  // Flush immediately, ensures SSE real-time delivery

    r.URL.Path = path
    r.Host = target.Host
    proxy.ServeHTTP(w, r)
}
```

**Why use standard library instead of fasthttp**:
1. All traffic is localhost loopback, QPS is driven by user interaction (single digits to tens), far from standard library bottleneck
2. axons full stack is based on `net/http` + `httprouter`, introducing fasthttp requires maintaining two HTTP stacks or rewriting all handlers
3. `httputil.ReverseProxy` naturally supports SSE streaming (`FlushInterval = -1` flushes immediately)
4. Desktop doesn't use proxy, proxy only applies to Web, even lower concurrency

#### SSE (EventSource) Cross-Origin Handling

| Runtime Mode | SSE Connection Method | Cross-origin? |
|---------|-------------|-------|
| Desktop | `new EventSource(plugin.endpoint + '/sse')` direct | Solved by plugin backend CORS headers |
| Web | `new EventSource('/v1/plugins/:id/proxy/sse')` via proxy | Same origin, no CORS |

Proxy-side `FlushInterval = -1` ensures SSE events are forwarded in real-time per event, no buffering delay.

#### Plugin UI Static File Loading

Plugin frontend component files (`ui/index.js`) are all served through axons `/plugins/:id/ui/*` static route. Both desktop and Web use axons same-origin, **no cross-origin issue**:

```typescript
// Load uniformly through axons static route
const entryUrl = `/plugins/${plugin.id}/${plugin.frontend.entry}`;
import(/* @vite-ignore */ entryUrl)
```

---

## 3. manifest.json Protocol

### 3.1 Complete Definition

> Filename: `manifest.json` — follows industry convention (Chrome Extension / PWA / Firefox Add-on all use this naming)

```jsonc
{
  // === Basic Info ===
  "id": "com.axons.huggingface",       // Reverse domain name, globally unique
  "name": "Hugging Face",               // Display name
  "version": "1.0.0",                    // Semantic version
  "description": "Manage local LLM models via Ollama",
  "author": "axons-community",
  "icon": "icon.svg",                    // Relative path
  "category": "productivity",            // analysis | visualization | search | productivity
  "minAxonsVersion": "0.8.0",           // Minimum compatible version

  // === Permission Declarations ===
  "permissions": [
    "graph:read",         // Read code graph data
    "project:read",       // Read project info
    "model:register",     // Register models to system
    "panel:create"        // Create UI panel
  ],
  // See Section 3.5 for valid permission values

  // === Backend Process ===
  "backend": {
    "command": [".venv/bin/python", "server.py"],  // Start command (platform exec.Command)
    // Note: Python plugins should create .venv in install.sh, command points to .venv/bin/python
    // This avoids depending on system global Python environment, better isolation
    "port": 0,                           // 0=OS dynamic allocation, or specify fixed port
    "healthCheck": "/health",            // Health check path (platform polls)
    "readyTimeout": "10s",               // Ready timeout (recommended values see table below)
    "env": {                             // Additional env vars (optional)
      "OLLAMA_HOST": "http://localhost:11434"
    },

    // Install: plugin provides script, platform only executes (Approach B: platform manages scheduling, plugin manages implementation)
    "install": {
      "command": ["bash", "install.sh"], // Install script, executed once, exit code 0 = success
      "timeout": "180s"                  // Timeout (default 180s, plugins that need to download large external files should explicitly set larger value, recommended values see table below)
    },

    // Uninstall: plugin optionally provides cleanup script
    "uninstall": {
      "command": ["bash", "uninstall.sh"] // Optional, cleans up plugin's own remnants
    },

    // Cross-platform overrides (optional): when defaults don't apply on specific platforms, use platforms.{os} to override
    // Supported os keys: "windows" | "linux" | "darwin"
    // Override rule: fields in platforms.{os} are deep-merged over backend's same-level defaults
    //                 Only command / install / uninstall / env and other runtime fields can be overridden
    //                 Undeclared fields keep their default values unchanged
    "platforms": {
      "windows": {
        "command": [".venv\\Scripts\\python.exe", "server.py"],
        "install": { "command": ["cmd", "/c", "install.bat"] },
        "uninstall": { "command": ["cmd", "/c", "uninstall.bat"] },
        "env": { "OLLAMA_HOST": "http://127.0.0.1:11434" }
      }
    }
  },

  // === Frontend UI ===
  "frontend": {
    "entry": "ui/index.js",             // UMD/ESM module entry
    "panels": [{
      "id": "huggingface",
      "title": "Hugging Face",
      "icon": "ui/icon.svg",
      "location": "right",              // left | right | center-bottom | modal
      "activator": "activityBar",       // footer | activityBar | node-select | gearMenu | command
      "footerSlot": "left",             // left | right | center — only effective when activator='footer', default left
      "order": 10                       // Sort weight, smaller values come first; built-in reserves 0~9, plugins recommended 10~99, default 10
    }],
    "commands": [{
      "id": "huggingface.open",
      "title": "Open Hugging Face",
      "shortcut": "Ctrl+Shift+M"
    }]
  },

  // === Activation Events (Lazy Loading) ===
  "activationEvents": ["onStartup", "onCommand:huggingface.open"]
}
```

#### 3.1.1 Cross-Platform Overrides: platforms Field

When a plugin's `backend` needs different start commands, install scripts, or environment variables on different operating systems, use the `platforms` field for incremental overrides instead of creating multiple manifest files.

**Why not use separate `manifest.windows.json` files**:

| Dimension | Separate File Approach | `platforms` Embedded Approach |
|------|------------|-------------------|
| Information duplication | Two files with 90% identical content (id/name/version/frontend/permissions all duplicated) | Only override diff fields, zero duplication |
| Consistency risk | Version/permissions/panel definitions may go out of sync | Single source of truth, impossible to be out of sync |
| Import validation | Need to parse two manifests simultaneously, complex merge logic | Parse one file, clear override rules |
| Frontend no difference | `frontend` shouldn't be written twice | frontend is naturally shared |

**Override Rules**:

1. Fields in `platforms.{os}` are **deep-merged** over `backend` same-level defaults
2. Only `command` / `install` / `uninstall` / `env` and other runtime fields can be overridden
3. Fields not explicitly declared in `platforms.{os}` keep their default values unchanged
4. Supported `os` keys: `windows` | `linux` | `darwin`
5. Existing plugins without `platforms` automatically run with defaults, **no breaking changes**

**Parsing Logic**:

```go
// Parsing logic pseudocode
func resolveBackend(raw json.RawMessage, goos string) *BackendConfig {
    base := parse(raw)                    // Default config
    if override, ok := base.Platforms[goos]; ok {
        base = deepMerge(base, override)  // Only override fields explicitly declared in override
    }
    base.Platforms = nil  // Runtime no longer needs platforms field
    return base
}
```

**Cross-Platform Script Naming Convention**:

| Unix (Default) | Windows |
|-------------|---------|
| `install.sh` | `install.bat` or `install.ps1` |
| `uninstall.sh` | `uninstall.bat` or `uninstall.ps1` |

**Complete Example** (Python plugin, Unix default + Windows override):

```jsonc
{
  "id": "com.axons.huggingface",
  "backend": {
    "command": [".venv/bin/python", "server.py"],       // Unix default
    "port": 0,
    "healthCheck": "/health",
    "readyTimeout": "15s",
    "env": { "OLLAMA_HOST": "http://localhost:11434" },
    "install": {
      "command": ["bash", "install.sh"],
      "timeout": "300s"
    },
    "uninstall": {
      "command": ["bash", "uninstall.sh"]
    },
    "platforms": {
      "windows": {
        "command": [".venv\\Scripts\\python.exe", "server.py"],
        "install": { "command": ["cmd", "/c", "install.bat"] },
        "uninstall": { "command": ["cmd", "/c", "uninstall.bat"] },
        "env": { "OLLAMA_HOST": "http://127.0.0.1:11434" }
      }
    }
  },
  "frontend": { /* ... frontend definition is consistent across all platforms, no override needed ... */ }
}
```

> **Note**: `frontend` (panels/commands/entry) is completely identical across all platforms and should not appear in `platforms` overrides.

#### 3.1.2 Timeout Parameter Recommended Values

`readyTimeout` and `install.timeout` should be set appropriately based on the plugin backend's tech stack:

**readyTimeout Recommended Values**:

| Plugin Type | Recommended | Reason |
|---------|--------|------|
| Go single binary | `5s` | Fast startup, almost no wait needed |
| Python + FastAPI / Flask | `15s` | First import is slow, CPython cold start overhead |
| Python + heavy SDK (e.g. huggingface_hub) | `20s` ~ `30s` | Large dependencies take longer to load first time |
| Node.js | `10s` | Medium startup speed |

**install.timeout Recommended Values**:

| Scenario | Recommended | Reason |
|------|--------|------|
| Pure pip install (no external downloads) | `120s` | Dependency installation usually fast |
| Needs external tool download (e.g. Ollama) | `300s` | External tool download time is unpredictable |
| Needs large model file download | `600s`+ | Model files may be several GB, depends on network speed |

> **Principle**: Default 180s works for most scenarios, but plugins that need to download large external files should explicitly set a larger value to avoid install timeout failures.

### 3.2 Plugin Capability Declaration

The `frontend` and `backend` fields in manifest.json serve as capability declarations simultaneously, without a separate `contributes` section:

**Static Declaration (in manifest.json frontend/backend)** — Structural extension points hardcoded in the plugin package, won't change at runtime:

| Declaration Location | Type | Purpose | System Consumption |
|---------|------|------|-------------|
| `frontend.panels` | Panel | Register UI panel | ActivityBar dynamic rendering, import() loads component |
| `frontend.commands` | Command | Register command | Command Palette / keyboard shortcut trigger |
| `frontend.skills` | Skill path | Declare skill directories contributed by plugin | Added to SkillRegistry scan at startup |

**Dynamic Discovery (Runtime)** — Only known at runtime, changes anytime, doesn't need and shouldn't be statically declared in manifest.json:

| Type | Discovery Method | Reason |
|------|---------|------|
| `tools` | Plugin backend acts as MCP Server, axons discovers via `tools/list` | MCP protocol has built-in discovery, static declaration is redundant and will go stale |
| `skills` | axons scans `skills/` directory at startup, reads each SKILL.md to register to Agent skill list | Skill directories are independent of plugins, Agent discovers and loads on demand |

> Decision criteria: **Structural** = hardcoded in plugin package, immutable at runtime (panels, commands) → statically declare in frontend/backend. **Dynamic data** = only known at runtime, changes anytime (tools, skills) → dynamically discovered.
>
> **Note: models are not a contribution type**. Model data is managed by axons DB uniformly (`/v1/models` API), plugins read/write model data via API, no registry merge mechanism needed.

#### Skills Discovery Mechanism

Skills differ from plugin panels/commands — they are not UI extension points but Agent capability extensions. Discovery flow:

```
axons startup
  ├── Scan project-level skills/ directory (current project .axons/skills/ or project root skills/)
  │   └── Read each subdirectory's SKILL.md → extract name, description, trigger conditions
  ├── Scan global ~/.axons/skills/ directory
  │   └── Same as above
  └── Register to Agent SkillRegistry (in-memory map)
      └── key = skillId, value = { name, description, trigger, skillPath }

Agent runtime
  ├── User input → Agent matches trigger conditions → loads corresponding skill's full instructions
  └── Skill instructions injected into Agent context, guiding Agent to execute specific workflows
```

SKILL.md extracted fields:

| Field | Purpose | Example |
|------|------|------|
| `name` | Skill identifier | `code-graph-analyzer` |
| `description` | Skill description, used by Agent to determine applicability | "Analyze codebases for architecture and dependencies" |
| `trigger` | Trigger condition keywords/regex | `["analyze architecture", "check dependencies"]` |
| Body content | Full instructions, injected into Agent context at runtime | The body portion of SKILL.md |

How plugins contribute skills:

```jsonc
// manifest.json — plugins can place skills in their own directory
{
  "id": "com.axons.search-tools",
  "frontend": {
    "skills": ["skills/code-search-assistant"]  // Relative path, points to SKILL.md within plugin directory
  }
}
```

When axons starts a plugin, it adds paths declared in `frontend.skills` to the SkillRegistry scan scope, managed uniformly with built-in skills.

### 3.3 Frontend Insertion Point Granularity

Multiple levels of insertion points exist in axons's current UI structure:

```
┌─── TopSearchBar ────────────────────────────────────┐  ← Insertion point: search result enhancement
├─── ActivityBar ─┬─ Main Content ─┬─ RightPanel ──────┤
│  [Home]         │                 │                   │  ← Insertion point: activity bar icon
│  [FolderTree]   │  GraphCanvas    │  AI Chat          │
│  [AI]           │                 │                   │
│  ─────────────  │  ──────────────  │  ─────────────── │
│  [Plugin-A] ◄──│  LeftPanel      │                   │  ← Insertion point: panel content
│  [Plugin-B] ◄──│  (Analysis panel area) │             │
│  ─────────────  │                 │                   │
│  [⚙ GearMenu]  │  ──────────────  │                   │
│                 │  BottomPanel    │                   │  ← Insertion point: bottom panel (Phase 2)
│                 │  [Term][Output] │                   │     Similar to IDE Panel area
├─────────────────┴─────────────────┴───────────────────┤
│  Footer: [Health][Analytics]... │ nodes/edges │ [Term] │
│         ← footerSlot:left →     ←  center →  ← right→ │
└───────────────────────────────────────────────────────┘
```

| Granularity | Insertion Point | Example | Declaration Method |
|------|--------|------|---------|
| **Panel** | Activity bar → left/right area shows full panel | Model management panel | `frontend.panels` |
| **Context menu item** | Right-click menu addition on graph node/file tree node | "Analyze this function with XX" | `frontend.contextMenu` (Phase 2) |
| **Search bar enhancement** | Append plugin results in TopSearchBar search results | Model search results mixed into code search | `frontend.searchProvider` (Phase 2) |
| **Graph node enhancement** | Append plugin info on GraphCanvas node hover | Node tooltip shows risk score | `frontend.nodeDecorator` (Phase 2) |
| **Bottom panel** | Center Column bottom area (similar to IDE Panel) | Test runner output panel | `frontend.bottomPanel` (Phase 2) |
| **Settings page Tab** | Append plugin config page to SettingsPanel | Model management config Tab | `frontend.settingsTab` (Phase 2) |
| **Notification/Status bar** | Bottom-right toast or Footer right-side status | "Model downloading 67%" | Runtime dynamic, no declaration needed |

#### Footer Panel Position Allocation: footerSlot

When `activator='footer'`, the panel button appears in the Footer bar. The Footer bar is divided into three areas, determined by the `footerSlot` field:

| footerSlot | Meaning | Typical Panels |
|---|---|---|
| `'left'` (default) | Analysis tool buttons, Footer left side | Health, Analytics, Impact, CFG, Sequence, Rules, Flow |
| `'center'` | Status indicators, Footer center | Reserved: build status, plugin running status, etc. |
| `'right'` | Independent function area, Footer right side | Terminal |

Design principles:
1. `footerSlot` is orthogonal to `location` — `location` describes where panel content renders (left/right/center-bottom/modal), `footerSlot` describes where the Footer button is placed, they are independent
2. `footerSlot` is orthogonal to `activator` — `activator` describes how to trigger the panel (footer/activityBar/command), `footerSlot` describes the button position within Footer
3. Default `'left'` — most footer panels are analysis tools, belonging to the left area; right (independent function area) and center (status area) have special semantics, require explicit declaration
4. Only effective when `activator='footer'` — panels with other activators don't go through Footer rendering, `footerSlot` is meaningless

Frontend consumption (`Footer.tsx`):
```
const leftPanels   = footerPanels.filter(p => (p.footerSlot ?? 'left') === 'left')
const centerPanels = footerPanels.filter(p => p.footerSlot === 'center')
const rightPanels  = footerPanels.filter(p => p.footerSlot === 'right')
```

**Phase 1 only implements panel granularity**, reasons:
1. Panels are the largest insertion point — covers 90% of plugin needs, any complex UI can be self-implemented within a panel
2. Fine-grained insertion points depend on axons internal component structure — right-click menus, node decorators, etc. require modifying existing components, each new insertion point requires axons code changes
3. Panels can self-build any fine-grained UI — e.g., if a plugin wants to add right-click menu items, it can listen for EventBus `node:rightClick` events within the panel and show its own menu, no need for axons to provide menu extension points

```
Phase 1:  frontend.panels (panel + activity bar icon)

Phase 2:  frontend.contextMenu (right-click menu)
          frontend.searchProvider (search enhancement)
          frontend.nodeDecorator (node decoration)
          frontend.bottomPanel (bottom panel — Center Column bottom Tab area)
          frontend.settingsTab (settings page Tab)
```

### 3.4 Plugin Package Forms

Both backend and frontend are optional, supporting four combinations:

| Form | Example | backend | frontend |
|------|------|---------|----------|
| **Frontend + Backend Service** | Model management plugin (panel + Ollama API) | Has HTTP service | Has panel |
| **Frontend + CLI Command** | Dependency tracker plugin (panel + calls axons API) | null | Has panel |
| **Backend Only (No Frontend)** | Custom MCP toolset (for Agent use) | Has MCP service | null |
| **Frontend Only (No Backend)** | Theme plugin, keyboard shortcut plugin | null | Has panel |

manifest.json examples (four forms):

```jsonc
// Form 1: Frontend + Backend Service
{
  "id": "com.axons.huggingface",
  "backend": { "command": ["python", "server.py"], "port": 0, "healthCheck": "/health" },
  "frontend": { "entry": "ui/index.js", "panels": [...], "commands": [...] }
}

// Form 2: Frontend + CLI (no backend service, frontend directly calls axons API)
{
  "id": "com.axons.dep-tracker",
  "backend": null,
  "frontend": {
    "entry": "ui/index.js",
    "panels": [...]
    // Frontend component directly uses pluginApi.fetch('/api/xxx') to call axons API
  }
}

// Form 3: Backend Only (no frontend, e.g. MCP toolset)
{
  "id": "com.axons.search-tools",
  "backend": { "command": ["./search-server"], "protocol": "mcp", "port": 0 },
  "frontend": null
  // Tools are dynamically discovered via MCP tools/list, no static declaration needed
}

// Form 4: Frontend Only (no backend)
{
  "id": "com.axons.dark-theme",
  "backend": null,
  "frontend": { "entry": "ui/index.js", "panels": [...] }
}
```

Startup flow adjustments:
- `backend` exists → exec.Command starts process + health check
- `backend` is null → skip process startup, directly register frontend's panels/commands

### 3.5 Permission Definitions

permissions declare what system resources the plugin needs to access. Phase 1 implements declaration validation (must declare in manifest.json), runtime enforcement deferred to Phase 2.

#### Valid Values & API Mapping

| Permission | Description | Corresponding API Routes |
|------|------|--------------|
| `graph:read` | Read code graph data | `GET /v1/graph/*`, `POST /v1/search`, `GET /v1/stats` |
| `project:read` | Read project info | `GET /v1/projects`, `GET /v1/repos` |
| `model:register` | Register/unregister models to system | `POST /api/llm-models`, `PUT /api/llm-models/:id`, `DELETE /api/llm-models/:id` |
| `panel:create` | Create UI panel | Auto-granted (panels managed by PluginRegistry) |
| `state:read` | Read other plugins' shared state | `GET /v1/plugins/state/:key` (required when crossing namespaces) |
| `state:write` | Write shared state | `PUT /v1/plugins/state/:key` |

**Phase 1 Rules**:
1. The `permissions` field in manifest.json must list the permissions the plugin actually needs; undeclared permissions are considered unauthorized
2. Phase 1 does not implement runtime enforcement (i.e., you can still call `POST /api/llm-models` without declaring `model:register`), but a warn log will be emitted
3. Phase 2 implements runtime enforcement: API layer validates whether the plugin corresponding to `AXONS_PLUGIN_TOKEN` has declared the required permissions

---

## 4. Backend Design

### 4.1 Directory Structure

```
internal/plugin/
├── manager.go           # PluginManager: process lifecycle management
├── registry.go          # PluginRegistry: unified registry
├── manifest.go          # manifest.json parsing and validation
├── process.go           # Process start/stop/health check/crash restart
├── proxy.go             # Plugin proxy handler (Web reverse proxy)
├── handlers.go          # API handler (registered to api server)
└── marketplace.go       # Cloud marketplace client (Phase 2)
```

### 4.2 Core Data Structures

```go
// PluginInstance runtime instance
type PluginInstance struct {
    Manifest  *PluginManifest
    Port      int
    Cmd       *exec.Cmd
    Status    string    // starting | running | stopped | crashed
    Restarts  int
    Token     string    // Auth token
    StartedAt time.Time
}

// PluginEntry registry entry
type PluginEntry struct {
    PluginID  string          `json:"pluginId"`
    Type      string          `json:"type"`      // panels | tools | skills | ...
    ID        string          `json:"id"`         // Unique ID within entry
    Def       json.RawMessage `json:"def"`        // Specific definition (schema varies by type)
    Endpoint  string          `json:"endpoint"`   // http://127.0.0.1:PORT
    Status    string          `json:"status"`     // running | stopped | downloading | ...
    UpdatedAt time.Time       `json:"updatedAt"`
}

// PluginRegistry unified registry
type PluginRegistry struct {
    mu     sync.RWMutex
    byType map[string][]PluginEntry   // type → entries
    byID   map[string]*PluginEntry    // "type:id" → entry (fast lookup)
}
```

#### ID Conflict Strategy

`panel.id` and `command.id` in the registry may be duplicated across plugins. A **first-registered-wins** strategy is adopted:

| Scenario | Handling |
|------|---------|
| Two plugins declare the same `panel.id` | First registered takes effect, later one logs a warn and is skipped, frontend doesn't render duplicate panel |
| Two plugins declare the same `command.id` | Same as above, first registered binds the keyboard shortcut, later one only appears in Command Palette (without shortcut) |
| Plugin startup order dependency | Phase 1 doesn't design `dependencies` declaration, starts in directory scan order; Phase 2 adds topological sorting |

### 4.3 Plugin Full Lifecycle

From acquisition to complete removal, a plugin goes through 6 stages:

**Core Principle: Platform manages scheduling, plugin manages implementation.** The platform doesn't make decisions for the plugin, the plugin doesn't manage resources for the platform.

| Stage | Platform Provides | Plugin Provides | Reason |
|------|---------|---------|------|
| **Import** | Download/receive files, extract, validate manifest.json legality, place files | manifest.json itself | Platform must validate package legality but doesn't understand package content |
| **Install** | Execute entry (install.command), progress reporting (stdout→SSE), failure rollback | Install script (installation logic) | Platform can't enumerate all language/dependency scenarios, plugin knows best what environment it needs |
| **Start** | Allocate port, inject env vars, exec.Command, health check, register frontend panels/commands | Command process, /health endpoint, /sync report | Platform manages process scheduling, plugin manages business logic |
| **Stop** | SIGTERM/SIGKILL, unregister panels/commands, SSE notification | Graceful exit (finish requests within 5s) | Platform manages process lifecycle, plugin manages resource cleanup |
| **Uninstall** | Stop process, delete directory, delete registry entries | Uninstall script (optional, cleans up plugin's own remnants) | Platform knows what it created, plugin knows what the platform doesn't |
| **Cleanup** | Delete shared state, delete token | None | Shared state is platform data, platform is fully responsible |

```
Import → Install → Start ⇄ Stop → Uninstall → Cleanup
 │      │      │      │       │      │
 │      │      │      │       │      └─ Delete residual data (shared state, etc.)
 │      │      │      │       └─ Delete plugin file directory
 │      │      │      └─ SIGTERM stop process, unregister panels/commands
 │      │      └─ exec.Command start process, register frontend panels/commands
 │      └─ Build runtime environment (language runtime/dependencies/external services)
 └─ Extract, validate manifest.json, place files
```

#### Stage 1: Import

Extract the plugin package locally, validate legality, place files. **No environment preparation**.

```
Sources:
  a) Cloud marketplace download (Phase 2): GET /v1/plugins/marketplace/:id/download → .axons-plugin.tar.gz
  b) Offline import: POST /v1/plugins/import (upload .axons-plugin.tar.gz file)

Flow:
  1. Download/receive .axons-plugin.tar.gz to /tmp/axons-import-:id/
  2. Extract to temporary directory
  3. Read and validate manifest.json:
     - Required fields: id, name, version (at least one of backend or frontend must be non-null)
     - id format: reverse domain name (com.axons.xxx)
     - version format: semantic version (1.0.0)
  4. Check minAxonsVersion compatibility
  5. Check if id is already installed (prevent duplicates)
  6. Validation passed → move temporary directory to ~/.axons/plugins/:id/
  7. Write installed.json registry (status = "imported")
  8. Validation failed → clean up temporary directory, return error

Status: imported (files in place, environment not prepared)

API:
  POST /v1/plugins/import         Offline import (multipart/form-data upload)
  POST /v1/plugins/marketplace/:id/download  Cloud download (Phase 2)
```

#### Stage 2: Install

Build the plugin's runtime environment, ensure all prerequisites are met so the plugin can start.

**Core Principle: Platform manages scheduling, plugin manages implementation.** The platform doesn't make decisions for the plugin (doesn't guess language/dependencies/services), the plugin knows best what environment it needs.

```
Two approaches compared:
  Approach A (Platform smart install): Platform reads runtime.type=python → check python → pip install → check ollama → start ollama
    Problem: Platform must adapt to all language scenarios, never-ending, every new language requires platform code changes

  Approach B (Plugin provides script, platform executes): ✅ Adopted
    Platform reads manifest.json → finds install.command → exec executes → waits for exit code 0
    Plugin writes its own install.sh / install.py / setup.js, decides what to install, check, and start
    Platform doesn't guess, just executes
```

```
What the platform does during installation:
  1. Read manifest.json's backend.install.command
  2. exec.Command executes install.command (working directory is plugin directory)
  3. Real-time stdout/stderr → SSE push to frontend (show install progress)
  4. Wait for exit code: 0=success, non-0=failure
  5. Failure → rollback: delete plugin directory, remove installed.json entry
  6. Timeout (install.timeout) → kill process, mark install failed
  7. Update installed.json status = "installed"

Plugin install script example (model management plugin):
  #!/bin/bash
  # install.sh — Plugin decides its own installation logic
  set -e

  # Check Python
  python3 --version || { echo "Python 3.9+ required"; exit 1; }

  # Create virtual environment & install dependencies
  python3 -m venv .venv
  source .venv/bin/activate
  pip install -r requirements.txt

  # Check Ollama
  ollama --version || { echo "Installing Ollama..."; curl -fsSL https://ollama.com/install.sh | sh; }

  # Start Ollama service (if not running)
  pgrep ollama || ollama serve &

  # Poll wait for service ready (recommended: polling > hardcoded sleep)
  MAX_WAIT=10
  for i in $(seq 1 $MAX_WAIT); do
      curl -s http://localhost:11434/ >/dev/null 2>&1 && break
      sleep 1
  done

  # Pre-pull default model
  ollama pull llama3

  echo "Install complete"

Status: installed (environment ready, can start)

Install script best practices:
  1. Polling wait > hardcoded sleep: After starting background service, use polling to detect readiness instead of fixed wait
     # Recommended
     for i in $(seq 1 $MAX_WAIT); do curl -s http://localhost:11434/ && break; sleep 1; done
     # Not recommended
     sleep 3  # Unreliable: service may not be ready in 3s, or may be ready in 1s wasting 2s
  2. Graceful degradation: When external service installation fails, warn rather than exit, allow partial plugin functionality
     ollama --version || { echo "WARN: Ollama not found, some features unavailable"; }
  3. Use venv: Python plugins should create .venv in install.sh, command points to .venv/bin/python
  4. Timeout estimation: When needing to download large external files (models/tools), install.timeout should be explicitly set to a larger value (300s+)

Install-related fields in manifest.json:
  "backend": {
    "install": {
      "command": ["bash", "install.sh"],   // Install script, executed once, exit code 0 = success
      "timeout": "120s"                    // Timeout (default 180s)
    },
    "uninstall": {
      "command": ["bash", "uninstall.sh"]  // Optional, cleans up plugin's own remnants
    }
  }

No install field: Skip install stage, go directly to installed status (e.g., Go single binary plugin)
```

#### Background Process Lifecycle Started by Install Script

Some plugins' install scripts start long-running external services (e.g., Ollama, Redis). Their lifecycle needs to be clarified:

| Scenario | Behavior | Description |
|------|------|------|
| Install script starts background service (e.g., `ollama serve &`) | **Not cleaned up by platform** | Platform only manages install.command main process; after install exits, main process ends, but child processes (background services) continue running |
| During uninstall | Plugin is responsible for deciding in uninstall.sh whether to stop/uninstall external services | Platform doesn't know what the install script started, doesn't make decisions for the plugin |
| When axons exits | External services are unaffected | Ollama etc. are system-level services, independent of axons lifecycle |

Principle: External services started by the install script and axons plugin processes have two independent lifecycles. Plugin processes are managed by the platform (start/stop/crash restart), external services are managed by the plugin itself (install creates, uninstall cleans up, runtime monitors health).

API:
  POST /v1/plugins/:id/install        Execute install (run install.command)
  GET  /v1/plugins/:id/install-status  Query install progress (SSE: real-time stdout push)
```

#### Stage 3: Start

Start the plugin backend process, register capabilities to the system.

```
Flow:
  1. Validate plugin status is installed/stopped (not running)
  2. Allocate port:
     - Platform net.Listen("tcp", "127.0.0.1:0") gets OS dynamic port (see Section 4.6 port unified allocation management)
     - Port number injected via stdin pipe (see Section 4.7), avoids TOCTOU race after Close
     - Also passed via AXONS_PLUGIN_PORT env var, for plugins that can't read stdin
  3. Construct environment variables:
     - AXONS_API_URL=http://127.0.0.1:{axonsPort}
     - AXONS_PLUGIN_PORT={assignedPort}
     - AXONS_PLUGIN_TOKEN={randomToken}
     - AXONS_PLUGIN_ID=com.axons.huggingface
  4. exec.Command starts child process (working directory is plugin directory)
  5. Poll healthCheck endpoint (200ms interval, max readyTimeout, default 10s)
  6. Ready → read frontend.panels/commands → register to PluginRegistry
  7. Plugin backend calls POST /v1/plugins/registry/sync → reports dynamic status
  8. SSE broadcast plugin.started event → frontend refreshes panel/model/tool list
  9. Update installed.json status = "running"

Status: running

API:
  POST /v1/plugins/:id/start       Start specified plugin
```

#### Stage 4: Stop

Stop the plugin backend process, unregister capabilities, but keep files.

```
Flow:
  1. Validate plugin status is running
  2. Call plugin cleanup endpoint (optional):
     - Platform sends POST /cleanup to plugin backend (5s timeout)
     - Plugin uses this opportunity to clean up side-effect data (e.g., unregister models from Axons)
     - If plugin doesn't implement /cleanup, skip this step (404 means skip)
  3. Send SIGTERM (cmd.Process.Signal)
  4. Wait 5s for graceful exit
  5. Timeout then SIGKILL
  6. Remove all panels/commands for this plugin from PluginRegistry
  7. Clean up this plugin's shared state (optional, default preserve)
  8. SSE broadcast plugin.stopped event → frontend removes panel/model/tool
  9. Update installed.json status = "stopped"

Status: stopped (process not running, files preserved, can start again)

API:
  POST /v1/plugins/:id/stop        Stop specified plugin

Crash handling (automatic):
  1. cmd.Wait() returns → detect unexpected exit
  2. Restarts < 3 → auto restart (exponential backoff: 2s/4s/8s)
  3. Restarts >= 3 → mark crashed, update installed.json status = "crashed"
  4. SSE broadcast plugin.crashed event → frontend shows "Plugin crashed" notification
```



#### Plugin Cleanup Endpoint /cleanup

Side-effect data registered by plugins via the Axons API (e.g., model configurations) are not managed by PluginRegistry, so the platform cannot automatically clean them up. Therefore, a cleanup endpoint is designed so plugins have the opportunity to clean up before stopping.

```
Plugin implementation (optional):
   POST /cleanup    — Called by platform before plugin stops, 5s timeout

Plugin cleanup endpoint responsibilities:
   - Unregister side-effect data created via Axons API
   - E.g., call DELETE /api/llm-models/:id to delete model configs registered by plugin
   - E.g., call DELETE /v1/plugins/state/:key to clean up shared state

Platform call timing:
   - Normal stop: POST /v1/plugins/:id/stop → call /cleanup first → then SIGTERM
   - Uninstall: Same as above, stop first then delete files
   - Crash: Cannot call /cleanup (process is dead), residual data fixed on next startup (see below)

Not implementing /cleanup: Platform receives 404 and skips, doesn't affect stop flow
```

#### Side-Effect Data Source Identification

The `/cleanup` endpoint needs to precisely identify "which side-effect data was registered by this plugin" to avoid mistakenly deleting other plugins' data. Recommended approach:

**Phase 1 Recommendation: Embed Plugin Identifier in name Field**

When a plugin registers side-effect data via the Axons API, it embeds a plugin identifier suffix in an identifiable field (e.g., `name`). At `/cleanup` time, it filters by this identifier:

```python
# During registration: embed [Ollama] identifier in name
def _register_to_axons(model_name: str):
    display_name = f"{base} ({quant}) [Ollama]"  # ← [Ollama] identifier
    # POST /api/llm-models { "name": display_name, ... }

# During cleanup: filter by identifier
@app.post("/cleanup")
async def cleanup():
    existing = _get_axons_models()
    for m in existing:
        if "[Ollama]" in m.get("name", ""):  # ← Precisely match own registrations
            # DELETE /api/llm-models/:id
```

> **Note**: Don't filter only by `provider == "custom"` — other plugins may also register models with the `custom` provider.

**Phase 2 Enhancement: Axons Auto-inject Source Marker**

Axons auto-attaches a `source_plugin_id` field to data in write APIs like `POST /api/llm-models`, based on the request's `AXONS_PLUGIN_TOKEN`. Plugin `/cleanup` matches precisely by this field:

```jsonc
// Phase 2: Axons auto-injects source_plugin_id
{
  "id": "model-123",
  "name": "Llama-3.2 [Ollama]",
  "provider": "custom",
  "source_plugin_id": "com.axons.huggingface"  // ← Auto-injected by Axons
}
```

#### State Recovery After Crash Restart

```
After a plugin crashes, side-effect data registered via Axons API (e.g., model configurations)
still exists in Axons DB, but the underlying service (e.g., Ollama models) may no longer be available.
The plugin should perform state recovery after restart:

Recommended implementation (plugin backend /health endpoint or startup self-check):
   1. Check if models registered by this plugin are still running in Ollama
   2. If Ollama models are no longer available → call DELETE /api/llm-models/:id to clean up residual configs
   3. If Ollama models are still running → keep registration unchanged

The platform doesn't perform state recovery for the plugin, because it doesn't know what side-effect data the plugin registered.
```

#### Stage 5: Uninstall

Delete the plugin file directory, can no longer start.

```
Flow:
  1. If plugin is running → execute stop flow first
  2. Remove all panels/commands for this plugin from PluginRegistry (if still present)
  3. Remove this plugin's entry from installed.json registry
  4. Delete plugin directory ~/.axons/plugins/:id/
  5. SSE broadcast plugin.uninstalled event → frontend removes all UI traces

Note: This stage does not clean shared state, preserves data for re-installation

Status: uninstalled (directory deleted, shared state may remain)

API:
  DELETE /v1/plugins/:id            Uninstall specified plugin
```

#### Stage 6: Cleanup

Clean up plugin residual shared data, completely remove all traces.

```
Flow:
  1. Delete all shared state written by this plugin (key prefix is pluginId)
  2. Clear this plugin's token record
  3. Clear SSE subscriptions

When triggered:
  a) User checks "Also clean data" during uninstall → uninstall+cleanup executed together
  b) Standalone cleanup API call (for plugins already uninstalled but with residual data)

Status: cleaned (completely removed, no residuals)

API:
  DELETE /v1/plugins/:id/data       Clean up plugin residual data
```

#### State Machine Summary

```
                  import                install              start
  [Not exists] ──────────────→ imported ──────────→ installed ──────────→ running
                               │                   │                   │  ▲
                               │ Validation failed │                   │  │
                               ▼                   │  stop             │  │ Auto restart
                            [Not exists]           │  (crashed)        │  │ (≤3 times)
                               │                   ▼                  │  │
                      Cancel import │               stopped ───────────────┘  │
                               ▼                 │                       │
                            [Not exists] uninstall │          crash ≥3 times  │
                                                 ▼                       │
                                             uninstalled            crashed
                                               │                       │
                               cleanup          │           uninstall   │
                                                 ▼                       │
                                             cleaned ◄──────────────────┘
```

#### API Route Summary

```go
// Plugin full lifecycle
s.router.POST("/v1/plugins/import", s.handleImportPlugin)         // Import (offline upload)
s.router.POST("/v1/plugins/install", s.handleInstallPlugin)       // Install (confirm after import)
s.router.POST("/v1/plugins/:id/start", s.handleStartPlugin)       // Start
s.router.POST("/v1/plugins/:id/stop", s.handleStopPlugin)         // Stop
s.router.DELETE("/v1/plugins/:id", s.handleUninstallPlugin)       // Uninstall (delete files)
s.router.DELETE("/v1/plugins/:id/data", s.handleCleanupPlugin)    // Cleanup (delete residual data)
s.router.GET("/v1/plugins", s.handleListPlugins)                  // Plugin list
s.router.POST("/v1/plugins/scan", s.handleScanPlugins)             // Re-scan plugin directory

// Registry
s.router.GET("/v1/plugins/registry/:type", s.handleGetPluginEntries)    // Query by type
s.router.POST("/v1/plugins/registry/sync", s.handleSyncPluginEntries)   // Plugin report

// Shared state (key auto-prefixed with pluginId:, plugin can only read/write its own namespace; cross-plugin read requires state:read permission in permissions)
s.router.GET("/v1/plugins/system-state", s.handleGetSystemState)      // System state mirror
s.router.GET("/v1/plugins/state/:key", s.handleGetPluginState)        // Read shared KV (auto-scoped to current plugin namespace)
s.router.PUT("/v1/plugins/state/:key", s.handleSetPluginState)        // Write shared KV (auto-scoped to current plugin namespace)

// Plugin proxy — Web only, desktop direct connection doesn't use this route
s.router.GET("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.POST("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.PUT("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.DELETE("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.PATCH("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.OPTIONS("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
```

#### SSE Event Types

Plugin lifecycle events are pushed to the frontend via SSE, and the frontend updates UI state accordingly:

| Event Type | Trigger Timing | Payload |
|---------|---------|---------|
| `plugin.imported` | Plugin package import complete | `{ pluginId, name, version }` |
| `plugin.installed` | Install script execution success | `{ pluginId, name, version }` |
| `plugin.installProgress` | Install script stdout output | `{ pluginId, line }` |
| `plugin.installFailed` | Install script execution failure | `{ pluginId, error }` |
| `plugin.started` | Plugin backend process ready | `{ pluginId, endpoint, panels, commands }` |
| `plugin.stopped` | Plugin backend process stopped | `{ pluginId }` |
| `plugin.crashed` | Plugin crashed and exceeded restart limit | `{ pluginId, restarts, lastError }` |
| `plugin.uninstalled` | Plugin uninstall complete | `{ pluginId }` |
| `plugin.cleaned` | Plugin residual data cleanup complete | `{ pluginId }` |

Frontend consumption example:

```tsx
useEffect(() => {
  const unsubscribe = subscribeSSE((event) => {
    switch (event.type) {
      case 'plugin.started':
        refreshPluginList();
        break;
      case 'plugin.crashed':
        showToast(`${event.pluginId} has crashed`);
        break;
    }
  });
  return unsubscribe;
}, []);
```

### 4.4 Authentication

- Plugin backend calls axons API with `Authorization: Bearer ${AXONS_PLUGIN_TOKEN}`
- axons API layer validates token + permissions whitelist
- System state mirror (system-state) returns: currentProjectId, selectedNodeId, activeFilePath, openPanels

**Token Lifecycle**:

| Stage | Token Behavior |
|------|-----------|
| Plugin start | Generate random token for this plugin, write to PluginInstance.Token, inject via AXONS_PLUGIN_TOKEN env var |
| Plugin running | Plugin backend carries token on each axons API call, axons validates token validity + permissions whitelist |
| Plugin stop | Destroy this plugin's token, subsequent requests with this token return 401 |
| Plugin crash restart | Generate new token, old token automatically invalidated |

Phase 1 uses process-level tokens (no expiration, tied to process lifecycle). Phase 2 will consider token refresh and fine-grained permission control.

### 4.5 Storage Paths

```
~/.axons/
├── plugins/
│   ├── installed.json              # Installed plugins registry
│   ├── com.axons.huggingface/    # Plugin directory
│   │   ├── manifest.json
│   │   ├── server.py               # Backend entry
│   │   ├── ui/
│   │   │   ├── index.js            # Frontend component
│   │   │   └── icon.svg
│   │   └── requirements.txt
│   └── ...
├── registry.json                   # Project registry (existing)
└── axons.db                        # Main database (existing)
```

### 4.6 Port Unified Allocation Management

The axons platform uniformly allocates ports for all plugin backends. Plugin backends must not choose their own ports. This ensures:

1. **No port conflicts**: Multiple plugins running simultaneously won't compete for the same port
2. **Traceability**: Platform maintains a port allocation table, can query which plugin occupies which port
3. **Controllable lifecycle**: Port is released when plugin stops, platform can check if port is still occupied before allocation

#### Port Allocation Strategy

```
Port range: 18080-18999 (reserves 920 ports, sufficient for all plugins)

Allocation flow:
  1. Platform maintains PortAllocator (in-memory map[int]string, port → pluginId)
  2. When new plugin starts:
     a. Check manifest.json's backend.port:
        - port = 0 (default): Platform dynamically allocates
        - port > 0: Fixed port, platform checks if available (rejects start if already occupied)
     b. Dynamic allocation:
        - net.Listen("tcp", "127.0.0.1:0") gets OS-allocated free port
        - Pass listener to plugin process stdin (don't Close, avoid TOCTOU)
        - Register to PortAllocator
  3. When plugin stops:
     - Remove from PortAllocator
     - Close listener (port released to OS at this point)
  4. When axons starts:
     - Scan installed.json for plugins with status=running
     - Re-allocate ports for them (previous ports may be occupied by other processes)
```

#### Port Allocator Implementation

```go
// internal/plugin/port.go
type PortAllocator struct {
    mu       sync.Mutex
    used     map[int]string      // port → pluginId
    listeners map[int]net.Listener // port → listener (held without Close, prevents preemption)
}

func (pa *PortAllocator) Allocate(pluginId string) (int, net.Listener, error) {
    pa.mu.Lock()
    defer pa.mu.Unlock()

    listener, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        return 0, nil, fmt.Errorf("failed to allocate port: %w", err)
    }

    port := listener.Addr().(*net.TCPAddr).Port
    pa.used[port] = pluginId
    pa.listeners[port] = listener
    return port, listener, nil
}

func (pa *PortAllocator) Release(pluginId string) {
    pa.mu.Lock()
    defer pa.mu.Unlock()

    for port, id := range pa.used {
        if id == pluginId {
            if ln, ok := pa.listeners[port]; ok {
                ln.Close() // Release port
                delete(pa.listeners, port)
            }
            delete(pa.used, port)
            return
        }
    }
}

func (pa *PortAllocator) GetPort(pluginId string) (int, bool) {
    pa.mu.Lock()
    defer pa.mu.Unlock()
    for port, id := range pa.used {
        if id == pluginId {
            return port, true
        }
    }
    return 0, false
}
```

> **Why not use fixed port range scanning**: `net.Listen("127.0.0.1:0")` lets the OS allocate a free port, which is more reliable than scanning 18080-18999 one by one. The OS kernel guarantees the port is available. Fixed port ranges are only useful during development debugging (via manifest.json `backend.port`).

### 4.7 stdin Port Injection Protocol

#### Problem

After `net.Listen("127.0.0.1:0")` gets a port, if we Close it before passing to the plugin, there's a TOCTOU (Time-of-check to time-of-use) race: after Close, the port may be preempted by another process. The solution is to hold the listener without Closing, and inject the port number to the plugin process via stdin pipe.

#### Protocol Definition

```
Protocol format:
  1. axons writes one line via cmd.Stdin (io.WriteCloser): "PORT:{number}\n"
  2. After writing, don't close stdin (plugin may need to read other subsequent protocol messages)
  3. Plugin reads the first line from stdin, parses "PORT:" prefix, gets port number
  4. Plugin binds to that port and starts HTTP service

Timeline:
  axons                                     plugin
    │                                          │
    │── cmd.Start() ──────────────────────────→│
    │── stdin.Write("PORT:18080\n") ──────────→│
    │                                          │── Read stdin, parse port
    │                                          │── net.Listen("127.0.0.1:18080")
    │                                          │── Start HTTP server
    │←─ healthCheck poll ─────────────────────│
    │                                          │
```

#### axons Side Implementation

```go
// internal/plugin/process.go — inject port when starting process
func (m *Manager) startPluginProcess(plugin *PluginInstance) error {
    port, listener, err := m.portAllocator.Allocate(plugin.Manifest.ID)
    if err != nil {
        return err
    }
    plugin.Port = port

    cmd := exec.Command(plugin.Manifest.Backend.Command[0], plugin.Manifest.Backend.Command[1:]...)
    cmd.Dir = plugin.Manifest.Dir
    cmd.Env = append(os.Environ(),
        "AXONS_API_URL=http://127.0.0.1:"+m.axonsPort,
        "AXONS_PLUGIN_PORT="+strconv.Itoa(port),
        "AXONS_PLUGIN_TOKEN="+plugin.Token,
        "AXONS_PLUGIN_ID="+plugin.Manifest.ID,
    )

    stdinPipe, err := cmd.StdinPipe()
    if err != nil {
        m.portAllocator.Release(plugin.Manifest.ID)
        return fmt.Errorf("failed to create stdin pipe: %w", err)
    }

    if err := cmd.Start(); err != nil {
        m.portAllocator.Release(plugin.Manifest.ID)
        return fmt.Errorf("failed to start process: %w", err)
    }

    // Inject port number via stdin
    fmt.Fprintf(stdinPipe, "PORT:%d\n", port)

    plugin.Cmd = cmd
    plugin.stdinPipe = stdinPipe
    return nil
}
```

#### Plugin Side Read Example

```python
# Python — Read port from stdin
import sys

def read_port_from_stdin():
    """Read axons-allocated port number from stdin"""
    line = sys.stdin.readline().strip()
    if line.startswith("PORT:"):
        return int(line[5:])
    # fallback: read from environment variable
    return int(os.environ.get("AXONS_PLUGIN_PORT", "18080"))

if __name__ == "__main__":
    port = read_port_from_stdin()
    uvicorn.run(app, host="127.0.0.1", port=port)
```

```python
# Python FastAPI + uvicorn — stdin PORT: protocol integration template
import sys
import os
import uvicorn
from fastapi import FastAPI

app = FastAPI()

@app.get("/health")
async def health():
    return {"status": "ok"}

def read_port_from_stdin() -> int:
    """Read axons-allocated port from stdin (preferred), fallback to env var"""
    try:
        line = sys.stdin.readline().strip()
        if line.startswith("PORT:"):
            return int(line[5:])
    except (ValueError, IOError):
        pass
    # fallback: read from environment variable
    return int(os.environ.get("AXONS_PLUGIN_PORT", "18080"))

if __name__ == "__main__":
    port = read_port_from_stdin()
    uvicorn.run(app, host="127.0.0.1", port=port)
```

> **Note**: FastAPI/uvicorn plugins should use the above template to read port from stdin, rather than relying solely on the `AXONS_PLUGIN_PORT` environment variable. The stdin channel has no TOCTOU race risk and is the more reliable approach.

```go
// Go — Read port from stdin
func readPortFromStdin() int {
    scanner := bufio.NewScanner(os.Stdin)
    if scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "PORT:") {
            if port, err := strconv.Atoi(strings.TrimPrefix(line, "PORT:")); err == nil {
                return port
            }
        }
    }
    // fallback: read from environment variable
    port, _ := strconv.Atoi(os.Getenv("AXONS_PLUGIN_PORT"))
    if port == 0 {
        port = 18080
    }
    return port
}
```

#### Dual-Channel Guarantee

| Channel | Priority | Applicable Scenarios | Reliability |
|------|--------|---------|--------|
| stdin `PORT:` protocol | Primary | Plugins that support reading stdin | No race, port cannot be preempted |
| `AXONS_PLUGIN_PORT` env var | Fallback | Plugins that don't support stdin (e.g., some Go binaries) | TOCTOU risk exists, but practically very low probability (local dev environment) |

> Plugins should **prefer reading stdin**, only fallback to env var when stdin is unavailable. axons writes to both channels, ensuring compatibility.

### 4.8 installed.json Format

`~/.axons/plugins/installed.json` is the plugin installation registry, recording the status and metadata of all imported/installed plugins.

```jsonc
{
  "version": 1,                    // Registry format version
  "plugins": {
    "com.axons.huggingface": {
      "id": "com.axons.huggingface",
      "name": "Hugging Face",
      "version": "1.0.0",
      "description": "Manage local LLM models via Ollama",
      "author": "axons-community",
      "icon": "ui/icon.svg",
      "category": "productivity",
      "status": "running",         // imported | installed | running | stopped | crashed
      "dir": "~/.axons/plugins/com.axons.huggingface",  // Plugin directory absolute path
      "port": 18081,               // Currently allocated port (only > 0 when running)
      "installedAt": "2026-05-14T10:30:00Z",   // Import time
      "updatedAt": "2026-05-14T11:00:00Z",     // Last status change time
      "manifestHash": "sha256:abc123...",       // manifest.json hash, used to detect unauthorized modifications
      "backend": {                 // Redundant storage, avoids reading manifest.json each time
        "command": [".venv/bin/python", "server.py"],
        "port": 0,
        "healthCheck": "/health"
      },
      "frontend": {                // Redundant storage
        "entry": "ui/index.js",
        "panels": [{"id": "huggingface", "title": "Hugging Face", "location": "right", "activator": "activityBar"}]
      }
    },
    "com.axons.dep-tracker": {
      "id": "com.axons.dep-tracker",
      "name": "Dependency Tracker",
      "version": "1.2.0",
      "status": "stopped",
      "dir": "~/.axons/plugins/com.axons.dep-tracker",
      "port": 0,
      "installedAt": "2026-05-13T08:00:00Z",
      "updatedAt": "2026-05-13T09:00:00Z",
      "backend": null,
      "frontend": {"entry": "ui/index.js", "panels": [...]}
    }
  }
}
```

#### Field Descriptions

| Field | Type | Description |
|------|------|------|
| `version` | int | Registry format version, incremented on future format changes |
| `plugins` | map | pluginId → plugin info |
| `plugins[id].status` | string | Current status: imported / installed / running / stopped / crashed |
| `plugins[id].dir` | string | Plugin directory absolute path, used as exec.Command working directory |
| `plugins[id].port` | int | Currently allocated port number, only > 0 when running |
| `plugins[id].manifestHash` | string | SHA256 of manifest.json, validated at startup to prevent tampering |
| `plugins[id].backend` / `frontend` | object | Redundantly stored from manifest.json, accelerates startup scan |

#### Persistence Timing

| Operation | Change |
|------|------|
| Import success | New entry, status=imported |
| Install success | status → installed |
| Start success | status → running, port → allocated port |
| Stop | status → stopped, port → 0 |
| Crash | status → crashed |
| Uninstall | Delete entry |

> **Note**: axons reads installed.json at startup and restarts plugins with status=running. If axons exits abnormally (kill -9), installed.json may have residual status=running entries. axons should downgrade these entries to installed on next startup before restarting.

### 4.9 Permission Warn Logging

Phase 1 doesn't implement runtime enforcement, but warn logs are recorded to help developers discover permission configuration omissions and provide data foundation for Phase 2 enforcement.

#### Log Format

```
[plugin-permission] WARN plugin={pluginId} permission={requiredPerm} method={method} path={path}
```

Example:
```
[plugin-permission] WARN plugin=com.axons.huggingface permission=model:register method=POST path=/api/llm-models
[plugin-permission] WARN plugin=com.axons.search-tools permission=graph:read method=GET path=/v1/graph/nodes
```

#### Implementation

```go
// internal/plugin/middleware.go — API middleware
func (m *Manager) PermissionCheckMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("Authorization")
        if strings.HasPrefix(token, "Bearer ") {
            token = strings.TrimPrefix(token, "Bearer ")
            if plugin, ok := m.GetPluginByToken(token); ok {
                requiredPerm := matchPermission(r.Method, r.URL.Path)
                if requiredPerm != "" && !plugin.HasPermission(requiredPerm) {
                    log.Printf("[plugin-permission] WARN plugin=%s permission=%s method=%s path=%s",
                        plugin.Manifest.ID, requiredPerm, r.Method, r.URL.Path)
                }
            }
        }
        next.ServeHTTP(w, r)
    })
}

// matchPermission maps API routes to permissions
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
```

---

### 5.1 ActivityBar Panel Sorting & Unified Rendering

#### Problem

In the current `ActivityBar.tsx`, built-in buttons (Home / FolderTree / AI) are hard-coded in the top area, while plugin buttons and Gear menu are placed in the `mt-auto` bottom area. This causes plugin icons to be close to the settings button and biased toward the bottom, with large whitespace between them and built-in buttons.

```
Current layout:
  [Home]        ← Top area (hard-coded)
  [FolderTree]
  [AI]
                  
  ──whitespace── ← mt-auto pushes to bottom
                  
  [Plugin-A]    ← Bottom area (registry dynamic)
  [Plugin-B]
  [⚙ GearMenu]
```

#### Goal

Plugin icons should follow built-in buttons in order, built-in buttons always come before plugin buttons, Gear menu stays at the very bottom.

```
Target layout:
  [Home]        ← Built-in buttons (registry, order 0~9)
  [FolderTree]
  [AI]
  [Plugin-A]    ← Plugin buttons (registry, order 10+)
  [Plugin-B]

  ──whitespace── ← mt-auto pushes to bottom

  [⚙ GearMenu]  ← Fixed bottom (activator='gearMenu')
```

#### Refactoring Plan

**Core approach**: All panels with `activator='activityBar'` (built-in + plugins) are uniformly rendered through `panelRegistry`, removing hard-coded buttons, using `order` field to control sorting.

**1. Register built-in buttons to panelRegistry (`useAppState.ts`)**

Among current built-in buttons, Home and AI buttons are not registered in the registry. Need to add registration and assign order:

```tsx
// Built-in activityBar panel registration — order 0~9
registerPanel({ id: 'home', title: 'Projects', icon: 'Home', location: 'left-top', activator: 'activityBar', component: ProjectSelector, order: 0 });
registerPanel({ id: 'fileTree', title: 'activitybar:files', icon: 'FolderTree', location: 'left-top', activator: 'activityBar', component: FileTreePanel, order: 1 });
registerPanel({ id: 'rightPanel', title: 'panels:chat.newConversation', icon: 'Sparkles', location: 'right', activator: 'activityBar', component: RightPanel, order: 2 });
```

**2. Plugin panels self-declare order (Approach B — industry convention)**

Plugins declare `order` in manifest.json's `PanelDef`, backend passes it through to frontend. This is the common practice in platforms like IDE (menus `group@number`, config `order`), JetBrains (ActionGroup `position`): sort weight is self-declared by plugins, platform prevents conflicts via convention ranges, no approval needed.

manifest.json example:
```jsonc
"frontend": {
  "panels": [{
    "id": "huggingface",
    "title": "Hugging Face",
    "icon": "ui/icon.svg",
    "location": "right",
    "activator": "activityBar",
    "order": 10       // ← New: plugin self-declared sort weight
  }]
}
```

Backend `manifest.go`'s `PanelDef` adds `Order` field:
```go
type PanelDef struct {
    ID         string            `json:"id"`
    Title      string            `json:"title"`
    TitleI18n  map[string]string `json:"titleI18n,omitempty"`
    Icon       string            `json:"icon"`
    Location   string            `json:"location"`
    Activator  string            `json:"activator"`
    FooterSlot string            `json:"footerSlot"`
    Order      int               `json:"order,omitempty"`    // ← New: sort weight
}
```

Frontend reads backend-passed order during registration, falls back to default 10 when not declared:
```tsx
registerPanel({
  id: entry.id,
  title: def.title || entry.id,
  icon: def.icon || 'Puzzle',
  location: def.location || 'left',
  activator: def.activator || 'activityBar',
  order: def.order ?? 10,     // ← Read plugin-declared order, default 10
  isPlugin: true,
  pluginId: entry.pluginId,
  endpoint: data.endpoint || entry.endpoint,
  asyncLoader: () => { ... },
});
```

Sort effect:
| order Range | Owner | Description |
|------------|------|------|
| 0~9 | Built-in buttons | Home(0), FolderTree(1), AI(2), platform reserved range |
| 10~99 | Plugin buttons (recommended) | Plugin self-declared, recommended in this range |
| 100+ | Plugin buttons (not restricted) | Not enforced, larger values sort later |

Sort rules:
1. Ascending by `order`, smaller values come first
2. Panels with the same `order` value are sorted by registration order (first registered comes first)
3. Plugin panels without declared `order` default to 10, sorted after built-in buttons
4. Built-in button range 0~9 is platform-reserved; plugin-declared order values in this range still take effect (not forcibly blocked, but documentation convention says not to use)

**Why Approach B over Approach A (frontend auto-assignment)**:
- Approach A: plugins can't control their own position, only sorted by registration order, inconsistent with industry convention
- IDE menus' `group@number`, config `order`, JetBrains' `position` weight are all plugin self-declared
- Approach B has comparable change volume to A (only one additional backend field), but gives plugin developers control

**3. ActivityBar.tsx Unified Rendering**

Top area changed to iterate `getPanelsByActivator('activityBar')`, removing hard-coded buttons:

```tsx
<div className="w-11 h-full bg-void flex flex-col items-center shrink-0 border-r border-border-subtle">
    {/* Top area: all activityBar panels (built-in + plugins), sorted by order */}
    <div className="flex flex-col items-center w-full">
        {getPanelsByActivator('activityBar')
            .sort((a, b) => (a.order ?? 0) - (b.order ?? 0))
            .map(panel => (
                <button
                    key={panel.id}
                    onClick={() => handlePanelClick(panel)}
                    className={iconBtnClass(isPanelActive(panel))}
                    title={panel.title.includes(':') ? t(panel.title) : panel.title}
                >
                    {renderPanelIcon(panel)}
                </button>
            ))}
    </div>

    {/* Bottom area: Gear menu (activator='gearMenu', not part of activityBar sorting) */}
    <div className="mt-auto w-full flex flex-col items-center pb-1">
        {/* GearMenu component */}
    </div>
</div>
```

#### Impact Scope

| File | Change |
|------|------|
| `ui/src/components/ActivityBar.tsx` | Top area changed to iterate registry rendering, bottom area only keeps GearMenu |
| `ui/src/hooks/useAppState.ts` | Add Home button registration; plugin registration reads `def.order ?? 10` |
| `ui/src/lib/panelRegistry.ts` | No change (`order` field and sort logic already exist) |
| `internal/plugin/manifest.go` | `PanelDef` adds `Order int \`json:"order,omitempty"\`` field |
| `internal/plugin/registry.go` | `/v1/plugins/registry/panels` API passes through `order` field (already in `def`, no additional change needed) |

#### Home Button Special Handling

The Home button opens a ProjectSelector popup on click, different from normal panel toggle behavior. After registering as a panel, the ActivityBar click callback needs to differentiate:

- Normal panels (`fileTree` / `rightPanel` / plugin panels): `togglePanel(id)`
- Home panel: opens ProjectSelector popup (keep existing `isHomeOpen` logic)

Implementation: Determine by `id === 'home'` in `PanelDef`, or extend `PanelDef` with `action: 'popup' | 'toggle'` field.

#### User Drag Reorder (Future Extension)

IDE's ActivityBar icons support user drag reorder, stored in user settings. axons can add this capability in the future:

1. After user drags, persist panel ID order to `~/.axons/activitybar-order.json`
2. When rendering: user custom order > `order` declaration > registration order
3. Reset button restores default `order`-declared order

---

### 5.2 ActivityBar Gear Menu Refactoring

The current `ActivityBar.tsx` bottom only has a Settings button. Change it to a dropdown menu:

```
 ┌──────────┐
 │ ⚙  ▸     │  ← Click to expand menu
 └──────────┘
   ┌──────────────┐
   │ ⚙ Settings   │  ← Original functionality
   ├──────────────┤
   │ 🧩 Extensions│  ← New entry
   └──────────────┘
```

Implementation: Reuse ProjectSelector's popup pattern (homeRef + click outside close), changed to GearMenu component.

### 5.3 Extensions Panel

After clicking "Extensions", a panel slides out on the right (same level as SettingsPanel):

```
┌─────────────────────────────────────┐
│ Extensions                        ✕ │
├─────────────────────────────────────┤
│ 🔍 Search plugins...                │
├─────────────────────────────────────┤
│ [All] [Analysis] [Visualization]    │
│ [Search] [Productivity]             │
├─────────────────────────────────────┤
│ ┌─────────────────────────────────┐ │
│ │ [Icon] Hugging Face     v1.0   │ │
│ │        Manage local LLM models  │ │
│ │        by axons-community       │ │
│ │        status: running          │ │
│ │                   [⋯]           │ │  ← Dropdown: Enable/Disable/Uninstall
│ └─────────────────────────────────┘ │
│ ┌─────────────────────────────────┐ │
│ │ [Icon] Dep Tracker       v1.2   │ │
│ │        Analyze dependencies     │ │
│ │        by axons-community       │ │
│ │        status: stopped          │ │
│ │               [Start]           │ │
│ └─────────────────────────────────┘ │
│                                     │
│ ┌─────────────────────────────────┐ │
│ │ 📥 Import from File...          │ │  ← Offline import entry
│ └─────────────────────────────────┘ │
└─────────────────────────────────────┘
```

### 5.4 Plugin Card Data Structure

```typescript
interface PluginCard {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  icon: string;
  category: 'analysis' | 'visualization' | 'search' | 'productivity';
  status: 'starting' | 'running' | 'stopped' | 'crashed';
  endpoint: string;              // http://127.0.0.1:PORT
  frontend?: {
    entry: string;
    panels: PanelDef[];
  };
}
```

Post-install behavior:
- Plugin frontend.panels → ActivityBar dynamically renders icons
- Plugin frontend.tools → MCP tool list merge

### 5.5 Frontend Component Dynamic Loading

```tsx
// Plugin UI bundle (ui/index.js) exports React component
export function ModelManagerPanel({ pluginApi }) {
  // pluginApi.fetch('/api/models') → Desktop direct plugin backend / Web via axons proxy
  // pluginApi.onEvent('node:selected', handler) → EventBus subscription
  // pluginApi.emitEvent('model:ready', payload) → EventBus broadcast
}

// axons frontend loading — plugin UI static files served via axons /plugins/:id/ui/* route, no CORS
function PluginPanel({ plugin }) {
  const [Component, setComponent] = useState(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    import(`/plugins/${plugin.id}/${plugin.frontend.entry}`)
      .then(mod => setComponent(() => mod.default))
      .catch(err => setError(err.message));
  }, [plugin]);

  if (error) return <PluginLoadError plugin={plugin} error={error} />;
  if (!Component) return <Spinner />;

  // pluginApi construction — auto-selects direct/proxy based on runtime environment
  const pluginApi = createPluginApi({
    pluginId: plugin.id,
    endpoint: plugin.endpoint,
  });

  return (
    <PluginErrorBoundary pluginId={plugin.id}>
      <Component pluginApi={pluginApi} />
    </PluginErrorBoundary>
  );
}
```

#### Component Error Handling

```tsx
// 1. Load failure — JS file missing/corrupted/network error
function PluginLoadError({ plugin, error }) {
  return (
    <div className="plugin-error">
      <p>Plugin {plugin.name} failed to load</p>
      <p className="text-sm text-text-secondary">{error}</p>
      <button onClick={() => window.location.reload()}>Retry</button>
    </div>
  );
}

// 2. Render crash — Internal JS error in component, isolated by ErrorBoundary, doesn't affect axons main UI
class PluginErrorBoundary extends React.Component {
  state = { hasError: false, error: null };
  static getDerivedStateFromError(error) {
    return { hasError: true, error };
  }
  render() {
    if (this.state.hasError) {
      return (
        <div className="plugin-error">
          <p>Plugin render error</p>
          <p className="text-sm text-text-secondary">{this.state.error.message}</p>
          <button onClick={() => this.setState({ hasError: false })}>Retry</button>
        </div>
      );
    }
    return this.props.children;
  }
}
```

Error handling principles:
- Plugin UI crash **does not affect** axons main UI and other plugins
- Show error message + retry button, no silent failure
- ErrorBoundary catches render-time errors, import catch catches load-time errors
```

> Desktop special handling: Plugin UI files are placed under `~/.axons/plugins/:id/ui/`, axons's static handler adds `/plugins/*` route pointing to that directory.

### 5.6 Unified Hook

```tsx
// hooks/usePluginRegistry.ts
export function usePluginRegistry<T>(type: string): T[] {
  const [items, setItems] = useState<T[]>([]);
  useEffect(() => {
    fetch(`/v1/plugins/registry/${type}`).then(r => r.json()).then(setItems);
  }, []);
  return items;
}

// Agent consumes skill list
const pluginSkills = usePluginRegistry('skills');

// ActivityBar consumes panels
const pluginPanels = usePluginRegistry('panels');
```

### 5.7 Plugin UI Style Consistency: axons-plugin-ui Shared Component Library

#### Problem

Plugin frontend components run in axons's React runtime, but plugin developers don't know axons's design specifications, color system, or component styles, causing plugin panels to be visually disconnected from the axons main UI.

#### Solution: axons-plugin-ui Theme-Aware Component Library

Axons builds `axons-plugin-ui.umd.js`, mounts it on `window.AxonsPluginUI` at startup, and globally loads CSS variables and component styles. Plugin developers set `axons-plugin-ui` as external to reference it, no need to install npm packages or copy files. All resources are provided by the axons host at runtime via HTTP routes.

##### axons-Side Build Artifacts (distributed with axons)

```
dist/plugin-sdk/
├── axons-plugin-ui.umd.js   # UMD bundle, mounts window.AxonsPluginUI
├── theme.css                # CSS variables (colors, fonts, shadows, border-radius)
└── components.css           # Component styles (.axons-btn, .axons-card, etc.)
```

##### Source Location (within axons project)

```
ui/src/plugin-sdk/
├── index.tsx                 # Component source + exports
├── theme.css                 # CSS variable definitions
└── components.css            # Component style definitions
```

Build flow: `vite build` (main app) → `vite build --config vite.plugin-sdk.config.ts` (UMD library) → `cp` CSS files to dist.

#### Usage

```tsx
// Plugin src/MyPanel.tsx
import { Button, Card, Spinner, ProgressBar } from 'axons-plugin-ui';

export default function MyPanel({ pluginApi, onClose, panelId }) {
  return (
    <Card>
      <h2>My Plugin</h2>
      <ProgressBar value={0.65} />
      <Button variant="primary" onClick={() => pluginApi.fetch('/api/data')}>Execute</Button>
      <Spinner size="sm" />
    </Card>
  );
}
```

> Note: Plugins don't need `import 'axons-plugin-ui/theme.css'`, CSS is already globally loaded by axons in `index.html`.

#### Vite Configuration (Plugin Project)

```js
// vite.config.js
export default {
  build: {
    lib: {
      entry: 'src/MyPanel.tsx',
      formats: ['es'],
      fileName: () => 'index.js'
    },
    rollupOptions: {
      // Externalize: reuse axons runtime's existing React and AxonsPluginUI, don't bundle into plugin artifact
      external: ['react', 'react-dom', 'axons-plugin-ui'],
      output: {
        globals: {
          react: 'React',
          'react-dom': 'ReactDOM',
          'axons-plugin-ui': 'AxonsPluginUI'
        }
      }
    }
  }
};
```

#### Runtime Loading Mechanism

axons loads in the following order at startup, ensuring plugin `import { Button } from 'axons-plugin-ui'` resolves to the global variable:

1. **`index.html`** loads CSS:
   ```html
   <link rel="stylesheet" href="/plugin-sdk/theme.css" />
   <link rel="stylesheet" href="/plugin-sdk/components.css" />
   ```

2. **`main.tsx`** exposes React global variables and loads UMD bundle:
   ```ts
   import * as React from 'react';
   import * as ReactDOM from 'react-dom';
   (window as any).React = React;
   (window as any).ReactDOM = ReactDOM;

   const script = document.createElement('script');
   script.src = '/plugin-sdk/axons-plugin-ui.umd.js';  // Mounts window.AxonsPluginUI
   document.head.appendChild(script);
   ```

3. **Plugin `index.js`** when dynamically imported, `import from 'axons-plugin-ui'` automatically resolves to `window.AxonsPluginUI`.

#### CSS Variable System

axons's theme is defined through CSS variables. Plugins can directly use these variables to automatically follow theme changes (dark/light, etc.):

```css
/* Already globally loaded via /plugin-sdk/theme.css, plugins don't need to import separately */
:root {
  /* Backgrounds */
  --axons-color-void: #06060a;
  --axons-color-deep: #0a0a10;
  --axons-color-surface: #101018;
  --axons-color-elevated: #16161f;
  --axons-color-hover: #1c1c28;

  /* Borders */
  --axons-border-subtle: #1e1e2a;
  --axons-border-default: #2a2a3a;

  /* Text */
  --axons-text-primary: #e4e4ed;
  --axons-text-secondary: #8888a0;
  --axons-text-muted: #5a5a70;

  /* Accent */
  --axons-accent: #7c3aed;
  --axons-accent-dim: #5b21b6;

  /* Status Colors */
  --axons-success: #10b981;
  --axons-warning: #f59e0b;
  --axons-error: #ef4444;
  --axons-info: #3b82f6;

  /* Fonts */
  --axons-font-sans: 'Inter', system-ui, sans-serif;
  --axons-font-mono: 'JetBrains Mono', 'Fira Code', monospace;

  /* Shadows */
  --axons-shadow-glow: 0 0 20px rgba(124, 58, 237, 0.4);
  --axons-shadow-soft: 0 0 40px rgba(124, 58, 237, 0.15);

  /* Spacing */
  --axons-radius-sm: 4px;
  --axons-radius-md: 6px;
  --axons-radius-lg: 8px;
}
```

> Plugin developers should use these CSS variables for colors and fonts even if they don't use `axons-plugin-ui` components, rather than hardcoding color values.

#### Phased Plan

| Phase | Content | Status |
|------|------|------|
| Phase 1 | UMD build + CSS variables + basic components (Button/Card/Input/Select/Badge/Spinner/ProgressBar/Tabs), distributed with axons | ✅ Implemented |
| Phase 2 | Extended component library + theme switching support (light mode) + Storybook component docs site | To be planned |

### 5.8 Frontend Component Style Isolation

Plugin components share the React runtime and DOM with axons main UI, so style conflicts need to be prevented.

#### Recommended Approach: CSS Variables + Class Prefix (Phase 1)

Phase 1 recommends a lightweight approach without introducing Shadow DOM complexity:

**Rules**:
1. All plugin CSS classes add `plugin-{pluginId}-` prefix, e.g., `.plugin-com-axons-huggingface__card`
2. Use axons CSS variables for colors/fonts, don't hardcode color values
3. When using `axons-plugin-ui` component library, class prefix is handled internally by the library

```css
/* Plugin style example */
.plugin-com-axons-huggingface__card {
  background: var(--color-surface);
  border: 1px solid var(--color-border-default);
  border-radius: 6px;
  padding: 12px;
  color: var(--color-text-primary);
  font-family: var(--font-sans);
}

.plugin-com-axons-huggingface__button {
  background: var(--color-accent);
  color: white;
  padding: 6px 12px;
  border-radius: 4px;
}
```

**Tailwind Plugin Projects**: Can auto-add prefix via `prefix` configuration:

```js
// tailwind.config.js
export default {
  prefix: 'axp-',  // axons plugin prefix
  theme: {
    extend: {
      colors: {
        'surface': 'var(--color-surface)',
        'elevated': 'var(--color-elevated)',
        'accent': 'var(--color-accent)',
      }
    }
  }
}
```

```tsx
// Usage: class="axp-bg-surface axp-border axp-border-border-default axp-text-text-primary"
```

#### Advanced Approach: Shadow DOM (Phase 2)

Phase 2 can optionally support Shadow DOM for complete isolation, suitable for style-complex plugins:

```tsx
// axons frontend — PluginPanel supports Shadow DOM mode
function PluginPanel({ plugin }) {
  const shadowRef = useRef(null);

  useEffect(() => {
    const shadow = shadowRef.current.attachShadow({ mode: 'open' });
    // Inject axons theme CSS
    const style = document.createElement('style');
    style.textContent = axonsThemeCSS;  // Extracted from axons index.css
    shadow.appendChild(style);

    // Render plugin component within shadow
    const root = createRoot(shadow);
    root.render(<Component pluginApi={pluginApi} />);
  }, []);
}
```

> **Why not Shadow DOM in Phase 1**: React components inside Shadow DOM don't share the React runtime (require independent createRoot), which increases complexity and memory overhead. CSS variables + class prefix is sufficient for Phase 1 needs.

---

## 6. Cross-Panel Communication

### 6.1 Three-Layer Communication Model

```
Layer 1: Frontend EventBus (plugin↔plugin, plugin↔built-in panel)
  ├── Pure frontend, doesn't go through backend
  ├── System events: node:selected, file:opened, project:changed
  └── Plugin events: model:ready, dep-tracker:complete, ...

Layer 2: Shared State API (plugin backend↔plugin backend, plugin backend↔system)
  ├── /v1/plugins/system-state  → Current system state (read-only)
  ├── /v1/plugins/state/:key    → Cross-plugin shared KV (read/write, key auto-prefixed with pluginId for isolation)
  └── Cross-process, relayed through axons

Layer 3: axons API (plugin backend → system capabilities)
  ├── /v1/search, /v1/graph, /v1/stats → Read graph data
  ├── /v1/build, /v1/watch → Trigger operations
  └── With AXONS_PLUGIN_TOKEN authentication
```

### 6.2 EventBus Design

```typescript
// ui/src/lib/pluginEventBus.ts
interface PluginEvent {
  type: string;
  source: string;    // "builtin:graph" | "plugin:com.axons.xxx"
  payload: any;
}

class PluginEventBus {
  private handlers = new Map<string, Set<EventHandler>>();
  on(type: string, handler: EventHandler): () => void;    // Returns unsubscribe
  off(type: string, handler: EventHandler): void;
  emit(event: PluginEvent): void;
}
```

### 6.3 Built-in Panel Integration (Incremental)

```tsx
// GraphCanvas.tsx — add 2 lines
const handleNodeClick = (nodeId: string) => {
  setSelectedNode(nodeId);   // Original, unchanged
  eventBus.emit({ type: 'node:selected', source: 'builtin:graph', payload: { nodeId } });  // New
};
```

### 6.4 Scenario Reference

| Scenario | Layer | Example |
|------|--------|------|
| User selects node → plugin panel responds | Layer 1 EventBus | node:selected → plugin refreshes |
| Plugin A completes → Plugin B responds | Layer 1 EventBus | model:ready → analysis panel |
| Plugin backend reads current selected node | Layer 2 system-state | GET /v1/plugins/system-state |
| Plugin backend shares computation result | Layer 2 shared-state | PUT /v1/plugins/state/key |
| Plugin backend reads graph data | Layer 3 axons API | GET /v1/search |

---

## 7. Security Design

| Layer | Mechanism |
|------|------|
| Network isolation | Plugin process binds 127.0.0.1, not externally accessible |
| Authentication | AXONS_PLUGIN_TOKEN, validated when calling axons API |
| Permissions | plugin.json permissions whitelist, undeclared means unauthorized |
| Process isolation | Each plugin has independent process, crashes don't affect axons main process |
| Offline import | .axons-plugin.tar.gz signature verification (Phase 2) |
| Frontend isolation | Shadow DOM recommended (optional, not mandatory) |

---

## 8. Impact on Existing Code

| File | Change | Impact |
|------|------|------|
| `ActivityBar.tsx` | Settings button changed to GearMenu dropdown | Small |
| `App.tsx` | Add PluginRegistryProvider + dynamic Panel rendering | Medium (new code, doesn't change existing) |
| `Footer.tsx` | No change (Footer is not a plugin extension point) | None |
| `RightPanel.tsx` | Agent skill list merges plugin skills | Small (append data source) |
| `useAppState.ts` | Add isExtensionsPanelOpen state | Small |
| `api.ts` | Add plugin CRUD API functions | Small (pure addition) |
| `config.ts` | Add runtimeMode detection + getRuntimeMode() | Small (pure addition) |
| `server.go` | registerRoutes adds ~14 routes (including 6 proxy) | Small (pure addition) |
| New `internal/plugin/` | Brand new module (including proxy.go) | No changes to existing code |
| New `ui/src/lib/pluginApi.ts` | Brand new file (createPluginApi + resolveUrl) | No changes to existing code |
| New `ui/src/lib/pluginEventBus.ts` | Brand new file | No changes to existing code |
| New `ui/src/hooks/usePluginRegistry.ts` | Brand new file | No changes to existing code |

**Core Principle**: All plugin-related code is new addition rather than modifying existing logic. The hybrid communication approach (desktop direct + Web proxy) ensures both runtime environments work properly.

---

## 9. Phased Implementation Plan

### Phase 1: Basic Framework + Local Plugins

1. `manifest.json` protocol definition + validator
2. `internal/plugin/` core module (Manager + Registry + Process + Handlers)
3. Extensions panel UI (card list + offline import)
4. ActivityBar GearMenu refactoring
5. Frontend dynamic component loading + usePluginRegistry Hook
6. EventBus basic implementation + built-in panel emit integration
7. Develop 1-2 sample plugins to validate the complete chain

### Phase 2: Cloud Marketplace + Ecosystem

1. Cloud Marketplace API service
2. Plugin categorization / search / rating
3. Plugin signature and security review
4. Plugin auto-update mechanism
5. Developer CLI tool (axons plugin create / pack / publish)
6. Plugin UI SDK (axons-plugin-ui theme-aware component library)

---

## 10. Plugin Development & Debugging Guide

### 10.1 Quick Start: Create a Plugin in 5 Minutes

Example: Creating a "Hello Panel" frontend-only plugin:

```
# 1. Create plugin directory
mkdir -p ~/.axons/plugins/com.example.hello-panel/ui

# 2. Write manifest.json
cat > ~/.axons/plugins/com.example.hello-panel/manifest.json << 'EOF'
{
  "id": "com.example.hello-panel",
  "name": "Hello Panel",
  "version": "0.1.0",
  "description": "My first axons plugin",
  "author": "you",
  "category": "productivity",
  "minAxonsVersion": "0.8.0",
  "permissions": ["panel:create"],
  "backend": null,
  "frontend": {
    "entry": "ui/index.js",
    "panels": [{
      "id": "hello-panel",
      "title": "Hello",
      "icon": "ui/icon.svg",
      "location": "left",
      "activator": "activityBar"
    }]
  },
  "activationEvents": ["onStartup"]
}
EOF

# 3. Write frontend component (ESM format)
cat > ~/.axons/plugins/com.example.hello-panel/ui/index.js << 'EOF'
export default function HelloPanel({ pluginApi }) {
  const [time, setTime] = React.useState(new Date().toLocaleTimeString());
  React.useEffect(() => {
    const timer = setInterval(() => setTime(new Date().toLocaleTimeString()), 1000);
    return () => clearInterval(timer);
  }, []);
  return React.createElement('div', { style: { padding: '16px' } },
    React.createElement('h2', null, 'Hello from Plugin!'),
    React.createElement('p', null, 'Current time: ' + time)
  );
}
EOF

# 4. (Restart axons or trigger scan via API)
curl -X POST http://127.0.0.1:9090/v1/plugins/scan
```

axons scans `manifest.json` → registers `frontend.panels` → ActivityBar shows new icon → click to see the panel.

### 10.2 Plugin Project Directory Convention

Recommended plugin project structure (during development):

```
my-plugin/
├── manifest.json          # Required: plugin declaration
├── README.md              # Recommended: plugin documentation
├── install.sh             # Optional: install script
├── uninstall.sh           # Optional: uninstall script
├── server.py              # Optional: backend entry (any language)
├── requirements.txt       # Optional: backend dependencies
├── ui/                    # Frontend resource directory
│   ├── index.js           # Frontend component entry (ESM/UMD)
│   ├── icon.svg           # Panel icon
│   └── ...                # Other static resources
├── skills/                # Optional: skills contributed by plugin
│   └── my-skill/
│       └── SKILL.md
└── .axons-ignore          # Optional: files to exclude during packaging
```

**Key Constraints**:

| Item | Rule |
|------|------|
| `manifest.json` | Must exist in plugin root directory, filename is fixed |
| `id` | Reverse domain name format (`com.example.xxx`), globally unique |
| `frontend.entry` | Relative path, points to ESM or UMD module file |
| `backend.command` | Relative path or system command, working directory is plugin root |
| `ui/` directory | All frontend resources placed in this directory, axons serves statically via `/plugins/:id/*` route |

### 10.3 Frontend Component Development

#### Component Interface Specification

Plugin frontend components are provided as ESM default exports, axons injects the `pluginApi` object:

```tsx
// ui/index.js — Plugin frontend component
export default function MyPanel({ pluginApi }) {
  // pluginApi properties overview:
  //   pluginApi.endpoint    — Plugin backend address (e.g., "http://127.0.0.1:18080"), null when no backend
  //   pluginApi.pluginId    — Current plugin ID
  //   pluginApi.fetch(path, opts) — Desktop direct plugin backend / Web via axons proxy (auto-selected, plugin doesn't need to care)
  //   pluginApi.onEvent(type, handler)   — Subscribe to EventBus events (frontend in-memory events, not HTTP SSE)
  //   pluginApi.emitEvent(type, payload) — Send EventBus event
  //   pluginApi.createEventSource(path)  — Create SSE connection (desktop direct plugin backend / Web via axons proxy, same auto-selection as fetch)
  //   pluginApi.getState(key)    — Read shared state
  //   pluginApi.setState(key, value) — Write shared state

  return <div>My Plugin Panel</div>;
}
```

#### Frontend Tech Stack Selection

| Approach | Applicable Scenarios | Description |
|------|---------|------|
| **Native JS / JSX** | Lightweight panels | Zero build steps, write ESM directly |
| **React components** | Complex interactive panels | Recommended to use Vite to build as UMD/ESM, axons frontend is based on React 19 |
| **Web Component** | Framework-agnostic | Encapsulated with Shadow DOM, best style isolation |

**Recommended: React + Vite build**, because axons frontend itself is a React app, sharing the React runtime means smaller bundle size:

```js
// vite.config.js
export default {
  build: {
    lib: {
      entry: 'src/MyPanel.tsx',
      formats: ['es'],
      fileName: () => 'index.js'
    },
    rollupOptions: {
      external: ['react', 'react-dom'],  // Externalize React, reuse axons's instance
      output: {
        globals: { react: 'React', 'react-dom': 'ReactDOM' }
      }
    }
  }
};
```

#### pluginApi.fetch Details

`pluginApi.fetch(path, opts)` internally selects the communication path based on runtime environment (plugin developers don't need to care):
- Desktop: `fetch('http://127.0.0.1:{pluginPort}' + path)` — Direct plugin backend
- Web: `fetch('/v1/plugins/:id/proxy' + path)` — Forwarded via axons proxy to plugin backend

```tsx
// Call plugin's own backend — unified syntax, path doesn't need host
const models = await pluginApi.fetch('/api/models').then(r => r.json());

// Call axons system API (via backend relay)
// Recommended: plugin backend calls axons API, frontend calls plugin backend
// Simple cases: frontend can also call axons API directly
const graphData = await fetch('/v1/graph').then(r => r.json());
```

#### pluginApi.createEventSource Details

`pluginApi.createEventSource(path)` creates an SSE (Server-Sent Events) connection. It internally selects the connection path based on runtime environment, same branching logic as `pluginApi.fetch`:

- Desktop: `new EventSource('http://127.0.0.1:{pluginPort}' + path)` — Direct plugin backend
- Web: `new EventSource('/v1/plugins/:id/proxy' + path)` — Forwarded via axons proxy

```typescript
// lib/pluginApi.ts — createEventSource implementation
const createEventSource = (path: string): EventSource => {
  const url = resolveUrl(path);  // Reuse same URL branching logic as fetch
  return new EventSource(url);
};
```

**Usage Example**:

```tsx
// Streaming scenarios like download progress
export default function MyPanel({ pluginApi }) {
  const [progress, setProgress] = useState(0);

  const startPull = (model: string) => {
    const es = pluginApi.createEventSource(`/api/models/pull?model=${encodeURIComponent(model)}`);
    es.addEventListener('pull_progress', (e) => {
      const data = JSON.parse(e.data);
      if (data.total && data.completed) {
        setProgress(data.completed / data.total);
      }
    });
    es.addEventListener('pull_complete', () => {
      setProgress(1);
      es.close();
    });
    es.onerror = () => es.close();
  };
}
```

> **Note**: Plugin developers **should not** directly `new EventSource(pluginApi.endpoint + path)`, because on Web the endpoint is unreachable. Always use `pluginApi.createEventSource(path)` to ensure cross-environment compatibility.

#### EventBus Usage Example

```tsx
export default function DepAnalyzerPanel({ pluginApi }) {
  const [nodeId, setNodeId] = useState(null);

  useEffect(() => {
    // Listen for user selecting node in GraphCanvas
    const unsub = pluginApi.onEvent('node:selected', (payload) => {
      setNodeId(payload.nodeId);
    });
    return unsub; // Auto-unsubscribe on component unmount
  }, []);

  // Notify other panels when analysis completes
  const handleAnalysisComplete = (result) => {
    pluginApi.emitEvent('dep-analysis:complete', result);
  };

  return <div>Selected: {nodeId}</div>;
}
```

### 10.4 Backend Development

#### Backend Responsibilities

A plugin backend is an independent HTTP service with core responsibilities:

1. Expose `/health` endpoint for axons health checks
2. Implement business API for frontend `pluginApi.fetch()` calls
3. Call axons system API via `AXONS_API_URL` + `AXONS_PLUGIN_TOKEN`
4. (Optional) Report dynamic status via `POST /v1/plugins/registry/sync`
5. (Optional) Act as MCP Server providing tools to Agent

#### Environment Variables (injected by axons when starting plugin)

| Variable | Description | Example |
|------|------|------|
| `AXONS_API_URL` | axons API address | `http://127.0.0.1:9090` |
| `AXONS_PLUGIN_PORT` | Port allocated to plugin backend | `18080` |
| `AXONS_PLUGIN_TOKEN` | Authentication token | `axons_plg_a1b2c3d4` |
| `AXONS_PLUGIN_ID` | Current plugin ID | `com.axons.huggingface` |

#### Python Backend Template

```python
# server.py — Minimal plugin backend template
import os
import json
from http.server import HTTPServer, BaseHTTPRequestHandler

PORT = int(os.environ.get("AXONS_PLUGIN_PORT", "18080"))
AXONS_API = os.environ.get("AXONS_API_URL", "http://127.0.0.1:9090")
TOKEN = os.environ.get("AXONS_PLUGIN_TOKEN", "")

class Handler(BaseHTTPRequestHandler):
    def _cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Authorization, Content-Type")

    def do_OPTIONS(self):
        self.send_response(204)
        self._cors()
        self.end_headers()

    def do_GET(self):
        self._cors()
        if self.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(json.dumps({"status": "ok"}).encode())
        elif self.path == "/api/models":
            # Business logic: call axons API to get graph data
            # import urllib.request
            # req = urllib.request.Request(f"{AXONS_API}/v1/search?q=test")
            # req.add_header("Authorization", f"Bearer {TOKEN}")
            # ...
            self.send_response(200)
            self.end_headers()
            self.wfile.write(json.dumps({"models": []}).encode())
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        self._cors()
        if self.path == "/api/models/pull":
            content_length = int(self.headers["Content-Length"])
            body = json.loads(self.rfile.read(content_length))
            # Handle pull logic...
            self.send_response(200)
            self.end_headers()
            self.wfile.write(json.dumps({"status": "pulling"}).encode())

if __name__ == "__main__":
    server = HTTPServer(("127.0.0.1", PORT), Handler)
    print(f"Plugin backend listening on 127.0.0.1:{PORT}")
    server.serve_forever()
```

> Recommend using mature frameworks like FastAPI / Flask; above is a minimal-dependency example.

#### Go Backend Template

```go
// main.go — Minimal Go plugin backend template
package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"
)

var (
    axonsAPI = os.Getenv("AXONS_API_URL")
    token    = os.Getenv("AXONS_PLUGIN_TOKEN")
)

func main() {
    port := os.Getenv("AXONS_PLUGIN_PORT")
    if port == "" {
        port = "18080"
    }

    mux := http.NewServeMux()
    mux.HandleFunc("/health", healthHandler)
    mux.HandleFunc("/api/data", dataHandler)

    // CORS middleware
    handler := corsMiddleware(mux)

    fmt.Printf("Plugin backend listening on 127.0.0.1:%s\n", port)
    http.ListenAndServe("127.0.0.1:"+port, handler)
}

func corsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
        if r.Method == "OPTIONS" {
            w.WriteHeader(204)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
    // Call axons API example:
    // req, _ := http.NewRequest("GET", axonsAPI+"/v1/graph", nil)
    // req.Header.Set("Authorization", "Bearer "+token)
    // resp, err := http.DefaultClient.Do(req)
    // ...
    json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
}
```

### 10.5 Debugging Methods

#### 10.5.1 Development Mode: Symlink Hot Reload

During development, no need to repeatedly import plugin packages. Use symlinks to point directly to the development directory:

```bash
# 1. Create plugin project in development directory
mkdir -p ~/projects/my-plugin && cd ~/projects/my-plugin
# Write manifest.json, ui/index.js, server.py, etc.

# 2. Symlink to axons plugin directory
ln -s ~/projects/my-plugin ~/.axons/plugins/com.example.my-plugin

# 3. Start axons — auto-scan and load plugin
# Frontend component changes: refresh page to take effect (Vite HMR or manual refresh)
# Backend changes: need to restart plugin process
#   curl -X POST http://127.0.0.1:9090/v1/plugins/com.example.my-plugin/stop
#   curl -X POST http://127.0.0.1:9090/v1/plugins/com.example.my-plugin/start
```

**Advantage**: Edit code → stop/start plugin → verify immediately, no packaging/import needed.

#### 10.5.2 Backend Standalone Debugging

Plugin backend is an independent HTTP process that can be started and debugged separately from axons:

```bash
# Manually set environment variables, start backend independently
export AXONS_API_URL=http://127.0.0.1:9090
export AXONS_PLUGIN_PORT=18080
export AXONS_PLUGIN_TOKEN=dev-test-token
export AXONS_PLUGIN_ID=com.example.my-plugin

# Start backend (can use IDE debug mode)
python server.py
# or
go run main.go

# Test API directly with curl
curl http://127.0.0.1:18080/health
curl http://127.0.0.1:18080/api/models

# Debug with IDE breakpoints
# Python: python -m debugpy --listen 5678 server.py
# Go:     dlv debug main.go
```

**Key**: `AXONS_PLUGIN_PORT` can specify a fixed port (instead of 0), convenient for stable debugging during development.

```jsonc
// Development manifest.json — specify fixed port
{
  "backend": {
    "command": ["python", "server.py"],
    "port": 18080,  // Fixed port, convenient for curl/Postman debugging
    "healthCheck": "/health"
  }
}
```

#### 10.5.3 Frontend Debugging

**Method 1: Browser DevTools**

axons is based on Electron, you can open DevTools:

1. Desktop app: `Cmd+Option+I` (macOS) / `F12` (Windows/Linux)
2. Check plugin loading logs in Console
3. Find `/plugins/:id/ui/index.js` in Sources panel, set breakpoints for debugging

**Method 2: Standalone Browser Preview**

Frontend components can be developed independently in a browser outside axons:

```bash
# Start dev server with Vite in plugin project directory
cd ~/projects/my-plugin/ui-src
npm run dev  # http://localhost:5173

# After development, build to ui/index.js
npm run build
```

Use Mock `pluginApi` during development:

```tsx
// src/dev-mock.tsx — Development only
const mockPluginApi = {
  endpoint: "http://127.0.0.1:18080",
  pluginId: "com.example.my-plugin",
  fetch: (path: string, opts?: RequestInit) => fetch("http://127.0.0.1:18080" + path, opts),
  onEvent: (type: string, handler: Function) => {
    console.log(`[Mock] Subscribed to ${type}`);
    return () => console.log(`[Mock] Unsubscribed from ${type}`);
  },
  emitEvent: (type: string, payload: any) => {
    console.log(`[Mock] Emitted ${type}:`, payload);
  },
  getState: (key: string) => Promise.resolve(null),
  setState: (key: string, value: any) => Promise.resolve(undefined),
};

// Dev mode entry
function App() {
  return <MyPanel pluginApi={mockPluginApi} />;
}
```

**Method 3: Log Investigation**

```tsx
// Use pluginApi.emitEvent inside plugin component to send debug logs
// axons frontend can filter "plugin:" prefix events in Console
useEffect(() => {
  pluginApi.emitEvent('plugin:debug', {
    message: 'Component mounted',
    data: { /* ... */ }
  });
}, []);
```

#### 10.5.4 Common Issue Troubleshooting

| Issue | Troubleshooting Method |
|------|---------|
| Plugin doesn't show in ActivityBar | Check `manifest.json` format: `frontend.panels[0].activator === "activityBar"`, `location` is `left`/`right`; check axons logs for manifest validation errors |
| Panel fails to load (blank/error) | Open DevTools Console to check import errors; confirm `frontend.entry` path is correct; confirm JS file is valid ESM/UMD module |
| Backend health check timeout | Manually `curl http://127.0.0.1:PORT/health`; check `healthCheck` path matches; check `readyTimeout` is sufficient; check backend process logs |
| Backend calls axons API 401 | Check request header `Authorization: Bearer ${AXONS_PLUGIN_TOKEN}`; check `permissions` declares required permissions |
| EventBus events not received | Confirm `pluginApi.onEvent` returns unsubscribe function; confirm event name spelling; filter `plugin:` events in DevTools Console |
| Plugin crashes and auto-restarts | Check axons logs for `plugin.crashed` events; check backend process stderr output; manually start backend process to reproduce |
| Frontend component render crash | DevTools check React ErrorBoundary captured errors; wrap suspicious code with `try/catch`; temporarily simplify component to locate issue |
| CORS error | Confirm backend response includes `Access-Control-Allow-Origin: *` header; confirm OPTIONS preflight request returns 204 |

#### 10.5.5 Logging & Observability

```
axons logs (stderr / file)
  ├── [plugin-manager] Scanning ~/.axons/plugins/...
  ├── [plugin-manager] Starting plugin com.example.my-plugin (port=18080)
  ├── [plugin-manager] Health check passed for com.example.my-plugin
  ├── [plugin-manager] Plugin com.example.my-plugin crashed (restarts=1/3)
  └── [plugin-manager] Plugin com.example.my-plugin exceeded max restarts

Plugin backend logs (stdout/stderr → captured by axons)
  ├── Plugin backend listening on 127.0.0.1:18080
  ├── GET /api/models → 200 (3ms)
  └── Error: connection refused (ollama not running)

Frontend Console
  ├── [PluginRegistry] Loaded 2 panels, 1 commands from plugin com.example.my-plugin
  ├── [PluginPanel] Error loading component: SyntaxError ...
  └── [EventBus] plugin:com.example.my-plugin → model:ready
```

### 10.6 Packaging & Distribution

#### Package as .axons-plugin.tar.gz

```bash
# Execute in plugin project root directory
cd ~/projects/my-plugin
tar -czf my-plugin-1.0.0.axons-plugin.tar.gz \
  --exclude='.git' \
  --exclude='node_modules' \
  --exclude='.venv' \
  --exclude='*.pyc' \
  manifest.json ui/ server.py requirements.txt install.sh

# Verify package contents
tar -tzf my-plugin-1.0.0.axons-plugin.tar.gz
```

**Packaging Requirements**:

| Item | Rule |
|------|------|
| `manifest.json` | Must be in package root directory |
| `ui/` | Frontend resources must be built artifacts (not source) |
| `.axons-ignore` | Similar to `.gitignore`, excludes development files |
| Package size | Recommended < 50MB, oversized packages have high install timeout risk |
| Executable files | Must include binaries for corresponding platform, or build during install via `install.sh` |

#### Installation Methods

```bash
# Method 1: Offline import via API
curl -X POST http://127.0.0.1:9090/v1/plugins/import \
  -F "file=@my-plugin-1.0.0.axons-plugin.tar.gz"

# Method 2: Drag and drop to Extensions panel import area

# Method 3: Symlink (development)
ln -s ~/projects/my-plugin ~/.axons/plugins/com.example.my-plugin
```

### 10.7 Developer CLI (Phase 2)

Phase 2 will provide `axons plugin` subcommands to simplify development and distribution:

```bash
# Scaffold: interactively create plugin project
axons plugin create --template python-react  # or go-react / pure-frontend
# → Generate manifest.json + directory structure + backend template + frontend template + dev mock

# Validate: check manifest.json validity
axons plugin validate

# Package: build .axons-plugin.tar.gz
axons plugin pack

# Publish: upload to cloud marketplace (Phase 2)
axons plugin publish

# Local dev: start axons with hot-load for specified plugin
axons plugin dev ./my-plugin
# → Auto-create symlink + start axons + watch file changes + auto-restart plugin backend
```

---

## 11. Comparison with VSCode Architecture

| | VSCode Extension Host | Axons Plugin System |
|---|---|---|
| Runtime | Single-process Node.js, all plugins share | Each plugin independent process, any language |
| Communication | IPC/JSON-RPC via VS Code relay | Desktop direct / Web via axons proxy |
| UI | Declarative (WebviewPanel/iframe) | React component direct mount |
| Language | JS/TS only | Any (Python/Go/Rust/...) |
| Cross-plugin communication | commands.executeCommand | EventBus + Shared state API |
| State sync | None | Unified registry + runtime dynamic sync |
| Extension points | Fixed (VS Code predefined) | Extensible (frontend custom extension types) |

**Core Philosophy**: VSCode is designed for IDEs (strong UI control, JS ecosystem); Axons is designed for AI-First workbanch (multi-language, process isolation, panel collaboration), with hybrid communication balancing desktop performance and Web compatibility.
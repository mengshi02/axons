# Axons Host Internationalization Refactoring Plan

> Version: v1.0 | Date: 2026-05-16 | Status: Implemented

## 1. Goals

Extract hardcoded English text from the Axons host (frontend UI + CLI + backend API) into i18n resources, achieving:
- Default English (embedded, works with zero configuration)
- Other languages supported via language pack plugins (see companion document "Language Pack Plugin Design and Implementation")
- Frontend updates instantly after language switch; CLI/API reads language preference

**Out of scope for translation**:
- LLM-generated Chat content (language determined by the model itself)
- API JSON field names and status enum values (`running`/`stopped`, etc. — these are protocol conventions)
- Code identifiers and file paths

## 2. Technology Selection

### 2.1 Frontend: react-i18next

| Dimension | Choice | Rationale |
|-----------|--------|-----------|
| Core framework | `i18next` + `react-i18next` | Community mainstream, de facto standard in React ecosystem, IDE also uses this approach |
| Language pack loading | `i18next-http-backend` | On-demand async loading of language resources, avoids loading all languages on first screen |
| Language detection | `i18next-browser-languagedetector` | Auto-detects browser language, supports localStorage persistence |
| Interpolation | i18next built-in | `{{count}}` / `{{name}}` interpolation, supports plural forms |
| Namespaces | Split by module | Avoids oversized single files, supports on-demand loading |

**Install dependencies**:
```bash
npm install i18next react-i18next i18next-http-backend i18next-browser-languagedetector
```

### 2.2 Backend Go: Lightweight Custom i18n Bundle

Not introducing `golang.org/x/text` (too heavy), building a minimal custom solution:

| Component | Implementation | Description |
|-----------|---------------|-------------|
| Language pack format | `en.toml` / `zh-CN.toml` | TOML has good readability, Go standard library needs no extra dependencies |
| Default language pack | `//go:embed` embedded | English embedded in binary, zero external dependencies |
| Translation function | `i18n.T(key, args...)` | Simple key-value lookup + `{{var}}` interpolation |
| Language preference source | Settings DB → `AXONS_LOCALE` env var | Priority: Settings > env var > default en |

## 3. Frontend Refactoring

### 3.1 Directory Structure

```
ui/src/
├── i18n/
│   ├── index.ts              # i18next initialization config
│   ├── en/                   # Default English language pack (embedded)
│   │   ├── common.json       # Common text: buttons, status, errors
│   │   ├── settings.json     # Settings panel
│   │   ├── panels.json       # Analysis panels
│   │   ├── chat.json         # AI Chat panel
│   │   ├── activitybar.json  # ActivityBar + Footer
│   │   ├── dropzone.json     # Landing page / DropZone
│   │   └── extensions.json   # Extension management panel
│   └── zh-CN/                # Chinese (provided by language pack plugin, not in main repo)
│       └── ...               # Same structure
```

### 3.2 i18next Initialization

```typescript
// ui/src/i18n/index.ts
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import HttpBackend from 'i18next-http-backend';
import LanguageDetector from 'i18next-browser-languagedetector';

// Embedded English resources (ensures offline availability + zero latency)
import common from './en/common.json';
import settings from './en/settings.json';
import panels from './en/panels.json';
import chat from './en/chat.json';
import activitybar from './en/activitybar.json';
import dropzone from './en/dropzone.json';
import extensions from './en/extensions.json';

const enResources = {
  common, settings, panels, chat, activitybar, dropzone, extensions,
};

i18n
  .use(HttpBackend)           // Async load other languages (provided by plugins)
  .use(LanguageDetector)      // Browser language detection
  .use(initReactI18next)      // React binding
  .init({
    resources: {
      en: enResources,        // English embedded, zero network requests on first screen
    },
    fallbackLng: 'en',
    ns: ['common', 'settings', 'panels', 'chat', 'activitybar', 'dropzone', 'extensions'],
    defaultNS: 'common',
    interpolation: { escapeValue: false },  // React already escapes
    backend: {
      // Language pack plugin resource path (dynamic function, see 4.8.3 for details)
      // en is embedded and doesn't use network; other languages served by daemon static route (HandlePluginStaticFiles)
      loadPath: (lngs: string[], namespaces: string[]) => {
        const lng = lngs[0];
        if (lng === 'en') return '';  // en is embedded, no loading needed

        // Look up the plugin ID for this language
        // Mapping source: GET /v1/plugins/locales (provided by language pack plugin scheme)
        const pluginId = localePluginMap[lng];
        if (!pluginId) return '';  // Language pack not installed

        return `/plugins/${pluginId}/locales/frontend/{{ns}}.json`;
      },
    },
    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: 'axons-locale',
      caches: ['localStorage'],
    },
  });

export default i18n;
```

**Timing of language→plugin ID mapping initialization**: `localePluginMap` must be fetched before i18next initializes, otherwise `changeLanguage` triggered http-backend loading won't find `pluginId`. Solution:

```typescript
// ui/src/main.tsx — fetch mapping first, then mount React
async function bootstrap() {
  // 1. Fetch language→plugin mapping (i18next initialization needs this data)
  const resp = await fetch('/v1/plugins/locales');
  const { locales } = await resp.json();
  window.__localePluginMap = Object.fromEntries(
    Object.entries(locales).map(([code, info]) => [code, info.pluginId])
  );

  // 2. Mount React (i18n/index.ts is already initialized when imported)
  ReactDOM.createRoot(document.getElementById('root')!).render(<App />);
}
bootstrap();
```

> If the mapping fetch fails (e.g., network error), the i18next loadPath function returns an empty string, and `changeLanguage` will fallback to en, not affecting basic functionality.

### 3.3 Key Naming Convention

Use `semantic path` naming, not component names (components may be refactored, semantics are relatively stable):

```jsonc
// en/common.json
{
  "action": {
    "save": "Save",
    "cancel": "Cancel",
    "close": "Close",
    "refresh": "Refresh",
    "delete": "Delete",
    "confirm": "Confirm",
    "edit": "Edit",
    "start": "Start",
    "stop": "Stop",
    "install": "Install",
    "uninstall": "Uninstall",
    "import": "Import",
    "testing": "Testing...",
    "saving": "Saving...",
    "loading": "Loading..."
  },
  "status": {
    "running": "Running",
    "stopped": "Stopped",
    "starting": "Starting",
    "crashed": "Crashed",
    "installed": "Installed",
    "imported": "Imported",
    "configured": "Configured",
    "notConfigured": "Not Configured",
    "installing": "installing"
  },
  "unit": {
    "nodes_one": "{{count}} node",
    "nodes_other": "{{count}} nodes",
    "edges_one": "{{count}} edge",
    "edges_other": "{{count}} edges",
    "callers_one": "{{count}} caller",
    "callers_other": "{{count}} callers",
    "callees_one": "{{count}} callee",
    "callees_other": "{{count}} callees",
    "embeddings_one": "{{count}} embedding",
    "embeddings_other": "{{count}} embeddings"
  },
  "error": {
    "loadFailed": "Failed to load data",
    "connectionFailed": "Connection failed",
    "unknownError": "Unknown error",
    "importFailed": "Import failed: {{message}}"
  }
}
```

```jsonc
// en/settings.json
{
  "title": "Settings",
  "tab": {
    "theme": "Theme",
    "embedding": "Embedding",
    "llm": "LLM",
    "rerank": "Rerank",
    "rag": "RAG",
    "language": "Language"
  },
  "theme": {
    "description": "Choose your preferred theme. The moon theme is dark with purple accents, perfect for night use. The sun theme is light and bright, ideal for daytime.",
    "moon": { "name": "Moon", "desc": "Dark & purple" },
    "sun": { "name": "Sun", "desc": "Light & bright" }
  },
  "embedding": {
    "enable": "Enable auto-embedding after build",
    "provider": "Provider",
    "apiKey": "API Key",
    "model": "Model",
    "baseUrl": "Base URL",
    "testConnection": "Test Connection",
    "maxContextTokens": "Max Context Tokens (n_ctx)",
    "maxContextTokensHint": "Set to 0 to use default (512 tokens). Required for local services where n_ctx cannot be queried.",
    "selectProvider": "Select a provider",
    "enterApiKey": "Enter your API key",
    "modelName": "Model name",
    "connected": "Connected! Model: {{model}}, Dimension: {{dimension}}"
  },
  "llm": {
    "enable": "Enable LLM Agent",
    "enableDesc": "Allow AI assistant to answer questions",
    "noModels": "No models configured yet. Click + to add one.",
    "addModel": "Add Model",
    "newModel": "New Model",
    "defaultEndpoint": "default endpoint",
    "multimodal": "multimodal",
    "unnamed": "Unnamed"
  }
}
```

```jsonc
// en/panels.json
{
  "codeHealth": {
    "title": "Code Health",
    "tab": {
      "hotspots": "Hotspots",
      "deadcode": "Dead Code",
      "cochange": "Co-Change"
    },
    "hotspotDesc": "Functions with highest call coupling (fan-in × 2 + fan-out)",
    "noHotspots": "No hotspots found",
    "unreachable": "Unreachable functions ({{count}})",
    "noDeadCode": "No unreachable code found",
    "unusedExports": "Unused exports ({{count}})",
    "noUnusedExports": "No unused exports found",
    "score": "score"
  },
  "graphAnalytics": {
    "title": "Graph Analytics",
    "tab": {
      "metrics": "Metrics",
      "pagerank": "PageRank",
      "communities": "Modules",
      "cycles": "Cycles"
    },
    "nodes": "Nodes",
    "edges": "Edges",
    "communities": "Communities",
    "cyclesScc": "Cycles (SCCs)",
    "degree": "Degree",
    "avgInDegree": "Avg In-Degree",
    "avgOutDegree": "Avg Out-Degree",
    "maxInDegree": "Max In-Degree",
    "maxOutDegree": "Max Out-Degree",
    "structure": "Structure",
    "density": "Density",
    "largestScc": "Largest SCC",
    "modularity": "Modularity",
    "isDag": "Is DAG",
    "pagerankDesc": "Most important nodes by random-walk probability (higher = more central)",
    "noData": "No data. Build the graph first."
  },
  "impact": {
    "title": "Impact Analysis",
    "tab": { "impact": "Impact", "callchain": "Call Chain" },
    "depth": "Depth",
    "all": "All",
    "selectFrom": "From",
    "selectTo": "To",
    "findChain": "Find Chain",
    "enterValidNodeIds": "Please enter valid node IDs",
    "affectedNodes": "Affected nodes",
    "directCallers": "Direct callers"
  },
  "cfg": {
    "title": "CFG / Dataflow",
    "tab": { "cfg": "CFG", "dataflow": "Dataflow" }
  },
  "sequence": {
    "title": "Sequence",
    "enterFunction": "Enter function name",
    "depth": "Depth",
    "generate": "Generate",
    "copyMermaid": "Copy Mermaid"
  },
  "rules": {
    "title": "Architecture Rules",
    "ruleName": "Rule name *",
    "noRules": "No rules defined yet"
  },
  "flow": {
    "title": "Process Flow",
    "detect": "Detect",
    "noProcesses": "No processes detected"
  }
}
```

```jsonc
// en/chat.json
{
  "newConversation": "New Conversation",
  "chatHistory": "Chat history",
  "placeholder": "Ask about your code...",
  "attachImage": "Attach image",
  "stopGenerating": "Stop generating",
  "deleteConversation": "Delete Conversation",
  "confirmDelete": "Are you sure? This cannot be undone.",
  "semanticSearch": "Semantic Search",
  "searchPlaceholder": "Search code semantically...",
  "relevanceScore": "Relevance score",
  "noResults": "No results found",
  "agentThinking": "Thinking...",
  "agentRunning": "Running {{tool}}...",
  "agentDone": "Done ({{ms}}ms)",
  "agentError": "Error",
  "rateLimitError": "Rate limit exceeded. Please wait and try again.",
  "authError": "Authentication failed. Please check your API key.",
  "serverError": "Server error. Please try again later."
}
```

```jsonc
// en/activitybar.json
{
  "projects": "Projects",
  "showFilePanel": "Show File Panel",
  "hideFilePanel": "Hide File Panel",
  "aiAssistant": "AI Assistant",
  "menu": "Menu",
  "settings": "Settings",
  "extensions": "Extensions",
  "terminal": "Terminal",
  "files": "Files",
  "code": "Code"
}
```

```jsonc
// en/dropzone.json
{
  "title": "Axons",
  "subtitle": "Visualize and explore your codebase as an interactive graph",
  "recentImports": "Recent Imports",
  "removeFromHistory": "Remove from history",
  "dropFolder": "Drop a folder here or click to browse",
  "supportedLanguages": "Supports Go, TypeScript, JavaScript, Python, and more",
  "or": "or",
  "importing": "Importing...",
  "importFrom": "Import from Local Path or Remote URL",
  "importHint": "Import from local directory or clone from GitHub",
  "features": {
    "visualize": { "title": "Visualize", "desc": "Interactive graph view" },
    "explore": { "title": "Explore", "desc": "Search & navigate" },
    "ai": { "title": "AI Assistant", "desc": "Agent powered" }
  },
  "building": {
    "title": "Building Knowledge Graph",
    "analyzing": "Analyzing {{name}}",
    "parsing": "Parsing source files...",
    "extracting": "Extracting code structure...",
    "building": "Building relationships...",
    "hint": "This may take a few moments depending on project size"
  }
}
```

```jsonc
// en/extensions.json
{
  "title": "Extensions",
  "noExtensions": "No extensions found",
  "placePlugins": "Place plugins in ~/.axons/plugins/",
  "importPlugin": "Import plugin from .tar.gz",
  "scanPlugins": "Scan for new plugins",
  "onlyTarGz": "Only .tar.gz archives are supported",
  "installLog": "Install Log",
  "uninstallTitle": "Uninstall Plugin",
  "uninstallMessage": "Are you sure you want to uninstall this plugin? This action cannot be undone.",
  "startingInstall": "Starting installation...",
  "installComplete": "Installation completed successfully.",
  "installFailed": "Installation failed: {{error}}",
  "category": {
    "all": "All",
    "analysis": "Analysis",
    "visualization": "Visualization",
    "search": "Search",
    "productivity": "Productivity"
  }
}
```

### 3.4 Component Refactoring Example

**Before refactoring**:
```tsx
// CodeHealthPanel.tsx
<span className="text-sm font-semibold text-text-primary">Code Health</span>
<span>No hotspots found</span>
<span>{h.fan_in} callers</span>
```

**After refactoring**:
```tsx
import { useTranslation } from 'react-i18next';

function CodeHealthPanel() {
  const { t } = useTranslation('panels');
  
  return (
    <span className="text-sm font-semibold text-text-primary">{t('codeHealth.title')}</span>
    <span>{t('codeHealth.noHotspots')}</span>
    <span>{t('common:unit.callers', { count: h.fan_in })}</span>
  );
}
```

**ConfirmDialog refactoring**: ConfirmDialog's `title`/`message`/`confirmLabel` are passed in as English strings by callers. After refactoring, callers pass i18n keys or translated strings instead:

```tsx
// Before: caller passes hardcoded English
<ConfirmDialog title="Uninstall Plugin" message="Are you sure?" confirmLabel="Uninstall" />

// After: caller passes t() translated strings
const { t } = useTranslation('extensions');
<ConfirmDialog
  title={t('uninstallTitle')}
  message={t('uninstallMessage')}
  confirmLabel={t('action:uninstall')}
/>
```

### 3.5 Plural Form Handling

i18next supports Unicode CLDR plural rules. The `unit` namespace in `en/common.json` needs to distinguish singular and plural forms:

```jsonc
// en/common.json — unit namespace refactoring
"unit": {
  "nodes_one": "{{count}} node",
  "nodes_other": "{{count}} nodes",
  "edges_one": "{{count}} edge",
  "edges_other": "{{count}} edges",
  "callers_one": "{{count}} caller",
  "callers_other": "{{count}} callers",
  "callees_one": "{{count}} callee",
  "callees_other": "{{count}} callees",
  "embeddings_one": "{{count}} embedding",
  "embeddings_other": "{{count}} embeddings"
}
```

```tsx
// Usage: i18next automatically selects _one/_other suffix based on count value
<span>{t('common:unit.nodes', { count: 1 })}</span>   // → "1 node"
<span>{t('common:unit.nodes', { count: 5 })}</span>   // → "5 nodes"
```

> Languages like Chinese that don't distinguish plural forms only need to provide the `_other` suffix key; i18next will automatically fallback.

### 3.6 Panel Registry Refactoring

Panel titles in [`useAppState.ts`](../ui/src/hooks/useAppState.ts:698) are static strings that need to be changed to i18n keys:

**Before refactoring**:
```tsx
registerPanel({ id: 'codeHealth', title: 'Health', ... });
```

**After refactoring**:
```tsx
// Approach: change title to i18n key, translate at render time
registerPanel({ id: 'codeHealth', title: 'panels:codeHealth.title', ... });

// Footer / ActivityBar rendering:
const { t } = useTranslation();
<span>{t(panel.title)}</span>  // t() automatically handles namespace prefix
```

### 3.7 Language Switch UI

Add a **Language** tab in the Settings panel (next to Theme):

```tsx
// SettingsPanel.tsx — new Language tab
<button onClick={() => setActiveTab('language')} className={tabClass}>
  <Globe className="w-3.5 h-3.5" />
  Language
</button>

// Language tab content
{activeTab === 'language' && (
  <div className="space-y-4">
    <p className="text-sm text-text-secondary">
      Select your preferred language. Additional languages can be installed via extension plugins.
    </p>
    <div className="grid grid-cols-2 gap-3">
      {availableLanguages.map(lang => (
        <button
          key={lang.code}
          onClick={() => { i18n.changeLanguage(lang.code); saveLocaleToSettings(lang.code); }}
          className={langBtnClass(lang.code === i18n.language)}
        >
          <span className="text-sm font-medium">{lang.nativeName}</span>
          <span className="text-xs text-text-muted">{lang.englishName}</span>
        </button>
      ))}
    </div>
    {availableLanguages.length <= 1 && (
      <p className="text-xs text-text-muted">
        Only English is available by default. Install a language pack extension for more languages.
      </p>
    )}
  </div>
)}
```

**Available language list source**:
- By default only `en`
- After installing a language plugin, the plugin registers locale resources in i18next, and the language list automatically expands
- Backend `GET /v1/settings` returns an `available_locales` field

### 3.8 File Change List

| File | Change Type | Description |
|------|-------------|-------------|
| `ui/src/i18n/index.ts` | New | i18next initialization + **http-backend language pack resource path adaptation** |
| `ui/src/i18n/en/*.json` | New (7 files) | English language packs |
| `ui/src/main.tsx` | Modified | Add `import './i18n'` |
| `ui/src/hooks/useAppState.ts` | Modified | Panel title changed to i18n key |
| `ui/src/components/SettingsPanel.tsx` | Modified | Text → `t()`, add Language tab + **listen for `locale.available` / `locale.unavailable` SSE events** |
| `ui/src/components/RightPanel.tsx` | Modified | Chat UI text → `t()` |
| `ui/src/components/DropZone.tsx` | Modified | Landing page text → `t()` |
| `ui/src/components/BuildingState.tsx` | Modified | Progress text → `t()` |
| `ui/src/components/CodeHealthPanel.tsx` | Modified | Panel text → `t()` |
| `ui/src/components/GraphAnalyticsPanel.tsx` | Modified | Panel text → `t()` |
| `ui/src/components/ImpactAnalysisPanel.tsx` | Modified | Panel text → `t()` |
| `ui/src/components/CfgDataflowPanel.tsx` | Modified | Panel text → `t()` |
| `ui/src/components/SequencePanel.tsx` | Modified | Panel text → `t()` |
| `ui/src/components/ArchRulesPanel.tsx` | Modified | Panel text → `t()` |
| `ui/src/components/ProcessPanel.tsx` | Modified | Panel text → `t()` |
| `ui/src/components/ExtensionsPanel.tsx` | Modified | Extension panel text → `t()` |
| `ui/src/components/ProjectSelector.tsx` | Modified | Project selector text → `t()` |
| `ui/src/components/ActivityBar.tsx` | Modified | title attribute → `t()` |
| `ui/src/components/Footer.tsx` | Modified | nodes/edges text → `t()` |
| `ui/src/components/TopSearchBar.tsx` | Modified | Search placeholder → `t()` |
| `ui/src/components/AgentManagerPanel.tsx` | Modified | Agent management text → `t()` |
| `ui/src/components/FileTreePanel.tsx` | Modified | Context menu → `t()` |
| `ui/src/components/ChangeList.tsx` | Modified | Change list text → `t()` |
| `ui/src/components/ConfirmDialog.tsx` | Modified | Confirm dialog → `t()` |
| `ui/src/components/UnifiedImportDialog.tsx` | Modified | Import dialog → `t()` |
| `ui/src/components/terminal/TerminalPanel.tsx` | Modified | Terminal panel → `t()` |
| `ui/src/lib/panelRegistry.ts` | Modified | PanelDef.title type annotation update |

## 4. Backend Refactoring

### 4.1 Directory Structure

```
internal/i18n/
├── i18n.go            # Bundle + T() function
├── locales/
│   ├── en.toml        # Default English (go:embed embedded)
│   └── zh-CN.toml     # Chinese (dynamically loaded when language pack plugin provides it)
```

### 4.2 Core Implementation

```go
// internal/i18n/i18n.go
package i18n

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

//go:embed locales/en.toml
var defaultFS embed.FS

var (
	mu       sync.RWMutex
	bundles  = map[string]map[string]string{} // locale → key → value
	locale   = "en"
)

func init() {
	// Load embedded English
	loadFromFS("en", defaultFS, "locales/en.toml")
}

// T translates a key with optional interpolation.
// Keys use dot notation: "cmd.build.short"
func T(key string, args ...any) string {
	mu.RLock()
	defer mu.RUnlock()

	bundle, ok := bundles[locale]
	if !ok {
		bundle = bundles["en"]
	}

	template, ok := bundle[key]
	if !ok {
		// fallback to en
		if bundle = bundles["en"]; bundle != nil {
			template, ok = bundle[key]
		}
		if !ok {
			return key // key itself as fallback
		}
	}

	if len(args) > 0 {
		// Support {{name}} interpolation
		if m, ok := args[0].(map[string]string); ok {
			result := template
			for k, v := range m {
				result = strings.ReplaceAll(result, "{{"+k+"}}", v)
			}
			return result
		}
		return fmt.Sprintf(template, args...)
	}
	return template
}

// SetLocale changes the active locale.
func SetLocale(l string) {
	mu.Lock()
	defer mu.Unlock()
	locale = l
}

// LoadBundle loads a locale bundle from a directory.
// Supports runtime incremental loading (called after language pack plugin install, no restart needed).
func LoadBundle(locale, dir string) error {
	// Scan .toml files under dir, merge into bundles[locale]
	// ...
}

// UnloadBundle removes a locale bundle from memory.
// Called when language pack plugin is uninstalled, frees memory + ensures subsequent T() calls fallback to en.
func UnloadBundle(locale string) {
	mu.Lock()
	defer mu.Unlock()
	delete(bundles, locale)
}

// GetLocale returns the current active locale.
func GetLocale() string {
	mu.RLock()
	defer mu.RUnlock()
	return locale
}

func loadFromFS(locale string, fs embed.FS, path string) {
	data, err := fs.ReadFile(path)
	if err != nil { return }
	var m map[string]string
	if err := toml.Unmarshal(data, &m); err != nil { return }
	mu.Lock()
	bundles[locale] = m
	mu.Unlock()
}
```

### 4.3 English Language Pack

```toml
# internal/i18n/locales/en.toml

# CLI command descriptions
"cmd.root.short" = "Axons - A code graph analysis tool"
"cmd.root.long" = """Axons is a powerful code graph analysis tool that helps you understand
and navigate your codebase."""

"cmd.build.short" = "Build the code graph"
"cmd.build.long" = "Build a code graph from the specified directory (no daemon required)."
"cmd.query.short" = "Query the code graph"
"cmd.search.short" = "Search code using semantic, keyword, or hybrid search"
"cmd.audit.short" = "Run comprehensive code audit"
"cmd.cfg.short" = "Show control flow graph for a function"
"cmd.owners.short" = "Show CODEOWNERS mapping for files and functions"
"cmd.path.short" = "Find call paths between two symbols"
"cmd.sequence.short" = "Generate a Mermaid sequence diagram from call graph"
"cmd.check.short" = "CI gate checks for code quality"
"cmd.complexity.short" = "Analyze code complexity"
"cmd.cochange.short" = "Identify co-changing files"
"cmd.dataflow.short" = "Analyze data flow for a function"
"cmd.diffImpact.short" = "Analyze impact of uncommitted changes or branch diff"
"cmd.branchCompare.short" = "Compare code structure between two branches/refs"
"cmd.stats.short" = "Show database statistics"
"cmd.export.short" = "Export the code graph"
"cmd.snapshot.short" = "Save and restore graph database snapshots"
"cmd.triage.short" = "Triage issues"
"cmd.daemon.short" = "Manage the axons daemon"
"cmd.daemon.start.short" = "Start the axons daemon"
"cmd.daemon.stop.short" = "Stop the axons daemon"
"cmd.daemon.ps.short" = "Show daemon status"
"cmd.watch.short" = "Manage file watchers for incremental updates"
"cmd.registry.short" = "Manage multi-repository registry"
"cmd.embed.short" = "Generate embeddings"

# API error messages (user-facing)
"api.error.pluginNotFound" = "plugin not running"
"api.error.invalidManifest" = "invalid manifest.json"
"api.error.pluginAlreadyInstalled" = "plugin already installed"
"api.error.settingsLoadFailed" = "failed to load settings"
"api.error.connectionFailed" = "connection failed"
"api.error.unsupportedProvider" = "unsupported provider: {{provider}}"
"api.error.llmNotEnabled" = "LLM not enabled"
"api.error.llmApiKeyRequired" = "API key is required for provider {{provider}}"
"api.error.methodNotAllowed" = "method not allowed"
"api.error.forbidden" = "access denied"

# Agent profile names and descriptions
"agent.default.name" = "AI Assistant"
"agent.default.description" = "Orchestrator: decomposes tasks and delegates to specialized sub-agents"
"agent.architect.name" = "Architect"
"agent.architect.description" = "Module boundaries, dependency analysis, architecture compliance"
"agent.quality.name" = "Code Quality Analyst"
"agent.quality.description" = "Complexity, dead code, hotspots, coupling detection"
"agent.impact.name" = "Impact Analyst"
"agent.impact.description" = "Change impact scope, call chains, blast radius assessment"
"agent.engineer.name" = "Code Engineer"
"agent.engineer.description" = "Read/write files, execute commands, complete coding tasks"
```

### 4.4 Cobra Command Refactoring

```go
// cmd/axons/cmd/build.go — Before refactoring
var buildCmd = &cobra.Command{
    Use:   "build [path]",
    Short: "Build the code graph",
    Long:  `Build a code graph from the specified directory...`,
}

// After refactoring
var buildCmd = &cobra.Command{
    Use:   "build [path]",
    Short: i18n.T("cmd.build.short"),
    Long:  i18n.T("cmd.build.long"),
}
```

**Note**: Cobra commands are registered during `init()`, at which point the language preference may not yet be loaded. Solutions:
- Cobra `Short`/`Long` support `func() string` (Cobra v1.8+), enabling lazy evaluation
- Or call `i18n.SetLocale()` in `rootCmd.PersistentPreRun`

### 4.5 Agent Profile Refactoring

```go
// internal/agent/profiles.go — After refactoring
var BuiltinProfiles = []AgentProfile{
    {
        ID:          "default",
        Name:        i18n.T("agent.default.name"),
        Description: i18n.T("agent.default.description"),
        // SystemPrompt is NOT translated — it's an instruction for the LLM, English works best
    },
}
```

**SystemPrompt is not translated**: The Agent's SystemPrompt is an instruction for the LLM; English prompts work better on most LLMs. If users use a Chinese LLM, a language pack plugin can provide `agent.default.systemPrompt` to override.

**Runtime language switch update issue**: `BuiltinProfiles` is a package-level variable; `i18n.T()` is evaluated at init time and won't automatically update. After switching languages at runtime, the Agent Name/Description returned by the API will still be in the old language. Solution:

```go
// Solution: change to a lazy evaluation function, don't cache translation results
func GetBuiltinProfiles() []AgentProfile {
    return []AgentProfile{
        {
            ID:          "default",
            Name:        i18n.T("agent.default.name"),
            Description: i18n.T("agent.default.description"),
        },
        // ...
    }
}
```

API handlers call `GetBuiltinProfiles()` instead of directly referencing the `BuiltinProfiles` variable, ensuring each request uses the current locale for translation.

### 4.6 API Handler Refactoring

```go
// internal/plugin/handlers.go — Refactoring example
// Before refactoring
writeJSONError(w, 404, "NOT_FOUND", "plugin not running")

// After refactoring
writeJSONError(w, 404, "NOT_FOUND", i18n.T("api.error.pluginNotFound"))
```

### 4.7 Language Preference Persistence

```go
// New locale setting in Settings table
// GET /v1/settings returns:
{
  "settings": {
    "locale": { "locale": { "value": "en" } }
  },
  // New: available language list (dynamically provided by language pack plugins, see language pack plugin scheme for details)
  "available_locales": [
    { "code": "en", "nativeName": "English", "englishName": "English" }
  ]
}

// PUT /v1/settings update:
{ "category": "locale", "settings": { "locale": "zh-CN" } }
```

**Settings handler integration**: When the locale setting is updated, the backend must synchronize `i18n.SetLocale()` to ensure subsequent API response error messages also use the corresponding language:

```go
// internal/api/server.go — add locale integration in handleUpdateSettings
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
    // ... existing logic ...

    // If locale setting was updated, sync to i18n bundle
    if category == "locale" {
        if localeVal, ok := settings["locale"]; ok {
            i18n.SetLocale(localeVal)
        }
    }
}
```

### 4.8 Language Pack Plugin Integration

The host system needs to reserve three integration points for language pack plugins, so that language pack plugin install/uninstall takes effect immediately (no restart required):

#### 4.8.1 Backend Integration: PluginManager

```go
// internal/plugin/manager.go — Add localization category handling in ImportPlugin / UninstallPlugin

func (m *Manager) ImportPlugin(archivePath string) error {
    // ... existing decompress + validate logic ...

    // Integration: if localization category, immediately load i18n resources + broadcast SSE event
    if manifest.Category == "localization" && manifest.Frontend != nil && manifest.Frontend.Locale != nil {
        locale := manifest.Frontend.Locale

        // 1. Load backend Go i18n resources
        for _, res := range locale.BackendResources {
            path := filepath.Join(manifest.Dir, res)
            i18n.LoadBundle(locale.Language, filepath.Dir(path))
        }

        // 2. Append to available languages list
        m.availableLocales = append(m.availableLocales, LocaleInfo{...})

        // 3. SSE broadcast locale.available → frontend immediately updates Language list
        m.emitEvent("locale.available", map[string]any{
            "locale":      locale.Language,
            "pluginId":    manifest.ID,
            "nativeName":  locale.DisplayName.Native,
            "englishName": locale.DisplayName.English,
        })
    }

    // ... existing return logic ...
}

func (m *Manager) UninstallPlugin(pluginID string) error {
    // ... existing stop process + delete directory logic ...

    // Integration: if localization category, unload i18n resources + rollback + broadcast SSE event
    if manifest.Category == "localization" {
        locale := manifest.Frontend.Locale.Language

        // 1. Unload backend Go i18n resources from memory
        i18n.UnloadBundle(locale)

        // 2. Remove from available languages list
        // ...

        // 3. If currently using this language, rollback to en
        if i18n.GetLocale() == locale {
            i18n.SetLocale("en")
        }

        // 4. SSE broadcast locale.unavailable → frontend immediately rolls back + updates list
        m.emitEvent("locale.unavailable", map[string]any{
            "locale":   locale,
            "pluginId": pluginID,
            "fallback": "en",
        })
    }

    // ... existing return logic ...
}
```

> Complete `loadSingleLocalePlugin()` / `unloadSingleLocalePlugin()` implementation details are in Section 3 of the "Language Pack Plugin Design and Implementation" document.

#### 4.8.2 Frontend Integration: useEventStream

[`useEventStream`](../ui/src/hooks/useEventStream.ts) needs to add two new SSE event callbacks:

```typescript
// ui/src/hooks/useEventStream.ts — callback interface extension
interface UseEventStreamOptions {
  // ... existing callbacks ...

  /** Language plugin installed and available */
  onLocaleAvailable?: (data: {
    locale: string;       // "zh-CN"
    pluginId: string;     // "com.axons.locale-zh-cn"
    nativeName: string;   // "简体中文"
    englishName: string;  // "Chinese (Simplified)"
  }) => void;

  /** Language plugin uninstalled and unavailable */
  onLocaleUnavailable?: (data: {
    locale: string;       // "zh-CN"
    pluginId: string;     // "com.axons.locale-zh-cn"
    fallback: string;     // "en"
  }) => void;
}

// Add to event dispatch switch:
case 'locale.available':
  opts.onLocaleAvailable?.(data);
  break;
case 'locale.unavailable':
  opts.onLocaleUnavailable?.(data);
  break;
```

#### 4.8.3 Frontend Integration: i18next Initialization

i18next's http-backend needs to configure the plugin resource path, so that `changeLanguage('zh-CN')` can load resources from the language pack plugin:

```typescript
// ui/src/i18n/index.ts — http-backend loadPath config
backend: {
  loadPath: (lngs: string[], namespaces: string[]) => {
    const lng = lngs[0];
    if (lng === 'en') return '';  // en is embedded, no loading needed

    // Look up the plugin ID for this language
    // Mapping source: GET /v1/plugins/locales (provided by language pack plugin scheme)
    const pluginId = localePluginMap[lng];
    if (!pluginId) return '';

    // Served by daemon static route, HandlePluginStaticFiles already supports file serving for non-running plugins
    return `/plugins/${pluginId}/locales/frontend/{{ns}}.json`;
  },
}
```

> Key point: [`HandlePluginStaticFiles`](../internal/plugin/proxy.go:85) already supports static file serving for non-running plugins (falls back to `ScanPlugins` directory lookup), so language plugins don't need to start a process to serve JSON resource files.

### 4.9 Backend File Change List

| File | Change Type | Description |
|------|-------------|-------------|
| `internal/i18n/i18n.go` | New | Bundle + T() + SetLocale() + **LoadBundle() + UnloadBundle() + GetLocale()** |
| `internal/i18n/locales/en.toml` | New | English language pack |
| `cmd/axons/cmd/*.go` (18 files) | Modified | Short/Long → `i18n.T()` |
| `internal/api/server.go` | Modified | Error messages → `i18n.T()`; **call `i18n.SetLocale()` on Settings locale update** |
| `internal/api/handlers_*.go` | Modified | User-facing error messages → `i18n.T()` |
| `internal/agent/profiles.go` | Modified | Name/Description → `i18n.T()` |
| `internal/plugin/manager.go` | Modified | **`ImportPlugin` / `UninstallPlugin` localization category integration: load/unload i18n resources + SSE broadcast** |
| `ui/src/hooks/useEventStream.ts` | Modified | **Add `onLocaleAvailable` / `onLocaleUnavailable` SSE event callbacks** |
| `go.mod` | Modified | Add `github.com/BurntSushi/toml` dependency |

## 5. Implementation Plan

### Phase 1: Infrastructure Setup (1.5 days)

| Step | Effort | Deliverable |
|------|--------|-------------|
| Install react-i18next suite + config | 0.5 day | `ui/src/i18n/index.ts` |
| Create 7 en.json English language packs | 0.5 day | `ui/src/i18n/en/*.json` |
| Go i18n infrastructure + en.toml | 0.5 day | `internal/i18n/` |

### Phase 2: Frontend Refactoring (4 days)

| Step | Effort | Deliverable |
|------|--------|-------------|
| SettingsPanel (most complex) | 1 day | All text converted to t() |
| RightPanel + Chat UI | 0.5 day | |
| DropZone + BuildingState + ProjectSelector | 0.5 day | |
| 6 analysis panels + ExtensionsPanel | 1 day | |
| Remaining components + panel registry | 0.5 day | |
| Language tab + switching functionality | 0.5 day | Settings → Language |

### Phase 3: Backend Refactoring (1.5 days)

| Step | Effort | Deliverable |
|------|--------|-------------|
| 18 CLI commands | 0.5 day | |
| API error messages + Agent profile | 0.5 day | |
| Locale persistence + integration testing | 0.5 day | |

### Phase 4: Regression Validation (1 day)

| Step | Effort | Deliverable |
|------|--------|-------------|
| Full component English text scan | 0.5 day | Confirm zero omissions |
| Language switching functionality testing | 0.5 day | |

**Total: 8 days**

## 6. Risks and Mitigations

### 6.1 High Risk

| # | Risk | Description | Mitigation |
|---|------|-------------|------------|
| H1 | Cobra init phase language not initialized | 18 CLI commands are registered during `init()`, at which point language preference is not yet loaded. Design relies on Cobra v1.8's `Short: func() string` lazy evaluation; if the current project's Cobra version is below v1.8, the approach is not feasible | **Check Cobra version in `go.mod` before implementation**; if below v1.8, upgrade first; verify existing command behavior has no regressions after upgrade |
| H2 | Panel title not updated at runtime | `useAppState.ts` uses `registerPanel({ title: 'Health' })` for synchronous static registration; title stores a literal string. Changing to i18n key requires simultaneously changing `PanelDef` type definition + all rendering sides in Footer / ActivityBar | Store i18n key in title, translate at render side with `t()`, **all consumers must be updated at once, no partial changes**, otherwise raw keys will be displayed |
| H3 | Agent Profile Name/Description not updated on runtime language switch | `BuiltinProfiles` is a package-level variable, `i18n.T()` is evaluated at init time and won't update | Change to `GetBuiltinProfiles()` function for lazy evaluation; **grep all reference points during implementation**, ensure all are changed to function calls |
| H4 | Frontend `localePluginMap` startup timing | `main.tsx` needs to `fetch('/v1/plugins/locales')` to get mapping before mounting React, blocking first screen render | Set 2s timeout; if timeout, skip mapping fetch and default to embedded en resources; when mapping fetch fails, i18next loadPath returns empty string, automatically falls back to en |

### 6.2 Medium Risk

| # | Risk | Description | Mitigation |
|---|------|-------------|------------|
| M1 | Backend `i18n.T()` global locale single instance | `locale` in `i18n.go` is a global variable, doesn't support per-request locale (multi-user scenario) | Current Axons is a single-user desktop application, no issue for now; **document this limitation in code comments** to prevent future misuse |
| M2 | API error message i18n | `internal/api/` has 12+ files with many `writeError()` calls; some messages are `err.Error()` dynamic content, not suitable for `i18n.T()` | **Distinguish two categories**: user-facing fixed messages use `i18n.T()`; technical dynamic messages (like `err.Error()`, `fmt.Sprintf("Failed to ...: %v", err)`) remain in English |
| M3 | Manifest validation conflicts with localization plugins | Current `ValidateManifest` requires at least one of `backend` and `frontend` to be non-empty, but localization plugins have `backend=null` and `frontend.entry=null` | **Adjust validation logic first**: localization category allows `backend=null` + `frontend.entry=null`, but requires `frontend.locale` to be present |
| M4 | SSE event type extension | `useEventStream.ts`'s `EventType` union type and `addEventListener` use a hardcoded pattern; adding `locale.available` / `locale.unavailable` requires synchronized changes in EventType + addEventListener + options interface | Changes are tedious but manageable; **ensure EventType union type matches backend event names**; recommend updating `useEventStream` JSDoc simultaneously |
| M5 | BurntSushi/toml dependency introduction | `go.mod` needs to add `github.com/BurntSushi/toml` | **Check `go.sum` before implementation** to see if the library is already an indirect dependency, assess version conflict risk |
| M6 | Settings locale update not synced to backend | If backend doesn't sync after frontend switches language, API error messages still use old language | In `handleUpdateSettings`, detect `category == "locale"` → call `i18n.SetLocale()` |
| M7 | Frontend doesn't detect language plugin install/uninstall | Frontend Language list doesn't update in real-time | SSE broadcast `locale.available` / `locale.unavailable` events; frontend `useEventStream` listens and immediately updates Language list |
| M8 | UI still shows uninstalled language after current language pack is removed | The language pack currently in use gets uninstalled, but UI doesn't roll back | Backend `UninstallPlugin` checks `i18n.GetLocale() == locale` → auto `i18n.SetLocale("en")`; frontend receives `locale.unavailable` event → `i18next.changeLanguage(fallback)` |

### 6.3 Low Risk

| # | Risk | Mitigation |
|---|------|------------|
| L1 | i18n key naming chaos causing maintenance difficulties | Strict `semantic path` naming + Code Review checks |
| L2 | en.json and code out of sync | CI check: grep for hardcoded English text not yet converted to t() |
| L3 | SystemPrompt translation reducing LLM effectiveness | SystemPrompt is not translated; language pack plugins can override as needed |
| L4 | http-backend `{{ns}}` placeholder compatibility | Verify `i18next-http-backend` version support for `{{ns}}` substitution in `loadPath` function return value |

### 6.4 Implementation Recommendations

1. **Check Cobra version first**: Check `go.mod`; if Cobra < v1.8, prioritize upgrade, otherwise CLI command refactoring approach needs adjustment (H1)
2. **Frontend: proceed by namespace in batches**: Follow common → settings → panels → chat → activitybar → dropzone → extensions order; each namespace is complete and functional when done, avoiding mixed-language UI
3. **API error messages: handle in two categories**: Fixed user messages use `i18n.T()`, technical `err.Error()` stays in English, avoid translating runtime error information (M2)
4. **Set timeout for first-screen mapping fetch**: In `main.tsx`, fetch mapping with 2s timeout recommendation; on timeout, fallback to embedded en, avoid blank screen (H4)
5. **Add CI check**: Use a script to grep for hardcoded English text not yet converted to t(), prevent incremental code regression (L2)
6. **Update all panel title consumers at once**: PanelDef.title semantics change (string → i18n key), all rendering sides like Footer / ActivityBar / Settings must be updated synchronously, no partial changes (H2)

## 7. Expected Benefits

| # | Benefit | Description |
|---|---------|-------------|
| 1 | Internationalization infrastructure | One-time investment; supporting any new language only requires creating a language pack, zero code changes |
| 2 | Language packs as plugins | Leverages full existing plugin system infrastructure (install/uninstall/marketplace), no additional distribution mechanism needed |
| 3 | Instant effect | Install/uninstall/switch language without restarting daemon; SSE event-driven, smooth user experience |
| 4 | Unified frontend-backend language switching | Frontend `i18next.changeLanguage()` + backend `i18n.SetLocale()` integration; API error messages follow language too |
| 5 | Zero-cost English | Default English embedded, no plugins needed, zero configuration and zero network requests |
| 6 | Community contribution | Anyone can create language packs and distribute them via plugin marketplace, reducing official translation burden |
| 7 | Extensibility | Plugin title's `titleI18n` declaration + language pack `titles.json` dual-layer coverage, high flexibility |
| 8 | Progressive translation | Language packs don't need 100% translation; i18next fallback mechanism ensures missing keys automatically fall back to English, no functionality impact |

Use `semantic path` naming, not component names (components may be refactored, semantics are relatively stable):
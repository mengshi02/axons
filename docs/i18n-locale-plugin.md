# Axons Locale Plugin Design and Implementation

> Version: v1.0 | Date: 2026-05-16 | Status: Implemented

## 1. Design Philosophy

**Core Idea**: Treat non-English language support as a special type of plugin package (`category: "localization"`), reusing the existing plugin system's installation, management, and lifecycle mechanisms, allowing language packs to be independently published and updated.

**Design Principles**:

| Principle | Description |
|-----------|-------------|
| Default English, zero dependencies | English is embedded in the main program; no plugin installation needed to use it |
| Language pack as plugin | Reuse the full plugin infrastructure (install/uninstall/version management/marketplace) |
| Unified frontend/backend loading | A single locale plugin package provides both frontend JSON and backend TOML resources |
| On-demand loading | Resources for a language are only loaded when the user selects that language |
| Community-contributable | Anyone can create a language pack and distribute it through the plugin marketplace |

## 2. Locale Plugin Package Specification

### 2.1 Directory Structure

```
com.axons.locale-zh-cn/
├── manifest.json                # Plugin manifest
├── locales/
│   ├── frontend/                # Frontend i18next resources
│   │   ├── common.json         # Corresponds to ui/src/i18n/en/common.json
│   │   ├── settings.json
│   │   ├── panels.json
│   │   ├── chat.json
│   │   ├── activitybar.json
│   │   ├── dropzone.json
│   │   └── extensions.json
│   ├── backend/                 # Backend Go i18n resources
│   │   └── messages.toml        # Corresponds to internal/i18n/locales/en.toml
│   └── plugin/                  # Plugin manifest i18n resources
│       └── titles.json          # Title translations for other plugins
└── README.md                    # Language pack description (translation guide, contributors, etc.)
```

### 2.2 manifest.json

```jsonc
{
  "id": "com.axons.locale-zh-cn",
  "name": "Chinese (Simplified) Language Pack",
  "version": "1.0.0",
  "description": "Simplified Chinese language pack, providing complete Chinese interface translation for Axons",
  "author": "axons-community",
  "icon": "icon.svg",
  "category": "localization",
  "minAxonsVersion": "0.8.0",

  // Locale pack specific declarations
  "backend": null,                // Locale pack has no backend process

  "frontend": {
    "entry": null,                // No UI entry component (locale pack doesn't need a panel)
    "locale": {
      "language": "zh-CN",       // BCP 47 language tag
      "displayName": {
        "native": "简体中文",     // Native language name (used in Language settings page)
        "english": "Chinese (Simplified)"  // English name
      },
      // Frontend resource paths (relative to plugin root directory)
      "resources": [
        "locales/frontend/common.json",
        "locales/frontend/settings.json",
        "locales/frontend/panels.json",
        "locales/frontend/chat.json",
        "locales/frontend/activitybar.json",
        "locales/frontend/dropzone.json",
        "locales/frontend/extensions.json"
      ],
      // Backend resource paths
      "backendResources": [
        "locales/backend/messages.toml"
      ],
      // Other plugin title translation path
      "pluginTitles": "locales/plugin/titles.json"
    }
  }
}
```

### 2.3 Category Validation Extension

The existing [`ValidCategories`](../internal/plugin/manifest.go:96) needs to add `localization`:

```go
// internal/plugin/manifest.go
var ValidCategories = map[string]bool{
    "analysis":      true,
    "visualization": true,
    "search":        true,
    "productivity":  true,
    "localization":  true,  // New
}
```

### 2.4 Locale Field Validation

```go
// New manifest validation: when category=localization, frontend.locale must exist
func ValidateManifest(m *PluginManifest) error {
    // ... existing validation ...

    if m.Category == "localization" {
        if m.Frontend == nil || m.Frontend.Locale == nil {
            return fmt.Errorf("manifest: localization plugin must declare frontend.locale")
        }
        if m.Frontend.Locale.Language == "" {
            return fmt.Errorf("manifest: frontend.locale.language is required")
        }
        // BCP 47 format validation (zh-CN, en, ja, ko, etc.)
        if !isValidBCP47(m.Frontend.Locale.Language) {
            return fmt.Errorf("manifest: frontend.locale.language must be valid BCP 47 tag, got %q", m.Frontend.Locale.Language)
        }
        if len(m.Frontend.Locale.Resources) == 0 {
            return fmt.Errorf("manifest: frontend.locale.resources must have at least one file")
        }
        // backend must be null
        if m.Backend != nil {
            return fmt.Errorf("manifest: localization plugin must not have backend (must be null)")
        }
        // frontend.entry must be null
        if m.Frontend.Entry != "" {
            return fmt.Errorf("manifest: localization plugin must not have frontend.entry")
        }
        // frontend.panels must be empty
        if len(m.Frontend.Panels) > 0 {
            return fmt.Errorf("manifest: localization plugin must not declare frontend.panels")
        }
    }
    return nil
}
```

## 3. Loading Mechanism

### 3.1 Core Feature: Switch Languages Without Restart After Installation

The loading/unloading/switching of locale plugin packages is entirely done at runtime, **no need to restart the axons daemon**:

| Operation | Restart Required | Principle |
|-----------|-----------------|-----------|
| Install locale plugin | No | `i18n.LoadBundle()` is a pure in-memory map write, takes effect immediately |
| Switch language | No | `i18n.SetLocale()` changes a global variable; `i18next.changeLanguage()` triggers React re-render |
| Uninstall locale plugin | No | `i18n.UnloadBundle()` deletes from in-memory map; frontend automatically falls back to en |

**Key Technical Points**:

1. **Backend instant loading**: `i18n.LoadBundle(locale, dir)` parses TOML files and writes to the `bundles[locale]` map; subsequent `i18n.T()` calls immediately hit the new language
2. **Frontend on-demand loading**: i18next's `changeLanguage('zh-CN')` triggers http-backend to request `/plugins/{pluginId}/locales/frontend/*.json`; the existing [`HandlePluginStaticFiles`](../internal/plugin/proxy.go:85) **already supports static file serving for non-running plugins** (fallback to `ScanPlugins` to find directories), so JSON files can be served without the plugin process running
3. **SSE event-driven**: After a locale plugin is installed/uninstalled, PluginManager broadcasts `locale.available` / `locale.unavailable` SSE events; the frontend immediately updates the available languages list on the Language settings page upon receiving them

### 3.2 Overall Flow

```
axons startup
  │
  ├── PluginManager.ScanPlugins()
  │     └── Scan ~/.axons/plugins/*/manifest.json
  │           ├── Identify category=localization plugins
  │           └── Read frontend.locale declarations
  │
  ├── Load installed locale resources (at startup)
  │     ├── Go backend: read backendResources → i18n.LoadBundle(locale, dir)
  │     └── Record available locales list to PluginManager.availableLocales
  │
  ├── SSE push availableLocales list to frontend
  │
  └── Frontend i18next
        ├── en embedded, loaded by default
        └── Other languages: i18next-http-backend loads on demand from /plugins/:id/locales/frontend/*.json
```

### 3.3 Runtime Installation Flow (No Restart Required)

```
User imports locale plugin → POST /v1/plugins/import
  │
  ├── PluginManager.ImportPlugin() succeeds
  │     └── Detect category=localization
  │
  ├── Backend instant loading
  │     ├── i18n.LoadBundle("zh-CN", pluginDir+"/locales/backend/")
  │     └── PluginManager.availableLocales adds { code: "zh-CN", ... }
  │
  ├── SSE broadcast locale.available
  │     └── { locale: "zh-CN", pluginId: "com.axons.locale-zh-cn",
  │          nativeName: "简体中文", englishName: "Chinese (Simplified)" }
  │
  └── Frontend receives SSE event
        ├── Settings → Language tab refreshes available languages list
        └── "简体中文" option appears immediately, click to switch
```

### 3.4 Runtime Uninstallation Flow (No Restart Required)

```
User uninstalls locale plugin → DELETE /v1/plugins/:id
  │
  ├── PluginManager detects category=localization
  │
  ├── Backend instant unloading
  │     ├── i18n.UnloadBundle("zh-CN")     // Delete from in-memory map
  │     ├── PluginManager.availableLocales removes the language
  │     └── If current locale == "zh-CN" → i18n.SetLocale("en") auto fallback
  │
  ├── SSE broadcast locale.unavailable
  │     └── { locale: "zh-CN", pluginId: "com.axons.locale-zh-cn", fallback: "en" }
  │
  └── Frontend receives SSE event
        ├── If current language == "zh-CN" → i18next.changeLanguage('en') auto fallback
        ├── Settings → Language tab refreshes available languages list
        └── Display toast: "Current language pack has been uninstalled, switched to English"
```

### 3.5 Backend Loading (Startup + Runtime Incremental)

```go
// internal/plugin/manager.go — New locale loading logic

// loadLocalePlugins loads resources for all localization category plugins
func (m *Manager) loadLocalePlugins() error {
    var locales []LocaleInfo

    for _, inst := range m.instances {
        if inst.Manifest.Category != "localization" {
            continue
        }
        locale := inst.Manifest.Frontend.Locale
        if locale == nil {
            continue
        }

        // 1. Load backend Go i18n resources
        for _, res := range locale.BackendResources {
            path := filepath.Join(inst.Manifest.Dir, res)
            if err := i18n.LoadBundle(locale.Language, filepath.Dir(path)); err != nil {
                logger.S().Warnw("Failed to load backend locale", "locale", locale.Language, "error", err)
            }
        }

        // 2. Record available locale
        locales = append(locales, LocaleInfo{
            Code:        locale.Language,
            NativeName:  locale.DisplayName.Native,
            EnglishName: locale.DisplayName.English,
            PluginID:    inst.Manifest.ID,
        })
    }

    // 3. Store available locales list (for API response)
    m.availableLocales = locales

    return nil
}

// LocaleInfo describes an available locale
type LocaleInfo struct {
    Code        string `json:"code"`        // "zh-CN"
    NativeName  string `json:"nativeName"`  // "简体中文"
    EnglishName string `json:"englishName"` // "Chinese (Simplified)"
    PluginID    string `json:"pluginId"`    // "com.axons.locale-zh-cn"
}

// loadSingleLocalePlugin loads a single locale plugin's resources (runtime incremental, no restart needed)
// When called:
//   - At startup: loadLocalePlugins() calls in a loop
//   - At runtime: called after ImportPlugin() succeeds, enabling install-to-activate
func (m *Manager) loadSingleLocalePlugin(manifest *PluginManifest) {
    locale := manifest.Frontend.Locale
    if locale == nil {
        return
    }

    // 1. Load backend Go i18n resources into memory
    for _, res := range locale.BackendResources {
        path := filepath.Join(manifest.Dir, res)
        if err := i18n.LoadBundle(locale.Language, filepath.Dir(path)); err != nil {
            logger.S().Warnw("Failed to load backend locale", "locale", locale.Language, "error", err)
        }
    }

    // 2. Append to available locales list
    m.availableLocales = append(m.availableLocales, LocaleInfo{
        Code:        locale.Language,
        NativeName:  locale.DisplayName.Native,
        EnglishName: locale.DisplayName.English,
        PluginID:    manifest.ID,
    })

    // 3. SSE broadcast locale.available event → Frontend immediately updates Language list
    m.emitEvent("locale.available", map[string]any{
        "locale":      locale.Language,
        "pluginId":    manifest.ID,
        "nativeName":  locale.DisplayName.Native,
        "englishName": locale.DisplayName.English,
    })

    logger.S().Infow("Locale plugin loaded", "locale", locale.Language, "pluginId", manifest.ID)
}

// ImportPlugin integration — Append locale loading logic after existing ImportPlugin succeeds
// (Pseudo-code, showing insertion point)
func (m *Manager) ImportPlugin(archivePath string) error {
    // ... existing decompression + validation logic ...

    // New: if localization category, immediately load resources + broadcast event
    if manifest.Category == "localization" {
        m.loadSingleLocalePlugin(&manifest)
    }

    // ... existing return logic ...
}

// UninstallPlugin integration — Append locale cleanup to existing uninstall logic
func (m *Manager) UninstallPlugin(pluginID string) error {
    // ... existing stop process + delete directory logic ...

    // New: if localization category, unload i18n resources + broadcast event
    inst, ok := m.GetInstance(pluginID)
    if ok && inst.Manifest.Category == "localization" {
        m.unloadSingleLocalePlugin(pluginID, inst.Manifest)
    }

    // ... existing return logic ...
}

// unloadSingleLocalePlugin unloads a single locale plugin's resources (at runtime, no restart needed)
func (m *Manager) unloadSingleLocalePlugin(pluginID string, manifest *PluginManifest) {
    locale := manifest.Frontend.Locale.Language

    // 1. Unload backend Go i18n resources from memory
    i18n.UnloadBundle(locale)

    // 2. Remove from available locales list
    m.availableLocales = slices.DeleteFunc(m.availableLocales, func(l LocaleInfo) bool {
        return l.PluginID == pluginID
    })

    // 3. If currently using this locale, fall back to en
    if i18n.GetLocale() == locale {
        i18n.SetLocale("en")
    }

    // 4. SSE broadcast locale.unavailable event → Frontend immediately falls back + updates list
    m.emitEvent("locale.unavailable", map[string]any{
        "locale":   locale,
        "pluginId": pluginID,
        "fallback": "en",
    })

    logger.S().Infow("Locale plugin unloaded", "locale", locale, "pluginId", pluginID)
}
```

### 3.6 Frontend Loading

i18next's `http-backend` loads locale resources from the daemon's static routes:

```
Frontend switches language → i18next.changeLanguage('zh-CN')
  → http-backend detects zh-CN resources not loaded
  → Request GET /plugins/com.axons.locale-zh-cn/locales/frontend/common.json
  → Daemon static route serves the file
  → i18next merges resources into zh-CN namespace
  → React components automatically re-render
```

**http-backend loadPath configuration**:

```typescript
// ui/src/i18n/index.ts — Key configuration
backend: {
  loadPath: (lngs: string[], namespaces: string[]) => {
    // Only non-en languages use the plugin path for loading
    const lng = lngs[0];
    if (lng === 'en') return '';  // en is embedded, no loading needed

    // Find the plugin ID corresponding to this language
    const pluginId = getLocalePluginId(lng);  // Get mapping from Settings API
    if (!pluginId) return '';  // Language pack not installed

    return `/plugins/${pluginId}/locales/frontend/{{ns}}.json`;
  },
}
```

**Language → Plugin ID Mapping**: Fetched from API at frontend startup:

```typescript
// GET /v1/plugins/locales →
// { "zh-CN": "com.axons.locale-zh-cn", "ja": "com.axons.locale-ja" }
```

**Mapping Fetch Timing**: `localePluginMap` must be ready before i18next initialization, otherwise `changeLanguage` triggered http-backend loading won't find `pluginId`. Solution: In `main.tsx`, first `fetch('/v1/plugins/locales')` to get the mapping and store in a global variable, then mount the React root component. See host design Section 3.2 for details.

**http-backend `{{ns}}` placeholder**: i18next-http-backend supports using `{{ns}}` placeholders in the return value of `loadPath` function. When `loadPath` is a function, http-backend performs `{{lng}}` and `{{ns}}` replacement on the returned template string before making the request. Therefore, `{{ns}}` in `/plugins/${pluginId}/locales/frontend/{{ns}}.json` will be replaced with the actual namespace name (e.g., `common`, `settings`).

### 3.7 Language Switching Flow

```
User selects "简体中文" in Settings → Language
  │
  ├── Frontend
  │     ├── i18next.changeLanguage('zh-CN')
  │     │     └── http-backend automatically loads zh-CN resources
  │     ├── localStorage.setItem('axons-locale', 'zh-CN')
  │     └── React components automatically re-render (useTranslation hook)
  │
  └── Backend
        ├── fetch PUT /v1/settings { category: "locale", settings: { locale: "zh-CN" } }
        ├── Server calls i18n.SetLocale("zh-CN") after receiving
        └── Error messages in subsequent API responses automatically use Chinese
```

## 4. Plugin Title Internationalization

### 4.1 Problem

The `panels[].title` and `commands[].title` in other plugins' (non-locale plugins) [`manifest.json`](../internal/plugin/manifest.go:73) are static English strings that don't change when the language is switched.

### 4.2 Solution: titleI18n Declaration + Language Pack Override

**Method 1: Plugin self-declares titleI18n (Recommended)**

```jsonc
// Plugin manifest.json
{
  "frontend": {
    "panels": [{
      "id": "huggingface",
      "title": "Hugging Face",          // Default (English)
      "titleI18n": {                      // Optional i18n override
        "zh-CN": "模型管理"
      }
    }],
    "commands": [{
      "id": "huggingface.open",
      "title": "Open Hugging Face",
      "titleI18n": {
        "zh-CN": "打开模型管理"
      }
    }]
  }
}
```

**Method 2: Locale plugin package provides centrally (Supplementary)**

A locale plugin package can provide title translations for other plugins, covering cases where the plugin itself hasn't declared them:

```jsonc
// com.axons.locale-zh-cn/locales/plugin/titles.json
{
  "com.axons.huggingface": {
    "panels": {
      "huggingface": "模型管理"
    },
    "commands": {
      "huggingface.open": "打开模型管理"
    }
  }
}
```

**Priority**: Plugin's own `titleI18n` > Language pack `titles.json` > Default `title`

### 4.3 Data Structure Extension

```go
// internal/plugin/manifest.go — PanelDef adds titleI18n
type PanelDef struct {
    ID        string            `json:"id"`
    Title     string            `json:"title"`
    TitleI18n map[string]string `json:"titleI18n,omitempty"`  // New
    Icon      string            `json:"icon"`
    Location  string            `json:"location"`
    Activator string            `json:"activator"`
    FooterSlot string           `json:"footerSlot"`
}

// CommandDef adds titleI18n
type CommandDef struct {
    ID        string            `json:"id"`
    Title     string            `json:"title"`
    TitleI18n map[string]string `json:"titleI18n,omitempty"`  // New
    Shortcut  string            `json:"shortcut"`
}
```

### 4.4 Frontend Consumption

```typescript
// When rendering panel title
function getLocalizedTitle(panel: PanelDef, locale: string): string {
  // Priority: titleI18n[locale] > pluginTitles[locale] > title
  if (panel.titleI18n?.[locale]) return panel.titleI18n[locale];

  // Look up language pack's titles.json
  const pluginTitles = i18n.getResource(locale, 'pluginTitles', panel.pluginId);
  if (pluginTitles?.panels?.[panel.id]) return pluginTitles.panels[panel.id];

  return panel.title;  // fallback
}
```

## 5. Locale Plugin Package API

### 5.1 New APIs

| Route | Method | Description |
|-------|--------|-------------|
| `/v1/plugins/locales` | GET | Returns available languages `{ locale → pluginId }` mapping |
| `/v1/plugins/:id/locale` | GET | Returns a single locale plugin's locale declaration (frontend resource paths, etc.) |

```go
// GET /v1/plugins/locales response
{
  "locales": {
    "zh-CN": {
      "pluginId": "com.axons.locale-zh-cn",
      "nativeName": "简体中文",
      "englishName": "Chinese (Simplified)",
      "resources": {
        "common": "/plugins/com.axons.locale-zh-cn/locales/frontend/common.json",
        "settings": "/plugins/com.axons.locale-zh-cn/locales/frontend/settings.json",
        // ...
      }
    }
  }
}
```

### 5.2 Settings API Extension

```go
// GET /v1/settings returns new field
{
  "settings": {
    "locale": {
      "locale": { "value": "zh-CN" }
    }
  },
  // New
  "available_locales": [
    { "code": "en", "nativeName": "English", "englishName": "English" },
    { "code": "zh-CN", "nativeName": "简体中文", "englishName": "Chinese (Simplified)" }
  ]
}
```

### 5.3 Plugin Static Routes

The existing `/plugins/:id/*filepath` route can already serve locale resource files (`.json` / `.toml`), no new routes needed.

**Key: [`HandlePluginStaticFiles`](../internal/plugin/proxy.go:85) already supports static file serving for non-running plugins**. When `GetInstance(pluginID)` returns false, it falls back to `ScanPlugins()` to find the plugin directory and reads files directly from disk. This means:

- Locale plugins have no backend process (not in the `instances` map)
- But when the frontend i18next-http-backend requests `/plugins/com.axons.locale-zh-cn/locales/frontend/common.json`, the static route can still correctly serve the file
- **No need to start a plugin process to load locale resources**

**CORS / Content-Type**:
- `.json` → `application/json; charset=utf-8`
- `.toml` → `application/toml` (backend reads directly, not via HTTP)

### 5.4 SSE Event Types

Locale plugin packages add two new SSE event types for the frontend to respond immediately:

| Event Type | Trigger | Payload |
|------------|---------|---------|
| `locale.available` | Locale plugin imported/installed successfully | `{ locale, pluginId, nativeName, englishName }` |
| `locale.unavailable` | Locale plugin uninstalled | `{ locale, pluginId, fallback }` |

Frontend consumption: Add `onLocaleAvailable` / `onLocaleUnavailable` callbacks in [`useEventStream`](../ui/src/hooks/useEventStream.ts); Settings → Language tab listens and updates the available languages list.

## 6. Locale Plugin Package Lifecycle

### 6.1 Differences from Regular Plugins

| Stage | Regular Plugin | Locale Plugin |
|-------|---------------|---------------|
| Import | Decompress + validate manifest | Same |
| Install | Execute install.command | **Skip** (no install.command) |
| Start | exec.Command starts process | **Skip** (no backend process) |
| Running | Health check + register panels/commands | **Load locale resources into i18n bundle** |
| Stop | SIGTERM | **Unload locale resources** |
| Uninstall | Stop process + delete directory | Same + cleanup i18n resources |

### 6.2 Key: No Process Started at Startup

When PluginManager starts a localization category plugin:

```go
// internal/plugin/process.go — StartPlugin modification
func (m *Manager) StartPlugin(pluginID string) error {
    inst := m.getInstance(pluginID)

    // Locale plugin: no backend process, only load resources
    if inst.Manifest.Category == "localization" {
        return m.loadLocaleResources(inst)
    }

    // Regular plugin: start process
    return m.startPluginProcess(inst)
}
```

### 6.3 i18n Resource Cleanup on Uninstall

```go
func (m *Manager) UnloadLocaleResources(pluginID string) {
    inst := m.getInstance(pluginID)
    locale := inst.Manifest.Frontend.Locale.Language

    // Unload backend Go i18n resources
    i18n.UnloadBundle(locale)

    // Remove from available locales list
    m.availableLocales = slices.DeleteFunc(m.availableLocales, func(l LocaleInfo) bool {
        return l.PluginID == pluginID
    })

    // SSE broadcast locale unavailable event
    m.eventBroker.Publish("locale.unavailable", map[string]any{
        "locale":   locale,
        "pluginId": pluginID,
    })
}
```

### 6.4 Language Fallback After Uninstall

If the language pack currently in use is uninstalled:

```
1. Backend detects locale setting ≠ "en" and the corresponding plugin is uninstalled
2. Backend automatically falls back locale setting to "en"
3. SSE broadcasts locale.changed { locale: "en", reason: "fallback" }
4. Frontend receives event → i18next.changeLanguage('en')
5. Frontend shows notification: "Current language pack has been uninstalled, switched to English"
```

## 7. Language Pack Creation Guide

### 7.1 Creating a Language Pack

```bash
# 1. Create directory structure
mkdir -p com.axons.locale-zh-cn/locales/{frontend,backend,plugin}

# 2. Copy English language pack as translation template
cp ui/src/i18n/en/*.json com.axons.locale-zh-cn/locales/frontend/
cp internal/i18n/locales/en.toml com.axons.locale-zh-cn/locales/backend/messages.toml

# 3. Translate frontend JSON
# Edit locales/frontend/*.json, replace English values with target language

# 4. Translate backend TOML
# Edit locales/backend/messages.toml

# 5. Write manifest.json

# 6. Package
tar czf com.axons.locale-zh-cn.tar.gz com.axons.locale-zh-cn/
```

### 7.2 Translation Guidelines

| Guideline | Description | Example |
|-----------|-------------|---------|
| Preserve JSON structure | Keys unchanged, only translate values | `"title": "Code Health"` → `"title": "代码健康"` |
| Preserve interpolation variables | `{{count}}` / `{{name}}` not translated | `"{{count}} callers"` → `"{{count}} 个调用者"` |
| Terminology consistency | Same term translated consistently throughout | `graph` consistently translated as "图", not mixed with "图谱" and "图" |
| Don't translate technical terms | Professional terms like API Key / LLM / Embedding stay in English | `"API Key"` → `"API Key"` |
| Don't translate SystemPrompt | Agent system prompts are for LLMs, keep in English | Don't translate `agent.default.systemPrompt` |

### 7.3 Glossary

| English | Translation | Notes |
|---------|-------------|-------|
| Graph | 图 | Code graph |
| Node | 节点 | Node in a graph |
| Edge | 边 | Edge in a graph |
| Hotspot | 热点 | Highly coupled functions |
| Dead Code | 死代码 | Unreachable code |
| Co-Change | 共变 | Files that change together |
| Embedding | Embedding | Not translated, professional term |
| PageRank | PageRank | Not translated, algorithm name |
| SCC | SCC | Strongly Connected Component, abbreviation not translated |
| Impact Analysis | 影响分析 | |
| Call Chain | 调用链 | |
| CFG | CFG | Control Flow Graph, abbreviation not translated |
| Dataflow | 数据流 | |
| Agent | Agent | Not translated, professional term |
| Plugin | 插件 | |
| Panel | 面板 | |

## 8. Frontend Component Adaptation

### 8.1 Language Settings Page

The Settings panel adds a Language tab that displays the available languages list:

```tsx
// SettingsPanel.tsx — Language tab
import { useTranslation } from 'react-i18next';
import { Globe } from 'lucide-react';

function LanguageTab() {
  const { t, i18n } = useTranslation('settings');
  const [availableLocales, setAvailableLocales] = useState([
    { code: 'en', nativeName: 'English', englishName: 'English' }
  ]);
  const currentLocale = i18n.language;

  useEffect(() => {
    // Get available languages list from Settings API
    fetch('/v1/settings')
      .then(r => r.json())
      .then(data => {
        if (data.available_locales) {
          setAvailableLocales(data.available_locales);
        }
      });
  }, []);

  const handleLanguageChange = async (code: string) => {
    // 1. Switch frontend language
    await i18n.changeLanguage(code);
    // 2. Persist to backend
    await fetch('/v1/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ category: 'locale', settings: { locale: code } }),
    });
  };

  return (
    <div className="space-y-4">
      <p className="text-sm text-text-secondary">
        {t('language.description')}
      </p>
      <div className="grid grid-cols-2 gap-3">
        {availableLocales.map(locale => (
          <button
            key={locale.code}
            onClick={() => handleLanguageChange(locale.code)}
            className={`p-4 rounded-lg border-2 transition-all ${
              currentLocale === locale.code
                ? 'border-accent bg-accent/10'
                : 'border-border-subtle hover:border-border-default hover:bg-hover'
            }`}
          >
            <div className="flex flex-col items-center gap-2">
              <Globe className={`w-8 h-8 ${currentLocale === locale.code ? 'text-accent' : 'text-text-muted'}`} />
              <span className="text-sm font-medium text-text-primary">
                {locale.nativeName}
              </span>
              <span className="text-xs text-text-muted">
                {locale.englishName}
              </span>
            </div>
          </button>
        ))}
      </div>
      {availableLocales.length <= 1 && (
        <p className="text-xs text-text-muted">
          {t('language.onlyDefault')}
        </p>
      )}
    </div>
  );
}
```

### 8.2 Dynamic Update of Available Languages List

Listen to SSE events to dynamically update available languages (no restart needed):

```typescript
// ui/src/hooks/useEventStream.ts — New locale event types

// New SSE event types
export interface LocaleAvailableEvent {
  locale: string;       // "zh-CN"
  pluginId: string;     // "com.axons.locale-zh-cn"
  nativeName: string;   // "简体中文"
  englishName: string;  // "Chinese (Simplified)"
}

export interface LocaleUnavailableEvent {
  locale: string;       // "zh-CN"
  pluginId: string;     // "com.axons.locale-zh-cn"
  fallback: string;     // "en"
}

// useEventStream callback extension
interface UseEventStreamOptions {
  // ... existing callbacks ...

  // New: locale plugin available
  onLocaleAvailable?: (data: LocaleAvailableEvent) => void;
  // New: locale plugin unavailable (uninstalled)
  onLocaleUnavailable?: (data: LocaleUnavailableEvent) => void;
}
```

```typescript
// SettingsPanel.tsx — Consuming SSE events in Language tab
import { useEventStream } from '../hooks/useEventStream';

function LanguageTab() {
  const { t, i18n } = useTranslation('settings');
  const [availableLocales, setAvailableLocales] = useState<LocaleInfo[]>([
    { code: 'en', nativeName: 'English', englishName: 'English' }
  ]);

  // Listen to locale SSE events, real-time update of available languages list
  useEventStream({
    onLocaleAvailable: useCallback((data) => {
      setAvailableLocales(prev => {
        if (prev.some(l => l.code === data.locale)) return prev;  // Dedup
        return [...prev, {
          code: data.locale,
          nativeName: data.nativeName,
          englishName: data.englishName,
          pluginId: data.pluginId,
        }];
      });
    }, []),
    onLocaleUnavailable: useCallback((data) => {
      // 1. Remove from available list
      setAvailableLocales(prev => prev.filter(l => l.code !== data.locale));
      // 2. If currently using this language, auto fallback
      if (i18n.language === data.locale) {
        i18n.changeLanguage(data.fallback);  // Typically fallback === "en"
        // Show toast notification
      }
    }, [i18n]),
  });

  // ... rendering logic ...
}
```

### 8.3 Plugin Panel Title Translation

```tsx
// Footer.tsx / ActivityBar.tsx — When rendering panel title
function LocalizedPanelTitle({ panel }: { panel: PanelDef }) {
  const { i18n } = useTranslation();
  const locale = i18n.language;

  // Priority: titleI18n > language pack titles.json > default title
  if (panel.titleI18n?.[locale]) {
    return <>{panel.titleI18n[locale]}</>;
  }

  // Panel title stores an i18n key (e.g., "panels:codeHealth.title")
  // t() automatically handles namespace prefix
  const { t } = useTranslation();
  return <>{t(panel.title)}</>;
}
```

## 9. File Change List

### 9.1 Backend Additions/Modifications

| File | Change Type | Description |
|------|-------------|-------------|
| `internal/plugin/manifest.go` | Modify | Add `localization` to `ValidCategories`; add `TitleI18n` to `PanelDef`/`CommandDef`; add locale constraint validation logic |
| `internal/plugin/manager.go` | Modify | Add `loadLocalePlugins()` / `loadSingleLocalePlugin()` / `unloadSingleLocalePlugin()` / `availableLocales`; `ImportPlugin` / `UninstallPlugin` integration with locale loading/unloading + SSE broadcast |
| `internal/plugin/process.go` | Modify | `StartPlugin` skips process startup for `localization` category |
| `internal/plugin/handlers.go` | Modify | Add `handleGetLocales` handler; `handleListPlugins` returns locale info |
| `internal/i18n/i18n.go` | Modify | Add `UnloadBundle()` function |

### 9.2 Frontend Additions/Modifications

| File | Change Type | Description |
|------|-------------|-------------|
| `ui/src/i18n/index.ts` | Modify | http-backend loadPath configuration for locale plugin resource paths |
| `ui/src/components/SettingsPanel.tsx` | Modify | Add Language tab |
| `ui/src/hooks/useEventStream.ts` | Modify | Listen to locale-related SSE events |
| `ui/src/components/Footer.tsx` | Modify | Panel title translation rendering |
| `ui/src/components/ActivityBar.tsx` | Modify | Panel title translation rendering |

### 9.3 Locale Plugin Package (Independent Repository)

| File | Description |
|------|-------------|
| `com.axons.locale-zh-cn/manifest.json` | Chinese language pack manifest |
| `com.axons.locale-zh-cn/locales/frontend/*.json` | Frontend Chinese translations (7 files) |
| `com.axons.locale-zh-cn/locales/backend/messages.toml` | Backend Chinese translations |
| `com.axons.locale-zh-cn/locales/plugin/titles.json` | Plugin title Chinese translations |
| `com.axons.locale-zh-cn/README.md` | Language pack description |

## 10. Implementation Plan

### Phase 1: Backend Locale Plugin Support (2 days)

| Step | Effort | Deliverable |
|------|--------|-------------|
| manifest.go extension + validation | 0.5 day | `localization` category + `TitleI18n` + locale validation |
| PluginManager locale loading/unloading | 0.5 day | `loadLocalePlugins()` + `UnloadLocaleResources()` |
| API extension + Settings returns available_locales | 0.5 day | `/v1/plugins/locales` + Settings extension |
| Language fallback logic + SSE events | 0.5 day | Uninstall fallback + event broadcast |

### Phase 2: Frontend Language Switching (1 day)

| Step | Effort | Deliverable |
|------|--------|-------------|
| i18next http-backend adaptation | 0.5 day | Plugin resource path loading |
| Language tab + available languages dynamic update | 0.5 day | Settings → Language |

### Phase 3: Chinese Language Pack Creation (2 days)

| Step | Effort | Deliverable |
|------|--------|-------------|
| Frontend 7 JSON translations | 1 day | ~200 string translations |
| Backend TOML + plugin titles translations | 0.5 day | ~50 string translations |
| manifest.json + packaging + installation testing | 0.5 day | End-to-end verification |

### Phase 4: Validation (1 day)

| Step | Effort | Deliverable |
|------|--------|-------------|
| Full component Chinese/English switching regression | 0.5 day | No omissions, no garbled text |
| Locale plugin install/uninstall/switch full flow | 0.5 day | Lifecycle verification |

**Total: 6 days**

## 11. Extensibility

### 11.1 More Languages

Creating a new language pack only requires:
1. Copy the English template
2. Translate all strings
3. Write manifest.json
4. Package and publish

No changes to the axons main program code needed.

### 11.2 Cloud Marketplace (Phase 2)

Locale plugin packages can be uploaded to the plugin marketplace, and users can install them with one click from the Extensions panel:

```
Extensions panel
  → Category filter "localization"
  → Select "Chinese (Simplified)"
  → Click Install
  → Takes effect immediately (no restart needed)
```

### 11.3 Language Pack Version and Compatibility

Language packs declare `minAxonsVersion`; axons checks at startup:
- Version matches → Load normally
- Version mismatch → Skip + log warning + frontend notification "Language pack version incompatible, please update"

### 11.4 Partial Translation

Language packs don't need to be 100% translated. i18next's fallback mechanism ensures:
- Missing keys → Automatically fall back to English
- Missing namespaces → Entire namespace falls back to English
- Language packs can be progressively improved without affecting usability
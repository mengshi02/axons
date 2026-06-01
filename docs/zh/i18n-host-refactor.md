# Axons 宿主国际化改造方案

> 版本: v1.0 | 日期: 2026-05-16 | 状态: 已实现

## 一、目标

将 Axons 宿主（前端 UI + CLI + 后端 API）中的硬编码英文文本提取为 i18n 资源，实现：
- 默认英文（内嵌，零配置即可使用）
- 其他语言通过安装语言插件包支持（见配套方案《多语言插件包设计与实现》）
- 切换语言后前端即时生效，CLI/API 读取语言偏好

**不翻译的范围**：
- LLM 生成的 Chat 内容（由模型自行决定语言）
- API JSON 字段名和状态枚举值（`running`/`stopped` 等，是协议约定）
- 代码标识符和文件路径

## 二、技术选型

### 2.1 前端：react-i18next

| 维度 | 选择 | 理由 |
|------|------|------|
| 核心框架 | `i18next` + `react-i18next` | 社区主流，React 生态事实标准，IDE Web 也用此方案 |
| 语言包加载 | `i18next-http-backend` | 按需异步加载语言资源，避免首屏加载所有语言 |
| 语言检测 | `i18next-browser-languagedetector` | 自动检测浏览器语言，支持 localStorage 持久化 |
| 插值 | i18next 内置 | `{{count}}` / `{{name}}` 插值，支持复数形式 |
| 命名空间 | 按模块拆分 | 避免单文件过大，支持按需加载 |

**安装依赖**：
```bash
npm install i18next react-i18next i18next-http-backend i18next-browser-languagedetector
```

### 2.2 后端 Go：轻量自建 i18n bundle

不引入 `golang.org/x/text`（过重），自建极简方案：

| 组件 | 实现 | 说明 |
|------|------|------|
| 语言包格式 | `en.toml` / `zh-CN.toml` | TOML 可读性好，Go 标准库无需额外依赖 |
| 默认语言包 | `//go:embed` 内嵌 | 英文内嵌到二进制，零外部依赖 |
| 翻译函数 | `i18n.T(key, args...)` | 简单 key-value 查找 + `{{var}}` 插值 |
| 语言偏好来源 | Settings DB → `AXONS_LOCALE` 环境变量 | 优先级：Settings > 环境变量 > 默认 en |

## 三、前端改造

### 3.1 目录结构

```
ui/src/
├── i18n/
│   ├── index.ts              # i18next 初始化配置
│   ├── en/                   # 默认英文语言包（内嵌）
│   │   ├── common.json       # 通用文本：按钮、状态、错误
│   │   ├── settings.json     # Settings 面板
│   │   ├── panels.json       # 各分析面板
│   │   ├── chat.json         # AI Chat 面板
│   │   ├── activitybar.json  # ActivityBar + Footer
│   │   ├── dropzone.json     # 引导页 / DropZone
│   │   └── extensions.json   # 插件管理面板
│   └── zh-CN/                # 中文（由语言插件包提供，不在主仓库）
│       └── ...               # 同结构
```

### 3.2 i18next 初始化

```typescript
// ui/src/i18n/index.ts
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import HttpBackend from 'i18next-http-backend';
import LanguageDetector from 'i18next-browser-languagedetector';

// 内嵌英文资源（确保离线可用 + 零延迟）
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
  .use(HttpBackend)           // 异步加载其他语言（插件包提供）
  .use(LanguageDetector)      // 浏览器语言检测
  .use(initReactI18next)      // React 绑定
  .init({
    resources: {
      en: enResources,        // 英文内嵌，首屏零网络请求
    },
    fallbackLng: 'en',
    ns: ['common', 'settings', 'panels', 'chat', 'activitybar', 'dropzone', 'extensions'],
    defaultNS: 'common',
    interpolation: { escapeValue: false },  // React 已转义
    backend: {
      // 语言插件包的资源路径（动态函数，详见 4.8.3）
      // en 内嵌不走网络；其他语言由 daemon 静态路由服务（HandlePluginStaticFiles）
      loadPath: (lngs: string[], namespaces: string[]) => {
        const lng = lngs[0];
        if (lng === 'en') return '';  // en 内嵌，不加载

        // 查找该语言对应的插件 ID
        // 映射来源：GET /v1/plugins/locales（由语言插件包方案提供）
        const pluginId = localePluginMap[lng];
        if (!pluginId) return '';  // 未安装该语言包

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

**初始化语言→插件ID映射的时机问题**：`localePluginMap` 需要在 i18next 初始化前获取，否则 `changeLanguage` 触发 http-backend 加载时找不到 `pluginId`。解决方案：

```typescript
// ui/src/main.tsx — 先获取映射，再挂载 React
async function bootstrap() {
  // 1. 获取语言→插件映射（i18next 初始化需要此数据）
  const resp = await fetch('/v1/plugins/locales');
  const { locales } = await resp.json();
  window.__localePluginMap = Object.fromEntries(
    Object.entries(locales).map(([code, info]) => [code, info.pluginId])
  );

  // 2. 挂载 React（i18n/index.ts 中 import 时已初始化完毕）
  ReactDOM.createRoot(document.getElementById('root')!).render(<App />);
}
bootstrap();
```

> 如果映射获取失败（如网络异常），i18next loadPath 函数返回空字符串，`changeLanguage` 会 fallback 到 en，不影响基础功能。

### 3.3 Key 命名规范

### 3.3 Key 命名规范

采用 `语义路径` 命名，不按组件名（组件可能重构，语义相对稳定）：

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

### 3.4 组件改造示例

**改造前**：
```tsx
// CodeHealthPanel.tsx
<span className="text-sm font-semibold text-text-primary">Code Health</span>
<span>No hotspots found</span>
<span>{h.fan_in} callers</span>
```

**改造后**：
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

**ConfirmDialog 改造**：ConfirmDialog 的 `title`/`message`/`confirmLabel` 由调用方传入英文字符串，改造后调用方改为传入 i18n key 或翻译后的字符串：

```tsx
// 改造前：调用方传入硬编码英文
<ConfirmDialog title="Uninstall Plugin" message="Are you sure?" confirmLabel="Uninstall" />

// 改造后：调用方传入 t() 翻译后的字符串
const { t } = useTranslation('extensions');
<ConfirmDialog
  title={t('uninstallTitle')}
  message={t('uninstallMessage')}
  confirmLabel={t('action:uninstall')}
/>
```

### 3.5 复数形式处理

i18next 支持 Unicode CLDR 复数规则。`en/common.json` 中的 `unit` 命名空间需要区分单复数：

```jsonc
// en/common.json — unit 命名空间改造
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
// 使用：i18next 自动根据 count 值选择 _one/_other 后缀
<span>{t('common:unit.nodes', { count: 1 })}</span>   // → "1 node"
<span>{t('common:unit.nodes', { count: 5 })}</span>   // → "5 nodes"
```

> 中文等不区分复数的语言只需提供 `_other` 后缀的 key，i18next 自动 fallback。

### 3.6 面板注册表改造

[`useAppState.ts`](../ui/src/hooks/useAppState.ts:698) 中的面板 title 是静态字符串，需要改为 i18n key：

**改造前**：
```tsx
registerPanel({ id: 'codeHealth', title: 'Health', ... });
```

**改造后**：
```tsx
// 方案：title 改为 i18n key，在渲染时翻译
registerPanel({ id: 'codeHealth', title: 'panels:codeHealth.title', ... });

// Footer / ActivityBar 渲染时：
const { t } = useTranslation();
<span>{t(panel.title)}</span>  // t() 自动处理命名空间前缀
```

### 3.7 语言切换 UI

在 Settings 面板新增 **Language** tab（位于 Theme 旁）：

```tsx
// SettingsPanel.tsx — 新增 Language tab
<button onClick={() => setActiveTab('language')} className={tabClass}>
  <Globe className="w-3.5 h-3.5" />
  Language
</button>

// Language tab 内容
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

**可用语言列表来源**：
- 默认只有 `en`
- 安装语言插件后，插件注册 locale 资源到 i18next，语言列表自动扩展
- 后端 `GET /v1/settings` 返回 `available_locales` 字段

### 3.8 文件改动清单

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `ui/src/i18n/index.ts` | 新增 | i18next 初始化 + **http-backend 语言插件资源路径适配** |
| `ui/src/i18n/en/*.json` | 新增 (7 个) | 英文语言包 |
| `ui/src/main.tsx` | 修改 | 引入 `import './i18n'` |
| `ui/src/hooks/useAppState.ts` | 修改 | 面板 title 改为 i18n key |
| `ui/src/components/SettingsPanel.tsx` | 修改 | 文本 → `t()`，新增 Language tab + **监听 `locale.available` / `locale.unavailable` SSE 事件** |
| `ui/src/components/RightPanel.tsx` | 修改 | Chat UI 文本 → `t()` |
| `ui/src/components/DropZone.tsx` | 修改 | 引导页文本 → `t()` |
| `ui/src/components/BuildingState.tsx` | 修改 | 进度文本 → `t()` |
| `ui/src/components/CodeHealthPanel.tsx` | 修改 | 面板文本 → `t()` |
| `ui/src/components/GraphAnalyticsPanel.tsx` | 修改 | 面板文本 → `t()` |
| `ui/src/components/ImpactAnalysisPanel.tsx` | 修改 | 面板文本 → `t()` |
| `ui/src/components/CfgDataflowPanel.tsx` | 修改 | 面板文本 → `t()` |
| `ui/src/components/SequencePanel.tsx` | 修改 | 面板文本 → `t()` |
| `ui/src/components/ArchRulesPanel.tsx` | 修改 | 面板文本 → `t()` |
| `ui/src/components/ProcessPanel.tsx` | 修改 | 面板文本 → `t()` |
| `ui/src/components/ExtensionsPanel.tsx` | 修改 | 插件面板文本 → `t()` |
| `ui/src/components/ProjectSelector.tsx` | 修改 | 项目选择器文本 → `t()` |
| `ui/src/components/ActivityBar.tsx` | 修改 | title 属性 → `t()` |
| `ui/src/components/Footer.tsx` | 修改 | nodes/edges 文本 → `t()` |
| `ui/src/components/TopSearchBar.tsx` | 修改 | 搜索提示 → `t()` |
| `ui/src/components/AgentManagerPanel.tsx` | 修改 | Agent 管理文本 → `t()` |
| `ui/src/components/FileTreePanel.tsx` | 修改 | 右键菜单 → `t()` |
| `ui/src/components/ChangeList.tsx` | 修改 | 变更列表文本 → `t()` |
| `ui/src/components/ConfirmDialog.tsx` | 修改 | 确认对话框 → `t()` |
| `ui/src/components/UnifiedImportDialog.tsx` | 修改 | 导入对话框 → `t()` |
| `ui/src/components/terminal/TerminalPanel.tsx` | 修改 | 终端面板 → `t()` |
| `ui/src/lib/panelRegistry.ts` | 修改 | PanelDef.title 类型注释更新 |

## 四、后端改造

### 4.1 目录结构

```
internal/i18n/
├── i18n.go            # Bundle + T() 函数
├── locales/
│   ├── en.toml        # 默认英文（go:embed 内嵌）
│   └── zh-CN.toml     # 中文（语言插件包提供时动态加载）
```

### 4.2 核心实现

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
	// 加载内嵌英文
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
			return key // key 本身作为 fallback
		}
	}

	if len(args) > 0 {
		// 支持 {{name}} 插值
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
// 支持运行时增量加载（语言插件包安装后调用，无需重启）。
func LoadBundle(locale, dir string) error {
	// 扫描 dir 下的 .toml 文件，合并到 bundles[locale]
	// ...
}

// UnloadBundle removes a locale bundle from memory.
// 语言插件包卸载时调用，释放内存 + 确保后续 T() 调用 fallback 到 en。
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

### 4.3 英文语言包

```toml
# internal/i18n/locales/en.toml

# CLI 命令描述
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

# API 错误消息（面向用户）
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

# Agent profile 名称和描述
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

### 4.4 Cobra 命令改造

```go
// cmd/axons/cmd/build.go — 改造前
var buildCmd = &cobra.Command{
    Use:   "build [path]",
    Short: "Build the code graph",
    Long:  `Build a code graph from the specified directory...`,
}

// 改造后
var buildCmd = &cobra.Command{
    Use:   "build [path]",
    Short: i18n.T("cmd.build.short"),
    Long:  i18n.T("cmd.build.long"),
}
```

**注意**：Cobra 命令在 `init()` 阶段注册，此时语言偏好可能尚未加载。解决方案：
- Cobra `Short`/`Long` 支持 `func() string`（Cobra v1.8+），延迟求值
- 或在 `rootCmd.PersistentPreRun` 中调用 `i18n.SetLocale()`

### 4.5 Agent Profile 改造

```go
// internal/agent/profiles.go — 改造后
var BuiltinProfiles = []AgentProfile{
    {
        ID:          "default",
        Name:        i18n.T("agent.default.name"),
        Description: i18n.T("agent.default.description"),
        // SystemPrompt 不翻译 — 它是给 LLM 看的指令，英文效果最好
    },
}
```

**SystemPrompt 不翻译**：Agent 的 SystemPrompt 是给 LLM 的指令，英文 prompt 在大多数 LLM 上效果更好。如果用户使用中文 LLM，可由语言插件包提供 `agent.default.systemPrompt` 覆盖。

**运行时切换语言的更新问题**：`BuiltinProfiles` 是包级变量，`i18n.T()` 在 init 时求值后不会自动更新。运行时切换语言后，API 返回的 Agent Name/Description 仍是旧语言。解决方案：

```go
// 方案：改为延迟求值函数，不缓存翻译结果
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

API handler 调用 `GetBuiltinProfiles()` 而非直接引用 `BuiltinProfiles` 变量，确保每次请求都用当前 locale 翻译。

### 4.6 API Handler 改造

```go
// internal/plugin/handlers.go — 改造示例
// 改造前
writeJSONError(w, 404, "NOT_FOUND", "plugin not running")

// 改造后
writeJSONError(w, 404, "NOT_FOUND", i18n.T("api.error.pluginNotFound"))
```

### 4.7 语言偏好持久化

```go
// Settings 表新增 locale 设置
// GET /v1/settings 返回:
{
  "settings": {
    "locale": { "locale": { "value": "en" } }
  },
  // 新增：可用语言列表（由语言插件包动态提供，详见多语言插件包方案）
  "available_locales": [
    { "code": "en", "nativeName": "English", "englishName": "English" }
  ]
}

// PUT /v1/settings 更新:
{ "category": "locale", "settings": { "locale": "zh-CN" } }
```

**Settings handler 联动**：当 locale 设置更新时，后端必须同步 `i18n.SetLocale()`，确保后续 API 响应中的错误消息也使用对应语言：

```go
// internal/api/server.go — handleUpdateSettings 中新增 locale 联动
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
    // ... 现有逻辑 ...

    // 如果更新了 locale 设置，同步到 i18n bundle
    if category == "locale" {
        if localeVal, ok := settings["locale"]; ok {
            i18n.SetLocale(localeVal)
        }
    }
}
```

### 4.8 语言插件包联动

宿主系统需要为语言插件包预留三个联动点，使语言插件包的安装/卸载能即时生效（无需重启）：

#### 4.8.1 后端联动：PluginManager

```go
// internal/plugin/manager.go — ImportPlugin / UninstallPlugin 中新增 localization 类别判断

func (m *Manager) ImportPlugin(archivePath string) error {
    // ... 现有解压 + 校验逻辑 ...

    // 联动：如果是 localization 类别，立即加载 i18n 资源 + 广播 SSE 事件
    if manifest.Category == "localization" && manifest.Frontend != nil && manifest.Frontend.Locale != nil {
        locale := manifest.Frontend.Locale

        // 1. 加载后端 Go i18n 资源
        for _, res := range locale.BackendResources {
            path := filepath.Join(manifest.Dir, res)
            i18n.LoadBundle(locale.Language, filepath.Dir(path))
        }

        // 2. 追加到可用语言列表
        m.availableLocales = append(m.availableLocales, LocaleInfo{...})

        // 3. SSE 广播 locale.available → 前端即时更新 Language 列表
        m.emitEvent("locale.available", map[string]any{
            "locale":      locale.Language,
            "pluginId":    manifest.ID,
            "nativeName":  locale.DisplayName.Native,
            "englishName": locale.DisplayName.English,
        })
    }

    // ... 现有返回逻辑 ...
}

func (m *Manager) UninstallPlugin(pluginID string) error {
    // ... 现有停止进程 + 删除目录逻辑 ...

    // 联动：如果是 localization 类别，卸载 i18n 资源 + 回退 + 广播 SSE 事件
    if manifest.Category == "localization" {
        locale := manifest.Frontend.Locale.Language

        // 1. 从内存卸载后端 Go i18n 资源
        i18n.UnloadBundle(locale)

        // 2. 从可用语言列表移除
        // ...

        // 3. 如果当前正在使用该语言，回退到 en
        if i18n.GetLocale() == locale {
            i18n.SetLocale("en")
        }

        // 4. SSE 广播 locale.unavailable → 前端即时回退 + 更新列表
        m.emitEvent("locale.unavailable", map[string]any{
            "locale":   locale,
            "pluginId": pluginID,
            "fallback": "en",
        })
    }

    // ... 现有返回逻辑 ...
}
```

> 完整的 `loadSingleLocalePlugin()` / `unloadSingleLocalePlugin()` 实现详见《多语言插件包设计与实现方案》第三节。

#### 4.8.2 前端联动：useEventStream

[`useEventStream`](../ui/src/hooks/useEventStream.ts) 需要新增两种 SSE 事件回调：

```typescript
// ui/src/hooks/useEventStream.ts — 回调接口扩展
interface UseEventStreamOptions {
  // ... 现有回调 ...

  /** 语言插件安装后可用 */
  onLocaleAvailable?: (data: {
    locale: string;       // "zh-CN"
    pluginId: string;     // "com.axons.locale-zh-cn"
    nativeName: string;   // "简体中文"
    englishName: string;  // "Chinese (Simplified)"
  }) => void;

  /** 语言插件卸载后不可用 */
  onLocaleUnavailable?: (data: {
    locale: string;       // "zh-CN"
    pluginId: string;     // "com.axons.locale-zh-cn"
    fallback: string;     // "en"
  }) => void;
}

// 事件分发 switch 中新增：
case 'locale.available':
  opts.onLocaleAvailable?.(data);
  break;
case 'locale.unavailable':
  opts.onLocaleUnavailable?.(data);
  break;
```

#### 4.8.3 前端联动：i18next 初始化

i18next 的 http-backend 需要配置插件资源路径，使 `changeLanguage('zh-CN')` 时能从语言插件包加载资源：

```typescript
// ui/src/i18n/index.ts — http-backend loadPath 配置
backend: {
  loadPath: (lngs: string[], namespaces: string[]) => {
    const lng = lngs[0];
    if (lng === 'en') return '';  // en 内嵌，不加载

    // 查找该语言对应的插件 ID
    // 映射来源：GET /v1/plugins/locales（由语言插件包方案提供）
    const pluginId = localePluginMap[lng];
    if (!pluginId) return '';

    // 由 daemon 静态路由服务，HandlePluginStaticFiles 已支持未运行插件的文件服务
    return `/plugins/${pluginId}/locales/frontend/{{ns}}.json`;
  },
}
```

> 关键：[`HandlePluginStaticFiles`](../internal/plugin/proxy.go:85) 已支持未运行插件的静态文件服务（fallback 到 `ScanPlugins` 查找目录），语言插件无需启动进程即可服务 JSON 资源文件。

### 4.9 后端文件改动清单

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `internal/i18n/i18n.go` | 新增 | Bundle + T() + SetLocale() + **LoadBundle() + UnloadBundle() + GetLocale()** |
| `internal/i18n/locales/en.toml` | 新增 | 英文语言包 |
| `cmd/axons/cmd/*.go` (18 个) | 修改 | Short/Long → `i18n.T()` |
| `internal/api/server.go` | 修改 | 错误消息 → `i18n.T()`；**Settings locale 更新时调用 `i18n.SetLocale()` 联动** |
| `internal/api/handlers_*.go` | 修改 | 面向用户错误消息 → `i18n.T()` |
| `internal/agent/profiles.go` | 修改 | Name/Description → `i18n.T()` |
| `internal/plugin/manager.go` | 修改 | **`ImportPlugin` / `UninstallPlugin` 中 `localization` 类别联动：加载/卸载 i18n 资源 + SSE 广播** |
| `ui/src/hooks/useEventStream.ts` | 修改 | **新增 `onLocaleAvailable` / `onLocaleUnavailable` SSE 事件回调** |
| `go.mod` | 修改 | 新增 `github.com/BurntSushi/toml` 依赖 |

## 五、实施计划

### 阶段 1：基础设施搭建（1.5 天）

| 步骤 | 工时 | 交付物 |
|------|------|--------|
| 安装 react-i18next 全家桶 + 配置 | 0.5 天 | `ui/src/i18n/index.ts` |
| 创建 7 个 en.json 英文语言包 | 0.5 天 | `ui/src/i18n/en/*.json` |
| Go i18n 基础设施 + en.toml | 0.5 天 | `internal/i18n/` |

### 阶段 2：前端改造（4 天）

| 步骤 | 工时 | 交付物 |
|------|------|--------|
| SettingsPanel（最复杂） | 1 天 | 全部文本 t() 化 |
| RightPanel + Chat UI | 0.5 天 | |
| DropZone + BuildingState + ProjectSelector | 0.5 天 | |
| 6 个分析面板 + ExtensionsPanel | 1 天 | |
| 其余组件 + 面板注册表 | 0.5 天 | |
| Language tab + 切换功能 | 0.5 天 | Settings → Language |

### 阶段 3：后端改造（1.5 天）

| 步骤 | 工时 | 交付物 |
|------|------|--------|
| 18 个 CLI 命令 | 0.5 天 | |
| API 错误消息 + Agent profile | 0.5 天 | |
| locale 持久化 + 集成测试 | 0.5 天 | |

### 阶段 4：回归验证（1 天）

| 步骤 | 工时 | 交付物 |
|------|------|--------|
| 全组件英文回扫 | 0.5 天 | 确认零遗漏 |
| 切换语言功能测试 | 0.5 天 | |

**总计：8 天**

## 六、风险与缓解

### 6.1 高风险

| # | 风险 | 说明 | 缓解 |
|---|------|------|------|
| H1 | Cobra init 阶段语言未初始化 | 18 个 CLI 命令在 `init()` 阶段注册，此时语言偏好尚未加载。设计依赖 Cobra v1.8 的 `Short: func() string` 延迟求值，若当前项目 Cobra 版本低于 v1.8 则方案不可行 | **落地前先检查 `go.mod` 中 Cobra 版本**，若低于 v1.8 需先升级；升级后验证现有命令行为无回归 |
| H2 | 面板 title 运行时不更新 | `useAppState.ts` 中 `registerPanel({ title: 'Health' })` 是同步静态注册，title 存字面量字符串。改为 i18n key 后需同时改 `PanelDef` 类型定义 + Footer / ActivityBar 所有渲染侧 | title 存储 i18n key，渲染侧 `t()` 翻译，**所有消费方必须一次性改完，不能半改**，否则出现 key 原文直接展示 |
| H3 | Agent Profile Name/Description 运行时切换不更新 | `BuiltinProfiles` 是包级变量，`i18n.T()` 在 init 时求值后不再更新 | 改为 `GetBuiltinProfiles()` 函数延迟求值；**落地时 grep 所有引用点**，确保全部改为函数调用 |
| H4 | 前端 `localePluginMap` 启动时序 | `main.tsx` 需先 `fetch('/v1/plugins/locales')` 获取映射再挂载 React，阻塞首屏渲染 | 设 2s 超时，超时则跳过映射获取，默认 en 内嵌资源不影响使用；映射获取失败时 i18next loadPath 返回空串，自动 fallback 到 en |

### 6.2 中风险

| # | 风险 | 说明 | 缓解 |
|---|------|------|------|
| M1 | 后端 `i18n.T()` 全局 locale 单实例 | `i18n.go` 中 `locale` 是全局变量，不支持 per-request locale（多用户场景） | 当前 Axons 是单用户桌面应用，暂无问题；**在代码注释中标注此限制**，避免未来误用 |
| M2 | API 错误消息 i18n 化 | `internal/api/` 下 12+ 文件有大量 `writeError()` 调用，部分消息是 `err.Error()` 动态内容，不适合 `i18n.T()` | **区分两类**：面向用户的固定消息用 `i18n.T()`；技术性动态消息（如 `err.Error()`、`fmt.Sprintf("Failed to ...: %v", err)`）保持英文原样 |
| M3 | Manifest 校验与 localization 插件冲突 | 当前 `ValidateManifest` 要求 `backend` 和 `frontend` 至少一个非空，但 localization 插件 `backend=null` 且 `frontend.entry=null` | **先调整校验逻辑**：localization 类别允许 `backend=null` + `frontend.entry=null`，但要求 `frontend.locale` 必须存在 |
| M4 | SSE 事件类型扩展 | `useEventStream.ts` 的 `EventType` 联合类型和 `addEventListener` 是硬编码模式，新增 `locale.available` / `locale.unavailable` 需在 EventType + addEventListener + options interface 三处同步修改 | 改动繁琐但可控，**注意 `EventType` 联合类型与后端事件名保持一致**；建议同步更新 `useEventStream` 的 JSDoc |
| M5 | BurntSushi/toml 依赖引入 | `go.mod` 需新增 `github.com/BurntSushi/toml` | **落地前检查 `go.sum`** 中是否已间接依赖该库，评估版本冲突风险 |
| M6 | Settings locale 更新不同步后端 | 前端切换语言后若后端未同步，API 错误消息仍用旧语言 | `handleUpdateSettings` 中检测 `category == "locale"` → 调用 `i18n.SetLocale()` |
| M7 | 语言插件安装/卸载后前端未感知 | 前端 Language 列表不实时更新 | SSE 广播 `locale.available` / `locale.unavailable` 事件；前端 `useEventStream` 监听并即时更新 Language 列表 |
| M8 | 卸载当前语言包后界面仍显示已卸载语言 | 用户正在使用的语言包被卸载，界面未回退 | 后端 `UninstallPlugin` 检测 `i18n.GetLocale() == locale` → 自动 `i18n.SetLocale("en")`；前端收到 `locale.unavailable` 事件 → `i18next.changeLanguage(fallback)` |

### 6.3 低风险

| # | 风险 | 缓解 |
|---|------|------|
| L1 | i18n key 命名混乱导致维护困难 | 严格 `语义路径` 命名 + Code Review 检查 |
| L2 | en.json 和代码不同步 | CI 检查：grep 未 t() 化的硬编码英文文本 |
| L3 | SystemPrompt 翻译降低 LLM 效果 | SystemPrompt 不翻译，由语言插件包按需覆盖 |
| L4 | http-backend `{{ns}}` 占位符兼容性 | 确认 `i18next-http-backend` 版本对 `loadPath` 函数返回值中 `{{ns}}` 的替换支持 |

### 6.4 实施建议

1. **先确认 Cobra 版本**：检查 `go.mod`，若 Cobra < v1.8 需优先升级，否则 CLI 命令改造方案需调整（H1）
2. **前端分 namespace 整批推进**：按 common → settings → panels → chat → activitybar → dropzone → extensions 顺序，每个 namespace 做完即完整可用，避免中英文混杂 UI
3. **API 错误消息分两类处理**：固定用户消息用 `i18n.T()`，技术性 `err.Error()` 保持英文，避免翻译运行时错误信息（M2）
4. **首屏映射获取设超时**：`main.tsx` 中 fetch 映射建议 2s 超时，超时即 fallback 到 en 内嵌，避免白屏（H4）
5. **加 CI 检查**：用脚本 grep 未 t() 化的硬编码英文文本，防止增量代码回退（L2）
6. **面板 title 一次性改完所有消费方**：PanelDef.title 语义变更（字符串 → i18n key），Footer / ActivityBar / Settings 等所有渲染侧必须同步改，不能半改（H2）

## 七、预期收益

| # | 收益 | 说明 |
|---|------|------|
| 1 | 国际化基础设施 | 一次投入，后续支持任何语言只需制作语言包，零代码改动 |
| 2 | 语言包即插件 | 复用现有插件系统全部基础设施（安装/卸载/市场），无需额外分发机制 |
| 3 | 即时生效 | 安装/卸载/切换语言无需重启 daemon，SSE 事件驱动，用户体验流畅 |
| 4 | 前后端统一语言切换 | 前端 `i18next.changeLanguage()` + 后端 `i18n.SetLocale()` 联动，API 错误消息也跟随语言 |
| 5 | 英文零成本 | 默认英文内嵌，无需安装任何插件，零配置零网络请求 |
| 6 | 社区贡献 | 任何人可制作语言包，通过插件市场分发，降低官方翻译负担 |
| 7 | 可扩展性 | 插件 title 的 `titleI18n` 声明 + 语言包 `titles.json` 双层覆盖，灵活度高 |
| 8 | 渐进式翻译 | 语言包不需要 100% 翻译，i18next fallback 机制确保缺失 key 自动回退到英文，不影响使用 |
# Axons 多语言插件包设计与实现方案

> 版本: v1.0 | 日期: 2026-05-16 | 状态: 已实现

## 一、设计理念

**核心思路**：将非英文语言支持做成一种特殊类型的插件包（`category: "localization"`），复用现有插件系统的安装、管理、生命周期机制，让语言包独立发布、独立更新。

**设计原则**：

| 原则 | 说明 |
|------|------|
| 默认英文零依赖 | 英文内嵌到主程序，无需安装任何插件即可使用 |
| 语言包即插件 | 复用插件系统的全部基础设施（安装/卸载/版本管理/市场） |
| 前后端统一加载 | 一个语言插件包同时提供前端 JSON 和后端 TOML 资源 |
| 按需加载 | 只有用户选择了某语言，才加载对应资源 |
| 社区可贡献 | 任何人可以制作语言包，通过插件市场分发 |

## 二、语言插件包规范

### 2.1 目录结构

```
com.axons.locale-zh-cn/
├── manifest.json                # 插件清单
├── locales/
│   ├── frontend/                # 前端 i18next 资源
│   │   ├── common.json         # 对应 ui/src/i18n/en/common.json
│   │   ├── settings.json
│   │   ├── panels.json
│   │   ├── chat.json
│   │   ├── activitybar.json
│   │   ├── dropzone.json
│   │   └── extensions.json
│   ├── backend/                 # 后端 Go i18n 资源
│   │   └── messages.toml        # 对应 internal/i18n/locales/en.toml
│   └── plugin/                  # 插件 manifest i18n 资源
│       └── titles.json          # 其他插件的 title 翻译
└── README.md                    # 语言包说明（翻译指南、贡献者等）
```

### 2.2 manifest.json

```jsonc
{
  "id": "com.axons.locale-zh-cn",
  "name": "Chinese (Simplified) Language Pack",
  "version": "1.0.0",
  "description": "简体中文语言包，为 Axons 提供完整的中文界面翻译",
  "author": "axons-community",
  "icon": "icon.svg",
  "category": "localization",
  "minAxonsVersion": "0.8.0",

  // 语言包特有声明
  "backend": null,                // 语言包无后端进程

  "frontend": {
    "entry": null,                // 无 UI 入口组件（语言包不需要面板）
    "locale": {
      "language": "zh-CN",       // BCP 47 语言标签
      "displayName": {
        "native": "简体中文",     // 原生语言名（用于 Language 设置页）
        "english": "Chinese (Simplified)"  // 英文名
      },
      // 前端资源路径（相对于插件根目录）
      "resources": [
        "locales/frontend/common.json",
        "locales/frontend/settings.json",
        "locales/frontend/panels.json",
        "locales/frontend/chat.json",
        "locales/frontend/activitybar.json",
        "locales/frontend/dropzone.json",
        "locales/frontend/extensions.json"
      ],
      // 后端资源路径
      "backendResources": [
        "locales/backend/messages.toml"
      ],
      // 其他插件 title 翻译路径
      "pluginTitles": "locales/plugin/titles.json"
    }
  }
}
```

### 2.3 category 校验扩展

现有 [`ValidCategories`](../internal/plugin/manifest.go:96) 需要新增 `localization`：

```go
// internal/plugin/manifest.go
var ValidCategories = map[string]bool{
    "analysis":      true,
    "visualization": true,
    "search":        true,
    "productivity":  true,
    "localization":  true,  // 新增
}
```

### 2.4 locale 字段校验

```go
// manifest 校验新增：category=localization 时 frontend.locale 必须存在
func ValidateManifest(m *PluginManifest) error {
    // ... 现有校验 ...

    if m.Category == "localization" {
        if m.Frontend == nil || m.Frontend.Locale == nil {
            return fmt.Errorf("manifest: localization plugin must declare frontend.locale")
        }
        if m.Frontend.Locale.Language == "" {
            return fmt.Errorf("manifest: frontend.locale.language is required")
        }
        // BCP 47 格式校验（zh-CN, en, ja, ko 等）
        if !isValidBCP47(m.Frontend.Locale.Language) {
            return fmt.Errorf("manifest: frontend.locale.language must be valid BCP 47 tag, got %q", m.Frontend.Locale.Language)
        }
        if len(m.Frontend.Locale.Resources) == 0 {
            return fmt.Errorf("manifest: frontend.locale.resources must have at least one file")
        }
        // backend 必须为 null
        if m.Backend != nil {
            return fmt.Errorf("manifest: localization plugin must not have backend (must be null)")
        }
        // frontend.entry 必须为 null
        if m.Frontend.Entry != "" {
            return fmt.Errorf("manifest: localization plugin must not have frontend.entry")
        }
        // frontend.panels 必须为空
        if len(m.Frontend.Panels) > 0 {
            return fmt.Errorf("manifest: localization plugin must not declare frontend.panels")
        }
    }
    return nil
}
```

## 三、加载机制

### 3.1 核心特性：安装后无需重启即可切换语言

语言插件包的加载/卸载/切换全程在运行时完成，**不需要重启 axons daemon**：

| 操作 | 是否需要重启 | 原理 |
|------|-------------|------|
| 安装语言插件包 | 否 | `i18n.LoadBundle()` 是纯内存 map 写入，即时生效 |
| 切换语言 | 否 | `i18n.SetLocale()` 改全局变量；`i18next.changeLanguage()` 触发 React 重渲染 |
| 卸载语言插件包 | 否 | `i18n.UnloadBundle()` 从内存 map 删除；前端自动回退到 en |

**关键技术点**：

1. **后端即时加载**：`i18n.LoadBundle(locale, dir)` 将 TOML 文件解析后写入 `bundles[locale]` map，后续 `i18n.T()` 调用立即命中新语言
2. **前端按需加载**：i18next 的 `changeLanguage('zh-CN')` 触发 http-backend 请求 `/plugins/{pluginId}/locales/frontend/*.json`；现有 [`HandlePluginStaticFiles`](../internal/plugin/proxy.go:85) **已支持未运行插件的静态文件服务**（fallback 到 `ScanPlugins` 查找目录），无需插件进程启动即可服务 JSON 文件
3. **SSE 事件驱动**：语言插件安装/卸载后，PluginManager 广播 `locale.available` / `locale.unavailable` SSE 事件，前端收到后即时更新 Language 设置页的可用语言列表

### 3.2 整体流程

```
axons 启动
  │
  ├── PluginManager.ScanPlugins()
  │     └── 扫描 ~/.axons/plugins/*/manifest.json
  │           ├── 识别 category=localization 插件
  │           └── 读取 frontend.locale 声明
  │
  ├── 加载已安装语言资源（启动时）
  │     ├── Go 后端：读取 backendResources → i18n.LoadBundle(locale, dir)
  │     └── 记录可用语言列表到 PluginManager.availableLocales
  │
  ├── SSE 推送 availableLocales 列表给前端
  │
  └── 前端 i18next
        ├── en 内嵌，默认加载
        └── 其他语言：i18next-http-backend 从 /plugins/:id/locales/frontend/*.json 按需加载
```

### 3.3 运行时安装流程（无需重启）

```
用户导入语言插件包 → POST /v1/plugins/import
  │
  ├── PluginManager.ImportPlugin() 成功
  │     └── 检测 category=localization
  │
  ├── 后端即时加载
  │     ├── i18n.LoadBundle("zh-CN", pluginDir+"/locales/backend/")
  │     └── PluginManager.availableLocales 新增 { code: "zh-CN", ... }
  │
  ├── SSE 广播 locale.available
  │     └── { locale: "zh-CN", pluginId: "com.axons.locale-zh-cn",
  │          nativeName: "简体中文", englishName: "Chinese (Simplified)" }
  │
  └── 前端收到 SSE 事件
        ├── Settings → Language tab 刷新可用语言列表
        └── "简体中文" 选项立即出现，点击即可切换
```

### 3.4 运行时卸载流程（无需重启）

```
用户卸载语言插件 → DELETE /v1/plugins/:id
  │
  ├── PluginManager 检测 category=localization
  │
  ├── 后端即时卸载
  │     ├── i18n.UnloadBundle("zh-CN")     // 从内存 map 删除
  │     ├── PluginManager.availableLocales 移除该语言
  │     └── 如果当前 locale == "zh-CN" → i18n.SetLocale("en") 自动回退
  │
  ├── SSE 广播 locale.unavailable
  │     └── { locale: "zh-CN", pluginId: "com.axons.locale-zh-cn", fallback: "en" }
  │
  └── 前端收到 SSE 事件
        ├── 如果当前语言 == "zh-CN" → i18next.changeLanguage('en') 自动回退
        ├── Settings → Language tab 刷新可用语言列表
        └── 显示 toast："当前语言包已卸载，已切换为 English"
```

### 3.5 后端加载（启动时 + 运行时增量）

```go
// internal/plugin/manager.go — 新增 locale 加载逻辑

// loadLocalePlugins 加载所有 localization 类别插件的资源
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

        // 1. 加载后端 Go i18n 资源
        for _, res := range locale.BackendResources {
            path := filepath.Join(inst.Manifest.Dir, res)
            if err := i18n.LoadBundle(locale.Language, filepath.Dir(path)); err != nil {
                logger.S().Warnw("Failed to load backend locale", "locale", locale.Language, "error", err)
            }
        }

        // 2. 记录可用语言
        locales = append(locales, LocaleInfo{
            Code:        locale.Language,
            NativeName:  locale.DisplayName.Native,
            EnglishName: locale.DisplayName.English,
            PluginID:    inst.Manifest.ID,
        })
    }

    // 3. 存储可用语言列表（供 API 返回）
    m.availableLocales = locales

    return nil
}

// LocaleInfo 描述一个可用的语言
type LocaleInfo struct {
    Code        string `json:"code"`        // "zh-CN"
    NativeName  string `json:"nativeName"`  // "简体中文"
    EnglishName string `json:"englishName"` // "Chinese (Simplified)"
    PluginID    string `json:"pluginId"`    // "com.axons.locale-zh-cn"
}

// loadSingleLocalePlugin 加载单个语言插件资源（运行时增量，无需重启）
// 调用时机：
//   - 启动时：loadLocalePlugins() 循环调用
//   - 运行时：ImportPlugin() 成功后调用，实现安装即生效
func (m *Manager) loadSingleLocalePlugin(manifest *PluginManifest) {
    locale := manifest.Frontend.Locale
    if locale == nil {
        return
    }

    // 1. 加载后端 Go i18n 资源到内存
    for _, res := range locale.BackendResources {
        path := filepath.Join(manifest.Dir, res)
        if err := i18n.LoadBundle(locale.Language, filepath.Dir(path)); err != nil {
            logger.S().Warnw("Failed to load backend locale", "locale", locale.Language, "error", err)
        }
    }

    // 2. 追加到可用语言列表
    m.availableLocales = append(m.availableLocales, LocaleInfo{
        Code:        locale.Language,
        NativeName:  locale.DisplayName.Native,
        EnglishName: locale.DisplayName.English,
        PluginID:    manifest.ID,
    })

    // 3. SSE 广播 locale.available 事件 → 前端即时更新 Language 列表
    m.emitEvent("locale.available", map[string]any{
        "locale":      locale.Language,
        "pluginId":    manifest.ID,
        "nativeName":  locale.DisplayName.Native,
        "englishName": locale.DisplayName.English,
    })

    logger.S().Infow("Locale plugin loaded", "locale", locale.Language, "pluginId", manifest.ID)
}

// ImportPlugin 联动 — 在现有 ImportPlugin 成功后追加 locale 加载逻辑
// （伪代码，展示插入点）
func (m *Manager) ImportPlugin(archivePath string) error {
    // ... 现有解压 + 校验逻辑 ...

    // 新增：如果是 localization 类别，立即加载资源 + 广播事件
    if manifest.Category == "localization" {
        m.loadSingleLocalePlugin(&manifest)
    }

    // ... 现有返回逻辑 ...
}

// UninstallPlugin 联动 — 在现有卸载逻辑中追加 locale 清理
func (m *Manager) UninstallPlugin(pluginID string) error {
    // ... 现有停止进程 + 删除目录逻辑 ...

    // 新增：如果是 localization 类别，卸载 i18n 资源 + 广播事件
    inst, ok := m.GetInstance(pluginID)
    if ok && inst.Manifest.Category == "localization" {
        m.unloadSingleLocalePlugin(pluginID, inst.Manifest)
    }

    // ... 现有返回逻辑 ...
}

// unloadSingleLocalePlugin 卸载单个语言插件资源（运行时，无需重启）
func (m *Manager) unloadSingleLocalePlugin(pluginID string, manifest *PluginManifest) {
    locale := manifest.Frontend.Locale.Language

    // 1. 从内存卸载后端 Go i18n 资源
    i18n.UnloadBundle(locale)

    // 2. 从可用语言列表移除
    m.availableLocales = slices.DeleteFunc(m.availableLocales, func(l LocaleInfo) bool {
        return l.PluginID == pluginID
    })

    // 3. 如果当前正在使用该语言，回退到 en
    if i18n.GetLocale() == locale {
        i18n.SetLocale("en")
    }

    // 4. SSE 广播 locale.unavailable 事件 → 前端即时回退 + 更新列表
    m.emitEvent("locale.unavailable", map[string]any{
        "locale":   locale,
        "pluginId": pluginID,
        "fallback": "en",
    })

    logger.S().Infow("Locale plugin unloaded", "locale", locale, "pluginId", pluginID)
}
```

### 3.6 前端加载

i18next 的 `http-backend` 从 daemon 静态路由加载语言资源：

```
前端切换语言 → i18next.changeLanguage('zh-CN')
  → http-backend 检测到 zh-CN 资源未加载
  → 请求 GET /plugins/com.axons.locale-zh-cn/locales/frontend/common.json
  → daemon 静态路由服务文件
  → i18next 合并资源到 zh-CN 命名空间
  → React 组件自动重渲染
```

**http-backend loadPath 配置**：

```typescript
// ui/src/i18n/index.ts — 关键配置
backend: {
  loadPath: (lngs: string[], namespaces: string[]) => {
    // 只有非 en 语言才走插件路径加载
    const lng = lngs[0];
    if (lng === 'en') return '';  // en 内嵌，不加载

    // 查找该语言对应的插件 ID
    const pluginId = getLocalePluginId(lng);  // 从 Settings API 获取映射
    if (!pluginId) return '';  // 没有安装该语言包

    return `/plugins/${pluginId}/locales/frontend/{{ns}}.json`;
  },
}
```

**语言→插件ID 映射**：前端启动时从 API 获取：

```typescript
// GET /v1/plugins/locales →
// { "zh-CN": "com.axons.locale-zh-cn", "ja": "com.axons.locale-ja" }
```

**映射获取时机**：`localePluginMap` 必须在 i18next 初始化前就绪，否则 `changeLanguage` 触发 http-backend 加载时找不到 `pluginId`。解决方案：`main.tsx` 中先 `fetch('/v1/plugins/locales')` 获取映射存入全局变量，再挂载 React 根组件。详见宿主方案 Section 3.2。

**http-backend `{{ns}}` 占位符**：i18next-http-backend 支持在 `loadPath` 函数返回值中使用 `{{ns}}` 占位符。当 `loadPath` 是函数时，http-backend 会对返回的模板字符串做 `{{lng}}` 和 `{{ns}}` 替换后再发起请求。因此 `/plugins/${pluginId}/locales/frontend/{{ns}}.json` 中的 `{{ns}}` 会被替换为实际命名空间名（如 `common`、`settings`）。

### 3.7 语言切换流程

```
用户在 Settings → Language 选择 "简体中文"
  │
  ├── 前端
  │     ├── i18next.changeLanguage('zh-CN')
  │     │     └── http-backend 自动加载 zh-CN 资源
  │     ├── localStorage.setItem('axons-locale', 'zh-CN')
  │     └── React 组件自动重渲染（useTranslation hook）
  │
  └── 后端
        ├── fetch PUT /v1/settings { category: "locale", settings: { locale: "zh-CN" } }
        ├── server 收到后调用 i18n.SetLocale("zh-CN")
        └── 后续 API 响应中的错误消息自动使用中文
```

## 四、插件 title 国际化

### 4.1 问题

其他插件（非语言插件）的 [`manifest.json`](../internal/plugin/manifest.go:73) 中 `panels[].title` 和 `commands[].title` 是静态英文字符串，切换语言后不会变化。

### 4.2 方案：titleI18n 声明 + 语言包覆盖

**方式一：插件自行声明 titleI18n（推荐）**

```jsonc
// 插件 manifest.json
{
  "frontend": {
    "panels": [{
      "id": "huggingface",
      "title": "Hugging Face",          // 默认（英文）
      "titleI18n": {                      // 可选 i18n 覆盖
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

**方式二：语言插件包集中提供（补充）**

语言插件包可以为其他插件提供 title 翻译，覆盖插件自身未声明的情况：

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

**优先级**：插件自身 `titleI18n` > 语言包 `titles.json` > 默认 `title`

### 4.3 数据结构扩展

```go
// internal/plugin/manifest.go — PanelDef 新增 titleI18n
type PanelDef struct {
    ID        string            `json:"id"`
    Title     string            `json:"title"`
    TitleI18n map[string]string `json:"titleI18n,omitempty"`  // 新增
    Icon      string            `json:"icon"`
    Location  string            `json:"location"`
    Activator string            `json:"activator"`
    FooterSlot string           `json:"footerSlot"`
}

// CommandDef 新增 titleI18n
type CommandDef struct {
    ID        string            `json:"id"`
    Title     string            `json:"title"`
    TitleI18n map[string]string `json:"titleI18n,omitempty"`  // 新增
    Shortcut  string            `json:"shortcut"`
}
```

### 4.4 前端消费

```typescript
// 渲染面板标题时
function getLocalizedTitle(panel: PanelDef, locale: string): string {
  // 优先级：titleI18n[locale] > pluginTitles[locale] > title
  if (panel.titleI18n?.[locale]) return panel.titleI18n[locale];

  // 查语言包的 titles.json
  const pluginTitles = i18n.getResource(locale, 'pluginTitles', panel.pluginId);
  if (pluginTitles?.panels?.[panel.id]) return pluginTitles.panels[panel.id];

  return panel.title;  // fallback
}
```

## 五、语言插件包 API

### 5.1 新增 API

| 路由 | 方法 | 说明 |
|------|------|------|
| `/v1/plugins/locales` | GET | 返回可用语言列表 `{ locale → pluginId }` 映射 |
| `/v1/plugins/:id/locale` | GET | 返回单个语言插件的 locale 声明（前端资源路径等） |

```go
// GET /v1/plugins/locales 响应
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

### 5.2 Settings API 扩展

```go
// GET /v1/settings 返回新增字段
{
  "settings": {
    "locale": {
      "locale": { "value": "zh-CN" }
    }
  },
  // 新增
  "available_locales": [
    { "code": "en", "nativeName": "English", "englishName": "English" },
    { "code": "zh-CN", "nativeName": "简体中文", "englishName": "Chinese (Simplified)" }
  ]
}
```

### 5.3 插件静态路由

现有 `/plugins/:id/*filepath` 路由已能服务语言资源文件（`.json` / `.toml`），无需新增路由。

**关键：[`HandlePluginStaticFiles`](../internal/plugin/proxy.go:85) 已支持未运行插件的静态文件服务**。当 `GetInstance(pluginID)` 返回 false 时，它会 fallback 到 `ScanPlugins()` 查找插件目录，直接从磁盘读取文件。这意味着：

- 语言插件没有后端进程（不在 `instances` map 中）
- 但前端 i18next-http-backend 请求 `/plugins/com.axons.locale-zh-cn/locales/frontend/common.json` 时，静态路由仍然能正确服务该文件
- **无需启动插件进程即可加载语言资源**

**CORS / Content-Type**：
- `.json` → `application/json; charset=utf-8`
- `.toml` → `application/toml`（后端直接读取，不通过 HTTP）

### 5.4 SSE 事件类型

语言插件包新增两种 SSE 事件类型，供前端即时响应：

| 事件类型 | 触发时机 | Payload |
|---------|---------|---------|
| `locale.available` | 语言插件导入/安装成功 | `{ locale, pluginId, nativeName, englishName }` |
| `locale.unavailable` | 语言插件卸载 | `{ locale, pluginId, fallback }` |

前端消费方式：在 [`useEventStream`](../ui/src/hooks/useEventStream.ts) 中新增 `onLocaleAvailable` / `onLocaleUnavailable` 回调，Settings → Language tab 监听并更新可用语言列表。

## 六、语言插件包生命周期

### 6.1 与常规插件的差异

| 阶段 | 常规插件 | 语言插件 |
|------|---------|---------|
| 导入 | 解压 + 校验 manifest | 相同 |
| 安装 | 执行 install.command | **跳过**（无 install.command） |
| 启动 | exec.Command 启动进程 | **跳过**（无后端进程） |
| 运行 | 健康检查 + 注册 panels/commands | **加载语言资源到 i18n bundle** |
| 停止 | SIGTERM | **卸载语言资源** |
| 卸载 | 停止进程 + 删除目录 | 相同 + 清理 i18n 资源 |

### 6.2 关键：启动时不启动进程

PluginManager 启动 localization 类别插件时：

```go
// internal/plugin/process.go — StartPlugin 修改
func (m *Manager) StartPlugin(pluginID string) error {
    inst := m.getInstance(pluginID)

    // 语言插件：无后端进程，只加载资源
    if inst.Manifest.Category == "localization" {
        return m.loadLocaleResources(inst)
    }

    // 常规插件：启动进程
    return m.startPluginProcess(inst)
}
```

### 6.3 卸载时清理 i18n 资源

```go
func (m *Manager) UnloadLocaleResources(pluginID string) {
    inst := m.getInstance(pluginID)
    locale := inst.Manifest.Frontend.Locale.Language

    // 卸载后端 Go i18n 资源
    i18n.UnloadBundle(locale)

    // 从可用语言列表移除
    m.availableLocales = slices.DeleteFunc(m.availableLocales, func(l LocaleInfo) bool {
        return l.PluginID == pluginID
    })

    // SSE 广播 locale 不可用事件
    m.eventBroker.Publish("locale.unavailable", map[string]any{
        "locale":   locale,
        "pluginId": pluginID,
    })
}
```

### 6.4 卸载后语言回退

如果用户当前使用的语言包被卸载：

```
1. 后端检测 locale 设置 ≠ "en" 且对应插件已卸载
2. 后端自动将 locale 设置回退为 "en"
3. SSE 广播 locale.changed { locale: "en", reason: "fallback" }
4. 前端收到事件 → i18next.changeLanguage('en')
5. 前端提示："当前语言包已卸载，已切换为 English"
```

## 七、语言包制作指南

### 7.1 创建语言包

```bash
# 1. 创建目录结构
mkdir -p com.axons.locale-zh-cn/locales/{frontend,backend,plugin}

# 2. 复制英文语言包作为翻译模板
cp ui/src/i18n/en/*.json com.axons.locale-zh-cn/locales/frontend/
cp internal/i18n/locales/en.toml com.axons.locale-zh-cn/locales/backend/messages.toml

# 3. 翻译前端 JSON
# 编辑 locales/frontend/*.json，将英文值替换为中文

# 4. 翻译后端 TOML
# 编辑 locales/backend/messages.toml

# 5. 编写 manifest.json

# 6. 打包
tar czf com.axons.locale-zh-cn.tar.gz com.axons.locale-zh-cn/
```

### 7.2 翻译规范

| 规范 | 说明 | 示例 |
|------|------|------|
| 保持 JSON 结构 | key 不变，只翻译 value | `"title": "Code Health"` → `"title": "代码健康"` |
| 保留插值变量 | `{{count}}` / `{{name}}` 不翻译 | `"{{count}} callers"` → `"{{count}} 个调用者"` |
| 术语一致性 | 同一术语全文统一翻译 | `graph` 统一翻译为"图"，不混用"图谱"和"图" |
| 技术术语不翻译 | API Key / LLM / Embedding 等专业术语保持英文 | `"API Key"` → `"API Key"` |
| SystemPrompt 不翻译 | Agent 系统提示词是给 LLM 的，保持英文 | 不翻译 `agent.default.systemPrompt` |

### 7.3 术语表

| 英文 | 翻译 | 说明 |
|------|------|------|
| Graph | 图 | 代码图 |
| Node | 节点 | 图中的节点 |
| Edge | 边 | 图中的边 |
| Hotspot | 热点 | 高耦合函数 |
| Dead Code | 死代码 | 不可达代码 |
| Co-Change | 共变 | 一起变更的文件 |
| Embedding | Embedding | 不翻译，专业术语 |
| PageRank | PageRank | 不翻译，算法名 |
| SCC | SCC | 强连通分量，不翻译缩写 |
| Impact Analysis | 影响分析 | |
| Call Chain | 调用链 | |
| CFG | CFG | 控制流图，不翻译缩写 |
| Dataflow | 数据流 | |
| Agent | Agent | 不翻译，专业术语 |
| Plugin | 插件 | |
| Panel | 面板 | |

## 八、前端组件适配

### 8.1 Language 设置页

Settings 面板新增 Language tab，显示可用语言列表：

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
    // 从 Settings API 获取可用语言列表
    fetch('/v1/settings')
      .then(r => r.json())
      .then(data => {
        if (data.available_locales) {
          setAvailableLocales(data.available_locales);
        }
      });
  }, []);

  const handleLanguageChange = async (code: string) => {
    // 1. 切换前端语言
    await i18n.changeLanguage(code);
    // 2. 持久化到后端
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

### 8.2 可用语言列表动态更新

监听 SSE 事件，动态更新可用语言（无需重启）：

```typescript
// ui/src/hooks/useEventStream.ts — 新增 locale 事件类型

// 新增 SSE 事件类型
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

// useEventStream 回调扩展
interface UseEventStreamOptions {
  // ... 现有回调 ...

  // 新增：语言插件可用
  onLocaleAvailable?: (data: LocaleAvailableEvent) => void;
  // 新增：语言插件不可用（卸载）
  onLocaleUnavailable?: (data: LocaleUnavailableEvent) => void;
}
```

```typescript
// SettingsPanel.tsx — Language tab 中消费 SSE 事件
import { useEventStream } from '../hooks/useEventStream';

function LanguageTab() {
  const { t, i18n } = useTranslation('settings');
  const [availableLocales, setAvailableLocales] = useState<LocaleInfo[]>([
    { code: 'en', nativeName: 'English', englishName: 'English' }
  ]);

  // 监听 locale SSE 事件，实时更新可用语言列表
  useEventStream({
    onLocaleAvailable: useCallback((data) => {
      setAvailableLocales(prev => {
        if (prev.some(l => l.code === data.locale)) return prev;  // 去重
        return [...prev, {
          code: data.locale,
          nativeName: data.nativeName,
          englishName: data.englishName,
          pluginId: data.pluginId,
        }];
      });
    }, []),
    onLocaleUnavailable: useCallback((data) => {
      // 1. 从可用列表移除
      setAvailableLocales(prev => prev.filter(l => l.code !== data.locale));
      // 2. 如果当前正在使用该语言，自动回退到 fallback
      if (i18n.language === data.locale) {
        i18n.changeLanguage(data.fallback);  // 通常 fallback === "en"
        // 显示 toast 提示
      }
    }, [i18n]),
  });

  // ... 渲染逻辑 ...
}
```

### 8.3 插件面板 title 翻译

```tsx
// Footer.tsx / ActivityBar.tsx — 渲染面板标题时
function LocalizedPanelTitle({ panel }: { panel: PanelDef }) {
  const { i18n } = useTranslation();
  const locale = i18n.language;

  // 优先级：titleI18n > 语言包 titles.json > 默认 title
  if (panel.titleI18n?.[locale]) {
    return <>{panel.titleI18n[locale]}</>;
  }

  // 面板 title 存储的是 i18n key（如 "panels:codeHealth.title"）
  // t() 会自动处理命名空间前缀
  const { t } = useTranslation();
  return <>{t(panel.title)}</>;
}
```

## 九、文件改动清单

### 9.1 后端新增/修改

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `internal/plugin/manifest.go` | 修改 | `ValidCategories` 新增 `localization`；`PanelDef`/`CommandDef` 新增 `TitleI18n`；校验逻辑新增 locale 约束 |
| `internal/plugin/manager.go` | 修改 | 新增 `loadLocalePlugins()` / `loadSingleLocalePlugin()` / `unloadSingleLocalePlugin()` / `availableLocales`；`ImportPlugin` / `UninstallPlugin` 联动 locale 加载/卸载 + SSE 广播 |
| `internal/plugin/process.go` | 修改 | `StartPlugin` 对 `localization` 类别跳过进程启动 |
| `internal/plugin/handlers.go` | 修改 | 新增 `handleGetLocales` handler；`handleListPlugins` 返回 locale 信息 |
| `internal/i18n/i18n.go` | 修改 | 新增 `UnloadBundle()` 函数 |

### 9.2 前端新增/修改

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `ui/src/i18n/index.ts` | 修改 | http-backend loadPath 配置语言插件资源路径 |
| `ui/src/components/SettingsPanel.tsx` | 修改 | 新增 Language tab |
| `ui/src/hooks/useEventStream.ts` | 修改 | 监听 locale 相关 SSE 事件 |
| `ui/src/components/Footer.tsx` | 修改 | 面板标题翻译渲染 |
| `ui/src/components/ActivityBar.tsx` | 修改 | 面板标题翻译渲染 |

### 9.3 语言插件包（独立仓库）

| 文件 | 说明 |
|------|------|
| `com.axons.locale-zh-cn/manifest.json` | 中文语言包清单 |
| `com.axons.locale-zh-cn/locales/frontend/*.json` | 前端中文翻译（7 个文件） |
| `com.axons.locale-zh-cn/locales/backend/messages.toml` | 后端中文翻译 |
| `com.axons.locale-zh-cn/locales/plugin/titles.json` | 插件标题中文翻译 |
| `com.axons.locale-zh-cn/README.md` | 语言包说明 |

## 十、实施计划

### 阶段 1：后端语言插件支持（2 天）

| 步骤 | 工时 | 交付物 |
|------|------|--------|
| manifest.go 扩展 + 校验 | 0.5 天 | `localization` 类别 + `TitleI18n` + locale 校验 |
| PluginManager locale 加载/卸载 | 0.5 天 | `loadLocalePlugins()` + `UnloadLocaleResources()` |
| API 扩展 + Settings 返回 available_locales | 0.5 天 | `/v1/plugins/locales` + Settings 扩展 |
| 语言回退逻辑 + SSE 事件 | 0.5 天 | 卸载回退 + 事件广播 |

### 阶段 2：前端语言切换（1 天）

| 步骤 | 工时 | 交付物 |
|------|------|--------|
| i18next http-backend 适配 | 0.5 天 | 插件资源路径加载 |
| Language tab + 可用语言动态更新 | 0.5 天 | Settings → Language |

### 阶段 3：中文语言包制作（2 天）

| 步骤 | 工时 | 交付物 |
|------|------|--------|
| 前端 7 个 JSON 翻译 | 1 天 | ~200 个字符串翻译 |
| 后端 TOML + 插件 titles 翻译 | 0.5 天 | ~50 个字符串翻译 |
| manifest.json + 打包 + 安装测试 | 0.5 天 | 端到端验证 |

### 阶段 4：验证（1 天）

| 步骤 | 工时 | 交付物 |
|------|------|--------|
| 全组件中英文切换回归 | 0.5 天 | 无遗漏、无乱码 |
| 语言插件安装/卸载/切换全流程 | 0.5 天 | 生命周期验证 |

**总计：6 天**

## 十一、扩展性

### 11.1 更多语言

制作新的语言包只需：
1. 复制英文模板
2. 翻译所有字符串
3. 编写 manifest.json
4. 打包发布

无需改动 axons 主程序代码。

### 11.2 云端市场（二期）

语言插件包可以上传到插件市场，用户通过 Extensions 面板一键安装：

```
Extensions 面板
  → 分类筛选 "localization"
  → 选择 "Chinese (Simplified)"
  → 点击 Install
  → 即时生效（无需重启）
```

### 11.3 语言包版本与兼容

语言包声明 `minAxonsVersion`，axons 启动时检查：
- 版本匹配 → 正常加载
- 版本不匹配 → 跳过 + 日志 warn + 前端提示 "语言包版本不兼容，请更新"

### 11.4 部分翻译

语言包不需要 100% 翻译。i18next 的 fallback 机制确保：
- 缺失的 key → 自动回退到英文
- 缺失的命名空间 → 整个命名空间回退到英文
- 语言包渐进式完善，不影响使用
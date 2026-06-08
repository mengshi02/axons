# Axons 插件 UI iframe 隔离方案设计

> 版本: v2.0 | 日期: 2026-05-16 | 状态: 已实现

## 一、背景与动机

### 1.1 当前问题

当前插件 UI 组件通过 `import()` 动态加载到主 UI 的 React 组件树中，与主 UI 共享同一个 JS 主线程。这导致：

- **插件异常夯住主 UI**：插件组件内的同步死循环、CPU 密集计算、无限 `setState` 重渲染都会阻塞整个 JS 事件循环
- **ErrorBoundary 无法兜底**：React 的 ErrorBoundary 只能捕获 render 阶段的 throw，无法处理运行时阻塞
- **DOM/CSS 互相污染**：插件组件与主 UI 共享 DOM 树和 CSS 命名空间，样式冲突难以避免
- **插件崩溃影响全局**：一个插件的 `pluginApi.fetch()` 无超时挂起，可能导致面板处于无限 loading 状态

### 1.2 改造窗口期

当前仅有一个插件，是改造的最佳时机。插件增多后迁移成本翻倍。

### 1.3 方案选型

| 方案 | 隔离级别 | 改造复杂度 | 解决夯住比例 | 插件开发者影响 |
|------|---------|-----------|-------------|--------------|
| 短期：加固当前架构 | 无隔离 | 低（2-3天） | ~60% | 无 |
| **中期：iframe 隔离** | **故障隔离（JS 线程隔离）** | **中（6-7天）** | **~95%** | **低** |
| 长期：Worker+iframe | 故障+安全双隔离 | 高（3-4周） | ~100% | Breaking Change |

选择中期方案：iframe 隔离在隔离效果、改造成本、插件开发者影响三者间取得最佳平衡。

> **⚠️ 重要区分**：iframe + `allow-same-origin` 方案提供的是**故障隔离**（插件 JS 异常不会夯住主 UI 事件循环），而非**安全隔离**。由于 iframe 与主 UI 同源且启用了 `allow-same-origin`，插件 JS 仍可通过 `window.parent` 访问主 UI 的 DOM 与全局变量。如需安全隔离（防止恶意插件），需在长期方案中移除 `allow-same-origin` 或采用 Worker + OOPIF 方案。

### 1.4 实现落地准则：前薄后厚

本方案遵循**前薄后厚**原则——前端只做基本渲染与交互，所有涉及性能、功能性的逻辑在后端用 Go 实现。

| 层级 | 职责边界 | 不做的事 |
|------|---------|---------|
| **前端（iframe 内）** | 渲染 UI、发 HTTP 请求、订阅 SSE、postMessage UI 控制信号 | 不做事件中继、不做请求代理、不做状态管理、不做消息去重 |
| **前端（主 UI）** | 管理 iframe 生命周期、响应 UI 控制信号（close/ready） | 不代发 API 请求、不桥接 EventBus、不做运行模式分流 |
| **后端（Go）** | EventBus 广播中心、API 代理、插件状态管理、SSE 推送、CSP 生成 | — |

**设计决策映射**：

| 决策点 | 前薄后厚选择 | 理由 |
|--------|-------------|------|
| PluginApi.fetch 走直连还是桥接？ | **桌面端直连插件后端，Web 端走 daemon 代理** | 与改造前行为一致，不因隔离而降级 |
| PluginApi.getState/setState 走桥接？ | **iframe 内直连 daemon API** | 同源 HTTP 请求，前端直接 fetch，Go 处理存储 |
| EventBus 事件中继？ | **daemon SSE 广播** | Go 做广播中心，天然无回环；前端只订阅，不做中继 |
| 桌面端/Web 端分流？ | **统一走 daemon 代理** | Go 代理层 stream 透传，性能与直连无差异；消除前端 runtimeMode 分流逻辑 |
| CSP connect-src 动态生成？ | **按运行模式动态生成** | 桌面端放行 `http://127.0.0.1:*`，Web 端 `'self'`；前端不做分流，后端注入 runtimeMode |
| theme.css 双源？ | **Single Source of Truth** | 一份 theme.css，主 UI @import 引用，Go 模板 link 引用 |
| postMessage 职责？ | **仅 UI 控制信号** | onClose / ready / error，不做数据通道 |

---

## 二、四端兼容性分析

### 2.1 前提：桌面端加载模式

参见 [`desktop/main.go`](../desktop/main.go) 顶部注释 — Electron 桌面端的 webview 直接加载 daemon 的 `http://127.0.0.1:<random-port>`，**三端均为同源 HTTP**，不存在 `file://` 或自定义协议。这意味着 iframe + sandbox + postMessage 的兼容性等同于浏览器在标准 HTTP 同源下的行为，不会触发各 WebView 在 `file://` 下的历史兼容问题。

### 2.2 各端 WebView 兼容性矩阵

| 运行环境 | 底层引擎 | iframe 渲染 | sandbox 属性 | postMessage | 同源 fetch | CSP frame-ancestors | 风险等级 |
|----------|---------|------------|-------------|-------------|-----------|---------------------|---------|
| macOS | WKWebView (Safari/WebKit) | ✅ | ✅ HTML5 标准全集 | ✅ | ✅ | ✅ | 极低 |
| Windows | WebView2 (Chromium Evergreen) | ✅ | ✅ HTML5 标准全集 | ✅ | ✅ | ✅ | 极低 |
| Linux | WebKitGTK 4.0/4.1 | ✅ | ✅ HTML5 标准全集 | ✅ | ✅ | ✅ | 低 |
| Web 浏览器 | Chromium/Firefox/Safari | ✅ | ✅ | ✅ | ✅ | ✅ | 极低 |

**版本说明**：
- macOS：WKWebView 跟随系统 Safari/WebKit，本项目最低支持 macOS 11+（Safari 14+），HTML5 iframe sandbox 自 Safari 10+ 即完整支持
- Windows：WebView2 为 Evergreen 模式自动更新，与最新 Chromium 同步
- Linux：WebKitGTK 4.0（即将 EOL）与 4.1 对本方案使用的 sandbox flag 均完整支持；项目 [`desktop/README.md`](../desktop/README.md) 已建议优先安装 4.1
- Web：现代浏览器全支持

### 2.3 关键风险与缓解

#### 风险 1：`window.close()` 在 WKWebView 下可能被拦截

WKWebView 出于安全考虑，对非脚本打开的窗口调用 `window.close()` 默认不响应。

**缓解**：`onClose` 回调**不使用** `window.close()`，改为 `postMessage('plugin:close')` 向主 UI 发送关闭请求，主 UI 收到后移除 iframe DOM。此方案四端通用，不依赖任何 sandbox flag。

#### 风险 2：iframe 无法继承主 UI CSS 变量

iframe 是独立的浏览上下文，无法继承父页面的 CSS 自定义属性。

**缓解**：
1. iframe 内通过 `<link href="/plugin-sdk/theme.css">` 引入主题 CSS（与主 UI 同源加载，零网络开销）
2. 在 [`ui/src/plugin-sdk/theme.css`](../ui/src/plugin-sdk/theme.css) 中为 `:root.moon-theme` 与 `:root.sun-theme` 显式声明 CSS 变量（不依赖父级继承）
3. 主 UI 主题切换时通过 `postMessage('plugin:theme', { theme })` 通知 iframe
4. iframe 内 JS 切换 `:root` 的 class（moon-theme/sun-theme），CSS 变量自动跟随

#### 风险 3：postMessage 通信延迟

每次跨 iframe postMessage 约 1-5ms 开销，高频场景可能影响性能。

**缓解**：`PluginApi` 实现层按操作频率分流：
- **高频操作**（`fetch`/`createEventSource`）→ iframe 内直连插件后端，不走 postMessage
- **低频操作**（`onEvent`/`emitEvent`/`getState`/`setState`）→ postMessage 桥接

#### 风险 4：WebKitGTK 不同发行版差异

部分老旧 Linux 发行版可能仍带 WebKitGTK 4.0（EOL 时间 2025），与 4.1 在本方案使用的功能上无差异，但建议在 PoC 阶段同时在 4.0/4.1 环境各跑一次回归。

### 2.4 验证策略

为消除所有不确定性，实施前先做 macOS / Windows / Linux 三端最小 PoC（见第八节实施计划），实际验证 iframe 渲染、sandbox 生效、postMessage 双向通信与同源 fetch 透传四项。

---

## 三、架构设计

### 3.1 改造前后对比

**改造前**：

```
┌─ 主 UI React 树 ─────────────────────────┐
│                                           │
│  <ErrorBoundary>                          │
│    <AsyncPluginPanel>                     │
│      import('/plugins/:id/ui/index.js')   │
│      ──→ 插件 Component 直接渲染 ──→      │
│      共享 JS 主线程 / DOM / CSS            │
│    </AsyncPluginPanel>                    │
│  </ErrorBoundary>                         │
│                                           │
└───────────────────────────────────────────┘
```

**改造后（前薄后厚）**：

```
┌─ 主 UI React 树 ────────────────────────────────────┐
│                                                      │
│  <IframePluginPanel>                                 │
│    <iframe src="/v1/plugins/:id/iframe-host"         │
│            sandbox="allow-same-origin                │
│                     allow-scripts allow-forms         │
│                     allow-modals">                    │
│      ┌─ iframe 独立浏览上下文 ────────────────┐     │
│      │                                        │     │
│      │  PluginApi (全部走 HTTP/SSE 直连 daemon) │     │
│      │  ├── fetch       → /v1/plugins/:id/proxy │     │
│      │  ├── SSE         → /v1/plugins/:id/proxy │     │
│      │  ├── getState    → /v1/plugins/state/:id │     │
│      │  ├── setState    → /v1/plugins/state/:id │     │
│      │  ├── emitEvent   → POST /v1/plugins/event │     │
│      │  ├── onEvent     → EventSource /v1/plugins/events │     │
│      │  └── onClose     → postMessage('plugin:close') │     │
│      │                                        │     │
│      └────────────────────────────────────────┘     │
│    </iframe>                                         │
│  </IframePluginPanel>                                │
│                                                      │
│  PluginIframeHost (主 UI 侧 — 仅 UI 控制)           │
│  ├── 监听 iframe postMessage (close/ready/error)    │
│  └── 通知主题变更 (postMessage 'plugin:theme')      │
│                                                      │
└──────────────────────────────────────────────────────┘

┌─ daemon (Go 后端 — 厚层) ───────────────────────────┐
│                                                      │
│  EventBus 广播中心                                    │
│  ├── 接收事件: POST /v1/plugins/event               │
│  ├── 广播事件: SSE /v1/plugins/events/stream         │
│  └── Go 高性能：内存 EventBus + SSE 推送            │
│                                                      │
│  API 代理层                                           │
│  ├── /v1/plugins/:id/proxy/*  → httputil.ReverseProxy│
│  └── stream 透传，不缓冲响应体                       │
│                                                      │
│  插件状态存储                                         │
│  ├── /v1/plugins/state/:id:key GET/PUT              │
│                                                      │
│  iframe-host 动态生成                                 │
│  ├── /v1/plugins/:id/iframe-host → CSP nonce 模板   │
│                                                      │
└──────────────────────────────────────────────────────┘
```

### 3.2 PluginApi 通信架构（前薄后厚）

**核心原则**：iframe 是一个独立的 Web 应用，通过标准 HTTP/SSE 与 daemon 通信，postMessage 只用于父子窗口间的 UI 协调。

```
PluginApi 接口（签名不变）
├── fetch(path, opts)         → 桌面端直连 endpoint+path；Web 端走 /v1/plugins/:id/proxy（与改造前行为一致）
├── createEventSource(path)   → 桌面端直连 endpoint+path；Web 端走 /v1/plugins/:id/proxy（与改造前行为一致）
├── getState(key)             → iframe 内 fetch → /v1/plugins/state/:id:key → daemon 状态存储
├── setState(key, value)      → iframe 内 fetch PUT → /v1/plugins/state/:id:key → daemon 状态存储
├── emitEvent(type, payload)  → iframe 内 fetch POST → /v1/plugins/event → daemon EventBus
├── onEvent(type, handler)    → iframe 内 EventSource → /v1/plugins/events/stream → daemon SSE 广播
└── onClose()                 → postMessage('plugin:close') → 主 UI 移除 iframe（唯一 postMessage 用途）
```

**为什么全部走 iframe 内 HTTP 直连？**

1. **getState/setState 本来就是 HTTP API** — iframe 同源可直接 fetch，无需 postMessage 绕一圈让主 UI 代发
2. **daemon 是天然的 EventBus 广播中心** — iframe 通过 SSE 订阅，天然无回环（daemon 是单一广播源）
3. **前端薄层 = 简单可靠** — 没有 Bridge.handleApiRequest、没有 pendingRequests、没有 AbortController、没有 messageId 去重
4. **Go 厚层 = 高性能** — daemon 用内存 EventBus + SSE 推送，比前端 JS 中继性能高数十倍

> **桌面端/Web 端 fetch 分流准则（不变）**：`fetch` / `createEventSource` 在桌面端直连插件后端（零代理开销），Web 焋走 daemon 代理。iframe-adapter 通过 `window.__AXONS_PLUGIN__.runtimeMode` 判断运行模式做分流，与改造前 [`pluginApi.ts`](../ui/src/lib/pluginApi.ts) 行为完全一致。其余方法（getState/setState/emitEvent/onEvent）是 daemon 自身 API，不存在分流，统一走同源 HTTP/SSE。
>
> **CSP 注意**：桌面端直连插件后端端口时，CSP `connect-src` 需放行 `http://127.0.0.1:*`（或具体端口）。因此 CSP 需按运行环境动态生成——桌面端 `connect-src 'self http://127.0.0.1:*'`，Web 端 `connect-src 'self'`。 daemon 模板注入 `runtimeMode` 供 iframe-adapter 分流。

---

## 四、postMessage 通信协议

> **前薄后厚设计**：postMessage 只用于 UI 控制信号（close/ready/theme），数据通道全部走 HTTP/SSE 直连 daemon。消息类型从原来的 10 种简化为 4 种。

### 4.1 消息格式

所有 postMessage 消息使用统一信封格式：

```typescript
interface PluginMessage {
  /** 消息协议标识 */
  protocol: 'axons-plugin-iframe';
  /** 协议版本号 — 用于未来双轨升级 */
  version: 1;
  /** 消息来源: 'host' | 'plugin' */
  source: 'host' | 'plugin';
  /** 插件 ID */
  pluginId: string;
  /** 消息类型 */
  type: MessageType;
  /** 消息载荷 */
  payload?: any;
}
```

**版本协商规则**：当前为 `version: 1`。未来协议升级时，host 与 plugin 双方对未知 version 应静默忽略（不抛错），并保留向前兼容。

### 4.2 origin 校验

由于 iframe 与主 UI 同源加载（详见 2.1），双向 postMessage 都必须做 origin 校验：

- **发送方**：`postMessage(msg, window.location.origin)` — `targetOrigin` 固定为当前 origin，禁止 `'*'`
- **接收方**：`if (event.origin !== window.location.origin) return` — 来源不匹配立即丢弃

### 4.3 消息类型（4 种）

```typescript
type MessageType =
  // ---- 主 UI → iframe ----
  | 'plugin:init'           // 初始化：注入 pluginId（iframe 内 HTTP 请求需要）
  | 'plugin:theme'          // 主题变更：{ theme: 'moon' | 'sun' }

  // ---- iframe → 主 UI ----
  | 'plugin:ready'          // iframe 加载完成
  | 'plugin:close'          // 请求关闭面板
```

**与 v1 设计的差异**：

| 移除的消息类型 | 原用途 | 现替代方案 |
|---------------|--------|-----------|
| `plugin:api-request` | getState/setState 走 postMessage | iframe 内直接 fetch daemon API |
| `plugin:api-response` | 返回 API 结果 | 不需要，iframe 直接处理 HTTP response |
| `plugin:event` (iframe→host) | emitEvent 走 postMessage | iframe 内 POST `/v1/plugins/event` |
| `plugin:event` (host→iframe) | onEvent 走 postMessage 桥接 | iframe 内 EventSource 订阅 daemon SSE |
| `plugin:resize` | 通知面板尺寸变更 | iframe 自行监听 `window.resize` |
| `plugin:error` | 插件错误报告 | iframe 内 `window.onerror` 自行处理 |

---

## 五、文件改动清单

### 5.1 前端改动（薄层）

| 文件 | 改动类型 | 内容 |
|------|---------|------|
| `ui/src/components/IframePluginPanel.tsx` | 新增 | 替代 AsyncPluginPanel，用 `<iframe>` 渲染插件 UI，仅管理 iframe 生命周期与主题通知 |
| `ui/src/components/AsyncPluginPanel.tsx` | 废弃 | 改造完成并验证后整体删除 |
| `ui/src/plugin-sdk/iframe-adapter.ts` | 新增 | iframe 内 PluginApi 适配层，所有方法走 HTTP/SSE 直连 daemon，仅 onClose 走 postMessage |
| `ui/vite.plugin-sdk.config.ts` | 修改 | 增加 iframe-adapter UMD 构建产物 |
| `ui/src/plugin-sdk/theme.css` | 修改 | 补充 moon/sun 主题 CSS 变量显式声明（作为 Single Source of Truth） |
| `ui/src/index.css` | 修改 | 开头 `@import './plugin-sdk/theme.css'`，主 UI 也引用同一份 CSS 变量 |
| `ui/src/App.tsx` | 修改 | AsyncPluginPanel → IframePluginPanel |

> **前薄后厚体现**：前端不新增 `pluginIframeBridge.ts`（无需桥接服务），不修改 `pluginEventBus.ts`（无需桥接转发），不修改 `pluginApi.ts`（iframe 内由 iframe-adapter 独立实现）。前端改动量从 v1 的 10 个文件降到 7 个。

### 5.2 后端改动（厚层）

| 文件 | 改动类型 | 内容 |
|------|---------|------|
| `internal/plugin/proxy.go` | 修改 | ① 修复 `HandlePluginStaticFiles` 路径穿越漏洞；② 补充 CSP 响应头（桌面端 `connect-src` 放行 `http://127.0.0.1:*`，Web 端 `'self'`；`style-src` 含 `unsafe-inline` 兼容 CSS-in-JS） |
| `internal/plugin/proxy.go` | 新增 | `HandlePluginIframeHost` — 动态生成 iframe 容器 HTML（注入 pluginId / endpoint / runtimeMode / CSP nonce），路由 `/v1/plugins/:id/iframe-host` |
| `internal/plugin/eventbus.go` | 新增 | Go 内存 EventBus：`POST /v1/plugins/event` 接收事件、`SSE /v1/plugins/events/stream` 广播事件 |
| `internal/plugin/handlers.go` | 修改 | 注册 `/v1/plugins/:id/iframe-host`、`/v1/plugins/event`、`/v1/plugins/events/stream` 路由 |

> **后端新增能力**：EventBus 广播中心（Go 内存实现 + SSE 推送）是 v2 的核心增量。Go 的 goroutine + channel 天然适合实现高并发 SSE 广播，比前端 JS 中继性能高数十倍。

### 5.3 插件侧改动

| 文件 | 改动类型 | 内容 |
|------|---------|------|
| 插件 `ui/index.js` | 修改 | 入口适配（3 行启动代码，调用 `AxonsPluginIframe.IframePluginApiAdapter`） |

> **说明**：一期不提供 `htmlPath` 自定义入口。所有插件 iframe 容器统一由 daemon 通过 `/v1/plugins/:id/iframe-host` 动态生成，确保 CSP nonce 安全模型一致性。如未来有自定义 HTML 需求（如引入外部 CDN 资源），需在二期设计安全的模板注入机制后再开放。

manifest.json 示例（一期 — 无 htmlPath 字段）：

```json
{
  "frontend": {
    "entry": "ui/index.js",
    "panels": [
      { "id": "main", "title": "Dep Tracker", "location": "right" }
    ]
  }
}
```

---

## 六、详细实现

### 6.1 IframePluginPanel 组件

```tsx
// ui/src/components/IframePluginPanel.tsx
// 前薄后厚：主 UI 侧仅管理 iframe 生命周期 + 主题通知，不做数据桥接

interface IframePluginPanelProps {
  def: PanelDef;
  onClose: () => void;
}

function IframePluginPanel({ def, onClose }: IframePluginPanelProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [panelWidth, setPanelWidth] = useState(DEFAULT_WIDTH);
  const [iframeReady, setIframeReady] = useState(false);
  const [iframeError, setIframeError] = useState<string | null>(null);

  // 监听 iframe postMessage（仅 UI 控制信号：ready/close）
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;
      const msg = event.data;
      if (msg?.protocol !== 'axons-plugin-iframe' || msg.version !== 1) return;
      if (msg.pluginId !== (def.pluginId || '')) return;

      switch (msg.type) {
        case 'plugin:ready':
          setIframeReady(true);
          break;
        case 'plugin:close':
          onClose();
          break;
      }
    };
    window.addEventListener('message', handleMessage);
    return () => window.removeEventListener('message', handleMessage);
  }, [def.pluginId, onClose]);

  // 主题变更通知（唯一主动发送的 postMessage）
  const { theme } = useTheme();
  useEffect(() => {
    if (!iframeRef.current?.contentWindow || !iframeReady) return;
    iframeRef.current.contentWindow.postMessage({
      protocol: 'axons-plugin-iframe',
      version: 1,
      source: 'host',
      pluginId: def.pluginId || '',
      type: 'plugin:theme',
      payload: { theme },
    }, window.location.origin);
  }, [theme, iframeReady]);

  return (
    <div className={containerClass} style={{ width: panelWidth }}>
      <div className={resizeHandleClass} onMouseDown={handleResizeMouseDown} />
      {!iframeReady && !iframeError && <Spinner />}
      {iframeError && <ErrorDisplay error={iframeError} onRetry={...} />}
      <iframe
        ref={iframeRef}
        src={`/v1/plugins/${def.pluginId}/iframe-host`}
        sandbox="allow-same-origin allow-scripts allow-forms allow-modals"
        className={iframeReady ? 'w-full h-full border-0' : 'hidden'}
        onError={() => setIframeError('Plugin failed to load')}
      />
    </div>
  );
}
```

### 6.2 不再需要 PluginIframeBridge

v1 设计中 `PluginIframeBridge` 承担了 API 请求代理、EventBus 事件桥接、request-response 匹配等职责（~120 行）。v2 中这些数据通道全部移至后端，主 UI 只需在 `IframePluginPanel` 内监听 `message` 事件（~20 行），无需独立的 Bridge 类。

**移除的复杂度**：
- `pendingRequests` Map + requestId 匹配 → 不需要
- `AbortController` + destroy 时中断 fetch → 不需要
- `messageId` 去重 LRU 缓存 → 不需要
- `handleApiRequest` (getState/setState 代发) → 不需要
- `forwardEvent` (EventBus 桥接) → 不需要

### 6.3 iframe-adapter（插件 SDK 侧，iframe 内运行）

> **前薄后厚核心体现**：所有 PluginApi 方法走 HTTP/SSE 直连 daemon，postMessage 只用于 UI 控制。

```typescript
// ui/src/plugin-sdk/iframe-adapter.ts

/**
 * iframe-adapter — 在插件 iframe 内运行，封装 HTTP/SSE 为 PluginApi 接口。
 * 前薄后厚：数据通道全部直连 daemon，UI 控制走 postMessage。
 * 插件开发者无需直接使用此模块，由 daemon iframe-host 模板自动加载。
 */

export class IframePluginApiAdapter implements PluginApi {
  private static readonly PROTOCOL_VERSION = 1;
  private readonly expectedOrigin = window.location.origin;
  private _pluginId = '';
  private eventSource: EventSource | null = null;
  private eventHandlers = new Map<string, Set<(payload: any) => void>>();

  constructor() {
    window.addEventListener('message', this.handleMessage);
  }

  /** 等待主 UI 发送 init 消息，初始化 pluginId */
  init(): Promise<{ pluginId: string }> {
    return new Promise((resolve) => {
      const handler = (event: MessageEvent) => {
        if (event.origin !== this.expectedOrigin) return;
        const msg = event.data;
        if (msg?.protocol !== 'axons-plugin-iframe' || msg.version !== IframePluginApiAdapter.PROTOCOL_VERSION) return;
        if (msg.type !== 'plugin:init') return;
        this._pluginId = msg.payload.pluginId;
        window.removeEventListener('message', handler);
        this.send({ type: 'plugin:ready' });
        resolve({ pluginId: this._pluginId });
      };
      window.addEventListener('message', handler);
    });
  }

  // ---- PluginApi 接口实现（全部走 HTTP/SSE 直连 daemon） ----

  get pluginId(): string { return this._pluginId; }

  /** fetch → 桌面端直连插件后端，Web 端走 daemon 代理（与改造前行为一致） */
  async fetch(path: string, opts?: RequestInit): Promise<Response> {
    return globalThis.fetch(this.resolveUrl(path), opts);
  }

  /** createEventSource → 桌面端直连插件后端，Web 端走 daemon 代理 */
  createEventSource(path: string): EventSource {
    return new EventSource(this.resolveUrl(path));
  }

  /** getState → iframe 内直连 daemon 状态 API */
  async getState(key: string): Promise<any> {
    const resp = await globalThis.fetch(`/v1/plugins/state/${this.pluginId}:${key}`);
    if (!resp.ok) return null;
    const data = await resp.json();
    return data.value;
  }

  /** setState → iframe 内直连 daemon 状态 API */
  async setState(key: string, value: any): Promise<void> {
    await globalThis.fetch(`/v1/plugins/state/${this.pluginId}:${key}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(value),
    });
  }

  /** onEvent → iframe 内直连 daemon SSE 广播 */
  onEvent(type: string, handler: (payload: any) => void): () => void {
    if (!this.eventHandlers.has(type)) {
      this.eventHandlers.set(type, new Set());
    }
    this.eventHandlers.get(type)!.add(handler);

    // 首次订阅时创建 SSE 连接
    if (!this.eventSource) {
      this.connectSSE();
    }

    return () => {
      this.eventHandlers.get(type)?.delete(handler);
    };
  }

  /** emitEvent → iframe 内直连 daemon EventBus POST */
  async emitEvent(type: string, payload: any): Promise<void> {
    await globalThis.fetch('/v1/plugins/event', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pluginId: this.pluginId, type, payload }),
    });
  }

  /** onClose → postMessage 到主 UI（唯一 postMessage 用途） */
  onClose(): void {
    this.send({ type: 'plugin:close' });
  }

  // ---- SSE 连接管理 ----

  /** resolveUrl — 桌面端直连插件后端，Web 端走 daemon 代理（与改造前 pluginApi.ts 行为一致） */
  private resolveUrl(path: string): string {
    if (window.__AXONS_PLUGIN__?.runtimeMode === 'desktop' && window.__AXONS_PLUGIN__?.endpoint) {
      return window.__AXONS_PLUGIN__.endpoint + path;
    }
    return `/v1/plugins/${this.pluginId}/proxy${path}`;
  }

  private connectSSE() {
    this.eventSource = new EventSource('/v1/plugins/events/stream');
    this.eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        // 广播所有事件类型到本地 handlers
        const handlers = this.eventHandlers.get(data.type);
        if (handlers) {
          handlers.forEach(h => {
            try { h(data.payload); } catch (e) { console.error('[Plugin] Event handler error:', e); }
          });
        }
      } catch (e) { console.error('[Plugin] SSE parse error:', e); }
    };
  }

  // ---- postMessage UI 控制 ----

  private handleMessage = (event: MessageEvent) => {
    if (event.origin !== this.expectedOrigin) return;
    const msg = event.data;
    if (msg?.protocol !== 'axons-plugin-iframe' || msg.source !== 'host') return;
    if (msg.version !== IframePluginApiAdapter.PROTOCOL_VERSION) return;

    switch (msg.type) {
      case 'plugin:theme': {
        const root = document.documentElement;
        root.classList.remove('moon-theme', 'sun-theme');
        root.classList.add(msg.payload.theme === 'moon' ? 'moon-theme' : 'sun-theme');
        break;
      }
    }
  };

  private send(msg: Omit<PluginMessage, 'protocol' | 'version' | 'source' | 'pluginId'>) {
    window.parent.postMessage({
      protocol: 'axons-plugin-iframe',
      version: IframePluginApiAdapter.PROTOCOL_VERSION,
      source: 'plugin',
      pluginId: this.pluginId,
      ...msg,
    }, this.expectedOrigin);
  }
}
```

### 6.4 iframe 容器 HTML

一期所有插件 iframe 容器统一由 daemon 通过 `/v1/plugins/:id/iframe-host` 动态生成（见 6.7.2），插件开发者**无需**编写 `index.html`。daemon 模板自动注入：theme.css、components.css、iframe-adapter.umd.js、axons-plugin-ui.umd.js、插件 `ui/index.js`，以及 `window.__AXONS_PLUGIN__ = { pluginId, protocolVersion }`。

> **二期预留**：如未来有自定义 HTML 需求（如引入外部 CDN 资源、附加 `<meta>` 标签），可在 manifest 新增 `htmlPath` 字段，但需设计安全的 nonce 注入机制（daemon 对静态 HTML 做二次模板处理），确保 CSP 安全模型不被绕过。一期暂不开放此能力。

### 6.5 插件 index.js 适配

插件业务代码需要做最小改动 — 使用 `IframePluginApiAdapter` 替代直接接收 `pluginApi` prop：

```javascript
// 改造前：组件接收 pluginApi prop
// export default function MyPanel({ pluginApi, onClose, panelId }) { ... }

// 改造后：通过 adapter 获取 pluginApi（所有方法走 HTTP/SSE 直连 daemon）
const adapter = new AxonsPluginIframe.IframePluginApiAdapter();

adapter.init().then(({ pluginId }) => {
  const pluginApi = adapter; // adapter 实现了 PluginApi 接口
  const onClose = () => adapter.onClose();

  // 使用 React 渲染（与之前完全一致）
  const root = ReactDOM.createRoot(document.getElementById('root'));
  root.render(React.createElement(MyPanel, { pluginApi, onClose }));
});
```

### 6.6 EventBus — daemon SSE 广播（后端厚层）

**前薄后厚设计**：daemon 作为 EventBus 广播中心，iframe 和内置面板都通过 HTTP/SSE 与 daemon 通信，不再有主 UI 前端 JS 桥接中继。

```
内置面板 emit ──→ POST /v1/plugins/event ──→ daemon EventBus ──→ SSE 广播
iframe emit ────→ POST /v1/plugins/event ──→ daemon EventBus ──→ SSE 广播
                                                              ↕
                                              EventSource /v1/plugins/events/stream
                                                        ↕        ↕
                                                   内置面板   iframe
```

**Go 实现**：

```go
// internal/plugin/eventbus.go

// PluginEventBus — Go 内存 EventBus，高性能广播中心
type PluginEventBus struct {
    mu      sync.RWMutex
    sinks   map[chan Event]struct{}  // SSE 订阅者
}

type Event struct {
    PluginID string `json:"pluginId"`
    Type     string `json:"type"`
    Payload  any    `json:"payload"`
}

var globalBus = &PluginEventBus{
    sinks: make(map[chan Event]struct{}),
}

// HandlePostEvent — POST /v1/plugins/event
// 接收来自内置面板或 iframe 的事件，广播到所有 SSE 订阅者
func (b *PluginEventBus) HandlePostEvent(w http.ResponseWriter, r *http.Request) {
    var event Event
    if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
        http.Error(w, "invalid event", http.StatusBadRequest)
        return
    }
    b.broadcast(event)
    w.WriteHeader(http.StatusNoContent)
}

// HandleEventStream — SSE /v1/plugins/events/stream
// 内置面板和 iframe 均通过此端点订阅事件
func (b *PluginEventBus) HandleEventStream(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", http.StatusInternalServerError)
        return
    }

    ch := make(chan Event, 64)
    b.register(ch)
    defer b.unregister(ch)

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    for {
        select {
        case event := <-ch:
            data, _ := json.Marshal(event)
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()
        case <-r.Context().Done():
            return
        }
    }
}

func (b *PluginEventBus) broadcast(event Event) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    for ch := range b.sinks {
        select {
        case ch <- event:
        default:
            // 慢消费者跳过，避免阻塞广播
        }
    }
}

func (b *PluginEventBus) register(ch chan Event) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.sinks[ch] = struct{}{}
}

func (b *PluginEventBus) unregister(ch chan Event) {
    b.mu.Lock()
    defer b.mu.Unlock()
    delete(b.sinks, ch)
    close(ch)
}
```

**内置面板适配**：主 UI 的 `pluginEventBus.ts` 改为通过 `POST /v1/plugins/event` 发送事件、`EventSource('/v1/plugins/events/stream')` 订阅事件，与 iframe 走同一通道。`registerBridge`/`forwardToIframe` 等桥接方法全部移除。

### 6.7 后端改动

后端涉及三处改动：① 修复现有静态文件路径穿越漏洞；② 新增 `HandlePluginIframeHost` 路由动态生成 iframe 容器 HTML；③ 新增 EventBus SSE 广播端点。

#### 6.7.1 修复 HandlePluginStaticFiles 路径穿越漏洞

当前 [`internal/plugin/proxy.go`](../internal/plugin/proxy.go:108) 直接拼接 `pluginDir + filePath`，构造 `../../../etc/passwd` 可能逃逸出插件目录。改造时一并修复：

```go
// internal/plugin/proxy.go — HandlePluginStaticFiles 改造

func (m *Manager) HandlePluginStaticFiles(w http.ResponseWriter, r *http.Request, pluginID string, filePath string) {
    // ... 现有插件查找逻辑 ...

    // 安全：清理路径并校验未逃逸出插件目录
    cleanPath := filepath.Clean("/" + filePath)
    fullPath := filepath.Join(pluginDir, cleanPath)
    absPluginDir, err := filepath.Abs(pluginDir)
    if err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    absFullPath, err := filepath.Abs(fullPath)
    if err != nil || !strings.HasPrefix(absFullPath, absPluginDir+string(os.PathSeparator)) {
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }

    // 允许同源 iframe 嵌入插件静态资源
    w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")

    http.ServeFile(w, r, absFullPath)
}
```

#### 6.7.2 新增 HandlePluginIframeHost — 动态生成 iframe 容器

daemon 提供统一的 iframe 容器路由 `/v1/plugins/:id/iframe-host`，动态注入 `pluginId` 与 CSP nonce：

```go
// internal/plugin/proxy.go — 新增

const iframeHostTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>{{.PluginID}}</title>
  <link rel="stylesheet" href="/plugin-sdk/theme.css" />
  <link rel="stylesheet" href="/plugin-sdk/components.css" />
  <style>
    body { margin: 0; padding: 0; overflow: hidden; background: var(--axons-color-surface, #101018); }
  </style>
</head>
<body>
  <div id="root"></div>
  <script nonce="{{.Nonce}}">
    window.__AXONS_PLUGIN__ = {
      pluginId: {{.PluginIDJSON}},
      endpoint: {{.EndpointJSON}},
      runtimeMode: {{.RuntimeModeJSON}},
      protocolVersion: 1
    };
  </script>
  <script nonce="{{.Nonce}}" src="/plugin-sdk/iframe-adapter.umd.js"></script>
  <script nonce="{{.Nonce}}" src="/plugin-sdk/axons-plugin-ui.umd.js"></script>
  <script nonce="{{.Nonce}}" src="/plugins/{{.PluginID}}/ui/index.js"></script>
</body>
</html>`

// HandlePluginIframeHost 渲染 iframe 容器 HTML 并设置 CSP。
// 路由：GET /v1/plugins/:id/iframe-host
func (m *Manager) HandlePluginIframeHost(w http.ResponseWriter, r *http.Request, pluginID string) {
    inst, ok := m.GetInstance(pluginID)
    var endpoint string
    if ok && inst.Backend != nil {
        endpoint = inst.Backend.URL()
    }

    // 校验 pluginID 合法（避免模板注入）
    if !validPluginID.MatchString(pluginID) {
        http.Error(w, "invalid plugin id", http.StatusBadRequest)
        return
    }

    // 判断运行模式：桌面端 vs Web 端
    runtimeMode := "web"
    if getRuntimeMode() == "desktop" {
        runtimeMode = "desktop"
    }

    // 生成一次性 nonce
    nonceBytes := make([]byte, 16)
    if _, err := rand.Read(nonceBytes); err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    nonce := base64.StdEncoding.EncodeToString(nonceBytes)

    // CSP — 桌面端 fetch 直连插件后端需放行本地端口
    // style-src 含 'unsafe-inline' 是一期妥协（CSS-in-JS 兼容），二期 nonce 化
    connectSrc := "'self'"
    if runtimeMode == "desktop" {
        connectSrc = "'self http://127.0.0.1:*'"
    }
    csp := fmt.Sprintf(
        "default-src 'self'; "+
            "script-src 'self' 'nonce-%s'; "+
            "style-src 'self' 'unsafe-inline'; "+
            "img-src 'self' data:; "+
            "connect-src %s; "+
            "frame-ancestors 'self'",
        nonce, connectSrc,
    )
    w.Header().Set("Content-Security-Policy", csp)
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Header().Set("X-Content-Type-Options", "nosniff")

    pluginIDJSON, _ := json.Marshal(pluginID)
    endpointJSON, _ := json.Marshal(endpoint)
    runtimeModeJSON, _ := json.Marshal(runtimeMode)

    tmpl := template.Must(template.New("host").Parse(iframeHostTemplate))
    _ = tmpl.Execute(w, map[string]any{
        "PluginID":        pluginID,
        "Nonce":           nonce,
        "PluginIDJSON":    template.JS(pluginIDJSON),
        "EndpointJSON":    template.JS(endpointJSON),
        "RuntimeModeJSON": template.JS(runtimeModeJSON),
    })
}
```

> **与 v1 差异**：移除了 `endpoint` / `runtimeMode` 注入（不再需要桌面端分流）和 `connect-src` 动态生成（统一走代理，静态 `'self'` 即可）。Go 后端不感知前端运行模式。

#### 6.7.3 路由注册

```go
// internal/plugin/handlers.go — RegisterRoutes 内增加

// iframe 容器
router.GET("/v1/plugins/:id/iframe-host", func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
    m.HandlePluginIframeHost(w, r, ps.ByName("id"))
})

// EventBus 广播中心（前薄后厚核心）
router.POST("/v1/plugins/event", globalBus.HandlePostEvent)
router.GET("/v1/plugins/events/stream", globalBus.HandleEventStream)
```

#### 6.7.4 桌面端说明

参见 [`desktop/main.go`](../desktop/main.go) 的架构注释，Electron 桌面端的 webview 加载 daemon 的 `http://127.0.0.1:<random-port>`，主 UI 与 iframe-host 由同一个 daemon HTTP 服务器提供：

- `frame-ancestors 'self'` 在三个桌面端均生效
- 桌面端 `fetch`/`EventSource` 直连插件后端端口，CSP `connect-src` 动态放行 `http://127.0.0.1:*`；Web 端统一走代理，`connect-src 'self'` 即可
- daemon 代理层使用 `httputil.ReverseProxy` 流式透传，SSE 场景延迟与直连无差异

### 6.8 theme.css — Single Source of Truth

iframe 是独立浏览上下文，无法继承父页面 CSS 变量，必须在 iframe 内重新加载主题 CSS。但"重新加载"不等于"重新声明"——`theme.css` 是唯一变量源，主 UI 和 iframe 都是它的消费者。

**架构**：

```
plugin-sdk/theme.css  ←  Single Source of Truth（唯一变量声明处）
    ↑ @import               ↑ <link>
主 UI index.css          daemon iframe-host 模板
```

**主 UI 适配**（`ui/src/index.css` 开头）：

```css
/* 主 UI 引用同一份 theme.css，不再重复声明颜色变量 */
@import './plugin-sdk/theme.css';
```

**theme.css 补充 moon/sun 主题变量**（iframe 内和主 UI 共用）：

```css
/* ui/src/plugin-sdk/theme.css — 唯一变量源 */

/* moon 主题 CSS 变量 */
:root.moon-theme {
  --axons-color-void: #06060a;
  --axons-color-deep: #0a0a10;
  --axons-color-surface: #101018;
  --axons-color-elevated: #16161f;
  --axons-color-hover: #1c1c28;
  --axons-border-subtle: #1e1e2a;
  --axons-border-default: #2a2a3a;
  --axons-text-primary: #e4e4ed;
  --axons-text-secondary: #8888a0;
  --axons-text-muted: #5a5a70;
  --axons-accent: #7c3aed;
  --axons-accent-dim: #5b21b6;
}

/* sun 主题 CSS 变量 */
:root.sun-theme {
  --axons-color-void: #f8f8fa;
  --axons-color-deep: #f0f0f4;
  --axons-color-surface: #ffffff;
  --axons-color-elevated: #f5f5f8;
  --axons-color-hover: #ebebf0;
  --axons-border-subtle: #e0e0e6;
  --axons-border-default: #d0d0d8;
  --axons-text-primary: #1a1a2e;
  --axons-text-secondary: #5a5a70;
  --axons-text-muted: #8888a0;
  --axons-accent: #7c3aed;
  --axons-accent-dim: #5b21b6;
}
```

> **消除双源风险**：主 UI 通过 `@import` 引用 `theme.css`，不再在 `index.css` 或 Tailwind 配置中重复声明颜色值。Tailwind 配置通过 `theme.extend.colors` 引用 CSS 变量（如 `var(--axons-accent)`），而非硬编码颜色值。变量只需改一处。

### 6.9 长期演进

本期方案聚焦 iframe 隔离 + 前薄后厚架构。长期演进方向：

1. **Worker + iframe 双隔离**：移除 `allow-same-origin`，改用 Worker 做 JS 计算隔离 + iframe 做 DOM 隔离，实现故障+安全双隔离
2. **二进制流支持**：daemon SSE 升级为 WebSocket，支持双向流式通信
3. **htmlPath 自定义入口**：二期按需开放，daemon 对静态 HTML 做二次模板处理注入 CSP nonce

> 以上仅为方向记录，本期不实现。协议版本号 `version: 1` 已就位，未来升级时可双轨过渡。

---

## 七、数据流

### 7.1 插件面板打开流程

```
1. 用户点击插件面板按钮
2. App.tsx 渲染 <IframePluginPanel def={panelDef}>
3. IframePluginPanel 创建 <iframe src="/v1/plugins/:id/iframe-host">
4. daemon 返回 iframe-host HTML，含 CSP nonce + 注入 window.__AXONS_PLUGIN__
5. iframe 加载 iframe-adapter.umd.js + axons-plugin-ui.umd.js + 插件 index.js
6. iframe-adapter 调用 adapter.init() 等待主 UI 的 plugin:init 消息
7. IframePluginPanel 监听 iframe load 事件后发送 plugin:init { pluginId }
8. iframe-adapter 初始化完成 → 发送 plugin:ready
9. 主 UI 收到 ready → iframeReady = true
10. iframe 内 React 组件渲染完成
```

### 7.2 插件 API 调用流程

```
fetch('/api/models'):
  桌面端：iframe 内 → resolveUrl → endpoint + '/api/models'
                    → globalThis.fetch (直连插件后端，零代理开销)
  Web 端：iframe 内 → resolveUrl → /v1/plugins/:id/proxy/api/models
                    → globalThis.fetch (同源，无跨域)
                    → axons daemon 代理 → 插件后端

getState('config'):
  iframe 内 → globalThis.fetch /v1/plugins/state/:id:config
            → daemon 状态存储 → 返回 JSON

emitEvent('node:selected', { nodeId }):
  iframe 内 → globalThis.fetch POST /v1/plugins/event
            → daemon EventBus → SSE 广播到所有订阅者

onEvent('node:selected'):
  iframe 内 → new EventSource('/v1/plugins/events/stream')
            → daemon SSE 推送 → iframe handler 被调用
```

### 7.3 主题切换流程

```
1. 用户切换主题 (moon → sun)
2. useTheme 更新 → IframePluginPanel 监听到 theme 变化
3. IframePluginPanel 发送 postMessage 'plugin:theme' 到 iframe
4. iframe-adapter 收到 → 切换 :root class
5. CSS 变量自动更新 → 插件 UI 样式跟随变化
```

---

## 八、实施计划

> **前薄后厚**：前端工作量从 v1 的 ~4 天降到 ~2 天（无需 Bridge、无需 EventBus 桥接、无需 runtimeMode 分流）；后端工作量从 ~1.5 天增到 ~3 天（新增 Go EventBus + SSE 广播）。总工期不变，但架构更简单可靠。

| 步骤 | 层 | 工作内容 | 工期 | 验证标准 |
|------|---|---------|------|---------|
| 0 | 后端 | **安全前置**：修复 `HandlePluginStaticFiles` 路径穿越 | 0.5天 | 单测：构造 `../../../etc/passwd` 返回 403 |
| 1 | 全栈 | **三端 PoC**：最小 iframe + postMessage + sandbox + CSP nonce + 同源 fetch | 1天 | 三端通过：iframe 渲染、postMessage 双向通信、sandbox 生效、同源 fetch 正常 |
| 2 | 后端 | Go EventBus + SSE 广播端点 | 1天 | 验证：POST event → SSE 推送；慢消费者不阻塞广播 |
| 3 | 后端 | `HandlePluginIframeHost` + CSP nonce（静态 connect-src 'self'） | 0.5天 | 验证：iframe-host 渲染正常；CSP 拦截非 nonce 内联脚本 |
| 4 | 前端 | IframePluginApiAdapter（全部 HTTP/SSE 直连 daemon） | 1天 | 单元测试：API 调用模拟、SSE 事件处理 |
| 5 | 前端 | IframePluginPanel 组件（仅 UI 控制：ready/close/theme） | 0.5天 | 浏览器验证：iframe 渲染、关闭、主题切换 |
| 6 | 前端 | theme.css Single Source of Truth + 主 UI @import 适配 | 0.5天 | 验证：moon/sun 切换时 iframe 内样式跟随；主 UI 样式不变 |
| 7 | 前端 | iframe-adapter UMD 构建 + 迁移 dependency-tracker | 0.5天 | 验证：现有插件功能正常，仅加 3 行启动代码 |
| 8 | 后端 | 内置面板 pluginEventBus.ts 适配（emit → POST, on → EventSource） | 0.5天 | 验证：内置面板事件流正常（如节点选择联动） |
| **合计** | | | **6-6.5 天** | |

> **vs v1 工期**：v1 为 7-7.5 天，v2 为 6-6.5 天。工期缩短原因：前端不需要 Bridge/EventBus 桥接/request-response 匹配等复杂实现。

### 8.1 第 0 步：安全前置

在 iframe 改造的任何代码动工之前，先用半天独立修复路径穿越问题。此修复独立于 iframe 方案本身，即使 iframe 改造暂停也应该合入。

### 8.2 第 1 步：三端 PoC 验证

完整改造前用 1 天做三端最小 PoC 验证（macOS 优先，Windows/Linux 可并行）：

1. 在 `App.tsx` 中临时插入一个 `<iframe src="/v1/plugins/test/iframe-host">`
2. daemon 返回最简 iframe-host HTML（含 `<script>` 和 `postMessage` 通信）
3. 在 Electron 桌面端运行，验证：
   - iframe 是否正常渲染
   - postMessage 双向通信、origin 校验是否正常
   - sandbox 属性是否生效
   - iframe 内 `fetch` 到 `/v1/plugins/:id/proxy/*` 是否正常
   - CSP nonce 是否被 webview 正确识别

若 PoC 通过，则按计划推进。若发现问题，调整方案后再推进。

---

## 九、附录

### A. 与现有插件系统设计文档的关系

本文档是 [`plugin-system-design.md`](plugin-system-design.md) 的补充，聚焦于插件 UI 的前端隔离改造。插件后端进程管理、manifest 协议、API 路由等设计保持不变。

实施后，[`plugin-developer-guide.md`](plugin-developer-guide.md) 中"开发插件 UI"章节需要同步更新（新增 `IframePluginApiAdapter` 启动代码、iframe 调试方法等），作为面向插件开发者的迁移指南。一期不开放自定义 `index.html` 入口，插件开发者仅需修改 `ui/index.js` 加 3 行启动代码。

### B. 对插件开发者的影响总结

| 维度 | 改造前 | 改造后 | 影响 |
|------|--------|--------|------|
| PluginApi 接口 | `props.pluginApi` | `adapter` (实现同一接口) | **无** — 方法签名不变 |
| 组件入口 | React 组件 default export | `index.js` 内 `adapter.init()` + 渲染 | **低** — 加 3 行启动代码 |
| HTML 入口 | 不需要 | 不需要，daemon 统一动态生成 | **无** — 一期不开放自定义 HTML |
| fetch 调用 | `pluginApi.fetch('/api/x')` | 相同（桌面端直连插件后端，Web 端走 daemon 代理） | **无** |
| EventBus | `pluginApi.onEvent/emitEvent` | 相同（走 daemon SSE 广播） | **无** — 行为等价 |
| 样式 | import CSS | daemon 自动注入 theme.css + components.css | **无** — 效果一致 |
| 调试 | 主线程 DevTools | iframe 内 DevTools（右键 iframe → "Inspect frame"） | **略有差异** — 需在 iframe 上下文中调试 |

### C. v1 → v2 架构差异总结

| 维度 | v1（半直连半桥接） | v2（前薄后厚） |
|------|-------------------|---------------|
| PluginApi 数据通道 | fetch/SSE 直连，其他走 postMessage 桥接 | **全部走 HTTP/SSE 直连 daemon** |
| postMessage 消息类型 | 10 种 | **4 种**（init/theme/ready/close） |
| 主 UI Bridge | ~120 行（API 代理 + EventBus 桥接 + request-response） | **不需要**（内联在 IframePluginPanel ~20 行） |
| EventBus 中继 | 主 UI JS 前端桥接 + messageId 去重 | **daemon Go SSE 广播**（天然无回环） |
| 桌面端/Web 端分流 | runtimeMode 注入 + resolveUrl 分流 + CSP 动态生成 | **保留**（fetch/SSE 桌面端直连，其余走 daemon） |
| theme.css | 双源声明（主 UI + iframe 各一份） | **Single Source of Truth**（@import 共享） |
| htmlPath 自定义入口 | 可选（但无 CSP nonce） | **一期不开放** |
| 前端改动文件数 | 10 个 | **7 个** |
| 后端改动文件数 | 3 个 | **4 个**（+eventbus.go） |
| 总工期 | 7-7.5 天 | **6-6.5 天** |

### D. sandbox 属性说明

```html
<iframe sandbox="allow-same-origin allow-scripts allow-forms allow-modals">
```

| flag | 作用 | 是否必需 |
|------|------|---------|
| `allow-same-origin` | 允许 iframe 保持同源，可访问同源 API | ✅ 必需 — 否则 fetch/postMessage 不可用 |
| `allow-scripts` | 允许执行 JavaScript | ✅ 必需 — 插件 UI 需要 JS |
| `allow-forms` | 允许表单提交 | ✅ 建议 — 插件可能有搜索表单 |
| `allow-modals` | 允许 alert/confirm/prompt | 可选 — 调试用 |
| `allow-popups` | 允许 window.open | ❌ 不加 — 插件不应打开新窗口 |
| `allow-popups-to-escape-sandbox` | 允许弹出窗口脱离 sandbox | ❌ 不加 — 不依赖此 flag |

**注意**：不加 `allow-popups-to-escape-sandbox`，关闭面板通过 `postMessage('plugin:close')` 实现，不依赖 `window.close()`。
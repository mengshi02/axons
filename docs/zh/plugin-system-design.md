# Axons 插件系统设计方案

> 版本: v1.1 | 日期: 2026-05-14 | 状态: 已实现

## 一、背景与动机

### 1.1 当前痛点

Axons 的分析面板（CodeHealth / ImpactAnalysis / CfgDataflow / Sequence / ArchRules / GraphAnalytics）全部硬编码在以下文件中，每新增一个功能需要改动 5+ 个文件：

- `ui/src/App.tsx` — 面板挂载与布局
- `ui/src/components/Footer.tsx` — Footer 功能按钮
- `ui/src/components/ActivityBar.tsx` — 活动栏图标
- `ui/src/hooks/useAppState.ts` — 面板状态管理
- 新建 Panel 组件文件

耦合严重，扩展成本高。

### 1.2 已有基础

- `internal/registry/` — 项目注册表机制
- `internal/mcp/` — MCP 工具注册机制
- `skills/` — SKILL.md 规范目录
- `internal/api/` — 完善的 HTTP API 层

### 1.3 是否值得做

**值得做，建议分两期落地**：

| 维度 | 评估 |
|------|------|
| 当前痛点 | 硬编码面板耦合严重，扩展成本高 |
| 已有基础 | 注册表、MCP、Skills 机制可自然延伸 |
| 竞品趋势 | VSCode / JetBrains / Cursor 均走插件扩展路线 |
| 风险 | 用户基数小，云端市场投入产出比低，二期再做 |

---

## 二、架构设计

### 2.1 实现宗旨：前薄后厚

在落地开发过程中，秉承**前薄后厚**的原则：

- **前端尽量轻**：前端只负责 UI 渲染与交互，不做业务逻辑计算，不持有复杂状态，尽量薄
- **能力尽量从后端实现**：数据处理、业务逻辑、算法计算等能力全部下沉到后端，利用后端（Go/Python 等）的高性能优势

| 维度 | 前端（薄） | 后端（厚） |
|------|-----------|-----------|
| 职责 | UI 渲染、用户交互、EventBus 事件订阅/转发 | 业务逻辑、数据处理、算法计算、状态管理 |
| 数据 | 仅展示用，不加工 | 原始数据获取、清洗、聚合、计算 |
| 通信 | `pluginApi.fetch()` 桌面端直连/Web端代理 | 调 axons API 获取图数据，计算后返回前端 |
| 状态 | 最小本地 UI 状态 | 共享状态 API (`/v1/plugins/state`) 持久化 |

**为什么前薄后厚**：
1. 后端（Go/Python）处理大规模代码图数据的性能远超浏览器 JS 运行时
2. 前端保持轻量，插件 UI 加载更快、内存占用更低
3. 后端能力可独立测试和复用，不依赖 UI 层

### 2.2 核心决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 插件协议 | `manifest.json`（自定义） | 遵循业内惯例 (Chrome/PWA/Firefox)，面向 axons 语义设计 |
| 运行时 | 不内嵌 goja，插件后端独立进程 | 多语言支持、OS 级进程隔离、调试简单 |
| 通信方式 | 混合方案：桌面端直连 + Web 端代理 | 桌面端零代理开销；Web 端走 axons 代理解决跨域和远程可达性（详见 2.6 节） |
| 跨面板通信 | EventBus（前端）+ 共享状态 API（后端） | 分层解决不同粒度的交互需求 |

### 2.3 整体架构

```
┌──────────────── axons daemon ──────────────────────┐
│                                                      │
│  PluginManager                                       │
│  ├── 扫描 ~/.axons/plugins/*/manifest.json            │
│  ├── 分配端口 (stdin+环境变量), exec.Command 启动插件后端进程   │
│  ├── 健康检查 (轮询 healthCheck 端点)                │
│  ├── 崩溃监听 (cmd.Wait) + 自动重启 (最多3次)        │
│  └── 读取 frontend.panels/commands → 注册到 PluginRegistry  │
│                                                      │
│  PluginRegistry (统一注册表)                    │
│  ├── 内存 map[type][]PluginEntry               │
│  ├── 支持 manifest.json 静态声明 (frontend.panels/commands) │
│  └── 支持插件运行时动态 sync 上报状态                │
│                                                      │
│  API 路由                                            │
│  ├── GET  /v1/plugins             插件列表            │
│  ├── GET  /v1/plugins/registry/:type  按类型查注册表  │
│  ├── POST /v1/plugins/registry/sync   插件上报状态    │
│  ├── GET  /v1/plugins/system-state    系统状态镜像    │
│  ├── GET  /v1/plugins/state/:key      共享状态读      │
│  ├── PUT  /v1/plugins/state/:key      共享状态写      │
│  ├── POST /v1/plugins/import          离线导入        │
│  └── /v1/plugins/:id/proxy/*path     插件代理(Web端) │
│                                                      │
└──────────────────────────────────────────────────────┘
      │ 注入环境变量                    │ SSE: plugin.crashed
      ▼                                ▼
┌─── Plugin Backend (任意语言) ───┐  ┌─── 前端 React ─────────────┐
│  读 AXONS_API_URL               │  │  usePluginRegistry()       │
│  读 AXONS_PLUGIN_PORT           │  │  → 拉取注册表, 合并到 UI    │
│  读 AXONS_PLUGIN_TOKEN          │  │  import() 动态加载组件      │
│  绑定 127.0.0.1:PORT            │  │  pluginApi.fetch():        │
│  加 CORS 头 (桌面端必需)        │  │    桌面端 → 直连插件后端    │
│  暴露 /health + 业务 API        │  │    Web端  → axons 代理转发  │
└─────────────────────────────────┘  │  EventBus 跨面板通信        │
                                     │  监听 SSE 插件崩溃通知      │
                                     └─────────────────────────────┘
```

### 2.4 数据流

```
1. axons 启动 → 扫描 ~/.axons/plugins/ → 逐个启动后端进程
2. 插件后端就绪 → 调 POST /v1/plugins/registry/sync 上报动态状态
3. 前端请求 GET /v1/plugins → 拿到插件列表(含 endpoint + 前端组件路径)
4. 前端 import() 加载插件 UI 组件, 注入 pluginApi
5. 插件 UI 组件内部用 pluginApi.fetch 请求插件后端:
   - 桌面端: pluginApi.fetch('/api/models') → 直连 http://127.0.0.1:{pluginPort}/api/models
   - Web 端:  pluginApi.fetch('/api/models') → fetch /v1/plugins/:id/proxy/api/models
              → axons 代理转发到 http://127.0.0.1:{pluginPort}/api/models
6. 插件后端需要 axons 数据时, 用 AXONS_API_URL 调 /v1/* API (Go HTTP, 无跨域)
```

### 2.5 跨域与混合通信方案

#### 问题

axons 同时支持桌面端（Wails WebView）和 Web 端（浏览器），插件前端访问插件后端时存在跨域问题：

| 场景 | 前端 origin | 插件后端 origin | 跨域? | 原因 |
|------|------------|----------------|-------|------|
| 桌面端 + 插件 | `http://127.0.0.1:{axons端口}` | `http://127.0.0.1:{插件端口}` | 是 | 端口不同 = 不同源 |
| Web 端 + 插件 | `http://{host}:9090` | `http://127.0.0.1:{插件端口}` | 是 | 端口不同 + 可能 host 也不同 |
| Web 端（远程部署） | `https://axons.example.com` | `http://127.0.0.1:{插件端口}` | 不可达 | 插件后端绑定 127.0.0.1，远程浏览器无法访问 |

#### 方案：混合通信（一期落地）

**桌面端：前端直连插件后端（零代理开销，性能最优）**
**Web 端：前端经 axons 反向代理访问插件后端（同源无跨域，解决远程可达性）**

```
桌面端 (Wails WebView):
  前端 ──直连──→ 插件后端 http://127.0.0.1:{pluginPort}/api/*
        ──直连──→ axons API  http://127.0.0.1:{axonsPort}/v1/*
        (插件后端加 CORS 头，前端同机直连，零延迟)

Web 端 (浏览器):
  前端 ──代理──→ axons /v1/plugins/:id/proxy/* ──转发──→ 插件后端
        ──直连──→ axons API  http(s)://{host}/v1/*
        (前端同源走代理，解决跨域 + 远程可达性)
```

#### 运行环境检测

Wails v3 在 WebView 加载完成后注入 `window._wails` 对象，但注入有延迟。检测逻辑：

```typescript
// lib/config.ts — 扩展
let runtimeMode: 'desktop' | 'web' = 'web';  // 默认 web

export function getRuntimeMode(): 'desktop' | 'web' {
  return runtimeMode;
}

export async function initConfig(): Promise<void> {
  const isLocalhost = window.location.hostname === '127.0.0.1'
                    || window.location.hostname === 'localhost';
  const hasWails = !!(window as any)._wails;

  // Wails 注入可能延迟，localhost + 短暂等待后重试
  if (isLocalhost && !hasWails) {
    await new Promise(r => setTimeout(r, 100));
  }
  runtimeMode = isLocalhost && !!(window as any)._wails ? 'desktop' : 'web';
}
```

判断依据：桌面端 webview 一定从 `127.0.0.1` 加载（`isLocalhost` 可靠），`_wails` 精确确认。

#### pluginApi URL 分支

```typescript
// lib/pluginApi.ts
const resolveUrl = (path: string): string => {
  if (getRuntimeMode() === 'desktop') {
    return config.endpoint + path;  // 直连插件后端
  }
  return `${getBaseURL()}/v1/plugins/${config.pluginId}/proxy${path}`;  // 走 axons 代理
};
```

插件开发者调用 `pluginApi.fetch('/api/models')`，无需关心运行环境，`resolveUrl` 是唯一分支点。

#### 桌面端 CORS 要求

桌面端直连插件后端，插件后端**必须**返回 CORS 头：

```python
# Python 模板 — 每个响应都加 CORS
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

Web 端走代理，插件后端无需 CORS 头（axons 代理是同源响应）。

#### axons 代理端点（Web 端专用）

路由注册：

```go
// server.go registerRoutes() 新增
s.router.GET("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.POST("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.PUT("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.DELETE("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.PATCH("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.OPTIONS("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
```

代理 Handler 使用标准库 `net/http/httputil.ReverseProxy`（性能完全满足，详见下文）：

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
    proxy.FlushInterval = -1  // 立即 flush，保证 SSE 实时性

    r.URL.Path = path
    r.Host = target.Host
    proxy.ServeHTTP(w, r)
}
```

**为什么用标准库而非 fasthttp**：
1. 流量全部是 localhost loopback，QPS 由用户交互驱动（个位数到几十），远未达到标准库瓶颈
2. axons 全栈基于 `net/http` + `httprouter`，引入 fasthttp 需维护两套 HTTP 栈或重写所有 handler
3. `httputil.ReverseProxy` 天然支持 SSE streaming（`FlushInterval = -1` 立即 flush）
4. 桌面端不走代理，代理只在 Web 端生效，并发量更低

#### SSE（EventSource）跨域处理

| 运行模式 | SSE 连接方式 | 跨域? |
|---------|-------------|-------|
| 桌面端 | `new EventSource(plugin.endpoint + '/sse')` 直连 | 插件后端 CORS 头解决 |
| Web 端 | `new EventSource('/v1/plugins/:id/proxy/sse')` 走代理 | 同源，无跨域 |

代理端 `FlushInterval = -1` 保证 SSE 事件逐条实时转发，无缓冲延迟。

#### 插件 UI 静态文件加载

插件前端组件文件（`ui/index.js`）统一通过 axons `/plugins/:id/ui/*` 静态路由服务，无论桌面端/Web 端都走 axons 同源，**不存在跨域问题**：

```typescript
// 统一通过 axons 静态路由加载
const entryUrl = `/plugins/${plugin.id}/${plugin.frontend.entry}`;
import(/* @vite-ignore */ entryUrl)
```

---

## 三、manifest.json 协议

### 3.1 完整定义

> 文件名: `manifest.json` — 遵循业内惯例 (Chrome Extension / PWA / Firefox Add-on 均使用此命名)

```jsonc
{
  // === 基础信息 ===
  "id": "com.axons.huggingface",       // 反向域名, 全局唯一
  "name": "Hugging Face",               // 显示名
  "version": "1.0.0",                    // 语义化版本
  "description": "Manage local LLM models via Ollama",
  "author": "axons-community",
  "icon": "icon.svg",                    // 相对路径
  "category": "productivity",            // analysis | visualization | search | productivity
  "minAxonsVersion": "0.8.0",           // 最低兼容版本

  // === 权限声明 ===
  "permissions": [
    "graph:read",         // 读取代码图数据
    "project:read",       // 读取项目信息
    "model:register",     // 注册模型到系统
    "panel:create"        // 创建面板
  ],
  // permissions 合法取值见 3.5 节

  // === 后端进程 ===
  "backend": {
    "command": [".venv/bin/python", "server.py"],  // 启动命令 (平台 exec.Command)
    // 注意: Python 插件建议在 install.sh 中创建 .venv, command 指向 .venv/bin/python
    // 这样不依赖系统全局 Python 环境, 隔离性更好
    "port": 0,                           // 0=OS动态分配, 或指定固定端口
    "healthCheck": "/health",            // 健康检查路径 (平台轮询)
    "readyTimeout": "10s",               // 就绪超时 (推荐值见下表)
    "env": {                             // 额外环境变量(可选)
      "OLLAMA_HOST": "http://localhost:11434"
    },

    // 安装: 插件提供脚本, 平台只管执行 (思路B: 平台管调度, 插件管实现)
    "install": {
      "command": ["bash", "install.sh"], // 安装脚本, 执行一次, 退出码0=成功
      "timeout": "180s"                  // 超时 (默认180s, 需下载外部大文件的插件应显式设更大值, 推荐值见下表)
    },

    // 卸载: 插件可选提供清理脚本
    "uninstall": {
      "command": ["bash", "uninstall.sh"] // 可选, 清理插件自身残留
    },

    // 跨平台覆盖 (可选): 当默认值在特定平台上不适用时, 用 platforms.{os} 覆盖
    // 支持的 os 键: "windows" | "linux" | "darwin"
    // 覆盖规则: platforms.{os} 中的字段深度合并覆盖 backend 同级默认值
    //           仅限 command / install / uninstall / env 等运行时字段
    //           未声明的字段保持默认值不变
    "platforms": {
      "windows": {
        "command": [".venv\\Scripts\\python.exe", "server.py"],
        "install": { "command": ["cmd", "/c", "install.bat"] },
        "uninstall": { "command": ["cmd", "/c", "uninstall.bat"] },
        "env": { "OLLAMA_HOST": "http://127.0.0.1:11434" }
      }
    }
  },

  // === 前端 UI ===
  "frontend": {
    "entry": "ui/index.js",             // UMD/ESM 模块入口
    "panels": [{
      "id": "huggingface",
      "title": "Hugging Face",
      "icon": "ui/icon.svg",
      "location": "right",              // left | right | center-bottom | modal
      "activator": "activityBar",       // footer | activityBar | node-select | gearMenu | command
      "footerSlot": "left",             // left | right | center — 仅 activator='footer' 时生效，默认 left
      "order": 10                       // 排序权重，越小越靠前；内置保留 0~9，插件推荐 10~99，默认 10
    }],
    "commands": [{
      "id": "huggingface.open",
      "title": "Open Hugging Face",
      "shortcut": "Ctrl+Shift+M"
    }]
  },

  // === 激活事件 (懒加载) ===
  "activationEvents": ["onStartup", "onCommand:huggingface.open"]
}
```

#### 3.1.1 跨平台覆盖：platforms 字段

当插件的 `backend` 在不同操作系统上需要不同的启动命令、安装脚本或环境变量时，使用 `platforms` 字段进行增量覆盖，而非创建多个 manifest 文件。

**为什么不用 `manifest.windows.json` 独立文件**：

| 维度 | 独立文件方案 | `platforms` 内嵌方案 |
|------|------------|-------------------|
| 信息重复 | 两个文件 90% 内容相同（id/name/version/frontend/permissions 全部重复） | 仅覆盖差异字段，零重复 |
| 一致性风险 | 版本号/权限/面板定义可能不同步 | 单一数据源，不可能不同步 |
| 导入校验 | 需同时解析两个 manifest，合并逻辑复杂 | 解析一个文件，覆盖规则清晰 |
| 前端无差异 | `frontend` 不应写两份 | frontend 天然共享 |

**覆盖规则**：

1. `platforms.{os}` 中的字段**深度合并**覆盖 `backend` 同级默认值
2. 仅 `command` / `install` / `uninstall` / `env` 等运行时字段可被覆盖
3. 未在 `platforms.{os}` 中显式声明的字段保持默认值不变
4. 支持的 `os` 键：`windows` | `linux` | `darwin`
5. 不加 `platforms` 的现有插件自动以默认值运行，**无破坏性变更**

**解析逻辑**：

```go
// 解析逻辑伪代码
func resolveBackend(raw json.RawMessage, goos string) *BackendConfig {
    base := parse(raw)                    // 默认配置
    if override, ok := base.Platforms[goos]; ok {
        base = deepMerge(base, override)  // 仅覆盖 override 中显式声明的字段
    }
    base.Platforms = nil  // 运行时不再需要 platforms 字段
    return base
}
```

**跨平台脚本命名约定**：

| Unix (默认) | Windows |
|-------------|---------|
| `install.sh` | `install.bat` 或 `install.ps1` |
| `uninstall.sh` | `uninstall.bat` 或 `uninstall.ps1` |

**完整示例**（Python 插件，Unix 默认 + Windows 覆盖）：

```jsonc
{
  "id": "com.axons.huggingface",
  "backend": {
    "command": [".venv/bin/python", "server.py"],       // Unix 默认
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
  "frontend": { /* ... 前端定义在所有平台一致，无需覆盖 ... */ }
}
```

> **注**：`frontend`（panels/commands/entry）在所有平台上完全一致，不应出现在 `platforms` 覆盖中。

#### 3.1.2 超时参数推荐值

`readyTimeout` 和 `install.timeout` 应根据插件后端的技术栈合理设置：

**readyTimeout 推荐值**：

| 插件类型 | 推荐值 | 理由 |
|---------|--------|------|
| Go 单二进制 | `5s` | 启动快，几乎无需等待 |
| Python + FastAPI / Flask | `15s` | 首次 import 较慢，CPython 冷启动开销 |
| Python + 重型 SDK（如 huggingface_hub） | `20s` ~ `30s` | 大型依赖首次加载耗时更长 |
| Node.js | `10s` | 中等启动速度 |

**install.timeout 推荐值**：

| 场景 | 推荐值 | 理由 |
|------|--------|------|
| 纯 pip install（无外部下载） | `120s` | 依赖安装通常较快 |
| 需下载外部工具（如 Ollama） | `300s` | 外部工具下载耗时不可控 |
| 需下载大型模型文件 | `600s`+ | 模型文件可能数 GB，依赖网络速度 |

> **原则**：默认 180s 适用于大多数场景，但需要下载外部大文件的插件应显式设更大值，避免安装超时失败。

### 3.2 插件能力声明

manifest.json 中 `frontend` 和 `backend` 字段同时承担能力声明，不单独设置 `contributes` 段：

**静态声明 (manifest.json 中 frontend/backend)** — 插件包里写死的结构性扩展点，不会在运行时增减：

| 声明位置 | 类型 | 用途 | 系统消费方式 |
|---------|------|------|-------------|
| `frontend.panels` | 面板 | 注册 UI 面板 | ActivityBar 动态渲染, import() 加载组件 |
| `frontend.commands` | 命令 | 注册命令 | Command Palette / 快捷键触发 |
| `frontend.skills` | 技能路径 | 声明插件贡献的 skill 目录 | 启动时加入 SkillRegistry 扫描 |

**动态发现 (运行时)** — 运行时才知道的，随时变化，不需要也不应该在 manifest.json 静态声明：

| 类型 | 发现方式 | 理由 |
|------|---------|------|
| `tools` | 插件后端作为 MCP Server, axons 通过 `tools/list` 发现 | MCP 协议自带发现机制，静态声明多余且会过时 |
| `skills` | axons 启动时扫描 `skills/` 目录，读取各 SKILL.md 注册到 Agent 技能列表 | 技能目录独立于插件，Agent 按需发现并加载 |

> 判断标准：**结构性** = 插件包里写死的，运行时不变（面板、命令）→ 静态声明在 frontend/backend 中。**动态数据** = 运行时才知道的，随时变化（工具、技能）→ 动态发现。
>
> **注：models 不作为 contribution 类型**。模型数据由 axons DB 统一管理（`/v1/models` API），插件通过 API 读写模型数据即可，不需要注册表合并机制。

#### Skills 发现机制

Skills 与插件面板/命令不同，它不是 UI 扩展点，而是 Agent 的能力扩展。发现流程：

```
axons 启动
  ├── 扫描项目级 skills/ 目录 (当前项目 .axons/skills/ 或项目根 skills/)
  │   └── 读取每个子目录的 SKILL.md → 提取 name, description, trigger 条件
  ├── 扫描全局 ~/.axons/skills/ 目录
  │   └── 同上
  └── 注册到 Agent SkillRegistry (内存 map)
      └── key = skillId, value = { name, description, trigger, skillPath }

Agent 运行时
  ├── 用户输入 → Agent 匹配 trigger 条件 → 加载对应 skill 的完整指令
  └── skill 指令注入到 Agent context，引导 Agent 执行特定工作流
```

SKILL.md 提取字段：

| 字段 | 用途 | 示例 |
|------|------|------|
| `name` | 技能标识 | `code-graph-analyzer` |
| `description` | 技能描述，用于 Agent 判断是否适用 | "Analyze codebases for architecture and dependencies" |
| `trigger` | 触发条件关键词/正则 | `["analyze architecture", "check dependencies"]` |
| 正文内容 | 完整指令，运行时注入 Agent context | SKILL.md 的 body 部分 |

插件贡献 skill 的方式：

```jsonc
// manifest.json — 插件可以在自己的目录下放置 skill
{
  "id": "com.axons.search-tools",
  "frontend": {
    "skills": ["skills/code-search-assistant"]  // 相对路径，指向插件目录内的 SKILL.md
  }
}
```

axons 启动插件时，将 `frontend.skills` 中声明的路径加入 SkillRegistry 扫描范围，与内置 skills 统一管理。

### 3.3 前端插入点粒度

axons 当前 UI 结构中存在多个层级的插入点：

```
┌─── TopSearchBar ────────────────────────────────────┐  ← 插入点: 搜索结果增强
├─── ActivityBar ─┬─ Main Content ─┬─ RightPanel ──────┤
│  [Home]         │                 │                   │  ← 插入点: 活动栏图标
│  [FolderTree]   │  GraphCanvas    │  AI Chat          │
│  [AI]           │                 │                   │
│  ─────────────  │  ──────────────  │  ─────────────── │
│  [Plugin-A] ◄──│  LeftPanel      │                   │  ← 插入点: 面板内容
│  [Plugin-B] ◄──│  (分析面板区)    │                   │
│  ─────────────  │                 │                   │
│  [⚙ GearMenu]  │  ──────────────  │                   │
│                 │  BottomPanel    │                   │  ← 插入点: 底部面板 (二期)
│                 │  [Term][Output] │                   │     类似 IDE Panel 区域
├─────────────────┴─────────────────┴───────────────────┤
│  Footer: [Health][Analytics]... │ nodes/edges │ [Term] │
│         ← footerSlot:left →     ←  center →  ← right→ │
└───────────────────────────────────────────────────────┘
```

| 粒度 | 插入点 | 举例 | 声明方式 |
|------|--------|------|---------|
| **面板** | 活动栏→左/右区域展示完整面板 | 模型管理面板 | `frontend.panels` |
| **右键菜单项** | 图节点/文件树节点的右键菜单追加项 | "用XX分析此函数" | `frontend.contextMenu` (二期) |
| **搜索栏增强** | TopSearchBar 搜索结果中追加插件结果 | 模型搜索结果混入代码搜索 | `frontend.searchProvider` (二期) |
| **图节点增强** | GraphCanvas 节点悬停时追加插件信息 | 节点 tooltip 显示风险评分 | `frontend.nodeDecorator` (二期) |
| **底部面板** | Center Column 底部区域 (类似 IDE Panel) | 测试运行器输出面板 | `frontend.bottomPanel` (二期) |
| **设置页 Tab** | SettingsPanel 追加插件配置页 | 模型管理配置 Tab | `frontend.settingsTab` (二期) |
| **通知/状态栏** | 右下角 toast 或 Footer 右侧状态 | "模型下载中 67%" | 运行时动态，不需声明 |

#### Footer 面板位置分配：footerSlot

当 `activator='footer'` 时，面板按钮出现在 Footer 栏中。Footer 栏分为三个区域，通过 `footerSlot` 字段决定按钮放在哪个区域：

| footerSlot | 含义 | 典型面板 |
|---|---|---|
| `'left'`（默认） | 分析工具类按钮，Footer 左侧 | Health, Analytics, Impact, CFG, Sequence, Rules, Flow |
| `'center'` | 状态指示器，Footer 中间 | 预留：构建状态、插件运行状态等 |
| `'right'` | 独立功能区，Footer 右侧 | Terminal |

设计原则：
1. `footerSlot` 与 `location` 正交 — `location` 描述面板内容渲染在哪（left/right/center-bottom/modal），`footerSlot` 描述 Footer 按钮放在哪，两者独立
2. `footerSlot` 与 `activator` 正交 — `activator` 描述如何触发面板（footer/activityBar/command），`footerSlot` 描述 Footer 内的按钮位置
3. 默认 `'left'` — 绝大多数 footer 面板是分析工具类，属于左侧区域；右侧（独立功能区）和中间（状态区）语义特殊，需显式声明
4. 仅 `activator='footer'` 时生效 — 其他 activator 的面板不经过 Footer 渲染，`footerSlot` 无意义

前端消费方式（`Footer.tsx`）：
```
const leftPanels   = footerPanels.filter(p => (p.footerSlot ?? 'left') === 'left')
const centerPanels = footerPanels.filter(p => p.footerSlot === 'center')
const rightPanels  = footerPanels.filter(p => p.footerSlot === 'right')
```

**一期只做面板粒度**，理由：
1. 面板是最大的插入点 — 覆盖 90% 的插件需求，任何复杂 UI 都可以在面板内自行实现
2. 细粒度插入点依赖 axons 内部组件结构 — 右键菜单、节点装饰器等需要改造现有组件，每加一个插入点都要改 axons 代码
3. 面板内可以自建任何细粒度 UI — 比如插件想加右键菜单项，可以在面板内监听 EventBus 的 `node:rightClick` 事件，弹出自己的菜单，不需要 axons 提供菜单扩展点

```
一期:  frontend.panels (面板 + 活动栏图标)

二期:  frontend.contextMenu (右键菜单)
      frontend.searchProvider (搜索增强)
      frontend.nodeDecorator (节点装饰)
      frontend.bottomPanel (底部面板 — Center Column 底部 Tab 区域)
      frontend.settingsTab (设置页 Tab)
```

### 3.4 插件包形式

backend 和 frontend 均为可选，支持四种组合：

| 形式 | 举例 | backend | frontend |
|------|------|---------|----------|
| **前端+后端服务** | 模型管理插件（面板+Ollama API） | 有 HTTP 服务 | 有面板 |
| **前端+CLI命令** | 依赖追踪插件（面板+调 axons API） | null | 有面板 |
| **纯后端（无前端）** | 自定义 MCP 工具集（给 Agent 用） | 有 MCP 服务 | null |
| **纯前端（无后端）** | 主题插件、快捷键插件 | null | 有面板 |

manifest.json 示例（四种形式）：

```jsonc
// 形式1: 前端+后端服务
{
  "id": "com.axons.huggingface",
  "backend": { "command": ["python", "server.py"], "port": 0, "healthCheck": "/health" },
  "frontend": { "entry": "ui/index.js", "panels": [...], "commands": [...] }
}

// 形式2: 前端+CLI (无后端服务, 前端直接调 axons API)
{
  "id": "com.axons.dep-tracker",
  "backend": null,
  "frontend": {
    "entry": "ui/index.js",
    "panels": [...]
    // 前端组件内直接用 pluginApi.fetch('/api/xxx') 调 axons API
  }
}

// 形式3: 纯后端 (无前端, 如 MCP 工具集)
{
  "id": "com.axons.search-tools",
  "backend": { "command": ["./search-server"], "protocol": "mcp", "port": 0 },
  "frontend": null
  // tools 通过 MCP tools/list 动态发现, 不需要静态声明
}

// 形式4: 纯前端 (无后端)
{
  "id": "com.axons.dark-theme",
  "backend": null,
  "frontend": { "entry": "ui/index.js", "panels": [...] }
}
```

启动流程调整:
- `backend` 存在 → exec.Command 启动进程 + 健康检查
- `backend` 为 null → 跳过进程启动，直接注册 frontend 中的 panels/commands

### 3.5 权限定义

permissions 声明插件需要访问的系统资源，一期做声明校验（manifest.json 中必须声明），运行时拦截放到二期。

#### 合法取值与 API 映射

| 权限 | 说明 | 对应 API 路由 |
|------|------|--------------|
| `graph:read` | 读取代码图数据 | `GET /v1/graph/*`, `POST /v1/search`, `GET /v1/stats` |
| `project:read` | 读取项目信息 | `GET /v1/projects`, `GET /v1/repos` |
| `model:register` | 注册/注销模型到系统 | `POST /api/llm-models`, `PUT /api/llm-models/:id`, `DELETE /api/llm-models/:id` |
| `panel:create` | 创建 UI 面板 | 自动授予（面板由 PluginRegistry 管理） |
| `state:read` | 读取其他插件的共享状态 | `GET /v1/plugins/state/:key`（跨命名空间时需要） |
| `state:write` | 写入共享状态 | `PUT /v1/plugins/state/:key` |

**一期规则**：
1. manifest.json 中 `permissions` 字段必须填写插件实际需要的权限，未声明的权限视为未授权
2. 一期不做运行时拦截（即未声明 `model:register` 也能调 `POST /api/llm-models`），但会在日志中打 warn
3. 二期实现运行时拦截：API 层校验 `AXONS_PLUGIN_TOKEN` 对应的插件是否声明了所需权限

---

## 四、后端设计

### 4.1 目录结构

```
internal/plugin/
├── manager.go           # PluginManager: 进程生命周期管理
├── registry.go          # PluginRegistry: 统一注册表
├── manifest.go          # manifest.json 解析与校验
├── process.go           # 进程启动/停止/健康检查/崩溃重启
├── proxy.go             # 插件代理 handler (Web 端反向代理)
├── handlers.go          # API handler (注册到 api server)
└── marketplace.go       # 云端市场客户端 (二期)
```

### 4.2 核心数据结构

```go
// PluginInstance 运行时实例
type PluginInstance struct {
    Manifest  *PluginManifest
    Port      int
    Cmd       *exec.Cmd
    Status    string    // starting | running | stopped | crashed
    Restarts  int
    Token     string    // 鉴权 token
    StartedAt time.Time
}

// PluginEntry 注册表条目
type PluginEntry struct {
    PluginID  string          `json:"pluginId"`
    Type      string          `json:"type"`      // panels | tools | skills | ...
    ID        string          `json:"id"`         // entry 内唯一 ID
    Def       json.RawMessage `json:"def"`        // 具体定义 (schema 因 type 而异)
    Endpoint  string          `json:"endpoint"`   // http://127.0.0.1:PORT
    Status    string          `json:"status"`     // running | stopped | downloading | ...
    UpdatedAt time.Time       `json:"updatedAt"`
}

// PluginRegistry 统一注册表
type PluginRegistry struct {
    mu     sync.RWMutex
    byType map[string][]PluginEntry   // type → entries
    byID   map[string]*PluginEntry    // "type:id" → entry (快速查找)
}
```

#### ID 冲突策略

注册表中 `panel.id` 和 `command.id` 可能跨插件重复，采用**先注册优先**策略：

| 场景 | 处理方式 |
|------|---------|
| 两个插件声明相同 `panel.id` | 先注册的生效，后注册的打 warn 日志并跳过，前端不渲染重复面板 |
| 两个插件声明相同 `command.id` | 同上，先注册的绑定快捷键，后注册的仅出现在 Command Palette（不带快捷键） |
| 插件启动顺序依赖 | 一期不设计 `dependencies` 声明，按目录扫描顺序启动；二期补充拓扑排序 |

### 4.3 插件全生命周期

插件从获取到彻底移除，经历 6 个阶段：

**核心原则：平台管调度，插件管实现。** 平台不替插件做决定，插件不替平台管资源。

| 阶段 | 平台提供 | 插件提供 | 理由 |
|------|---------|---------|------|
| **导入** | 下载/接收文件、解压、校验 manifest.json 合法性、文件就位 | manifest.json 本身 | 平台必须验证包的合法性，但不理解包的内容 |
| **安装** | 执行入口 (install.command)、进度上报 (stdout→SSE)、失败回滚 | install 脚本（安装逻辑） | 平台无法穷举所有语言/依赖场景，插件最清楚自己需要什么环境 |
| **启动** | 分配端口、注入环境变量、exec.Command、健康检查、注册 frontend panels/commands | command 进程、/health 端点、/sync 上报 | 平台管进程调度，插件管业务逻辑 |
| **停止** | SIGTERM/SIGKILL、注销 panels/commands、SSE 通知 | 优雅退出（5s 内处理完请求） | 平台管进程生命周期，插件管资源释放 |
| **卸载** | 停止进程、删除目录、删除注册表条目 | uninstall 脚本（可选，清理插件自己的残留） | 平台知道自己创建了什么，插件知道平台不知道的 |
| **清理** | 删除 shared state、删除 token | 无 | shared state 是平台的数据，平台全权负责 |

```
导入 → 安装 → 启动 ⇄ 停止 → 卸载 → 清理
 │      │      │      │       │      │
 │      │      │      │       │      └─ 删除残留数据(shared state 等)
 │      │      │      │       └─ 删除插件文件目录
 │      │      │      └─ SIGTERM 停止进程, 注销 panels/commands
 │      │      └─ exec.Command 启动进程, 注册 frontend panels/commands
 │      └─ 构建运行环境 (语言运行时/依赖/外部服务)
 └─ 解压、校验 manifest.json, 文件就位
```

#### 阶段 1: 导入 (Import)

将插件包解压到本地，校验合法性，文件就位。**不做任何环境准备**。

```
来源:
  a) 云端市场下载 (二期): GET /v1/plugins/marketplace/:id/download → .axons-plugin.tar.gz
  b) 离线导入: POST /v1/plugins/import (上传 .axons-plugin.tar.gz 文件)

流程:
  1. 下载/接收 .axons-plugin.tar.gz 到 /tmp/axons-import-:id/
  2. 解压到临时目录
  3. 读取并校验 manifest.json:
     - 必填字段: id, name, version (backend 和 frontend 至少有一个非 null)
     - id 格式: 反向域名 (com.axons.xxx)
     - version 格式: 语义化版本 (1.0.0)
  4. 检查 minAxonsVersion 兼容性
  5. 检查 id 是否已安装 (防止重复)
  6. 校验通过 → 将临时目录移动到 ~/.axons/plugins/:id/
  7. 写入 installed.json 注册表 (status = "imported")
  8. 校验失败 → 清理临时目录, 返回错误

状态: imported (文件已就位, 环境未准备)

API:
  POST /v1/plugins/import         离线导入 (multipart/form-data 上传)
  POST /v1/plugins/marketplace/:id/download  云端下载 (二期)
```

#### 阶段 2: 安装 (Install)

构建插件的运行环境，确保所有前置条件满足，使插件可启动。

**核心原则：平台管调度，插件管实现。** 平台不替插件做决定（不猜语言/依赖/服务），插件最清楚自己需要什么环境。

```
两种思路对比:
  思路A (平台智能安装): 平台读 runtime.type=python → 检查 python → pip install → 检查 ollama → 启动 ollama
    问题: 平台要适配所有语言场景, 永无止境, 每加一种语言平台就要加代码

  思路B (插件提供脚本, 平台执行): ✅ 采用
    平台读 manifest.json → 发现 install.command → exec 执行 → 等待退出码0
    插件自己写 install.sh / install.py / setup.js, 自己决定要装什么、检查什么、启动什么
    平台不猜, 只执行
```

```
平台在安装阶段做的事:
  1. 读取 manifest.json 的 backend.install.command
  2. exec.Command 执行 install.command (工作目录为插件目录)
  3. 实时 stdout/stderr → SSE 推给前端 (显示安装进度)
  4. 等待退出码: 0=成功, 非0=失败
  5. 失败 → 回滚: 删除插件目录, 移除 installed.json 条目
  6. 超时 (install.timeout) → kill 进程, 标记安装失败
  7. 更新 installed.json status = "installed"

插件 install 脚本示例 (模型管理插件):
  #!/bin/bash
  # install.sh — 插件自行决定安装逻辑
  set -e

  # 检查 Python
  python3 --version || { echo "Python 3.9+ required"; exit 1; }

  # 创建虚拟环境 & 安装依赖
  python3 -m venv .venv
  source .venv/bin/activate
  pip install -r requirements.txt

  # 检查 Ollama
  ollama --version || { echo "Installing Ollama..."; curl -fsSL https://ollama.com/install.sh | sh; }

  # 启动 Ollama 服务 (如果未运行)
  pgrep ollama || ollama serve &

  # 轮询等待服务就绪 (推荐: 轮询 > 硬编码 sleep)
  MAX_WAIT=10
  for i in $(seq 1 $MAX_WAIT); do
      curl -s http://localhost:11434/ >/dev/null 2>&1 && break
      sleep 1
  done

  # 预拉取默认模型
  ollama pull llama3

  echo "Install complete"

状态: installed (环境就绪, 可启动)

install 脚本最佳实践:
  1. 轮询等待 > 硬编码 sleep: 启动后台服务后, 用轮询检测就绪而非固定等待
     # 推荐
     for i in $(seq 1 $MAX_WAIT); do curl -s http://localhost:11434/ && break; sleep 1; done
     # 不推荐
     sleep 3  # 不可靠: 服务可能3s内未就绪, 也可能1s就绪白白浪费2s
  2. 优雅降级: 外部服务安装失败时 warn 而非 exit, 允许插件部分功能可用
     ollama --version || { echo "WARN: Ollama not found, some features unavailable"; }
  3. 使用 venv: Python 插件建议在 install.sh 中创建 .venv, command 指向 .venv/bin/python
  4. 超时预估: 需下载外部大文件(模型/工具)时, install.timeout 应显式设更大值(300s+)

manifest.json 中安装相关字段:
  "backend": {
    "install": {
      "command": ["bash", "install.sh"],   // 安装脚本, 执行一次, 退出码0=成功
      "timeout": "120s"                    // 超时 (默认180s)
    },
    "uninstall": {
      "command": ["bash", "uninstall.sh"]  // 可选, 清理插件自身残留
    }
  }

无 install 字段: 跳过安装阶段, 直接进入 installed 状态 (如 Go 单二进制插件)

#### install 脚本启动的后台进程生命周期

某些插件的 install 脚本会启动长期运行的外部服务（如 Ollama、Redis），需要明确其生命周期：

| 场景 | 行为 | 说明 |
|------|------|------|
| install 脚本启动后台服务（如 `ollama serve &`） | **不被平台清理** | 平台只管理 install.command 主进程，install 退出后主进程结束，但子进程（后台服务）继续运行 |
| uninstall 时 | 插件负责在 uninstall.sh 中决定是否停止/卸载外部服务 | 平台不知道 install 脚本启动了什么，不替插件做决定 |
| axons 退出时 | 外部服务不受影响 | Ollama 等是系统级服务，独立于 axons 生命周期 |

原则：install 脚本启动的外部服务与 axons 插件进程是两个独立的生命周期。插件进程由平台管理（启停/崩溃重启），外部服务由插件自己管理（install 创建、uninstall 清理、运行时监控健康状态）。

API:
  POST /v1/plugins/:id/install        执行安装 (运行 install.command)
  GET  /v1/plugins/:id/install-status  查询安装进度 (SSE: 实时推送 stdout)
```

#### 阶段 3: 启动 (Start)

启动插件后端进程，注册能力到系统。

```
流程:
  1. 校验插件状态为 installed/stopped (非 running)
  2. 分配端口:
     - 平台 net.Listen("tcp", "127.0.0.1:0") 获取 OS 动态端口（详见 4.6 节端口统一分配管理）
     - 端口号通过 stdin 管道注入（详见 4.7 节），避免 Close 后端口被抢占的 TOCTOU 竞态
     - 同时通过 AXONS_PLUGIN_PORT 环境变量传入，供不支持 stdin 读取的插件使用
  3. 构造环境变量:
     - AXONS_API_URL=http://127.0.0.1:{axonsPort}
     - AXONS_PLUGIN_PORT={assignedPort}
     - AXONS_PLUGIN_TOKEN={randomToken}
     - AXONS_PLUGIN_ID=com.axons.huggingface
  4. exec.Command 启动子进程 (工作目录为插件目录)
  5. 轮询 healthCheck 端点 (200ms 间隔, 最多 readyTimeout, 默认 10s)
  6. 就绪 → 读取 frontend.panels/commands → 注册到 PluginRegistry
  7. 插件后端调 POST /v1/plugins/registry/sync → 上报动态状态
  8. SSE 广播 plugin.started 事件 → 前端刷新面板/模型/工具列表
  9. 更新 installed.json status = "running"

状态: running

API:
  POST /v1/plugins/:id/start       启动指定插件
```

#### 阶段 4: 停止 (Stop)

停止插件后端进程，注销能力，但文件保留。

```
流程:
  1. 校验插件状态为 running
  2. 调用插件清理端点 (可选):
     - 平台向插件后端发 POST /cleanup (5s 超时)
     - 插件利用此机会清理副作用数据（如从 Axons 取消注册模型）
     - 插件不实现 /cleanup 则跳过此步（404 即视为跳过）
  3. 发送 SIGTERM (cmd.Process.Signal)
  4. 等待 5s 优雅退出
  5. 超时则 SIGKILL
  6. 从 PluginRegistry 移除该插件的所有 panels/commands
  7. 清理该插件的 shared state (可选, 默认保留)
  8. SSE 广播 plugin.stopped 事件 → 前端移除面板/模型/工具
  9. 更新 installed.json status = "stopped"

状态: stopped (进程不在, 文件保留, 可再次启动)

API:
  POST /v1/plugins/:id/stop        停止指定插件

崩溃处理 (自动):
  1. cmd.Wait() 返回 → 检测非预期退出
  2. Restarts < 3 → 自动重启 (指数退避: 2s/4s/8s)
  3. Restarts >= 3 → 标记 crashed, 更新 installed.json status = "crashed"
  4. SSE 广播 plugin.crashed 事件 → 前端显示 "插件已崩溃" 提示
```



#### 插件清理端点 /cleanup

插件通过 Axons API 注册的副作用数据（如模型配置）不属于 PluginRegistry 管理范围，平台无法自动清理。因此设计清理端点，让插件在停止前有机会自行清理。

```
插件实现 (可选):
   POST /cleanup    — 插件停止前由平台调用，5s 超时

插件清理端点职责:
   - 取消注册通过 Axons API 创建的副作用数据
   - 如: 调 DELETE /api/llm-models/:id 删除插件注册的模型配置
   - 如: 调 DELETE /v1/plugins/state/:key 清理共享状态

平台调用时机:
   - 正常停止: POST /v1/plugins/:id/stop → 先调 /cleanup → 再 SIGTERM
   - 卸载: 同上，先停止再删文件
   - 崩溃: 无法调用 /cleanup（进程已死），残留数据由下次启动时修复（见下）

不实现 /cleanup: 平台收到 404 即跳过，不影响停止流程
```

#### 副作用数据来源标识

`/cleanup` 端点需要精确识别"本插件注册了哪些副作用数据"，避免误删其他插件的数据。推荐方案：

**一期推荐：name 字段嵌入插件标识**

插件在通过 Axons API 注册副作用数据时，在可辨识字段（如 `name`）中嵌入插件标识后缀，`/cleanup` 时按此标识过滤：

```python
# 注册时：在 name 中嵌入 [Ollama] 标识
def _register_to_axons(model_name: str):
    display_name = f"{base} ({quant}) [Ollama]"  # ← [Ollama] 标识
    # POST /api/llm-models { "name": display_name, ... }

# 清理时：按标识过滤
@app.post("/cleanup")
async def cleanup():
    existing = _get_axons_models()
    for m in existing:
        if "[Ollama]" in m.get("name", ""):  # ← 精确匹配自己的注册
            # DELETE /api/llm-models/:id
```

> **注意**：不能仅按 `provider == "custom"` 过滤——其他插件也可能用 `custom` provider 注册模型。

**二期增强：Axons 自动注入来源标记**

Axons 在 `POST /api/llm-models` 等写入 API 中，根据请求的 `AXONS_PLUGIN_TOKEN` 自动在数据中附加 `source_plugin_id` 字段，插件 `/cleanup` 时按此字段精确匹配：

```jsonc
// 二期：Axons 自动注入 source_plugin_id
{
  "id": "model-123",
  "name": "Llama-3.2 [Ollama]",
  "provider": "custom",
  "source_plugin_id": "com.axons.huggingface"  // ← Axons 自动注入
}
```

#### 崩溃重启后的状态修复

```
插件崩溃后，通过 Axons API 注册的副作用数据（如模型配置）仍然存在于 Axons DB 中，
但对应的底层服务（如 Ollama 模型）可能已不可用。插件重启后应执行状态修复：

推荐实现（插件后端 /health 端点或启动时自检）:
   1. 检查本插件注册的模型是否仍在 Ollama 中运行
   2. 如果 Ollama 中模型已不在 → 调 DELETE /api/llm-models/:id 清理残留配置
   3. 如果 Ollama 中模型仍在运行 → 保持注册不变

平台不替插件做状态修复，因为平台不知道插件注册了什么副作用数据。
```

#### 阶段 5: 卸载 (Uninstall)

删除插件文件目录，不可再启动。

```
流程:
  1. 如果插件正在运行 → 先执行停止流程
  2. 从 PluginRegistry 移除该插件的所有 panels/commands (若还在)
  3. 从 installed.json 注册表移除该插件条目
  4. 删除插件目录 ~/.axons/plugins/:id/
  5. SSE 广播 plugin.uninstalled 事件 → 前端移除所有 UI 痕迹

注意: 此阶段不清理 shared state, 保留数据供重新安装后使用

状态: uninstalled (目录已删, shared state 可能残留)

API:
  DELETE /v1/plugins/:id            卸载指定插件
```

#### 阶段 6: 清理 (Cleanup)

清除插件残留的共享数据，彻底移除所有痕迹。

```
流程:
  1. 删除该插件写入的所有 shared state (key 前缀为 pluginId)
  2. 清除该插件的 token 记录
  3. 清除 SSE 订阅

何时触发:
  a) 卸载时用户勾选 "同时清除数据" → 卸载+清理合并执行
  b) 单独调用清理 API (针对已卸载但残留数据的插件)

状态: cleaned (彻底移除, 无任何残留)

API:
  DELETE /v1/plugins/:id/data       清理插件残留数据
```

#### 状态机总结

```
                  import                install              start
  [不存在] ──────────────→ imported ──────────→ installed ──────────→ running
                              │                   │                   │  ▲
                              │ 校验失败          │                   │  │
                              ▼                   │  stop             │  │ 自动重启
                           [不存在]               │  (crashed)        │  │ (≤3次)
                              │                   ▼                  │  │
                     取消导入  │               stopped ───────────────┘  │
                              ▼                 │                       │
                           [不存在] uninstall   │          crash ≥3次   │
                                                ▼                       │
                                            uninstalled            crashed
                                              │                       │
                              cleanup          │           uninstall   │
                                                ▼                       │
                                            cleaned ◄──────────────────┘
```

#### API 路由汇总

```go
// 插件全生命周期
s.router.POST("/v1/plugins/import", s.handleImportPlugin)         // 导入(离线上传)
s.router.POST("/v1/plugins/install", s.handleInstallPlugin)       // 安装(导入后确认)
s.router.POST("/v1/plugins/:id/start", s.handleStartPlugin)       // 启动
s.router.POST("/v1/plugins/:id/stop", s.handleStopPlugin)         // 停止
s.router.DELETE("/v1/plugins/:id", s.handleUninstallPlugin)       // 卸载(删文件)
s.router.DELETE("/v1/plugins/:id/data", s.handleCleanupPlugin)    // 清理(删残留数据)
s.router.GET("/v1/plugins", s.handleListPlugins)                  // 插件列表
s.router.POST("/v1/plugins/scan", s.handleScanPlugins)             // 重新扫描插件目录

// 注册表
s.router.GET("/v1/plugins/registry/:type", s.handleGetPluginEntries)    // 按类型查
s.router.POST("/v1/plugins/registry/sync", s.handleSyncPluginEntries)   // 插件上报

// 共享状态 (key 自动加 pluginId: 前缀, 插件只能读写自己的命名空间; 跨插件读需声明 permissions 中的 state:read 权限)
s.router.GET("/v1/plugins/system-state", s.handleGetSystemState)      // 系统状态镜像
s.router.GET("/v1/plugins/state/:key", s.handleGetPluginState)        // 读共享KV (自动限缩到当前插件命名空间)
s.router.PUT("/v1/plugins/state/:key", s.handleSetPluginState)        // 写共享KV (自动限缩到当前插件命名空间)

// 插件代理 — Web 端专用，桌面端直连不走此路由
s.router.GET("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.POST("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.PUT("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.DELETE("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.PATCH("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
s.router.OPTIONS("/v1/plugins/:id/proxy/*path", s.handlePluginProxy)
```

#### SSE 事件类型

插件生命周期事件通过 SSE 推送给前端，前端据此更新 UI 状态：

| 事件类型 | 触发时机 | Payload |
|---------|---------|---------|
| `plugin.imported` | 插件包导入完成 | `{ pluginId, name, version }` |
| `plugin.installed` | 安装脚本执行成功 | `{ pluginId, name, version }` |
| `plugin.installProgress` | 安装脚本 stdout 输出 | `{ pluginId, line }` |
| `plugin.installFailed` | 安装脚本执行失败 | `{ pluginId, error }` |
| `plugin.started` | 插件后端进程就绪 | `{ pluginId, endpoint, panels, commands }` |
| `plugin.stopped` | 插件后端进程停止 | `{ pluginId }` |
| `plugin.crashed` | 插件崩溃且超过重启上限 | `{ pluginId, restarts, lastError }` |
| `plugin.uninstalled` | 插件卸载完成 | `{ pluginId }` |
| `plugin.cleaned` | 插件残留数据清理完成 | `{ pluginId }` |

前端消费示例：

```tsx
useEffect(() => {
  const unsubscribe = subscribeSSE((event) => {
    switch (event.type) {
      case 'plugin.started':
        refreshPluginList();
        break;
      case 'plugin.crashed':
        showToast(`${event.pluginId} 已崩溃`);
        break;
    }
  });
  return unsubscribe;
}, []);
```

### 4.4 鉴权

- 插件后端调用 axons API 时带 `Authorization: Bearer ${AXONS_PLUGIN_TOKEN}`
- axons API 层校验 token + permissions 白名单
- 系统状态镜像 (system-state) 返回: currentProjectId, selectedNodeId, activeFilePath, openPanels

**Token 生命周期**：

| 阶段 | Token 行为 |
|------|-----------|
| 插件启动 | 为该插件生成随机 token, 写入 PluginInstance.Token, 通过 AXONS_PLUGIN_TOKEN 环境变量注入 |
| 插件运行 | 插件后端每次调 axons API 时携带 token, axons 校验 token 有效性 + permissions 白名单 |
| 插件停止 | 销毁该插件的 token, 后续用该 token 的请求返回 401 |
| 插件崩溃重启 | 生成新 token, 旧 token 自动失效 |

一期采用进程级 token（不设过期时间，跟随进程生命周期），二期再考虑 token 刷新和细粒度权限控制。

### 4.5 存储路径

```
~/.axons/
├── plugins/
│   ├── installed.json              # 已安装插件注册表
│   ├── com.axons.huggingface/    # 插件目录
│   │   ├── manifest.json
│   │   ├── server.py               # 后端入口
│   │   ├── ui/
│   │   │   ├── index.js            # 前端组件
│   │   │   └── icon.svg
│   │   └── requirements.txt
│   └── ...
├── registry.json                   # 项目注册表 (现有)
└── axons.db                        # 主数据库 (现有)
```

### 4.6 端口统一分配管理

axons 平台统一为所有插件后端分配端口，插件后端不得自行选择端口。这确保了：

1. **无端口冲突**：多个插件同时运行时不会争抢同一端口
2. **可追溯性**：平台维护端口分配表，可查询哪个插件占用了哪个端口
3. **生命周期可控**：插件停止时端口释放，平台可在分配前检查端口是否仍被占用

#### 端口分配策略

```
端口范围: 18080-18999 (预留 920 个端口, 足够所有插件使用)

分配流程:
  1. 平台维护 PortAllocator (内存 map[int]string, port → pluginId)
  2. 新插件启动时:
     a. 检查 manifest.json 的 backend.port:
        - port = 0 (默认): 平台动态分配
        - port > 0: 固定端口, 平台检查是否可用 (已被占用则报错拒绝启动)
     b. 动态分配:
        - net.Listen("tcp", "127.0.0.1:0") 获取 OS 分配的空闲端口
        - 将 listener 传入插件进程 stdin (不 Close, 避免 TOCTOU)
        - 注册到 PortAllocator
  3. 插件停止时:
     - 从 PortAllocator 移除
     - Close listener (此时端口释放给 OS)
  4. axons 启动时:
     - 扫描 installed.json 中 status=running 的插件
     - 重新为它们分配端口 (之前的端口可能已被其他进程占用)
```

#### 端口分配器实现

```go
// internal/plugin/port.go
type PortAllocator struct {
    mu       sync.Mutex
    used     map[int]string      // port → pluginId
    listeners map[int]net.Listener // port → listener (持有不 Close, 防抢占)
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
                ln.Close() // 释放端口
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

> **为什么不用固定端口范围扫描**：`net.Listen("127.0.0.1:0")` 让 OS 分配空闲端口比逐个扫描 18080-18999 更可靠，OS 内核保证端口可用。固定端口范围只在开发调试期有意义（通过 manifest.json `backend.port` 指定）。

### 4.7 stdin 端口注入协议

#### 问题

`net.Listen("127.0.0.1:0")` 获取端口后如果先 Close 再传给插件，存在 TOCTOU（Time-of-check to time-of-use）竞态：Close 后端口可能被其他进程抢占。解决方案是持有 listener 不 Close，将端口号通过 stdin 管道注入插件进程。

#### 协议定义

```
协议格式:
  1. axons 通过 cmd.Stdin (io.WriteCloser) 写入一行: "PORT:{number}\n"
  2. 写完后不关闭 stdin (插件可能还需要读取其他后续协议消息)
  3. 插件从 stdin 读取第一行，解析 "PORT:" 前缀，获取端口号
  4. 插件绑定到该端口启动 HTTP 服务

时序:
  axons                                     plugin
    │                                          │
    │── cmd.Start() ──────────────────────────→│
    │── stdin.Write("PORT:18080\n") ──────────→│
    │                                          │── 读取 stdin, 解析 port
    │                                          │── net.Listen("127.0.0.1:18080")
    │                                          │── 启动 HTTP server
    │←─ healthCheck 轮询 ─────────────────────│
    │                                          │
```

#### axons 端实现

```go
// internal/plugin/process.go — 启动进程时注入端口
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

    // 通过 stdin 注入端口号
    fmt.Fprintf(stdinPipe, "PORT:%d\n", port)

    plugin.Cmd = cmd
    plugin.stdinPipe = stdinPipe
    return nil
}
```

#### 插件端读取示例

```python
# Python — 读取 stdin 获取端口
import sys

def read_port_from_stdin():
    """从 stdin 读取 axons 分配的端口号"""
    line = sys.stdin.readline().strip()
    if line.startswith("PORT:"):
        return int(line[5:])
    # fallback: 从环境变量读取
    return int(os.environ.get("AXONS_PLUGIN_PORT", "18080"))

if __name__ == "__main__":
    port = read_port_from_stdin()
    uvicorn.run(app, host="127.0.0.1", port=port)
```

```python
# Python FastAPI + uvicorn — stdin PORT: 协议集成模板
import sys
import os
import uvicorn
from fastapi import FastAPI

app = FastAPI()

@app.get("/health")
async def health():
    return {"status": "ok"}

def read_port_from_stdin() -> int:
    """从 stdin 读取 axons 分配的端口号 (优先), fallback 到环境变量"""
    try:
        line = sys.stdin.readline().strip()
        if line.startswith("PORT:"):
            return int(line[5:])
    except (ValueError, IOError):
        pass
    # fallback: 从环境变量读取
    return int(os.environ.get("AXONS_PLUGIN_PORT", "18080"))

if __name__ == "__main__":
    port = read_port_from_stdin()
    uvicorn.run(app, host="127.0.0.1", port=port)
```

> **注意**：FastAPI/uvicorn 插件应使用上述模板从 stdin 读取端口，而非仅依赖 `AXONS_PLUGIN_PORT` 环境变量。stdin 通道无 TOCTOU 竞态风险，是更可靠的方式。

```go
// Go — 读取 stdin 获取端口
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
    // fallback: 从环境变量读取
    port, _ := strconv.Atoi(os.Getenv("AXONS_PLUGIN_PORT"))
    if port == 0 {
        port = 18080
    }
    return port
}
```

#### 双通道保障

| 通道 | 优先级 | 适用场景 | 可靠性 |
|------|--------|---------|--------|
| stdin `PORT:` 协议 | 主 | 支持读取 stdin 的插件 | 无竞态，端口不会被抢占 |
| `AXONS_PLUGIN_PORT` 环境变量 | 备 | 不支持 stdin 的插件（如某些 Go 二进制） | 存在 TOCTOU 风险，但实际概率极低（本地开发环境） |

> 插件应**优先读取 stdin**，仅当 stdin 不可用时 fallback 到环境变量。axons 两个通道都会写入，确保兼容性。

### 4.8 installed.json 格式

`~/.axons/plugins/installed.json` 是插件安装注册表，记录所有已导入/已安装插件的状态和元数据。

```jsonc
{
  "version": 1,                    // 注册表格式版本
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
      "dir": "~/.axons/plugins/com.axons.huggingface",  // 插件目录绝对路径
      "port": 18081,               // 当前分配的端口 (仅 running 时有值)
      "installedAt": "2026-05-14T10:30:00Z",   // 导入时间
      "updatedAt": "2026-05-14T11:00:00Z",     // 最后状态变更时间
      "manifestHash": "sha256:abc123...",       // manifest.json 哈希, 用于检测非法修改
      "backend": {                 // 冗余存储, 避免每次读 manifest.json
        "command": [".venv/bin/python", "server.py"],
        "port": 0,
        "healthCheck": "/health"
      },
      "frontend": {                // 冗余存储
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

#### 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `version` | int | 注册表格式版本，未来格式变更时递增 |
| `plugins` | map | pluginId → 插件信息 |
| `plugins[id].status` | string | 当前状态：imported / installed / running / stopped / crashed |
| `plugins[id].dir` | string | 插件目录绝对路径，用于 exec.Command 的工作目录 |
| `plugins[id].port` | int | 当前分配的端口号，仅 running 时 > 0 |
| `plugins[id].manifestHash` | string | manifest.json 的 SHA256，启动时校验防止被篡改 |
| `plugins[id].backend` / `frontend` | object | 从 manifest.json 冗余存储，加速启动扫描 |

#### 持久化时机

| 操作 | 变更 |
|------|------|
| 导入成功 | 新增条目，status=imported |
| 安装成功 | status → installed |
| 启动成功 | status → running，port → 分配的端口 |
| 停止 | status → stopped，port → 0 |
| 崩溃 | status → crashed |
| 卸载 | 删除条目 |

> **注意**：axons 启动时会读取 installed.json，对 status=running 的插件重新启动。如果 axons 非正常退出（kill -9），installed.json 中可能残留 status=running，axons 下次启动时应将这些条目降级为 installed 再重新启动。

### 4.9 权限 warn 日志

一期不做运行时拦截，但会记录 warn 日志，帮助开发者发现权限配置遗漏，并为二期拦截提供数据基础。

#### 日志格式

```
[plugin-permission] WARN plugin={pluginId} permission={requiredPerm} method={method} path={path}
```

示例：
```
[plugin-permission] WARN plugin=com.axons.huggingface permission=model:register method=POST path=/api/llm-models
[plugin-permission] WARN plugin=com.axons.search-tools permission=graph:read method=GET path=/v1/graph/nodes
```

#### 实现方式

```go
// internal/plugin/middleware.go — API 中间件
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

// matchPermission 根据 API 路由映射到权限
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

### 5.1 ActivityBar 面板排序与统一渲染

#### 问题

当前 `ActivityBar.tsx` 中，内置按钮（Home / FolderTree / AI）硬编码在顶部区域，插件按钮与 Gear 菜单放在 `mt-auto` 的底部区域，导致插件图标紧贴设置按钮、偏底部，与内置按钮之间有大段空白。

```
当前布局:
  [Home]        ← 顶部区域 (硬编码)
  [FolderTree]
  [AI]
                  
  ──空白──      ← mt-auto 推到底部
                  
  [Plugin-A]    ← 底部区域 (registry 动态)
  [Plugin-B]
  [⚙ GearMenu]
```

#### 目标

插件图标紧跟内置按钮往下排列，内置按钮始终在插件按钮前面，Gear 菜单保持在最底部。

```
目标布局:
  [Home]        ← 内置按钮 (registry, order 0~9)
  [FolderTree]
  [AI]
  [Plugin-A]    ← 插件按钮 (registry, order 10+)
  [Plugin-B]

  ──空白──      ← mt-auto 推到底部

  [⚙ GearMenu]  ← 底部固定 (activator='gearMenu')
```

#### 改造方案

**核心思路**：所有 `activator='activityBar'` 的面板（内置 + 插件）统一通过 `panelRegistry` 渲染，去掉硬编码按钮，用 `order` 字段控制排序。

**1. 内置按钮注册到 panelRegistry（`useAppState.ts`）**

当前内置按钮中，Home 和 AI 按钮未在 registry 注册，需补充注册并分配 order：

```tsx
// 内置 activityBar 面板注册 — order 0~9
registerPanel({ id: 'home', title: 'Projects', icon: 'Home', location: 'left-top', activator: 'activityBar', component: ProjectSelector, order: 0 });
registerPanel({ id: 'fileTree', title: 'activitybar:files', icon: 'FolderTree', location: 'left-top', activator: 'activityBar', component: FileTreePanel, order: 1 });
registerPanel({ id: 'rightPanel', title: 'panels:chat.newConversation', icon: 'Sparkles', location: 'right', activator: 'activityBar', component: RightPanel, order: 2 });
```

**2. 插件面板自声明 order（方案 B — 业内惯例）**

插件在 manifest.json 的 `PanelDef` 中声明 `order`，后端透传给前端。这是 IDE（menus `group@number`、config `order`）、JetBrains（ActionGroup `position`）等平台的通用做法：排序权重由插件自声明，平台通过约定区间防冲突，不做审批。

manifest.json 示例：
```jsonc
"frontend": {
  "panels": [{
    "id": "huggingface",
    "title": "Hugging Face",
    "icon": "ui/icon.svg",
    "location": "right",
    "activator": "activityBar",
    "order": 10       // ← 新增：插件自声明排序权重
  }]
}
```

后端 `manifest.go` 的 `PanelDef` 增加 `Order` 字段：
```go
type PanelDef struct {
    ID         string            `json:"id"`
    Title      string            `json:"title"`
    TitleI18n  map[string]string `json:"titleI18n,omitempty"`
    Icon       string            `json:"icon"`
    Location   string            `json:"location"`
    Activator  string            `json:"activator"`
    FooterSlot string            `json:"footerSlot"`
    Order      int               `json:"order,omitempty"`    // ← 新增：排序权重
}
```

前端注册时读取后端透传的 order，未声明时回退到默认值 10：
```tsx
registerPanel({
  id: entry.id,
  title: def.title || entry.id,
  icon: def.icon || 'Puzzle',
  location: def.location || 'left',
  activator: def.activator || 'activityBar',
  order: def.order ?? 10,     // ← 读取插件声明的 order，默认 10
  isPlugin: true,
  pluginId: entry.pluginId,
  endpoint: data.endpoint || entry.endpoint,
  asyncLoader: () => { ... },
});
```

排序效果：
| order 范围 | 归属 | 说明 |
|------------|------|------|
| 0~9 | 内置按钮 | Home(0), FolderTree(1), AI(2)，平台保留区间 |
| 10~99 | 插件按钮（推荐） | 插件自声明，推荐在此区间 |
| 100+ | 插件按钮（不限制） | 不做强制，大数值排在后面 |

排序规则：
1. 按 `order` 升序排列，值越小越靠前
2. 同 `order` 值的面板按注册顺序排列（先注册排前面）
3. 未声明 `order` 的插件面板默认为 10，排在内置按钮之后
4. 内置按钮 0~9 为平台保留区间，插件声明的 order 值若落在此区间仍生效（不做强制拦截，但文档约定不使用）

**为什么选方案 B 而非方案 A（前端自动赋值）**：
- 方案 A 插件无法控制自身位置，只能按注册顺序排列，与业内惯例不符
- IDE menus 的 `group@number`、config 的 `order`，JetBrains 的 `position` 权重，都是插件自声明
- 方案 B 改动量与 A 相当（仅多一个后端字段），但赋予了插件开发者控制权

**3. ActivityBar.tsx 统一渲染**

顶部区域改为遍历 `getPanelsByActivator('activityBar')`，去掉硬编码按钮：

```tsx
<div className="w-11 h-full bg-void flex flex-col items-center shrink-0 border-r border-border-subtle">
    {/* 顶部区域：所有 activityBar 面板（内置 + 插件），按 order 排序 */}
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

    {/* 底部区域：Gear 菜单（activator='gearMenu'，不参与 activityBar 排序） */}
    <div className="mt-auto w-full flex flex-col items-center pb-1">
        {/* GearMenu 组件 */}
    </div>
</div>
```

#### 影响面

| 文件 | 改动 |
|------|------|
| `ui/src/components/ActivityBar.tsx` | 顶部区域改为遍历 registry 渲染，底部区域只保留 GearMenu |
| `ui/src/hooks/useAppState.ts` | 补充 Home 按钮注册；插件注册时读取 `def.order ?? 10` |
| `ui/src/lib/panelRegistry.ts` | 无改动（`order` 字段和排序逻辑已有） |
| `internal/plugin/manifest.go` | `PanelDef` 增加 `Order int \`json:"order,omitempty"\`` 字段 |
| `internal/plugin/registry.go` | `/v1/plugins/registry/panels` API 透传 `order` 字段（已在 `def` 中，无需额外改动） |

#### Home 按钮特殊处理

Home 按钮点击后弹出 ProjectSelector 浮层，与普通面板的 toggle 行为不同。注册为 panel 后，需在 ActivityBar 点击回调中区分处理：

- 普通面板（`fileTree` / `rightPanel` / 插件面板）：`togglePanel(id)`
- Home 面板：弹出 ProjectSelector 浮层（保持现有 `isHomeOpen` 逻辑）

实现方式：在 `PanelDef` 中通过 `id === 'home'` 判断，或在 `PanelDef` 扩展 `action: 'popup' | 'toggle'` 字段。

#### 用户拖拽重排（未来扩展）

IDE 的 ActivityBar 图标支持用户拖拽重排，存储在用户设置中。axons 未来可加入此能力：

1. 用户拖拽后，将面板 ID 顺序持久化到 `~/.axons/activitybar-order.json`
2. 渲染时：用户自定义顺序 > `order` 声明 > 注册顺序
3. 重置按钮恢复为 `order` 声明的默认顺序

---

### 5.2 活动栏齿轮菜单改造

当前 `ActivityBar.tsx` 底部只有 Settings 按钮，改为下拉菜单:

```
 ┌──────────┐
 │ ⚙  ▸     │  ← 点击展开菜单
 └──────────┘
   ┌──────────────┐
   │ ⚙ Settings   │  ← 原有功能
   ├──────────────┤
   │ 🧩 Extensions│  ← 新增入口
   └──────────────┘
```

实现方式: 复用 ProjectSelector 的弹出面板模式 (homeRef + click outside close), 改为 GearMenu 组件。

### 5.3 Extensions 面板

点击"Extensions"后右侧滑出面板 (与 SettingsPanel 同级):

```
┌─────────────────────────────────────┐
│ Extensions                        ✕ │
├─────────────────────────────────────┤
│ 🔍 搜索插件...                       │
├─────────────────────────────────────┤
│ [All] [Analysis] [Visualization]    │
│ [Search] [Productivity]             │
├─────────────────────────────────────┤
│ ┌─────────────────────────────────┐ │
│ │ [Icon] Hugging Face     v1.0   │ │
│ │        Manage local LLM models  │ │
│ │        by axons-community       │ │
│ │        status: running          │ │
│ │                   [⋯]           │ │  ← 下拉: Enable/Disable/Uninstall
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
│ │ 📥 Import from File...          │ │  ← 离线导入入口
│ └─────────────────────────────────┘ │
└─────────────────────────────────────┘
```

### 5.4 插件卡片数据结构

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

安装后行为:
- 插件 frontend.panels → 活动栏动态渲染图标
- 插件 frontend.tools → MCP 工具列表合并

### 5.5 前端组件动态加载

```tsx
// 插件 UI 包 (ui/index.js) 导出 React 组件
export function ModelManagerPanel({ pluginApi }) {
  // pluginApi.fetch('/api/models') → 桌面端直连插件后端 / Web端走 axons 代理
  // pluginApi.onEvent('node:selected', handler) → EventBus 订阅
  // pluginApi.emitEvent('model:ready', payload) → EventBus 广播
}

// axons 前端加载 — 插件 UI 静态文件走 axons /plugins/:id/ui/* 路由，无跨域
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

  // pluginApi 构造 — 根据运行环境自动选择直连/代理
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

#### 组件容错机制

```tsx
// 1. 加载失败 — JS 文件不存在/损坏/网络错误
function PluginLoadError({ plugin, error }) {
  return (
    <div className="plugin-error">
      <p>插件 {plugin.name} 加载失败</p>
      <p className="text-sm text-text-secondary">{error}</p>
      <button onClick={() => window.location.reload()}>重试</button>
    </div>
  );
}

// 2. 渲染崩溃 — 组件内部 JS 错误，用 ErrorBoundary 隔离，不影响 axons 主界面
class PluginErrorBoundary extends React.Component {
  state = { hasError: false, error: null };
  static getDerivedStateFromError(error) {
    return { hasError: true, error };
  }
  render() {
    if (this.state.hasError) {
      return (
        <div className="plugin-error">
          <p>插件渲染出错</p>
          <p className="text-sm text-text-secondary">{this.state.error.message}</p>
          <button onClick={() => this.setState({ hasError: false })}>重试</button>
        </div>
      );
    }
    return this.props.children;
  }
}
```

容错原则：
- 插件 UI 崩溃**不影响** axons 主界面和其他插件
- 展示错误信息 + 重试按钮，不静默失败
- ErrorBoundary 捕获渲染时错误，import catch 捕获加载时错误
```

> Wails 特殊处理: 插件 UI 文件放在 `~/.axons/plugins/:id/ui/` 下, axons 的 static handler 增加 `/plugins/*` 路由指向该目录。

### 5.6 统一 Hook

```tsx
// hooks/usePluginRegistry.ts
export function usePluginRegistry<T>(type: string): T[] {
  const [items, setItems] = useState<T[]>([]);
  useEffect(() => {
    fetch(`/v1/plugins/registry/${type}`).then(r => r.json()).then(setItems);
  }, []);
  return items;
}

// Agent 消费技能列表
const pluginSkills = usePluginRegistry('skills');

// ActivityBar 消费面板
const pluginPanels = usePluginRegistry('panels');
```

### 5.7 插件 UI 风格一致性：axons-plugin-ui 共享组件库

#### 问题

插件前端组件运行在 axons 的 React 运行时中，但插件开发者不知道 axons 的设计规范、颜色体系、组件样式，导致插件面板与 axons 主界面风格割裂。

#### 方案：axons-plugin-ui 主题感知组件库

axons 构建 `axons-plugin-ui.umd.js`，在启动时挂载 `window.AxonsPluginUI`，同时全局加载 CSS 变量和组件样式。插件开发者将 `axons-plugin-ui` 设为 external 即可引用，无需安装 npm 包，无需拷贝文件，所有资源由 axons 宿主在运行时通过 HTTP 路由提供。

##### axons 端构建产物（随 axons 分发）

```
dist/plugin-sdk/
├── axons-plugin-ui.umd.js   # UMD 包，挂载 window.AxonsPluginUI
├── theme.css                # CSS 变量（颜色、字体、阴影、圆角）
└── components.css           # 组件样式（.axons-btn、.axons-card 等）
```

##### 源码位置（axons 工程内）

```
ui/src/plugin-sdk/
├── index.tsx                 # 组件源码 + 导出
├── theme.css                 # CSS 变量定义
└── components.css            # 组件样式定义
```

构建流程：`vite build`（主应用）→ `vite build --config vite.plugin-sdk.config.ts`（UMD 库）→ `cp` CSS 文件到 dist。

#### 使用方式

```tsx
// 插件 src/MyPanel.tsx
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

> 注意：插件不需要 `import 'axons-plugin-ui/theme.css'`，CSS 已由 axons 在 `index.html` 中全局加载。

#### Vite 配置（插件项目）

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
      // 外部化：复用 axons 运行时已有的 React 和 AxonsPluginUI，不打包进插件产物
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

#### 运行时加载机制

axons 启动时按以下顺序加载，确保插件 `import { Button } from 'axons-plugin-ui'` 能命中全局变量：

1. **`index.html`** 加载 CSS：
   ```html
   <link rel="stylesheet" href="/plugin-sdk/theme.css" />
   <link rel="stylesheet" href="/plugin-sdk/components.css" />
   ```

2. **`main.tsx`** 暴露 React 全局变量并加载 UMD 包：
   ```ts
   import * as React from 'react';
   import * as ReactDOM from 'react-dom';
   (window as any).React = React;
   (window as any).ReactDOM = ReactDOM;

   const script = document.createElement('script');
   script.src = '/plugin-sdk/axons-plugin-ui.umd.js';  // 挂载 window.AxonsPluginUI
   document.head.appendChild(script);
   ```

3. **插件 `index.js`** 被动态 import 时，`import from 'axons-plugin-ui'` 自动解析到 `window.AxonsPluginUI`。

#### CSS 变量体系

axons 的主题通过 CSS 变量定义，插件直接使用这些变量即可自动跟随主题变化（暗色/亮色等）：

```css
/* 已通过 /plugin-sdk/theme.css 全局加载，插件无需单独引入 */
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

> 插件开发者即使不使用 `axons-plugin-ui` 组件，也应使用这些 CSS 变量来定义颜色、字体，而不是硬编码色值。

#### 分期计划

| 阶段 | 内容 | 状态 |
|------|------|------|
| 一期 | UMD 构建 + CSS 变量 + 基础组件（Button/Card/Input/Select/Badge/Spinner/ProgressBar/Tabs），随 axons 分发 | ✅ 已实现 |
| 二期 | 扩展组件库 + 主题切换支持（亮色模式）+ Storybook 组件文档站 | 待规划 |

### 5.8 前端组件样式隔离

插件组件与 axons 主界面共享 React 运行时和 DOM，需要防止样式冲突。

#### 推荐方案：CSS 变量 + class 前缀（一期）

一期推荐轻量级方案，不引入 Shadow DOM 的复杂性：

**规则**：
1. 插件所有 CSS class 加 `plugin-{pluginId}-` 前缀，如 `.plugin-com-axons-huggingface__card`
2. 使用 axons CSS 变量定义颜色/字体，不硬编码色值
3. 使用 `axons-plugin-ui` 组件库时，class 前缀由组件库内部处理

```css
/* 插件样式示例 */
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

**Tailwind 插件项目**：可通过 `prefix` 配置自动加前缀：

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
// 使用: class="axp-bg-surface axp-border axp-border-border-default axp-text-text-primary"
```

#### 进阶方案：Shadow DOM（二期）

二期可选支持 Shadow DOM 完全隔离，适用于样式复杂的插件：

```tsx
// axons 前端 — PluginPanel 支持 Shadow DOM 模式
function PluginPanel({ plugin }) {
  const shadowRef = useRef(null);

  useEffect(() => {
    const shadow = shadowRef.current.attachShadow({ mode: 'open' });
    // 注入 axons 主题 CSS
    const style = document.createElement('style');
    style.textContent = axonsThemeCSS;  // 从 axons index.css 提取
    shadow.appendChild(style);

    // 在 shadow 内渲染插件组件
    const root = createRoot(shadow);
    root.render(<Component pluginApi={pluginApi} />);
  }, []);
}
```

> **一期不做 Shadow DOM 的原因**：Shadow DOM 内的 React 组件不共享 React 运行时（需要独立 createRoot），会增加复杂度和内存开销。CSS 变量 + class 前缀足以满足一期需求。

---

## 六、跨面板通信

### 6.1 三层通信模型

```
Layer 1: 前端 EventBus (插件↔插件, 插件↔内置面板)
  ├── 纯前端, 不经过后端
  ├── 系统事件: node:selected, file:opened, project:changed
  └── 插件事件: model:ready, dep-tracker:complete, ...

Layer 2: 共享状态 API (插件后端↔插件后端, 插件后端↔系统)
  ├── /v1/plugins/system-state  → 系统当前状态 (只读)
  ├── /v1/plugins/state/:key    → 插件间共享 KV (读写, key 自动加 pluginId 前缀隔离)
  └── 跨进程, 通过 axons 中转

Layer 3: axons API (插件后端 → 系统能力)
  ├── /v1/search, /v1/graph, /v1/stats → 读取图数据
  ├── /v1/build, /v1/watch → 触发操作
  └── 带 AXONS_PLUGIN_TOKEN 鉴权
```

### 6.2 EventBus 设计

```typescript
// ui/src/lib/pluginEventBus.ts
interface PluginEvent {
  type: string;
  source: string;    // "builtin:graph" | "plugin:com.axons.xxx"
  payload: any;
}

class PluginEventBus {
  private handlers = new Map<string, Set<EventHandler>>();
  on(type: string, handler: EventHandler): () => void;    // 返回 unsubscribe
  off(type: string, handler: EventHandler): void;
  emit(event: PluginEvent): void;
}
```

### 6.3 内置面板接入 (渐进式)

```tsx
// GraphCanvas.tsx — 加 2 行
const handleNodeClick = (nodeId: string) => {
  setSelectedNode(nodeId);   // 原有, 不变
  eventBus.emit({ type: 'node:selected', source: 'builtin:graph', payload: { nodeId } });  // 新增
};
```

### 6.4 场景对照

| 场景 | 走哪层 | 举例 |
|------|--------|------|
| 用户选节点 → 插件面板响应 | Layer 1 EventBus | node:selected → 插件刷新 |
| 插件 A 完成 → 插件 B 响应 | Layer 1 EventBus | model:ready → 分析面板 |
| 插件后端读当前选中节点 | Layer 2 system-state | GET /v1/plugins/system-state |
| 插件后端共享计算结果 | Layer 2 shared-state | PUT /v1/plugins/state/key |
| 插件后端读图数据 | Layer 3 axons API | GET /v1/search |

---

## 七、安全设计

| 层级 | 机制 |
|------|------|
| 网络隔离 | 插件进程绑定 127.0.0.1, 外部不可访问 |
| 鉴权 | AXONS_PLUGIN_TOKEN, 调 axons API 时校验 |
| 权限 | plugin.json permissions 白名单, 未声明不可调用 |
| 进程隔离 | 每插件独立进程, 崩溃不影响 axons 主进程 |
| 离线导入 | .axons-plugin.tar.gz 签名校验 (二期) |
| 前端隔离 | 建议 Shadow DOM (可选, 非强制) |

---

## 八、对存量代码的影响

| 文件 | 改动 | 影响 |
|------|------|------|
| `ActivityBar.tsx` | Settings 按钮改为 GearMenu 下拉 | 小 |
| `App.tsx` | 新增 PluginRegistryProvider + 动态 Panel 渲染 | 中 (新增代码, 不改现有) |
| `Footer.tsx` | 无改动 (Footer 不作为插件扩展点) | 无 |
| `RightPanel.tsx` | Agent 技能列表合并 plugin skills | 小 (追加数据源) |
| `useAppState.ts` | 新增 isExtensionsPanelOpen 状态 | 小 |
| `api.ts` | 新增插件 CRUD API 函数 | 小 (纯新增) |
| `config.ts` | 新增 runtimeMode 检测 + getRuntimeMode() | 小 (纯新增) |
| `server.go` | registerRoutes 新增 ~14 个路由 (含 6 条代理) | 小 (纯新增) |
| 新增 `internal/plugin/` | 全新模块 (含 proxy.go) | 无改动现有代码 |
| 新增 `ui/src/lib/pluginApi.ts` | 全新文件 (createPluginApi + resolveUrl) | 无改动现有代码 |
| 新增 `ui/src/lib/pluginEventBus.ts` | 全新文件 | 无改动现有代码 |
| 新增 `ui/src/hooks/usePluginRegistry.ts` | 全新文件 | 无改动现有代码 |

**核心原则**: 所有插件相关代码都是新增而非修改现有逻辑。混合通信方案（桌面端直连 + Web 端代理）确保两种运行环境均可正常工作。

---

## 九、分期落地计划

### 一期: 基础框架 + 本地插件

1. `manifest.json` 协议定义 + 校验器
2. `internal/plugin/` 核心模块 (Manager + Registry + Process + Handlers)
3. Extensions 面板 UI (卡片列表 + 离线导入)
4. 活动栏 GearMenu 改造
5. 前端动态组件加载 + usePluginRegistry Hook
6. EventBus 基础实现 + 内置面板 emit 接入
7. 开发 1-2 个示例插件验证完整链路

### 二期: 云端市场 + 生态

1. 云端 Marketplace API 服务
2. 插件分类 / 搜索 / 评分
3. 插件签名与安全审核
4. 插件自动更新机制
5. 开发者 CLI 工具 (axons plugin create / pack / publish)
6. 插件 UI SDK (axons-plugin-ui 主题感知组件库)

---

## 十、插件开发与调试指南

### 10.1 快速开始：5 分钟创建一个插件

以创建一个 "Hello Panel" 纯前端插件为例：

```
# 1. 创建插件目录
mkdir -p ~/.axons/plugins/com.example.hello-panel/ui

# 2. 编写 manifest.json
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

# 3. 编写前端组件 (ESM 格式)
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

# 4. (重启 axons 或通过 API 触发扫描)
curl -X POST http://127.0.0.1:9090/v1/plugins/scan
```

axons 扫描到 `manifest.json` → 注册 `frontend.panels` → ActivityBar 出现新图标 → 点击即可看到面板。

### 10.2 插件项目目录规范

推荐的插件项目结构（开发期）：

```
my-plugin/
├── manifest.json          # 必需：插件声明
├── README.md              # 推荐：插件说明
├── install.sh             # 可选：安装脚本
├── uninstall.sh           # 可选：卸载脚本
├── server.py              # 可选：后端入口 (任意语言)
├── requirements.txt       # 可选：后端依赖
├── ui/                    # 前端资源目录
│   ├── index.js           # 前端组件入口 (ESM/UMD)
│   ├── icon.svg           # 面板图标
│   └── ...                # 其他静态资源
├── skills/                # 可选：插件贡献的 skill
│   └── my-skill/
│       └── SKILL.md
└── .axons-ignore          # 可选：打包时排除的文件
```

**关键约束**：

| 项目 | 规则 |
|------|------|
| `manifest.json` | 必须存在于插件根目录，文件名固定 |
| `id` | 反向域名格式 (`com.example.xxx`)，全局唯一 |
| `frontend.entry` | 相对路径，指向 ESM 或 UMD 模块文件 |
| `backend.command` | 相对路径或系统命令，工作目录为插件根目录 |
| `ui/` 目录 | 所有前端资源放在此目录下，axons 通过 `/plugins/:id/*` 路由静态服务 |

### 10.3 前端组件开发

#### 组件接口规范

插件前端组件以 ESM 默认导出形式提供，axons 注入 `pluginApi` 对象：

```tsx
// ui/index.js — 插件前端组件
export default function MyPanel({ pluginApi }) {
  // pluginApi 属性一览:
  //   pluginApi.endpoint    — 插件后端地址 (如 "http://127.0.0.1:18080"), 无后端时为 null
  //   pluginApi.pluginId    — 当前插件 ID
  //   pluginApi.fetch(path, opts) — 桌面端直连插件后端 / Web端走 axons 代理 (自动选择, 插件无需关心)
  //   pluginApi.onEvent(type, handler)   — 订阅 EventBus 事件 (前端内存事件, 非 HTTP SSE)
  //   pluginApi.emitEvent(type, payload) — 发送 EventBus 事件
  //   pluginApi.createEventSource(path)  — 创建 SSE 连接 (桌面端直连插件后端 / Web端走 axons 代理, 同 fetch 自动选择)
  //   pluginApi.getState(key)    — 读取共享状态
  //   pluginApi.setState(key, value) — 写入共享状态

  return <div>My Plugin Panel</div>;
}
```

#### 前端技术栈选择

| 方式 | 适用场景 | 说明 |
|------|---------|------|
| **原生 JS / JSX** | 轻量面板 | 零构建步骤，直接写 ESM |
| **React 组件** | 复杂交互面板 | 推荐用 Vite 打包为 UMD/ESM，axons 前端基于 React 19 |
| **Web Component** | 框架无关 | 用 Shadow DOM 封装，样式隔离最佳 |

**推荐：React + Vite 打包**，因为 axons 前端本身是 React 应用，共享 React 运行时，包体积更小：

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
      external: ['react', 'react-dom'],  // 外部化 React，复用 axons 的实例
      output: {
        globals: { react: 'React', 'react-dom': 'ReactDOM' }
      }
    }
  }
};
```

#### pluginApi.fetch 详解

`pluginApi.fetch(path, opts)` 内部根据运行环境自动选择通信路径（插件开发者无需关心）：
- 桌面端：`fetch('http://127.0.0.1:{pluginPort}' + path)` — 直连插件后端
- Web 端：`fetch('/v1/plugins/:id/proxy' + path)` — 经 axons 代理转发到插件后端

```tsx
// 调用插件自己的后端 — 写法统一，路径无需含 host
const models = await pluginApi.fetch('/api/models').then(r => r.json());

// 调用 axons 系统 API (通过后端中转)
// 推荐方式: 插件后端调 axons API, 前端调插件后端
// 简单场景: 前端也可以直接调 axons API
const graphData = await fetch('/v1/graph').then(r => r.json());
```

#### pluginApi.createEventSource 详解

`pluginApi.createEventSource(path)` 用于创建 SSE（Server-Sent Events）连接，内部根据运行环境自动选择连接路径，与 `pluginApi.fetch` 的分支逻辑一致：

- 桌面端：`new EventSource('http://127.0.0.1:{pluginPort}' + path)` — 直连插件后端
- Web 端：`new EventSource('/v1/plugins/:id/proxy' + path)` — 经 axons 代理转发

```typescript
// lib/pluginApi.ts — createEventSource 实现
const createEventSource = (path: string): EventSource => {
  const url = resolveUrl(path);  // 复用与 fetch 相同的 URL 分支逻辑
  return new EventSource(url);
};
```

**使用示例**：

```tsx
// 下载进度等流式场景
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

> **注意**：插件开发者**不应**直接 `new EventSource(pluginApi.endpoint + path)`，因为 Web 端下 endpoint 不可达。始终使用 `pluginApi.createEventSource(path)` 确保跨环境兼容。

#### EventBus 使用示例

```tsx
export default function DepAnalyzerPanel({ pluginApi }) {
  const [nodeId, setNodeId] = useState(null);

  useEffect(() => {
    // 监听用户在 GraphCanvas 中选节点
    const unsub = pluginApi.onEvent('node:selected', (payload) => {
      setNodeId(payload.nodeId);
    });
    return unsub; // 组件卸载时自动取消订阅
  }, []);

  // 分析完成后通知其他面板
  const handleAnalysisComplete = (result) => {
    pluginApi.emitEvent('dep-analysis:complete', result);
  };

  return <div>Selected: {nodeId}</div>;
}
```

### 10.4 后端开发

#### 后端职责

插件后端是一个独立的 HTTP 服务，核心职责：

1. 暴露 `/health` 端点供 axons 健康检查
2. 实现业务 API 供前端 `pluginApi.fetch()` 调用
3. 通过 `AXONS_API_URL` + `AXONS_PLUGIN_TOKEN` 调用 axons 系统 API
4. (可选) 通过 `POST /v1/plugins/registry/sync` 上报动态状态
5. (可选) 作为 MCP Server 提供 tools 给 Agent

#### 环境变量（axons 启动插件时注入）

| 变量 | 说明 | 示例 |
|------|------|------|
| `AXONS_API_URL` | axons API 地址 | `http://127.0.0.1:9090` |
| `AXONS_PLUGIN_PORT` | 分配给插件后端的端口 | `18080` |
| `AXONS_PLUGIN_TOKEN` | 鉴权令牌 | `axons_plg_a1b2c3d4` |
| `AXONS_PLUGIN_ID` | 当前插件 ID | `com.axons.huggingface` |

#### Python 后端模板

```python
# server.py — 插件后端最小模板
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
            # 业务逻辑: 调用 axons API 获取图数据
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
            # 处理拉取逻辑...
            self.send_response(200)
            self.end_headers()
            self.wfile.write(json.dumps({"status": "pulling"}).encode())

if __name__ == "__main__":
    server = HTTPServer(("127.0.0.1", PORT), Handler)
    print(f"Plugin backend listening on 127.0.0.1:{PORT}")
    server.serve_forever()
```

> 推荐使用 FastAPI / Flask 等成熟框架，上面是最小依赖示例。

#### Go 后端模板

```go
// main.go — Go 插件后端最小模板
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

    // CORS 中间件
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
    // 调用 axons API 示例:
    // req, _ := http.NewRequest("GET", axonsAPI+"/v1/graph", nil)
    // req.Header.Set("Authorization", "Bearer "+token)
    // resp, err := http.DefaultClient.Do(req)
    // ...
    json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
}
```

### 10.5 调试方法

#### 10.5.1 开发模式：符号链接热加载

开发期无需反复导入插件包，用符号链接直接指向开发目录：

```bash
# 1. 在开发目录创建插件项目
mkdir -p ~/projects/my-plugin && cd ~/projects/my-plugin
# 编写 manifest.json, ui/index.js, server.py 等...

# 2. 符号链接到 axons 插件目录
ln -s ~/projects/my-plugin ~/.axons/plugins/com.example.my-plugin

# 3. 启动 axons — 自动扫描并加载插件
# 前端组件修改: 刷新页面即生效 (Vite HMR 或手动刷新)
# 后端修改: 需重启插件进程
#   curl -X POST http://127.0.0.1:9090/v1/plugins/com.example.my-plugin/stop
#   curl -X POST http://127.0.0.1:9090/v1/plugins/com.example.my-plugin/start
```

**优势**：修改代码 → 停止/启动插件 → 立即验证，无需打包导入。

#### 10.5.2 后端独立调试

插件后端是独立的 HTTP 进程，可以脱离 axons 单独启动和调试：

```bash
# 手动设置环境变量，独立启动后端
export AXONS_API_URL=http://127.0.0.1:9090
export AXONS_PLUGIN_PORT=18080
export AXONS_PLUGIN_TOKEN=dev-test-token
export AXONS_PLUGIN_ID=com.example.my-plugin

# 启动后端 (可以用 IDE 的 debug 模式)
python server.py
# 或
go run main.go

# 用 curl 直接测试 API
curl http://127.0.0.1:18080/health
curl http://127.0.0.1:18080/api/models

# 用 IDE 断点调试
# Python: python -m debugpy --listen 5678 server.py
# Go:     dlv debug main.go
```

**关键**：`AXONS_PLUGIN_PORT` 可以指定固定端口（而非 0），方便开发期稳定调试。

```jsonc
// 开发期 manifest.json — 指定固定端口
{
  "backend": {
    "command": ["python", "server.py"],
    "port": 18080,  // 固定端口，方便 curl/Postman 调试
    "healthCheck": "/health"
  }
}
```

#### 10.5.3 前端调试

**方法一：浏览器 DevTools**

axons 基于 Wails (WebView)，可以打开 DevTools：

1. 桌面应用：`Cmd+Option+I` (macOS) / `F12` (Windows/Linux)
2. 在 Console 中检查插件加载日志
3. 在 Sources 面板中找到 `/plugins/:id/ui/index.js`，设断点调试

**方法二：独立浏览器预览**

前端组件可以脱离 axons 在浏览器中独立开发：

```bash
# 在插件项目目录用 Vite 启动开发服务器
cd ~/projects/my-plugin/ui-src
npm run dev  # http://localhost:5173

# 开发完成后打包到 ui/index.js
npm run build
```

开发时用 Mock `pluginApi`：

```tsx
// src/dev-mock.tsx — 仅开发期使用
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

// 开发模式入口
function App() {
  return <MyPanel pluginApi={mockPluginApi} />;
}
```

**方法三：日志排查**

```tsx
// 插件组件内用 pluginApi.emitEvent 发送调试日志
// axons 前端可在 Console 中过滤 "plugin:" 前缀的事件
useEffect(() => {
  pluginApi.emitEvent('plugin:debug', {
    message: 'Component mounted',
    data: { /* ... */ }
  });
}, []);
```

#### 10.5.4 常见问题排查

| 问题 | 排查方法 |
|------|---------|
| 插件不显示在 ActivityBar | 检查 `manifest.json` 格式: `frontend.panels[0].activator === "activityBar"`, `location` 为 `left`/`right`; 查看 axons 日志中 manifest 校验错误 |
| 面板加载失败 (白屏/报错) | 打开 DevTools Console 查看 import 错误; 确认 `frontend.entry` 路径正确; 确认 JS 文件是有效的 ESM/UMD 模块 |
| 后端健康检查超时 | 手动 `curl http://127.0.0.1:PORT/health`; 检查 `healthCheck` 路径是否匹配; 检查 `readyTimeout` 是否足够; 查看后端进程日志 |
| 后端调 axons API 401 | 检查请求头 `Authorization: Bearer ${AXONS_PLUGIN_TOKEN}`; 检查 `permissions` 是否声明了所需权限 |
| EventBus 事件收不到 | 确认 `pluginApi.onEvent` 返回了 unsubscribe 函数; 确认事件名拼写; 在 DevTools Console 过滤 `plugin:` 事件 |
| 插件崩溃自动重启 | 查看 axons 日志中 `plugin.crashed` 事件; 检查后端进程 stderr 输出; 手动启动后端进程复现问题 |
| 前端组件渲染崩溃 | DevTools 查看 React ErrorBoundary 捕获的错误; 用 `try/catch` 包裹可疑代码; 临时简化组件定位问题 |
| CORS 报错 | 确认后端响应包含 `Access-Control-Allow-Origin: *` 头; 确认 OPTIONS 预检请求返回 204 |

#### 10.5.5 日志与可观测性

```
axons 日志 (stderr / 文件)
  ├── [plugin-manager] Scanning ~/.axons/plugins/...
  ├── [plugin-manager] Starting plugin com.example.my-plugin (port=18080)
  ├── [plugin-manager] Health check passed for com.example.my-plugin
  ├── [plugin-manager] Plugin com.example.my-plugin crashed (restarts=1/3)
  └── [plugin-manager] Plugin com.example.my-plugin exceeded max restarts

插件后端日志 (stdout/stderr → axons 捕获)
  ├── Plugin backend listening on 127.0.0.1:18080
  ├── GET /api/models → 200 (3ms)
  └── Error: connection refused (ollama not running)

前端 Console
  ├── [PluginRegistry] Loaded 2 panels, 1 commands from plugin com.example.my-plugin
  ├── [PluginPanel] Error loading component: SyntaxError ...
  └── [EventBus] plugin:com.example.my-plugin → model:ready
```

### 10.6 打包与分发

#### 打包为 .axons-plugin.tar.gz

```bash
# 在插件项目根目录执行
cd ~/projects/my-plugin
tar -czf my-plugin-1.0.0.axons-plugin.tar.gz \
  --exclude='.git' \
  --exclude='node_modules' \
  --exclude='.venv' \
  --exclude='*.pyc' \
  manifest.json ui/ server.py requirements.txt install.sh

# 验证包内容
tar -tzf my-plugin-1.0.0.axons-plugin.tar.gz
```

**打包要求**：

| 项目 | 规则 |
|------|------|
| `manifest.json` | 必须在包根目录 |
| `ui/` | 前端资源必须为构建后的产物 (非源码) |
| `.axons-ignore` | 类似 `.gitignore`，排除开发期文件 |
| 包大小 | 建议 < 50MB，超大包安装超时风险高 |
| 可执行文件 | 需包含对应平台的二进制，或通过 `install.sh` 在安装期构建 |

#### 安装方式

```bash
# 方式一: 通过 API 离线导入
curl -X POST http://127.0.0.1:9090/v1/plugins/import \
  -F "file=@my-plugin-1.0.0.axons-plugin.tar.gz"

# 方式二: 拖拽到 Extensions 面板的导入区域

# 方式三: 符号链接 (开发期)
ln -s ~/projects/my-plugin ~/.axons/plugins/com.example.my-plugin
```

### 10.7 开发者 CLI (二期)

二期将提供 `axons plugin` 子命令，简化开发和分发流程：

```bash
# 脚手架: 交互式创建插件项目
axons plugin create --template python-react  # 或 go-react / pure-frontend
# → 生成 manifest.json + 目录结构 + 后端模板 + 前端模板 + dev mock

# 校验: 检查 manifest.json 合法性
axons plugin validate

# 打包: 构建 .axons-plugin.tar.gz
axons plugin pack

# 发布: 上传到云端市场 (二期)
axons plugin publish

# 本地开发: 启动 axons 并热加载指定插件
axons plugin dev ./my-plugin
# → 自动创建符号链接 + 启动 axons + 监听文件变更 + 自动重启插件后端
```

---

## 十一、与 VSCode 架构对比

| | VSCode Extension Host | Axons 插件系统 |
|---|---|---|
| 运行时 | 单进程 Node.js, 所有插件共享 | 每插件独立进程, 任意语言 |
| 通信 | IPC/JSON-RPC 经 VS Code 中转 | 桌面端直连 / Web端经 axons 代理 |
| UI | 声明式 (WebviewPanel/iframe) | React 组件直挂 |
| 语言 | 仅 JS/TS | 任意 (Python/Go/Rust/...) |
| 跨插件通信 | commands.executeCommand | EventBus + 共享状态 API |
| 状态同步 | 无 | 统一注册表 + 运行时动态 sync |
| 扩展点 | 固定 (VS Code 预定义) | 可扩展 (frontend 自定义扩展类型) |

**核心理念**: VSCode 为 IDE 设计 (UI 强管控、JS 生态); Axons 为AI-Frist工作台设计 (多语言、进程隔离、面板协作), 混合通信方案兼顾桌面端性能与 Web 端兼容性。
# Exec Command Bridge 设计方案

> 版本: v1.0 | 日期: 2026-05-25 | 状态: 设计中

## 一、背景

axons 前端 [`readSystemClipboardFiles()`](../ui/src/components/FileTreePanel.tsx:56) 调用 `/api/clipboard/files` 读取系统剪贴板中的文件路径（用户从 Finder/Explorer/Dolphin 复制的文件），但后端未实现。

该功能依赖 OS 级命令行工具：
- macOS: `osascript`（预装）
- Windows: `PowerShell`（预装）
- Linux: `xclip` / `xsel` / `wl-paste`（**未预装**）

将此类 OS 特定逻辑硬编码到 axons daemon 不合理——每新增一个类似场景（git bridge、shell bridge 等）都要改 daemon 代码。

**核心思路**：插件负责声明"安装什么工具"和"怎么执行"，daemon 提供通用的 exec 引擎负责执行。exec 成为 daemon 通用能力，后续同类场景只需写 manifest.json 声明 + 脚本，零 Go 代码。

## 二、整体架构

```
┌─────────── axons daemon ───────────────────────┐
│                                                  │
│  ExecEngine (通用命令执行引擎)                    │
│  ├── ExecCommand(pluginId, commandId) → result   │
│  ├── OS 分叉: 按 runtime.GOOS 选取命令           │
│  ├── fallback 链: 主命令失败自动尝试降级命令       │
│  └── parse 策略: 内置解析器处理 stdout 格式       │
│                                                  │
│  RouteMapper (虚拟端点注册)                       │
│  ├── 插件声明 toolExec[].route                   │
│  ├── 启动时: 注册 HTTP handler → ExecEngine      │
│  └── 停止时: 注销路由                            │
│                                                  │
│  ToolInstaller (工具安装)                         │
│  ├── 插件声明 toolExec[].install                  │
│  ├── check 通过 → 跳过安装                       │
│  └── check 失败 → exec install.command           │
│                                                  │
└──────────────────────────────────────────────────┘
       ↕ manifest.json + 脚本文件
┌─────────── 插件侧 ─────────────────────────────┐
│  ~/.axons/plugins/com.axons.clipboard-bridge/    │
│  ├── manifest.json        ← 声明命令模版         │
│  ├── install.sh           ← Linux 安装 xclip     │
│  └── uninstall.sh         ← 清理                 │
└──────────────────────────────────────────────────┘
```

### 数据流

```
用户粘贴 → 前端 fetch('/api/clipboard/files')
         → daemon RouteMapper 命中虚拟端点
         → ExecEngine 读取命令模版 (manifest.toolExec)
         → 按 OS 选取命令 → exec.Command 执行
         → parse 策略解析 stdout
         → JSON 响应返回前端
```

## 三、manifest.json 扩展

### 3.1 新增 `toolExec` 字段

在 `PluginManifest` 中新增 `toolExec` 数组，声明插件提供的"工具执行命令"。每个条目定义一个可通过 HTTP 端点触发的命令。

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

  // 无后端进程！无需 backend 字段
  "backend": null,
  "frontend": null,

  // 新增：工具执行命令
  "toolExec": [
    {
      "id": "clipboard.read-files",
      "description": "Read file paths from system clipboard",

      // HTTP 端点 — daemon 自动注册虚拟路由
      "route": "GET /api/clipboard/files",
      "timeout": "3s",

      // 命令模版：按 OS 分叉
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

      // 工具安装：按 OS 分叉
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

### 3.2 字段定义

#### ToolExecDef

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | string | 是 | 命令唯一标识，插件内唯一 |
| `description` | string | 是 | 命令描述，用于日志和 UI |
| `route` | string | 是 | HTTP 路由声明，格式 `METHOD PATH` |
| `timeout` | string | 否 | 执行超时，默认 `5s` |
| `exec` | map[string]ExecDef | 是 | 按 OS 分叉的命令定义，key 为 `darwin`/`linux`/`windows` |
| `install` | map[string]InstallDef | 否 | 按 OS 分叉的安装定义 |

#### ExecDef

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `command` | []string | 是 | 执行命令，第一个元素为可执行文件 |
| `parse` | string | 是 | stdout 解析策略名 |
| `fallback` | ExecDef | 否 | 降级命令（主命令失败时尝试） |

#### InstallDef（toolExec 级别）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `check` | []string | 否 | 检查命令，退出码 0 表示已安装 |
| `command` | []string | 否 | 安装命令 |
| `timeout` | string | 否 | 安装超时，默认 `60s` |

#### 模版变量

`command` 和 `check` 数组中的字符串支持以下模版变量：

| 变量 | 替换为 | 示例 |
|------|--------|------|
| `{pluginDir}` | 插件安装目录绝对路径 | `~/.axons/plugins/com.axons.clipboard-bridge` |
| `{dataDir}` | 插件数据目录绝对路径 | `~/.axons/plugins/data/com.axons.clipboard-bridge` |

### 3.3 parse 策略

daemon 内置以下 stdout 解析策略：

| 策略名 | 用途 | 输入示例 | 解析逻辑 |
|--------|------|---------|---------|
| `line-list` | 逐行文本，每行一个路径 | `/foo/bar.ts\n/baz/qux.go\n` | 按换行分割，去空行，trim 空白 |
| `uri-list` | URI 列表（text/uri-list 格式） | `file:///foo/bar.ts\r\nfile:///baz/qux.go\r\n` | 按 `\r\n` 或 `\n` 分割，去 `file://` 前缀，URL 解码 |
| `osascript-file-url` | macOS osascript 输出 | `file:///Users/x/foo.ts\nfile:///Users/x/bar.go\n` | 同 `uri-list`，额外处理 `«class furl»` 的 POSIX path 编码 |
| `json` | JSON 输出 | `{"files":["/a.ts","/b.go"]}` | JSON unmarshal，提取顶层 `result` 字段 |
| `raw` | 原始文本，不解析 | 任意 | 直接返回 stdout 作为 `result` 字符串 |

**所有策略的输出统一为**：

```json
{
  "result": ["path1", "path2"]
}
```

`line-list` / `uri-list` / `osascript-file-url` 返回路径数组。`json` 返回解析后的数组。`raw` 返回 `["整段 stdout 文本"]`。

### 3.4 与现有 manifest 字段的关系

| 场景 | backend | frontend | toolExec | 插件类型 |
|------|---------|----------|----------|---------|
| 有后端进程 + 有面板 | ✓ | ✓ | — | 现有：全功能插件 |
| 无后端 + 有面板 | null | ✓ | — | 现有：纯前端插件 |
| 有后端 + 无面板 | ✓ | null | — | 现有：纯后端插件 |
| **无后端 + 无面板 + 有工具命令** | null | null | ✓ | **新增：tool-exec 插件** |
| 混合：有后端 + 有面板 + 有工具命令 | ✓ | ✓ | ✓ | 可选：全功能 + 工具命令 |

**tool-exec 插件**是一种新的插件形态：无进程、无面板，只声明命令模版，由 daemon 代为执行。

### 3.5 验证规则

在 `ValidateManifest` 中增加：

```
toolExec 校验:
  - id 必填，插件内唯一
  - route 必填，格式必须为 "METHOD PATH"（METHOD ∈ {GET,POST,PUT,DELETE}）
  - exec 必填，至少包含当前 OS 的条目
  - exec.{os}.command 必填，至少一个元素
  - exec.{os}.parse 必填，必须为内置策略名之一
  - install.{os}.command 存在时，check 也应存在（否则每次启动都重装）
```

`backend` 和 `frontend` 都为 null 时，`toolExec` 必须非空（否则插件无任何贡献）。

## 四、宿主侧（daemon）实现

### 4.1 目录结构

```
internal/plugin/
├── manager.go           # 新增: toolExec 启动/停止逻辑
├── manifest.go          # 新增: ToolExecDef 结构体 + 验证
├── exec_engine.go       # 新增: ExecEngine — 命令执行引擎
├── route_mapper.go      # 新增: RouteMapper — 虚拟端点注册/注销
├── parse_strategy.go    # 新增: parse 策略实现
├── handlers.go          # 现有: 不变
├── registry.go          # 现有: 不变
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
    // 1. 查找 resolved command
    // 2. exec.Command 执行
    // 3. 若退出码非 0 且有 fallback → 执行 fallback
    // 4. 根据 parse 策略解析 stdout
    // 5. 返回 ExecResult
}
```

核心执行逻辑：

```go
func (e *ExecEngine) execResolved(rc *resolvedCommand) (*ExecResult, error) {
    ctx, cancel := context.WithTimeout(context.Background(), rc.Timeout)
    defer cancel()

    cmd := exec.CommandContext(ctx, rc.Command[0], rc.Command[1:]...)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    if err := cmd.Run(); err != nil {
        // 命令失败，尝试 fallback
        if rc.Fallback != nil {
            return e.execResolved(rc.Fallback)
        }
        return nil, fmt.Errorf("exec failed: %w\nstderr: %s", err, stderr.String())
    }

    // 解析 stdout
    parsed, err := ParseOutput(rc.Parse, stdout.String())
    if err != nil {
        return nil, fmt.Errorf("parse failed: %w", err)
    }

    return &ExecResult{Result: parsed}, nil
}
```

### 4.3 Parse 策略

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
            result = append(result, u.Path)  // URL 解码自动完成
        } else {
            result = append(result, line)
        }
    }
    return result, nil
}

func parseOsascriptFileURL(stdout string) ([]string, error) {
    // macOS «class furl» 输出格式:
    //   file:///Users/x/foo.ts
    //   file:///Users/x/bar.go
    // 复用 uri-list 解析
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

RouteMapper 负责在 daemon HTTP router 上动态注册/注销虚拟端点。

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
            // 注册 handler — 所有虚拟端点指向同一个 dispatch handler
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
        // 工具未安装等场景 → 返回空结果而非错误
        json.NewEncoder(w).Encode(map[string]any{"result": []string{}})
        return
    }

    json.NewEncoder(w).Encode(result)
}
```

**路由冲突处理**：如果多个插件声明相同 route，先注册的生效（与 PanelDef ID 冲突策略一致）。

### 4.5 ToolInstaller

在插件启动阶段，检查 `toolExec[].install` 是否需要执行：

```go
// 在 StartPlugin 流程中，backend == null && toolExec != null 时的处理:

func (m *Manager) startToolExecPlugin(inst *PluginInstance) {
    manifest := inst.Manifest

    for _, te := range manifest.ToolExec {
        osKey := runtime.GOOS
        installDef, ok := te.Install[osKey]
        if !ok || installDef == nil || len(installDef.Command) == 0 {
            continue  // 当前 OS 无需安装
        }

        // check 命令：退出码 0 表示已安装
        if len(installDef.Check) > 0 {
            checkCmd := resolveTemplateVars(installDef.Check, manifest.Dir)
            cmd := exec.Command(checkCmd[0], checkCmd[1:]...)
            if cmd.Run() == nil {
                continue  // 已安装，跳过
            }
        }

        // 执行安装
        installCmd := resolveTemplateVars(installDef.Command, manifest.Dir)
        // 复用现有 InstallPlugin 的执行逻辑（stdout/stderr 流式推送）
        m.runToolInstall(inst.Manifest.ID, installCmd, installDef.Timeout)
    }
}
```

### 4.6 启动流程调整

```go
// StartPlugin 中新增 toolExec 分支:

func (m *Manager) StartPlugin(pluginID string) error {
    // ... 现有逻辑 ...

    if manifest.HasBackend() {
        // 现有流程：启动后端进程
        endpoint, err := m.startProcess(inst)
        // ...
    } else if len(manifest.ToolExec) > 0 {
        // 新增流程：tool-exec 插件
        // 1. 检查并安装工具
        m.startToolExecPlugin(inst)
        // 2. 解析命令模版（按当前 OS）
        m.execEngine.RegisterCommands(pluginID, manifest.ToolExec)
        // 3. 注册虚拟路由
        m.routeMapper.Register(pluginID, manifest.ToolExec, s.router)
        // 4. 标记运行
        inst.Status = StatusRunning
    } else {
        // 纯前端插件 — 现有流程
        inst.Status = StatusRunning
    }

    // ... 注册 frontend contributions, 更新 installed.json ...
}
```

### 4.7 停止流程调整

```go
func (m *Manager) StopPlugin(pluginID string) error {
    inst, ok := m.instances[pluginID]
    // ...

    // 注销 toolExec 路由
    if len(inst.Manifest.ToolExec) > 0 {
        m.execEngine.UnregisterCommands(pluginID)
        m.routeMapper.Unregister(pluginID, s.router)
    }

    // ... 现有停止逻辑 ...
}
```

### 4.8 manifest.go 改动

```go
// PluginManifest 新增字段
type PluginManifest struct {
    // ... 现有字段 ...
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

### 4.9 权限新增

```go
// ValidPermissions 新增
"clipboard:read": true,
"tool:exec":      true,  // 通用工具执行权限
```

## 五、插件侧实现

### 5.1 clipboard-bridge 插件目录结构

```
com.axons.clipboard-bridge/
├── manifest.json       ← 声明命令模版
├── install.sh          ← Linux 安装 xclip/xsel
└── uninstall.sh        ← Linux 清理
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

### 5.3 install.sh（Linux 安装脚本）

```bash
#!/bin/bash
# install.sh — clipboard-bridge Linux 依赖安装
set -e

echo "[clipboard-bridge] Installing clipboard tools for Linux..."

# 检测包管理器并安装 xclip
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

# 验证安装
if command -v xclip &>/dev/null; then
    echo "[clipboard-bridge] xclip installed successfully."
else
    echo "[clipboard-bridge] WARN: xclip not found after install."
    exit 1
fi
```

### 5.4 uninstall.sh（Linux 清理脚本）

```bash
#!/bin/bash
# uninstall.sh — clipboard-bridge 清理
# 注意：不卸载 xclip — 它可能是其他程序依赖的系统工具
echo "[clipboard-bridge] Uninstalled. (xclip left installed as system tool)"
```

### 5.5 不安装时的行为

插件未安装时，前端 `readSystemClipboardFiles()` 的行为：

```typescript
async function readSystemClipboardFiles(): Promise<string[]> {
  try {
    const res = await fetch('/api/clipboard/files');
    if (!res.ok) return [];  // 404 → 插件未安装 → 空数组
    const data = await res.json();
    return data.result ?? [];  // 注意：字段从 files 改为 result（统一 parse 输出）
  } catch {
    return [];
  }
}
```

| 场景 | 前端行为 |
|------|---------|
| 插件未安装 | `/api/clipboard/files` 返回 404 → `[]` → 仅内部复制/粘贴 |
| 插件已安装，工具可用 | 返回文件路径数组 → 外部复制/粘贴生效 |
| 插件已安装，工具不可用（Linux 未装 xclip） | exec 失败 → 返回 `{"result":[]}` → 优雅降级 |

## 六、前端侧改动

### 6.1 FileTreePanel.tsx

将 `readSystemClipboardFiles()` 的返回字段从 `data.files` 改为 `data.result`：

```typescript
async function readSystemClipboardFiles(): Promise<string[]> {
  try {
    const res = await fetch('/api/clipboard/files');
    if (!res.ok) return [];
    const data = await res.json();
    return data.result ?? [];  // 统一为 result（ExecEngine 输出格式）
  } catch {
    return [];
  }
}
```

其他代码不变——`handlePaste` 的 Priority 1 逻辑完全复用。

## 七、后续扩展示例

exec 成为通用能力后，类似场景只需写 manifest.json + 脚本：

### 7.1 git-bridge（读取 git 仓库信息）

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

### 7.2 shell-bridge（执行系统命令）

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

> 注：`{cmd}` 等 request-body 参数注入需要 daemon 在 exec 前做参数替换，属于二期增强。

## 八、落地步骤

| 步骤 | 内容 | 涉及模块 |
|------|------|---------|
| 1 | `manifest.go` 增加 `ToolExecDef` / `ExecDef` / `ToolInstallDef` 结构体 + 验证 | `internal/plugin/manifest.go` |
| 2 | 实现 `parse_strategy.go` — 5 个内置 parse 策略 | `internal/plugin/parse_strategy.go` |
| 3 | 实现 `exec_engine.go` — 命令执行 + fallback 链 | `internal/plugin/exec_engine.go` |
| 4 | 实现 `route_mapper.go` — 虚拟端点注册/注销 | `internal/plugin/route_mapper.go` |
| 5 | `manager.go` StartPlugin/StopPlugin 增加 toolExec 分支 | `internal/plugin/manager.go` |
| 6 | 前端 `readSystemClipboardFiles()` 返回字段改为 `data.result` | `ui/src/components/FileTreePanel.tsx` |
| 7 | 开发 clipboard-bridge 插件（manifest.json + install.sh） | 独立目录 |
| 8 | 打包、测试 | — |

## 九、风险评估

| 风险 | 缓解措施 |
|------|---------|
| 命令注入（恶意插件声明危险命令） | 权限声明 `tool:exec`，一期 warn 日志，二期运行时拦截 |
| 路由冲突（两个插件声明相同 route） | 先注册优先 + warn 日志 |
| exec 阻塞（命令长时间不返回） | timeout 字段，默认 5s，context.WithTimeout 强制取消 |
| install.sh 需要 sudo | 提示用户手动安装；install 脚本 exit 1 不会阻塞插件启动 |
| httprouter 不支持动态路由注销 | 使用 ServeHTTP 拦截 + 内部 map 分发，绕过 httprouter 限制 |
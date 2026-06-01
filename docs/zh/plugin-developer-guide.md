# Axons 插件开发者手册

> 版本: v1.0 | 适用于 Axons ≥ 0.8.0

本手册帮助开发者从零构建一个 Axons 插件，覆盖目录结构、manifest 协议、前端构建、后端开发、打包发布全流程。

**[English](../plugin-developer-guide.md) | 简体中文**

> 官方扩展包仓库：[axons-extension-packages](https://github.com/mengshi02/axons-extension-packages) — 包含语言包、模型管理等完整示例，可直接参考或贡献插件。

---

## 目录

- [1. 快速开始](#1-快速开始)
- [2. 目录结构](#2-目录结构)
- [3. manifest.json 协议](#3-manifestjson-协议)
- [4. 前端开发](#4-前端开发)
  - [4.1 项目初始化](#41-项目初始化)
  - [4.2 vite.config.js 配置（重要）](#42-viteconfigjs-配置重要)
  - [4.3 组件协议](#43-组件协议)
  - [4.4 使用 axons-plugin-ui 组件库](#44-使用-axons-plugin-ui-组件库)
  - [4.5 样式与主题](#45-样式与主题)
  - [4.6 使用 pluginApi](#46-使用-pluginapi)
- [5. 后端开发](#5-后端开发)
  - [5.1 环境变量](#51-环境变量)
  - [5.2 健康检查](#52-健康检查)
  - [5.3 CORS（桌面端必需）](#53-cors桌面端必需)
  - [5.4 调用 Axons API](#54-调用-axons-api)
- [6. 安装脚本](#6-安装脚本)
- [7. 打包与发布](#7-打包与发布)
- [8. 调试技巧](#8-调试技巧)
- [9. 常见问题](#9-常见问题)

---

## 1. 快速开始

一个最简的"纯前端"插件只需 3 个文件：

```
com.example.hello/
├── manifest.json
├── ui/
│   └── index.js
└── ui/
    └── icon.svg
```

**manifest.json：**

```json
{
  "id": "com.example.hello",
  "name": "Hello World",
  "version": "1.0.0",
  "description": "我的第一个 Axons 插件",
  "author": "example",
  "icon": "ui/icon.svg",
  "category": "productivity",
  "minAxonsVersion": "0.8.0",
  "permissions": ["panel:create"],
  "frontend": {
    "entry": "ui/index.js",
    "panels": [{
      "id": "hello",
      "title": "Hello",
      "icon": "ui/icon.svg",
      "location": "right",
      "activator": "activityBar"
    }]
  },
  "activationEvents": ["onStartup"]
}
```

**ui/index.js：**

```js
export default function HelloPanel({ pluginApi, onClose, panelId }) {
  const el = document.createElement('div');
  el.innerHTML = '<h2>Hello Axons!</h2>';
  return el;  // 注意：实际 React 组件写法见第 4 节
}
```

但推荐使用 React + Vite 开发，详见下文。

---

## 2. 目录结构

推荐的完整插件目录：

```
com.example.my-plugin/
├── manifest.json          # 插件清单（必需）
├── install.sh             # 安装脚本（有后端依赖时必需）
├── uninstall.sh           # 卸载脚本（可选）
├── requirements.txt       # Python 依赖（Python 后端时）
├── server.py              # 后端服务（可选）
├── .venv/                 # Python 虚拟环境（install.sh 创建）
├── src/                   # 前端源码
│   ├── index.tsx          # 入口组件
│   └── types.ts           # 类型定义
├── ui/                    # 前端构建产物 + 静态资源
│   ├── index.js           # 构建产物（由 Vite 生成）
│   └── icon.svg           # 面板图标
├── package.json           # 前端依赖
├── tsconfig.json          # TypeScript 配置
├── vite.config.js         # Vite 构建配置
└── .axons-ignore          # 打包排除文件
```

### .axons-ignore

打包时排除不需要的文件：

```
node_modules/
.venv/
src/
.git/
*.tar.gz
package-lock.json
tsconfig.json
vite.config.js
```

---

## 3. manifest.json 协议

### 完整字段

```jsonc
{
  // ─── 基础信息 ───
  "id": "com.example.my-plugin",    // 反向域名，全局唯一
  "name": "My Plugin",              // 显示名
  "version": "1.0.0",              // 语义化版本
  "description": "插件描述",
  "author": "author-name",
  "icon": "ui/icon.svg",           // 相对路径
  "category": "productivity",      // analysis | visualization | search | productivity
  "minAxonsVersion": "0.8.0",     // 最低兼容版本

  // ─── 权限声明 ───
  "permissions": [
    "project:read",      // 读取项目信息
    "graph:read",        // 读取代码图数据
    "model:register",    // 注册/注销 LLM 模型
    "panel:create",      // 创建 UI 面板
    "state:read",        // 读取共享状态
    "state:write"        // 写入共享状态
  ],

  // ─── 后端（可选） ───
  "backend": {
    "command": [".venv/bin/python", "server.py"],  // 启动命令
    "port": 0,                    // 0 = OS 动态分配，或指定固定端口
    "healthCheck": "/health",     // 健康检查路径
    "readyTimeout": "15s",        // 就绪超时
    "install": {
      "command": ["bash", "install.sh"],
      "timeout": "300s"
    },
    "uninstall": {
      "command": ["bash", "uninstall.sh"]
    }
  },

  // ─── 前端（可选） ───
  "frontend": {
    "entry": "ui/index.js",       // ES Module 入口
    "panels": [{
      "id": "my-panel",           // 面板 ID（插件内唯一）
      "title": "My Panel",        // 面板标题
      "icon": "ui/icon.svg",      // 面板图标
      "location": "right",        // right | left | center-bottom | modal
      "activator": "activityBar"  // activityBar | footer | node-select | gearMenu | command
    }],
    "commands": [{
      "id": "my-plugin.open",
      "title": "Open My Plugin",
      "shortcut": "Ctrl+Shift+P"
    }]
  },

  // ─── 激活事件 ───
  "activationEvents": ["onStartup"]
}
```

### 四种插件形式

| 形式 | 后端 | 前端 | 适用场景 |
|------|------|------|---------|
| 前端+后端 | ✓ | ✓ | 面板 + 自有 API 服务 |
| 纯前端 | ✗ | ✓ | 只调 axons API 的面板 |
| 纯后端 | ✓ | ✗ | MCP 工具集 |
| 前端+CLI | ✗ | ✓ | 前端直接调 axons API |

---

## 4. 前端开发

### 4.1 项目初始化

```bash
mkdir com.example.my-plugin && cd com.example.my-plugin
npm init -y
npm install react react-dom
npm install -D vite @vitejs/plugin-react typescript @types/react @types/react-dom
```

### 4.2 vite.config.js 配置（重要）

这是最容易踩坑的地方。插件作为 ES Module 被 Axons 动态加载，**必须正确配置外部化和环境变量**：

```js
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],

  // ⚠️ 必须定义，否则浏览器报 "process is not defined"
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },

  build: {
    lib: {
      entry: 'src/index.tsx',
      formats: ['es'],          // 必须是 ES Module
      fileName: () => 'index.js',
    },
    rollupOptions: {
      // ⚠️ 外部化：React 和 axons-plugin-ui 由 Axons 运行时提供
      // 不要打包进插件产物，否则会导致 React 多实例（hooks 失效）
      external: ['react', 'react-dom', 'axons-plugin-ui'],
      output: {
        globals: {
          react: 'React',
          'react-dom': 'ReactDOM',
          'axons-plugin-ui': 'AxonsPluginUI',
        },
      },
    },
    outDir: 'ui',
    emptyOutDir: false,   // 保留 ui/icon.svg 等静态文件
  },
});
```

**两个关键点：**

1. **`define: { 'process.env.NODE_ENV': ... }`** — 浏览器没有 `process` 全局变量，不加这行会报 `process is not defined`。Vite 在构建时会把 `process.env.NODE_ENV` 替换为字符串常量，开发模式的代码也会被 tree-shake 掉。

2. **`external: ['react', ...]`** — React 等由 Axons 宿主通过 import map 提供，插件不打包这些依赖。如果打包进去，会出现 React 多实例问题，导致 hooks 报错。

### 4.3 组件协议

插件入口组件必须遵循 `PluginPanelProps` 接口：

```tsx
// src/index.tsx
import React from 'react';

interface PluginPanelProps {
  pluginApi: import('../lib/pluginApi').PluginApi;
  onClose: () => void;
  panelId: string;
}

export default function MyPanel({ pluginApi, onClose, panelId }: PluginPanelProps) {
  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 你的面板内容 */}
    </div>
  );
}
```

- **`pluginApi`** — 与 Axons 平台通信的唯一入口，详见 4.6 节
- **`onClose`** — 关闭当前面板
- **`panelId`** — 当前面板 ID

> 注意：面板已被 Axons 包裹在 resizable 容器中（默认 384px 宽，可拖拽调整），你的组件只需关注内容区域。

### 4.4 使用 axons-plugin-ui 组件库

Axons 提供了一套 UI 组件库，插件可直接使用，风格与主界面一致：

```tsx
import React, { useState } from 'react';
import { Button, Card, CardHeader, CardBody, Badge, Spinner, Tabs } from 'axons-plugin-ui';

export default function MyPanel({ pluginApi }) {
  const [activeTab, setActiveTab] = useState('tab1');

  return (
    <div style={{ height: '100%', padding: '12px' }}>
      <Tabs
        tabs={[{ id: 'tab1', label: '标签一' }, { id: 'tab2', label: '标签二' }]}
        activeTab={activeTab}
        onChange={setActiveTab}
      />
      <Card>
        <CardHeader>标题</CardHeader>
        <CardBody>
          <Badge variant="success">就绪</Badge>
          <Button variant="primary" onClick={() => {}}>操作</Button>
        </CardBody>
      </Card>
    </div>
  );
}
```

**可用组件：**

| 组件 | 说明 |
|------|------|
| `Button` | 按钮，variant: primary/secondary/ghost，size: default/sm |
| `Card` / `CardHeader` / `CardBody` | 卡片容器 |
| `Input` | 输入框 |
| `Select` | 下拉选择 |
| `Textarea` | 多行文本框 |
| `Badge` | 徽章，variant: default/success/warning/error/info |
| `Divider` | 水平分割线，spacing: default/lg |
| `EmptyState` | 空状态占位（图标 + 标题 + 描述） |
| `Spinner` | 加载动画 |
| `ProgressBar` | 进度条，value 0-1 |
| `List` / `ListItem` | 列表，支持图标、激活态、点击 |
| `Tabs` | 标签页切换（含键盘导航） |
| `Modal` | 模态弹窗（含焦点陷阱） |
| `ConfirmDialog` | 确认对话框 |

**TypeScript 支持：** Axons 在 `/plugin-sdk/axons-plugin-ui.d.ts` 提供类型声明。该文件**仅用于编译阶段**——不会进入插件打包产物。宿主是唯一的维护者，插件开发者无需编辑它。

开发阶段引用方式：

- **在 axons 仓库内：** 在 `tsconfig.json` 中添加 `paths`：
  ```jsonc
  {
    "compilerOptions": {
      "paths": {
        "axons-plugin-ui": ["../axons/internal/api/static/dist/plugin-sdk/axons-plugin-ui"]
      }
    }
  }
  ```
- **不在 axons 仓库内：** 从 axons 仓库手动复制类型声明文件到插件项目中（宿主更新组件后重新复制）：
  ```bash
  cp <axons-repo>/internal/api/static/dist/plugin-sdk/axons-plugin-ui.d.ts src/axons-plugin-ui.d.ts
  ```

运行时，`axons-plugin-ui` 由宿主 iframe 提供（UMD 全局对象 + ESM shim）——插件的 `index.js` 不包含它。

### 4.5 样式与主题

Axons 通过 CSS 变量提供主题支持，插件应使用变量而非硬编码色值：

```css
/* 在组件的 style 中使用 CSS 变量 */
background: var(--axons-color-surface);
color: var(--axons-text-primary);
border: 1px solid var(--axons-border-subtle);
font-family: var(--axons-font-sans);
```

**常用变量：**

| 变量 | 用途 |
|------|------|
| `--axons-color-void` | 最深背景 |
| `--axons-color-deep` | 次深背景 |
| `--axons-color-surface` | 面板背景 |
| `--axons-color-elevated` | 浮层背景 |
| `--axons-color-hover` | 悬停背景 |
| `--axons-border-subtle` | 细边框 |
| `--axons-border-default` | 默认边框 |
| `--axons-text-primary` | 主文字 |
| `--axons-text-secondary` | 次文字 |
| `--axons-text-muted` | 弱文字 |
| `--axons-accent` | 主题强调色 |
| `--axons-success` / `warning` / `error` / `info` | 状态色 |
| `--axons-font-sans` | 无衬线字体 |
| `--axons-font-mono` | 等宽字体 |

### 4.6 使用 pluginApi

`pluginApi` 是插件与 Axons 平台通信的唯一入口，自动处理桌面端/Web端差异：

#### 请求插件后端 API

```tsx
// 自动选择：桌面端直连插件后端 / Web端走 axons 代理
const resp = await pluginApi.fetch('/api/models');
const data = await resp.json();
```

#### SSE 实时推送

```tsx
const es = pluginApi.createEventSource('/api/events');
es.onmessage = (e) => {
  const data = JSON.parse(e.data);
  // 处理推送数据
};
// 清理
es.close();
```

#### EventBus 跨面板通信

```tsx
// 订阅事件（返回取消函数）
const unsubscribe = pluginApi.onEvent('node:selected', (payload) => {
  console.log('选中节点:', payload);
});

// 广播事件
pluginApi.emitEvent('model:downloaded', { name: 'llama3' });

// 组件卸载时取消订阅
unsubscribe();
```

#### 共享状态

```tsx
// 写入状态（按 pluginId 命名空间隔离）
await pluginApi.setState('lastModel', { name: 'llama3', size: '4.7G' });

// 读取状态
const lastModel = await pluginApi.getState('lastModel');
```

---

## 5. 后端开发

### 5.1 环境变量

Axons 启动插件后端进程时注入以下环境变量：

| 变量 | 说明 |
|------|------|
| `AXONS_API_URL` | Axons API 地址，如 `http://127.0.0.1:8080` |
| `AXONS_PLUGIN_PORT` | 插件应绑定的端口（`manifest.json` 中 `port: 0` 时由 OS 分配） |
| `AXONS_PLUGIN_TOKEN` | 插件鉴权 token |

Python 后端示例：

```python
import os

API_URL = os.environ.get('AXONS_API_URL', 'http://127.0.0.1:8080')
PORT = int(os.environ.get('AXONS_PLUGIN_PORT', '0'))
TOKEN = os.environ.get('AXONS_PLUGIN_TOKEN', '')
```

### 5.2 健康检查

`manifest.json` 中声明的 `healthCheck` 端点必须返回 200：

```python
# server.py (FastAPI 示例)
from fastapi import FastAPI

app = FastAPI()

@app.get("/health")
async def health():
    return {"status": "ok"}
```

### 5.3 CORS（桌面端必需）

桌面端前端直连插件后端，**必须**返回 CORS 头：

```python
# FastAPI — 全局 CORS 中间件
from fastapi.middleware.cors import CORSMiddleware

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)
```

Web 端走 Axons 代理，无需 CORS，但加上也不影响。

### 5.4 调用 Axons API

后端使用 `AXONS_API_URL` 直接调 Axons API（同机 HTTP，无跨域）：

```python
import requests

API_URL = os.environ.get('AXONS_API_URL', 'http://127.0.0.1:8080')

# 获取代码图
resp = requests.get(f"{API_URL}/v1/graph/{project_id}")
graph = resp.json()

# 语义搜索
resp = requests.post(f"{API_URL}/v1/search", json={
    "query": "authentication logic",
    "project_id": project_id
})
```

---

## 6. 安装脚本

`install.sh` 在用户导入插件后执行一次，用于安装后端依赖：

```bash
#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# 1. 检查运行环境
if ! command -v python3 &>/dev/null; then
    echo "错误: 未找到 python3"
    exit 1
fi

# 2. 创建虚拟环境并安装依赖
python3 -m venv "$SCRIPT_DIR/.venv"
source "$SCRIPT_DIR/.venv/bin/activate"
pip install --quiet -r "$SCRIPT_DIR/requirements.txt"

echo "安装完成"
```

**注意事项：**
- Python 插件务必使用 `.venv`，`manifest.json` 的 `backend.command` 指向 `.venv/bin/python`
- 脚本必须以 exit code 0 表示成功，非 0 表示失败
- 避免使用 `sudo`，不要修改系统级配置

---

## 7. 打包与发布

### 打包

```bash
# 1. 构建前端
npx vite build

# 2. 打包为 .tar.gz（文件名格式：{id}-{version}.axons-plugin.tar.gz）
cd ..
tar -czf com.example.my-plugin-1.0.0.axons-plugin.tar.gz \
  -C com.example.my-plugin \
  --exclude-from=com.example.my-plugin/.axons-ignore \
  .
```

### 导入

用户在 Axons 的 Extensions 面板中上传 `.axons-plugin.tar.gz` 文件，或通过 API：

```bash
curl -X POST http://localhost:8080/v1/plugins/import \
  -F "file=@com.example.my-plugin-1.0.0.axons-plugin.tar.gz"
```

---

## 8. 调试技巧

### 前端调试

1. **浏览器 DevTools** — 在 Axons Web 端打开 DevTools，Sources 面板可以找到插件代码
2. **console.log** — 插件的 `console.log` 输出到浏览器控制台
3. **Vite 开发服务器** — 可以单独运行 `npx vite dev` 开发组件，但注意 `pluginApi` 在独立模式下不可用

### 后端调试

1. **手动启动** — 设置环境变量后直接运行后端：
   ```bash
   export AXONS_API_URL=http://127.0.0.1:8080
   export AXONS_PLUGIN_PORT=18080
   export AXONS_PLUGIN_TOKEN=test
   .venv/bin/python server.py
   ```
2. **curl 测试** — 直接请求插件后端 API
3. **Axons 日志** — 查看插件的 stdout/stderr 输出

### 常用调试 API

```bash
# 查看已安装插件
curl http://localhost:8080/v1/plugins

# 查看插件面板注册表
curl http://localhost:8080/v1/plugins/registry/panels

# 读取共享状态
curl http://localhost:8080/v1/plugins/state/com.example.my-plugin:lastModel

# 写入共享状态
curl -X PUT http://localhost:8080/v1/plugins/state/com.example.my-plugin:key \
  -H "Content-Type: application/json" \
  -d '{"value": "test"}'
```

---

## 9. 常见问题

### Q: 插件加载报 "Failed to resolve module specifier 'react'"

**原因：** 浏览器无法解析裸模块 specifier（如 `import from "react"`），Axons 通过 import map 解决此问题。确保你的 `vite.config.js` 中 `external` 配置了 `react`、`react-dom` 和 `axons-plugin-ui`。

### Q: 插件加载报 "process is not defined"

**原因：** 构建产物中残留了 `process.env.NODE_ENV` 引用，浏览器没有 `process` 全局变量。

**解决：** 在 `vite.config.js` 中添加：
```js
define: {
  'process.env.NODE_ENV': JSON.stringify('production'),
}
```

### Q: React hooks 报错 "Invalid hook call"

**原因：** 插件打包了 React，导致与 Axons 宿主的 React 形成两个实例。

**解决：** 确保 `vite.config.js` 中 `external: ['react', 'react-dom']`，不要将 React 打包进插件产物。

### Q: 桌面端请求插件后端报 CORS 错误

**解决：** 插件后端必须添加 CORS 头，允许跨域请求。FastAPI 示例见 5.3 节。

### Q: 插件后端启动失败

排查步骤：
1. 手动设置环境变量后运行 `backend.command`
2. 检查端口是否被占用
3. 检查 `healthCheck` 端点是否在 `readyTimeout` 内就绪
4. 查看 Axons 日志中的插件 stdout/stderr

### Q: 修改插件代码后如何生效？

1. 重新构建前端：`npx vite build`
2. 重新打包插件：`tar -czf ...`
3. 在 Axons Extensions 面板中卸载旧插件，导入新包
4. 或者通过 API：先 `DELETE /v1/plugins/{id}` 再 `POST /v1/plugins/import`

### Q: 如何让面板支持不同位置？

在 `manifest.json` 的 `panels` 中设置 `location`：

| location | 说明 | 面板宽度 |
|----------|------|---------|
| `right` | 右侧面板（默认） | 384px，可拖拽调整 |
| `left` | 左侧面板 | 跟随左侧面板容器 |
| `center-bottom` | 底部面板 | 全宽 |
| `modal` | 弹窗面板 | 居中弹窗 |

---

## 附录：完整示例参考

### 官方扩展包仓库

完整的示例代码和开箱即用的插件模板，请参考官方扩展包仓库：[axons-extension-packages](https://github.com/mengshi02/axons-extension-packages)

| 插件 | 类型 | 说明 |
|---|---|---|
| [`com.axons.locale-zh-cn`](https://github.com/mengshi02/axons-extension-packages/tree/main/language/com.axons.locale-zh-cn) | 纯静态 | 简体中文语言包 |
| [`com.axons.huggingface`](https://github.com/mengshi02/axons-extension-packages/tree/main/huggingface/com.axons.huggingface) | 全栈（前端+后端） | HuggingFace GGUF 模型浏览与本地 LLM 管理 |

### 更多开发文档

- [插件创作指南](https://github.com/mengshi02/axons-extension-packages/blob/main/docs/PLUGIN_AUTHORING_zh-CN.md) — 三步从零创建插件
- [开发手册](https://github.com/mengshi02/axons-extension-packages/blob/main/docs/DEVELOPMENT_zh-CN.md) — monorepo 开发、校验与维护
- [发布流程](https://github.com/mengshi02/axons-extension-packages/blob/main/docs/RELEASING.md) — 版本发布工作流
# 插件面板样式/滚动问题与宿主侧修改建议

> 编写日期：2026-05-18
> 关联插件：`chat.axons.local-models` (本地模型管理插件)
> 关联宿主：`/Users/mengshi3/go/src/github.com/mengshi02/axons`
> 文档用途：交付给宿主 (`axons`) 维护者作为改造依据；同时记录插件侧自查与修复方案

---

## 一、问题现象

在加载 `chat.axons.local-models` 插件面板时，观察到两个直接影响可用性的问题：

1. **面板风格与宿主主界面不一致**
   - 插件面板的背景、文字色、边框、字体与宿主的 moon-theme（深色）外观对不上
   - 看起来像浏览器默认外观（透明背景、黑色正文、无边框分隔）
2. **模型列表无法向下滚动**
   - HuggingFace / 本地模型列表条目超过可视高度后被裁剪
   - 鼠标滚轮、拖拽滚动条都不生效，无法看到下方内容

两个问题在不同分辨率、不同 tab（本地 / HuggingFace）下均能稳定复现。

---

## 二、背景：宿主 ↔ 插件的 UI 隔离模型

宿主把每个插件 UI 渲染在独立的 `<iframe sandbox>` 内（参见
`/Users/mengshi3/go/src/github.com/mengshi02/axons/ui/src/components/IframePluginPanel.tsx`），
通过下面这条 HTML 模板生成 iframe 内文档（位于
`/Users/mengshi3/go/src/github.com/mengshi02/axons/internal/plugin/proxy.go` 第 147-200 行）：

```html
<!DOCTYPE html>
<html lang="en" class="{{.ThemeClass}}">
<head>
  <meta charset="UTF-8" />
  <link rel="stylesheet" href="/plugin-sdk/theme.css" />
  <link rel="stylesheet" href="/plugin-sdk/components.css" />
  <style>
    body { margin: 0; padding: 0; overflow: hidden;
           background: var(--axons-color-surface, #101018); }
  </style>
</head>
<body>
  <div id="root"></div>
  <!-- ... runtime + plugin bootstrap ... -->
</body>
</html>
```

其中：

- `theme.css` 定义了所有 `--axons-*` CSS 变量（颜色、字体、阴影、圆角等），
  挂在 `:root.moon-theme` / `:root.sun-theme` 类上
- `components.css` 提供 `axons-btn`、`axons-card`、`axons-tabs` 等组件类
- 服务端渲染时默认写入 `<html class="moon-theme">`，主题切换通过
  `postMessage` 异步通知 iframe 内适配器更新 class

宿主 UI 通过 `IframePluginPanel` 将 iframe 设为 `w-full h-full border-0`，
也就是 **iframe 本身被父容器拉满**，剩下的高度链由 iframe 内部接管。

---

## 三、问题根因分析

### 问题 1：风格不一致

#### 已确认的事实

- `theme.css` 变量定义完整，且 `:root.moon-theme` 与 `:root.sun-theme`
  都给出了显式 hex 值
- iframe 模板写死 `<html class="moon-theme">`，理论上变量可被解析
- `components.css` 与 `theme.css` 的静态文件路径在嵌入产物
  `internal/api/static/dist/plugin-sdk/` 中确实存在

#### 真正的风险点

1. **fallback `:root` 块在 iframe 中失效**
   `theme.css` 第 69-106 行的 fallback `:root` 块大量使用
   `var(--color-*)` 引用 Tailwind v4 变量。iframe 内**没有加载 Tailwind**，
   所以一旦 `<html>` 上的 `moon-theme` 类因为某种原因丢失（例如未来
   有人重构模板、或 SSR 写入失败），iframe 内的变量会全部回落到 fallback
   表达式 → 命中"引用 Tailwind 变量"的链路 → 仍然解析失败 → 文字色、边框、字体全部为空。
2. **iframe 内未规定默认 `color` 与 `font-family`**
   iframe 模板内联样式只设了 `background`，没有 `color` / `font-family`。
   插件每个 `<div>` 都要自己写 `color: var(--axons-text-primary)` 才能拿到正确颜色，
   一旦遗漏就退化为浏览器默认黑字 + 默认 serif/sans-serif，与宿主明显不同。
3. **嵌入产物可能未与源码同步**
   宿主前端是 Vite 构建后嵌入 Go 二进制（`internal/api/static/dist/`），
   如果开发者修改了 `theme.css` 但忘记 `npm run build`，旧版样式会继续被服务。

> 排查命令（用户侧自检）：在插件面板的 iframe 上右键 → 检查元素，
> Network 看 `/plugin-sdk/theme.css` 是否 200 且内容含 `:root.moon-theme`；
> Elements 看 `<html>` 是否有 `moon-theme` 类；
> Computed 面板看 `--axons-text-primary` 是否解析为 `#e4e4ed`。

### 问题 2：列表无法下滑

#### 关键观察

iframe 模板里 body 的样式仅有：
```css
body { margin: 0; padding: 0; overflow: hidden; background: ...; }
```

**没有给 `html` / `body` / `#root` 设置 `height: 100%`**。

#### 因果链

1. iframe 标签被宿主拉满 → iframe 内部 viewport 高度确定
2. **但 iframe 内的 `<html>`、`<body>`、`<div id="root">` 高度都是 `auto`** —— 链路上没有任何一级被赋予确定高度
3. 插件根组件 `ModelManagerPanel` 最外层写的是 `height: 100%`
   → 父级是 auto → 自己也塌成 `auto` → 按内容撑开
4. 内部用 `display:flex; flexDirection:column` + 子级 `flex:1; overflowY:auto`
   做滚动容器 → 父链没有确定高度，`flex:1` 退化为内容高度
5. 滚动容器自身高度 ≡ 内容高度 → `overflow-y:auto` 永远不触发
6. 与此同时 body 上的 `overflow:hidden` 又把超出 viewport 的部分裁掉
   → 结果就是 **内容被裁但滚不动**

这是 flex 布局 + 可滚动子项的经典陷阱，唯一可靠的解法是
**让 html/body/#root 三层都拥有确定高度**。

---

## 四、修改建议

### 4.1 宿主侧必改 ✅

#### 改动 A：iframe HTML 模板补全 height/color 链路

**文件**：`/Users/mengshi3/go/src/github.com/mengshi02/axons/internal/plugin/proxy.go`
**位置**：`iframeHostTemplate` 常量内的 `<style>` 块（约第 155-157 行）

**当前内容**：
```html
<style>
  body { margin: 0; padding: 0; overflow: hidden;
         background: var(--axons-color-surface, #101018); }
</style>
```

**建议改为**：
```html
<style>
  html, body, #root { height: 100%; }
  body {
    margin: 0; padding: 0; overflow: hidden;
    background: var(--axons-color-surface, #101018);
    color: var(--axons-text-primary, #e4e4ed);
    font-family: var(--axons-font-sans, 'Inter', system-ui, sans-serif);
    font-size: 13px;
    line-height: 1.5;
  }
</style>
```

**作用**：

- `html, body, #root { height: 100% }` —— 修复"列表无法下滑"。让插件
  使用 `height: 100%` + flex 布局时，整条父链都有明确高度，
  `flex:1 + overflow-y:auto` 能正常生效。
- `color` / `font-family` —— 修复"风格不一致"。即便插件作者忘记
  在内层节点指定文字色，body 默认色也会跟着 moon/sun 主题走，
  不会退化为浏览器黑字 + serif 字体。

**注意**：改完需要 `cd ui && npm run build`，确保
`internal/api/static/dist/plugin-sdk/` 的嵌入产物同步更新；否则 Go
二进制里仍是旧 HTML 模板（注意：模板是 Go 源码常量、不是静态资源，
所以严格说**重编 Go 二进制就够**，无需重打前端。这里提醒前端是因为
改动 B 涉及静态资源）。

#### 改动 B（可选加固）：theme.css fallback 块去 Tailwind 依赖

**文件**：`/Users/mengshi3/go/src/github.com/mengshi02/axons/ui/src/plugin-sdk/theme.css`
**位置**：fallback `:root` 块（约第 69-106 行）

**当前问题**：fallback 块依赖 `var(--color-*)` (Tailwind v4 变量)，
而 iframe 内不加载 Tailwind，一旦 `moon-theme` / `sun-theme` 类没挂上，
所有变量全部失效。

**建议**：把 fallback `:root` 块的内容直接复制成 `:root.moon-theme`
等价的硬编码值（不再引用 `--color-*`），让"没有任何主题类"也能
退化到 moon-theme 的颜色，而不是退化到空字符串。

示意（节选）：
```css
:root {
  --axons-color-surface: #101018;     /* 不再 var(--color-surface, ...) */
  --axons-text-primary: #e4e4ed;
  --axons-border-subtle: #1e1e2a;
  --axons-accent: #7c3aed;
  --axons-font-sans: 'Inter', system-ui, sans-serif;
  /* ... 其余复制 :root.moon-theme 的值 ... */
}
```

**作用**：增强 iframe 内的鲁棒性 —— 即便未来某次重构忘了写
`ThemeClass`、或 `iframe-adapter.ts` 的 `plugin:init` 没及时到达，
插件视觉也能稳定落在 moon-theme，不出现"白屏 + 黑字"的退化态。

改完需要重新 `npm run build`，让 `internal/api/static/dist/plugin-sdk/theme.css`
跟着更新（这一项不重新构建是无效的）。

#### 改动 C（可选加固）：服务端按宿主当前主题写 ThemeClass

**文件**：`/Users/mengshi3/go/src/github.com/mengshi02/axons/internal/plugin/proxy.go`
**位置**：`HandlePluginIframeHost` 函数中初始化 `themeClass` 处（约第 252 行）

**现状**：写死 `themeClass := "moon-theme"`，所以当用户在宿主切到 sun-theme
打开插件面板时，会先看到一帧 moon-theme，再切到 sun-theme（闪烁）。

**建议**：从宿主全局状态读取当前主题（如果有 daemon 侧的设置 store），
SSR 时直接写正确的 class，消除首屏闪烁。如果当前架构里 daemon 不持有
该状态，可由 `IframePluginPanel.tsx` 在 iframe `onLoad` 之前通过
查询字符串 `?theme=sun` 传给 iframe-host 路由。

---

### 4.2 插件侧必改 ✅（不依赖宿主修不修，都该做）

#### 改动 D：滚动容器加 `minHeight: 0`

**文件**：`local-models/chat.axons.local-models/src/ModelManagerPanel.tsx`
**位置**：内容区滚动容器（第 54 行附近）

**当前内容**：
```jsx
<div style={{ flex: 1, overflowY: 'auto' }}>
```

**改为**：
```jsx
<div style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
```

**原因**：flex 子元素默认 `min-height: auto`，会被内容撑破，
导致 `overflow:auto` 失效。这是浏览器规范行为，与宿主是否修
height 链路无关，**任何 flex 列内的滚动子项都应该加 `min-height:0`**。

#### 改动 E：列表组件改为内部双段 flex

**文件**：
- `local-models/chat.axons.local-models/src/HFModelList.tsx`
- `local-models/chat.axons.local-models/src/LocalModelList.tsx`

**目标**：让搜索栏固定在顶部、列表区独立滚动，避免搜索栏跟着列表
一起滚出可视区。

**HFModelList.tsx 当前结构**：
```jsx
<div>
  <div>...搜索栏...</div>
  <div>...列表 map...</div>
</div>
```

**建议结构**：
```jsx
<div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
  <div style={{ flexShrink: 0 }}>...搜索栏...</div>
  <div style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
    ...列表 map...
  </div>
</div>
```

LocalModelList 同理（没有搜索栏的话至少把外层改成
`height:100%; overflowY:auto`）。

#### 改动 F：所有 `var(--axons-*)` 加 fallback

**涉及文件**：
- `ModelManagerPanel.tsx`
- `EngineStatusBar.tsx`
- `LocalModelCard.tsx`
- `HFModelCard.tsx`
- 其余使用 inline style 引用变量的组件

**模式**（与 `SearchBar.tsx` 已有写法对齐）：
```jsx
// 不要：
background: 'var(--axons-color-surface)'
// 改为：
background: 'var(--axons-color-surface, #101018)'
```

需要补 fallback 的常用变量：

| 变量 | 兜底值 (moon-theme) |
|---|---|
| `--axons-color-surface` | `#101018` |
| `--axons-color-elevated` | `#16161f` |
| `--axons-color-hover` | `#1c1c28` |
| `--axons-border-subtle` | `#1e1e2a` |
| `--axons-border-default` | `#2a2a3a` |
| `--axons-text-primary` | `#e4e4ed` |
| `--axons-text-secondary` | `#8888a0` |
| `--axons-text-muted` | `#5a5a70` |
| `--axons-accent` | `#7c3aed` |
| `--axons-success` | `#10b981` |
| `--axons-warning` | `#f59e0b` |
| `--axons-error` | `#ef4444` |
| `--axons-font-sans` | `'Inter', system-ui, sans-serif` |

#### 改动 G：重新构建 ui/index.js

修完源码后执行：
```bash
cd local-models/chat.axons.local-models
npm run build   # 或 npx vite build
```

确认 `ui/index.js` 已被覆盖，再装载到宿主验证。

---

## 五、验证清单

修复后逐项确认：

- [ ] 插件面板背景与宿主侧边栏背景视觉一致（深色：`#101018` 附近）
- [ ] 正文文字色为浅灰 (`#e4e4ed`)，非纯黑
- [ ] 卡片之间有 `#1e1e2a` 的细分割线
- [ ] HuggingFace tab 搜索栏顶部固定，下方模型列表可独立滚动
- [ ] 本地模型 tab 列表条目超过可视区时可以滚动到底
- [ ] 切换宿主主题 (moon ↔ sun) 时，插件面板跟随切换、不出现裸样式闪烁
- [ ] DevTools Computed 面板：`--axons-text-primary` 解析为非空 hex 值

---

## 六、附：相关源码定位速查

| 文件 | 关键位置 |
|---|---|
| `axons/internal/plugin/proxy.go` | L147-200 iframe HTML 模板；L252 themeClass 写死 |
| `axons/ui/src/plugin-sdk/theme.css` | L14-37 moon；L40-65 sun；L69-106 fallback :root |
| `axons/ui/src/plugin-sdk/components.css` | 组件类样式（axons-btn / axons-card / axons-tabs 等） |
| `axons/ui/src/plugin-sdk/iframe-adapter.ts` | L67-77 plugin:init 处理；L200-208 plugin:theme 处理 |
| `axons/ui/src/components/IframePluginPanel.tsx` | L208-216 iframe DOM、sandbox、尺寸 |
| `axons/internal/api/static/dist/plugin-sdk/` | 嵌入到 Go 二进制的最终静态资源 |
| `axons-extension-packages/local-models/chat.axons.local-models/src/ModelManagerPanel.tsx` | 插件根组件，承担高度链顶层 |
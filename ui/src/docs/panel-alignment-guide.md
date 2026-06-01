# 右侧面板对齐规范

## 问题背景

ExtensionsPanel 的分类区域与 CodeReferencesPanel、RightPanel 的 Tabs 区域未对齐，排查过程曲折，原因是三层微小差异叠加（每层仅 1~4px），肉眼难以直接定位。

## 对齐规则

所有 `location: 'right'` 的面板，Header 和 Header 下方的第二行（Tabs / 分类 / 筛选）必须严格对齐。

### Header 行 — 已统一

```
className="flex items-center justify-between px-3 py-2 h-[38px] border-b border-border-subtle"
```

- 固定高度 `h-[38px]`，不可省略
- `px-3 py-2` 内边距统一

### 第二行 — 关键对齐点

**基准：CodeReferencesPanel / RightPanel 的 Tabs 行**

```
容器: className="flex border-b border-border-subtle"
按钮: className="flex-1 px-4 py-1.5 text-xs font-medium" + 激活态 "border-b-2 border-accent"
```

计算高度：py-1.5(12px) + text-xs 行高(16px) + border-b-2(2px) = 30px + 容器 border-b(1px) = **31px**

**新增面板第二行必须满足：**

| 属性 | 要求 | 说明 |
|------|------|------|
| 容器总高 | **31px** | 与 Code/AI Tabs 行一致 |
| 内容水平起点 | **px-4 (16px)** | 与 Code/AI 按钮文本起点一致 |
| 容器垂直 padding | **不要用 py-** | 用固定高度代替，避免 padding 叠加按钮高度产生偏差 |

### 推荐写法

#### Tab 风格（与 Code/AI 完全一致）

```tsx
<div className="flex border-b border-border-subtle">
  <button className="flex-1 px-4 py-1.5 text-xs font-medium ...">
```

#### Pill / Chip 风格（如 ExtensionsPanel）

```tsx
<div className="flex items-center gap-1 px-4 border-b border-border-subtle overflow-x-auto"
     style={{ height: 31 }}>
  <button className="px-2 py-0.5 text-[11px] rounded-full ...">
```

关键点：
- 容器用 `style={{ height: 31 }}` 固定高度，**不用 `py-`**
- `px-4` 保证水平起点对齐
- `items-center` 让小按钮垂直居中

## 常见陷阱

1. **容器 `py-1.5` + 小按钮** — 容器 padding 叠加按钮自身高度，总高超出 31px
2. **容器 `px-3`** — 比 Code/AI 按钮的 `px-4` 少 4px，水平不对齐
3. **按钮非 `flex-1`** — 无法撑满容器，容器 padding 变成额外高度
4. **省略 `h-[38px]`** — Header 靠 padding 撑高，与固定高度的 Header 差几 px
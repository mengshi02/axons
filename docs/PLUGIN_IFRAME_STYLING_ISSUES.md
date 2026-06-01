# Plugin Panel Styling/Scrolling Issues and Host-Side Modification Suggestions

> Date: 2026-05-18
> Related plugin: `chat.axons.local-models` (local model management plugin)
> Related host: `/Users/mengshi3/go/src/github.com/mengshi02/axons`
> Document purpose: Deliver to the host (`axons`) maintainers as a basis for refactoring; also record the plugin-side self-check and fix plan

---

## 1. Problem Symptoms

When loading the `chat.axons.local-models` plugin panel, two issues that directly affect usability were observed:

1. **Panel style is inconsistent with the host main interface**
   - The plugin panel's background, text color, borders, and font do not match the host's moon-theme (dark) appearance
   - It looks like the browser's default appearance (transparent background, black text, no border separation)
2. **Model list cannot scroll down**
   - HuggingFace / local model list entries beyond the visible height are clipped
   - Mouse wheel and drag scrollbar both have no effect; content below cannot be seen

Both issues reproduce reliably across different resolutions and different tabs (Local / HuggingFace).

---

## 2. Background: Host ↔ Plugin UI Isolation Model

The host renders each plugin UI inside an independent `<iframe sandbox>` (see
`/Users/mengshi3/go/src/github.com/mengshi02/axons/ui/src/components/IframePluginPanel.tsx`),
generating the iframe's internal document via the following HTML template (located at
`/Users/mengshi3/go/src/github.com/mengshi02/axons/internal/plugin/proxy.go` lines 147-200):

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

Where:

- `theme.css` defines all `--axons-*` CSS variables (colors, fonts, shadows, border-radius, etc.),
  scoped under `:root.moon-theme` / `:root.sun-theme` classes
- `components.css` provides component classes such as `axons-btn`, `axons-card`, `axons-tabs`
- During server-side rendering, `<html class="moon-theme">` is written by default; theme switching
  notifies the iframe adapter to update the class asynchronously via `postMessage`

The host UI sets the iframe to `w-full h-full border-0` via `IframePluginPanel`,
meaning **the iframe itself is stretched to fill its parent container**, and the remaining height chain is managed inside the iframe.

---

## 3. Root Cause Analysis

### Problem 1: Style Inconsistency

#### Confirmed Facts

- `theme.css` variable definitions are complete, and both `:root.moon-theme` and `:root.sun-theme`
  provide explicit hex values
- The iframe template hardcodes `<html class="moon-theme">`, so variables should theoretically be resolvable
- `components.css` and `theme.css` static file paths do exist in the embedded assets
  at `internal/api/static/dist/plugin-sdk/`

#### Actual Risk Points

1. **Fallback `:root` block fails inside iframe**
   The fallback `:root` block at lines 69-106 of `theme.css` heavily uses
   `var(--color-*)` references to Tailwind v4 variables. The iframe **does not load Tailwind**,
   so if the `moon-theme` class on `<html>` is lost for any reason (e.g., someone refactors the template
   in the future, or SSR write fails), all variables inside the iframe will fall back to the fallback
   expressions → hit the "reference Tailwind variables" path → still fail to resolve → text color, borders, and fonts all become empty.
2. **No default `color` or `font-family` specified inside iframe**
   The iframe template's inline style only sets `background`, not `color` / `font-family`.
   Every `<div>` in the plugin must explicitly write `color: var(--axons-text-primary)` to get the correct color;
   if omitted, it degrades to the browser's default black text + default serif/sans-serif, which is visibly different from the host.
3. **Embedded assets may be out of sync with source**
   The host frontend is built by Vite and embedded into the Go binary (`internal/api/static/dist/`);
   if a developer modifies `theme.css` but forgets `npm run build`, the old styles will continue to be served.

> Debugging command (user-side self-check): Right-click the iframe of the plugin panel → Inspect Element,
> check Network for `/plugin-sdk/theme.css` — should be 200 with content containing `:root.moon-theme`;
> check Elements for whether `<html>` has the `moon-theme` class;
> check the Computed panel for whether `--axons-text-primary` resolves to `#e4e4ed`.

### Problem 2: List Cannot Scroll

#### Key Observation

The iframe template's body style is only:
```css
body { margin: 0; padding: 0; overflow: hidden; background: ...; }
```

**No `height: 100%` is set on `html` / `body` / `#root`.**

#### Causal Chain

1. The iframe tag is stretched by the host → the iframe's internal viewport height is determined
2. **But the iframe's `<html>`, `<body>`, and `<div id="root">` heights are all `auto`** — no level in the chain is given a definite height
3. The plugin's root component `ModelManagerPanel` uses `height: 100%` on its outermost layer
   → parent is auto → itself also collapses to `auto` → sized by content
4. Internally uses `display:flex; flexDirection:column` + child `flex:1; overflowY:auto`
   for the scroll container → parent chain has no definite height, `flex:1` degrades to content height
5. Scroll container's own height ≡ content height → `overflow-y:auto` never triggers
6. Meanwhile, `overflow:hidden` on body clips anything beyond the viewport
   → result is **content is clipped but cannot scroll**

This is a classic pitfall of flex layout + scrollable children; the only reliable fix is
**to give all three layers — html/body/#root — a definite height**.

---

## 4. Modification Suggestions

### 4.1 Host-Side Required Changes ✅

#### Change A: Complete the height/color chain in the iframe HTML template

**File**: `/Users/mengshi3/go/src/github.com/mengshi02/axons/internal/plugin/proxy.go`
**Location**: The `<style>` block inside the `iframeHostTemplate` constant (around lines 155-157)

**Current content**:
```html
<style>
  body { margin: 0; padding: 0; overflow: hidden;
         background: var(--axons-color-surface, #101018); }
</style>
```

**Suggested change**:
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

**Effect**:

- `html, body, #root { height: 100% }` — fixes "list cannot scroll". When the plugin
  uses `height: 100%` + flex layout, the entire parent chain has an explicit height,
  and `flex:1 + overflow-y:auto` works correctly.
- `color` / `font-family` — fixes "style inconsistency". Even if the plugin author forgets
  to specify text color on inner nodes, the body's default color follows the moon/sun theme,
  and won't degrade to browser black text + serif font.

**Note**: After making this change, run `cd ui && npm run build` to ensure
the embedded assets at `internal/api/static/dist/plugin-sdk/` are updated; otherwise the Go
binary will still contain the old HTML template (note: the template is a Go source constant, not a static resource,
so strictly speaking **recompiling the Go binary is sufficient** without rebuilding the frontend. The frontend
build reminder is here because Change B involves static resources).

#### Change B (optional hardening): Remove Tailwind dependency from theme.css fallback block

**File**: `/Users/mengshi3/go/src/github.com/mengshi02/axons/ui/src/plugin-sdk/theme.css`
**Location**: The fallback `:root` block (around lines 69-106)

**Current issue**: The fallback block depends on `var(--color-*)` (Tailwind v4 variables),
but the iframe doesn't load Tailwind; if the `moon-theme` / `sun-theme` class isn't applied,
all variables fail.

**Suggestion**: Replace the fallback `:root` block content with the equivalent hardcoded values
from `:root.moon-theme` (no longer referencing `--color-*`), so that "no theme class" also
degrades to moon-theme colors instead of empty strings.

Example (excerpt):
```css
:root {
  --axons-color-surface: #101018;     /* no longer var(--color-surface, ...) */
  --axons-text-primary: #e4e4ed;
  --axons-border-subtle: #1e1e2a;
  --axons-accent: #7c3aed;
  --axons-font-sans: 'Inter', system-ui, sans-serif;
  /* ... copy remaining values from :root.moon-theme ... */
}
```

**Effect**: Improves robustness inside the iframe — even if a future refactor forgets to write
`ThemeClass`, or `iframe-adapter.ts`'s `plugin:init` hasn't arrived yet,
the plugin's visuals will stably fall back to moon-theme instead of showing a degraded state of "white screen + black text".

After making this change, run `npm run build` again so that `internal/api/static/dist/plugin-sdk/theme.css`
is updated (this change is ineffective without rebuilding).

#### Change C (optional hardening): Server-side writes ThemeClass based on host's current theme

**File**: `/Users/mengshi3/go/src/github.com/mengshi02/axons/internal/plugin/proxy.go`
**Location**: The `themeClass` initialization in the `HandlePluginIframeHost` function (around line 252)

**Current state**: Hardcodes `themeClass := "moon-theme"`, so when a user switches to sun-theme
in the host and opens a plugin panel, they first see one frame of moon-theme before it switches to sun-theme (flash).

**Suggestion**: Read the current theme from the host's global state (if there's a daemon-side settings store),
and write the correct class during SSR to eliminate first-screen flash. If the current architecture doesn't
have the daemon holding this state, `IframePluginPanel.tsx` can pass the theme via query string
`?theme=sun` to the iframe-host route before the iframe's `onLoad`.

---

### 4.2 Plugin-Side Required Changes ✅ (should be done regardless of whether the host is fixed)

#### Change D: Add `minHeight: 0` to scroll container

**File**: `local-models/chat.axons.local-models/src/ModelManagerPanel.tsx`
**Location**: Content area scroll container (around line 54)

**Current content**:
```jsx
<div style={{ flex: 1, overflowY: 'auto' }}>
```

**Change to**:
```jsx
<div style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
```

**Reason**: Flex children default to `min-height: auto`, which can be stretched by content,
causing `overflow:auto` to not work. This is per browser spec behavior and is independent of
whether the host fixes the height chain; **any scrollable child in a flex column should have `min-height:0`**.

#### Change E: Refactor list components to use internal two-segment flex

**Files**:
- `local-models/chat.axons.local-models/src/HFModelList.tsx`
- `local-models/chat.axons.local-models/src/LocalModelList.tsx`

**Goal**: Keep the search bar fixed at the top with the list area scrolling independently,
preventing the search bar from scrolling out of view with the list.

**HFModelList.tsx current structure**:
```jsx
<div>
  <div>...search bar...</div>
  <div>...list map...</div>
</div>
```

**Suggested structure**:
```jsx
<div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
  <div style={{ flexShrink: 0 }}>...search bar...</div>
  <div style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
    ...list map...
  </div>
</div>
```

Same approach for LocalModelList (if there's no search bar, at least change the outer layer to
`height:100%; overflowY:auto`).

#### Change F: Add fallback values to all `var(--axons-*)` usages

**Affected files**:
- `ModelManagerPanel.tsx`
- `EngineStatusBar.tsx`
- `LocalModelCard.tsx`
- `HFModelCard.tsx`
- Other components using inline styles with variable references

**Pattern** (aligned with existing `SearchBar.tsx` style):
```jsx
// Don't:
background: 'var(--axons-color-surface)'
// Change to:
background: 'var(--axons-color-surface, #101018)'
```

Common variables that need fallback values:

| Variable | Fallback value (moon-theme) |
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

#### Change G: Rebuild ui/index.js

After fixing the source code, run:
```bash
cd local-models/chat.axons.local-models
npm run build   # or npx vite build
```

Confirm that `ui/index.js` has been overwritten, then load it into the host for verification.

---

## 5. Verification Checklist

After applying fixes, verify each item:

- [ ] Plugin panel background visually matches the host sidebar background (dark: around `#101018`)
- [ ] Body text color is light gray (`#e4e4ed`), not pure black
- [ ] Cards have a thin `#1e1e2a` divider between them
- [ ] HuggingFace tab search bar stays fixed at top; model list below scrolls independently
- [ ] Local models tab list can scroll to bottom when entries exceed the visible area
- [ ] Switching host theme (moon ↔ sun) causes the plugin panel to follow without bare-style flash
- [ ] DevTools Computed panel: `--axons-text-primary` resolves to a non-empty hex value

---

## 6. Appendix: Quick Reference for Related Source Code Locations

| File | Key location |
|---|---|
| `axons/internal/plugin/proxy.go` | L147-200 iframe HTML template; L252 themeClass hardcoded |
| `axons/ui/src/plugin-sdk/theme.css` | L14-37 moon; L40-65 sun; L69-106 fallback :root |
| `axons/ui/src/plugin-sdk/components.css` | Component class styles (axons-btn / axons-card / axons-tabs etc.) |
| `axons/ui/src/plugin-sdk/iframe-adapter.ts` | L67-77 plugin:init handler; L200-208 plugin:theme handler |
| `axons/ui/src/components/IframePluginPanel.tsx` | L208-216 iframe DOM, sandbox, sizing |
| `axons/internal/api/static/dist/plugin-sdk/` | Final static assets embedded into Go binary |
| `axons-extension-packages/local-models/chat.axons.local-models/src/ModelManagerPanel.tsx` | Plugin root component, serves as the top of the height chain |
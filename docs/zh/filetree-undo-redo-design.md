# FileTree Undo/Redo 设计与性能优化方案

## 1 背景

FileTree 面板已支持 Shift/Cmd 多选，但文件操作（复制/剪切/删除）执行后无法撤回。用户误操作（如 copy 错位置、剪切错文件）只能手动补救，体验与主流代码编辑器存在差距。

同时，当前所有文件操作完成后均通过 `loadTree(recursive=true)` 全量刷新整棵目录树，大项目下存在性能隐患，Undo/Redo 连续操作会进一步放大此问题。

## 2 设计目标

1. 支持 ⌘Z Undo / ⌘Shift+Z Redo，覆盖 Create、Copy、Cut-Move、Rename、Delete 五种操作
2. Delete 操作优先走系统回收站（通过 trash-bridge 插件），系统回收站不可用时永久删除（不入 undo 栈）
3. 同步实施增量更新优化，消除 `loadTree` 全量刷新瓶颈
4. 对存量功能零破坏，后端新 API 与旧 API 并存

## 3 整体架构

```
┌─────────────────────────────────────────────────┐
│                   前端 FileTreePanel              │
│                                                   │
│  用户操作 → executeOp() ──→ 后端 API              │
│                │          ← 返回 stat 信息         │
│                │                                  │
│                ├── 增量更新 entries（不再 loadTree）│
│                ├── 压入 undoStack（上限 100 步）    │
│                └── （无自动一致性校验，见 4.3.6）   │
│                                                   │
│  ⌘Z → pop undoStack → executeUndo() → 后端 API   │
│                  │         ├── preValidate() 校验  │
│                  │         ├── 增量更新             │
│                  │         └── 压入 redoStack       │
│                                                   │
│  ⌘Shift+Z → pop redoStack → executeRedo() → ...  │
└─────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────┐
│                   后端 API 层                      │
│                                                   │
│  现有路由（保留不变）:                               │
│    POST   /api/filetree/file     create file      │
│    DELETE /api/filetree/file     delete file      │
│    POST   /api/filetree/folder   create folder    │
│    DELETE /api/filetree/folder   delete folder    │
│    POST   /api/filetree/rename   rename/move      │
│    POST   /api/filetree/copy     copy             │
│                                                   │
│  新增路由（Phase 3 增量更新，返回值增强）:           │
│    所有写操作返回 entry 字段（见 4.3.2）             │
│                                                   │
│  trash-bridge 插件路由（系统回收站，见 Phase 2）:    │
│    POST   /api/filetree/trash        移入回收站    │
│    POST   /api/filetree/trash/restore 从回收站恢复  │
└─────────────────────────────────────────────────┘
```

## 4 分阶段实现计划

### Phase 1：Copy/Cut-Move/Create/Rename Undo + Redo

**改动范围**：前端改动为主，依赖 Phase 3 后端返回值增强（create/copy 操作需 `entry.mod_time`）。

#### 4.1.1 操作类型定义

```ts
type ValidateResult = 'valid' | 'modified' | 'missing';

type FileOperation =
  | { type: 'create'; path: string; isDir: boolean; modTime: string }     // v4: 新增 modTime
  | { type: 'copy';   srcPath: string; dstPath: string; isDir: boolean; modTime: string }  // v4: 新增 modTime
  | { type: 'move';   oldPath: string; newPath: string }
  | { type: 'delete'; path: string; isDir: boolean; trashId: string }

/** 批量操作作为一个整体 undo 单元 */
interface CompoundOperation {
  type: 'compound';
  ops: FileOperation[];
}

type UndoableOperation = FileOperation | CompoundOperation;
```

> **v2 变更**：`copy` 操作增加 `isDir` 字段，undo 时需要知道副本是文件还是目录才能选择 `deleteFile` 还是 `deleteFolder`。
>
> **v4 变更**：`create` 和 `copy` 操作增加 `modTime` 字段，用于 undo 时检测文件是否被修改过（见 5.1a）。新增 `ValidateResult` 三态类型，供 `preValidateUndo` 返回使用。

#### 4.1.2 状态管理

```ts
const MAX_UNDO_STACK_SIZE = 100;

const [undoStack, setUndoStack] = useState<UndoableOperation[]>([]);
const [redoStack, setRedoStack] = useState<UndoableOperation[]>([]);
const [isUndoRedoing, setIsUndoRedoing] = useState(false); // 防止 undo/redo 期间触发新操作入栈

// 项目切换时清空栈（useEffect 监听 projectId 变化）
useEffect(() => {
  setUndoStack([]);
  setRedoStack([]);
}, [projectId]);
```

> **v2 变更**：
> - 新增 `MAX_UNDO_STACK_SIZE` 上限，防止极端场景内存膨胀
> - 新增 `projectId` 变化时清空栈的 `useEffect`，解决组件不 unmount 但项目切换时栈残留的问题

#### 4.1.3 操作执行与入栈

每个文件操作执行后，将逆向信息压入 undoStack，同时清空 redoStack：

```ts
async function executeAndTrack(op: UndoableOperation) {
  // op 已执行完毕，此时做增量更新 entries
  updateEntriesFromOp(op);
  setUndoStack(prev => {
    const next = [...prev, op];
    // 超出上限时丢弃最早的记录
    if (next.length > MAX_UNDO_STACK_SIZE) next.shift();
    return next;
  });
  setRedoStack([]);  // 新操作清空 redo
}
```

批量操作（多选 copy 5 个文件）必须包装为单个 `compound` 操作，确保一次 ⌘Z 撤回全部：

```ts
// handlePaste 中批量 copy 后（v4: 入栈时记录 modTime）
const ops: FileOperation[] = clipboard.entries.map(ce => ({
  type: 'copy' as const,
  srcPath: ce.path,
  dstPath: resolvedDstPaths[ce.path],
  isDir: ce.is_dir,
  modTime: ce.mod_time,  // v4: 来自后端返回的 entry
}));
executeAndTrack({ type: 'compound', ops });
```

#### 4.1.4 Undo 执行

```ts
async function executeUndo(op: UndoableOperation) {
  setIsUndoRedoing(true);
  try {
    if (op.type === 'compound') {
      // 反序执行，确保依赖关系正确
      for (const sub of [...op.ops].reverse()) {
        const result = await preValidateUndo(sub);
        if (result === 'missing') continue; // 跳过已失效的子操作，但记录提示
        if (result === 'modified') {
          showToast({ type: 'warning', message: t('fileTree.undoModifiedWarning') });
          // 仍允许继续 undo，但已发出警告
        }
        await undoSingle(sub);
      }
    } else {
      const result = await preValidateUndo(op);
      if (result === 'missing') {
        // 目标已失效，弹出确认对话框
        const skip = await showConfirmDialog({
          title: t('fileTree.undoConflictTitle'),
          message: t('fileTree.undoConflictMessage'),
          confirmLabel: t('fileTree.undoSkip'),
          cancelLabel: t('fileTree.undoCancel'),
        });
        if (!skip) return; // 用户取消，保留栈不变
      } else if (result === 'modified') {
        // 文件已被修改，弹出警告对话框
        const proceed = await showConfirmDialog({
          title: t('fileTree.undoModifiedTitle'),
          message: t('fileTree.undoModifiedMessage'),
          confirmLabel: t('fileTree.undoProceed'),
          cancelLabel: t('fileTree.undoCancel'),
        });
        if (!proceed) return; // 用户取消，保留栈不变
      }
      await undoSingle(op);
    }
    setUndoStack(prev => prev.slice(0, -1));
    setRedoStack(prev => [...prev, op]);
  } finally {
    setIsUndoRedoing(false);
  }
}

/** 校验 undo 目标是否仍然存在/有效，避免静默跳过导致栈与实际状态不一致 */
async function preValidateUndo(op: FileOperation): Promise<ValidateResult> {
  switch (op.type) {
    case 'create':
    case 'copy': {
      const checkPath = op.type === 'create' ? op.path : op.dstPath;
      const stat = await statEntry(checkPath, projectId);
      if (!stat.exists) return 'missing';
      if (!op.isDir && stat.mod_time !== op.modTime) return 'modified';
      return 'valid';
    }
    case 'move': {
      const stat = await statEntry(op.newPath, projectId);
      if (!stat.exists) return 'missing';
      return 'valid';
    }
    case 'delete':
      return 'valid'; // Phase 2 中由 restore API 返回校验
  }
}

async function undoSingle(op: FileOperation) {
  switch (op.type) {
    case 'create':
      // undo create = 删除刚创建的文件/目录
      op.isDir ? await deleteFolder(op.path, projectId) : await deleteFile(op.path, projectId);
      removeEntryFromState(op.path);
      break;
    case 'copy':
      // undo copy = 删除副本
      op.isDir ? await deleteFolder(op.dstPath, projectId) : await deleteFile(op.dstPath, projectId);
      removeEntryFromState(op.dstPath);
      break;
    case 'move':
      // undo move = 反向 rename
      await renameEntry(op.newPath, op.oldPath, projectId);
      moveEntryInState(op.newPath, op.oldPath);
      break;
    case 'delete':
      // Phase 2 实现，此处预留
      await restoreFromTrash(op.trashId, op.path, projectId);
      break;
  }
}

/** 调用 /api/filetree/stat 获取路径的 stat 信息 */
async function statEntry(path: string, projectId: string): Promise<{
  exists: boolean; is_dir?: boolean; size?: number; mod_time?: string; abs_path?: string;
}> {
  try {
    const res = await fetch(`/api/filetree/stat?path=${encodeURIComponent(path)}&project_id=${projectId}`);
    const data = await res.json();
    return data;
  } catch {
    return { exists: false };
  }
}
```

> **v2 变更**：
> - 新增 `preValidateUndo` 校验，undo 执行前先 stat 目标是否存在
> - 目标失效时弹出确认对话框而非静默跳过，避免用户以为撤回了 5 步实际只撤回了 3 步
> - compound 操作中失效的子操作跳过但继续 undo 其余项，避免连锁中断
>
> **v4 变更**：
> - `preValidateUndo` 返回值从 `boolean` 改为 `ValidateResult = 'valid' | 'modified' | 'missing'`，支持检测文件被修改的场景
> - `statExists` 改为 `statEntry`，返回完整 stat 信息（含 `mod_time`），供内容变更检测
> - `executeUndo` 中对 `modified` 结果弹出警告对话框，防止用户无意丢失编辑内容

#### 4.1.4a 前端前置改动

**api.ts 新增 `statEntry` 封装**：

后端 `/api/filetree/stat` 路由已实现（`internal/api/handlers_filetree.go`），前端需在 `api.ts` 中新增封装：

```ts
/** Stat a single path — check existence and get metadata */
export async function statEntry(path: string, projectId: string): Promise<{
  exists: boolean; is_dir?: boolean; size?: number; mod_time?: string; abs_path?: string;
}> {
  const params = new URLSearchParams({ path, project_id: projectId });
  const response = await fetch(`${getBaseURL()}/api/filetree/stat?${params}`);
  if (!response.ok) return { exists: false };
  return response.json();
}
```

**FileTreePanel 根元素添加 `data-panel` 属性**：

为支持 `isFileTreeFocused()` 焦点判断，在组件根 `<div>` 上添加 `data-panel="filetree"`：

```tsx
// FileTreePanel.tsx render 方法
<div
  data-panel="filetree"    // v4: 新增，供 isFileTreeFocused() 判断
  className="h-full bg-surface flex flex-col overflow-hidden"
  ...
>
```

**错误提示升级为 Toast**：

当前组件使用 `alert()` 做错误提示，undo/redo 场景下体验差（阻塞交互）。需替换为非阻塞的 Toast 通知。项目中已有 Toast 基础设施（`showToast`），直接复用即可。

> **v4 新增**：明确 Phase 1 的三处前端前置改动，避免编码时遗漏。

#### 4.1.5 Redo 执行

```ts
async function executeRedo(op: UndoableOperation) {
  setIsUndoRedoing(true);
  try {
    if (op.type === 'compound') {
      for (const sub of op.ops) {
        await redoSingle(sub);
      }
    } else {
      await redoSingle(op);
    }
    setRedoStack(prev => prev.slice(0, -1));
    setUndoStack(prev => [...prev, op]);
  } finally {
    setIsUndoRedoing(false);
  }
}

async function redoSingle(op: FileOperation) {
  switch (op.type) {
    case 'create':
      op.isDir ? await createFolder(op.path, projectId) : await createFile(op.path, projectId);
      break;
    case 'copy':
      await copyEntry(op.srcPath, op.dstPath, projectId);
      break;
    case 'move':
      await renameEntry(op.oldPath, op.newPath, projectId);
      break;
    case 'delete':
      await moveToTrash(op.path, projectId);  // Phase 2
      break;
  }
  updateEntriesFromOp(op);
}
```

#### 4.1.6 快捷键

在现有 `useEffect` 键盘监听中增加，**仅当焦点在 FileTree 区域时生效**，避免与 CodePanel 的 ⌘Z 冲突：

```ts
if (isMod && e.key === 'z' && !isShift && undoStack.length > 0) {
  // 仅当焦点在 FileTree 区域时拦截，否则让 CodePanel 处理
  if (isFileTreeFocused()) {
    e.preventDefault();
    executeUndo(undoStack[undoStack.length - 1]);
  }
} else if (isMod && e.key === 'z' && isShift && redoStack.length > 0) {
  if (isFileTreeFocused()) {
    e.preventDefault();
    executeRedo(redoStack[redoStack.length - 1]);
  }
}

/** 判断当前焦点是否在 FileTree 面板内 */
function isFileTreeFocused(): boolean {
  const active = document.activeElement;
  if (!active) return false;
  return !!active.closest('[data-panel="filetree"]');
}
```

> **v2 变更**：新增 `isFileTreeFocused()` 焦点判断，防止 ⌘Z 与 CodePanel 的 undo 拦截冲突。FileTree 根元素需添加 `data-panel="filetree"` 属性。

#### 4.1.7 涉及修改的调用点

| 函数 | 操作 | 压入 undoStack 的类型 | modTime 来源 |
|------|------|----------------------|-------------|
| `handlePaste` (copy mode) | copyEntry × N | `compound<copy>` | 后端 copy 返回的 `entry.mod_time` |
| `handlePaste` (cut mode) | renameEntry × N | `compound<move>` | move 不需要 modTime |
| `handleNewItemCommit` | createFile / createFolder | `create` | 后端 create 返回的 `entry.mod_time` |
| `handleRenameCommit` | renameEntry | `move` | move 不需要 modTime |

> **v4 变更**：调用点表格增加 `modTime 来源` 列。create 和 copy 操作入栈时需记录后端返回的 `entry.mod_time`，move 操作不涉及内容变更检测，无需 modTime。

#### 4.1.8 并发操作防护

`isUndoRedoing` 仅防止 undo/redo 期间的新操作入栈，但大目录 copy 执行期间用户仍可触发其他操作。新增 `isOperating` 标志：

```ts
const [isOperating, setIsOperating] = useState(false);

// 所有文件操作入口处：
async function executeAndTrack(op: UndoableOperation) {
  if (isOperating || isUndoRedoing) return;
  setIsOperating(true);
  try {
    // ... 执行操作
  } finally {
    setIsOperating(false);
  }
}
```

> **v2 新增**：`isOperating` 标志防止操作执行期间用户重复触发，与 `isUndoRedoing` 共同构成互斥保护。

### Phase 2：Delete Undo（系统回收站）

**改动范围**：前端改动 + trash-bridge 插件（零后端 Go 代码改动）。

**设计决策**：不实现自建 trash 目录（如 `.axons-trash/`），而是复用操作系统原生回收站，与 IDE 行为一致。理由：

1. 自建 trash 恢复体验差——用户在 Finder/Explorer 里看不到，只能通过 undo 恢复，心智模型割裂
2. 自建 trash 增加复杂度——清理策略、跨文件系统 fallback、`ignoredDirs` 维护，收益不大
3. 系统回收站不可用时（Docker、SSH 远程），用户本就没有 Finder/Explorer，对"移入回收站"期望不高

#### 4.2.1 系统回收站调用方式

通过 exec-command-bridge 的 **trash-bridge 插件** 调用系统回收站，与 clipboard-bridge 同构：

| OS | 命令 | 说明 |
|-----|------|------|
| macOS | `osascript -e 'tell application "Finder" to delete POSIX file "{path}"'` | 移入废纸篓 |
| Windows | `powershell -command "(New-Object -ComObject Shell.Application).Namespace(10).MoveHere(\"{path}\")"` | 移入回收站 |
| Linux | `gio trash {path}` / fallback: `trash-put {path}` | 移入回收站 |

插件 manifest 详情见 `exec-command-bridge-design.md` Section 7.3。

#### 4.2.2 Delete 流程

```
用户 Delete → handleDeleteConfirm()
  ├── trash-bridge 插件已安装？
  │     ├── 是 → 调用 POST /api/filetree/trash（系统回收站）
  │     │     成功 → removeEntryFromState + 压入 undoStack（type: 'delete'）
  │     │     失败 → 永久删除（现有行为）+ 不入 undo 栈
  │     └── 否 → 永久删除（现有行为）+ 不入 undo 栈
```

**关键规则**：系统回收站不可用时，delete 操作**不入 undo 栈**。因为永久删除不可逆，undo 栈中的 delete 记录无法可靠执行——与 IDE 行为一致（IDE 也不提供 undo delete）。

#### 4.2.3 前端改动

```ts
// api.ts 新增
export async function trashEntry(path: string, projectId: string): Promise<{
  trash_id: string;
  original_path: string;
  entry: FileTreeEntry;
}> { ... }

export async function restoreFromTrash(trashId: string, originalPath: string, projectId: string): Promise<{
  restored_path: string;
  entry: FileTreeEntry;
}> { ... }
```

```ts
// handleDeleteConfirm 改为尝试调 trash
const handleDeleteConfirm = async () => {
  // 检测 trash-bridge 插件是否可用
  const trashAvailable = await isTrashBridgeAvailable();

  if (trashAvailable) {
    // 走系统回收站
    const undoOps: FileOperation[] = [];
    try {
      for (const target of deleteTargets) {
        const { trash_id, entry } = await trashEntry(target.path, projectId);
        removeEntryFromState(target.path);
        undoOps.push({ type: 'delete', path: target.path, isDir: target.is_dir, trashId: trash_id });
      }
      executeAndTrack({ type: 'compound', ops: undoOps });
      // ConfirmDialog 文案: t('fileTree.moveToTrash')
    } catch {
      // trash 失败，降级为永久删除
      await permanentDelete(deleteTargets);
      // 不入 undo 栈
    }
  } else {
    // 无 trash 插件，永久删除（现有行为）
    await permanentDelete(deleteTargets);
    // 不入 undo 栈
  }
};

/** 检测 trash-bridge 是否可用（简单探测路由是否存在） */
async function isTrashBridgeAvailable(): Promise<boolean> {
  try {
    const res = await fetch('/api/filetree/trash', { method: 'OPTIONS' });
    return res.ok;
  } catch {
    return false;
  }
}
```

#### 4.2.4 Undo Delete

undo delete 时调用 `restoreFromTrash`，将文件从系统回收站恢复：

```ts
async function undoSingle(op: FileOperation) {
  switch (op.type) {
    case 'delete':
      await restoreFromTrash(op.trashId, op.path, projectId);
      insertEntryToTree(entries, parentDir, entry);
      break;
    // ... 其他 case
  }
}
```

> **注意**：如果用户在系统回收站中已手动清空了该文件，`restoreFromTrash` 会失败。此时 `preValidateUndo` 无法预先检测（回收站内部状态对前端不可见），undo 失败后弹出错误提示，栈正常 pop/push。

#### 4.2.5 现有 Delete API 兼容性

- `DELETE /api/filetree/file` 和 `DELETE /api/filetree/folder` **保留不动**
- MCP 工具、其他消费者仍可直接真删
- 前端 FileTreePanel 优先使用 trash API，降级为永久删除

### Phase 3：增量更新优化（消除 loadTree 全量刷新）

**改动范围**：前后端均需改动。

#### 4.3.1 问题分析

当前每次文件操作后都调 `loadTree()`：

```
前端 → fetch /api/filetree?recursive=true
后端 → os.ReadDir 递归遍历整个项目
     → JSON 序列化（大项目可达 MB 级）
     → 网络传输
前端 → React setState → 整棵树重渲染
```

10k 文件的项目，单次操作的网络+渲染延迟约 200-500ms。Undo 连续操作 5 次就是 1-2.5s 的卡顿。

#### 4.3.2 后端 API 返回值增强

**所有写操作统一返回 `entry: FileTreeEntry`**，供前端直接增量更新：

```go
// create file 成功后返回
fi, _ := os.Stat(absPath)
rel, _ := filepath.Rel(projectRoot, absPath)
writeJSON(w, http.StatusOK, map[string]interface{}{
    "message":  "File created",
    "abs_path": absPath,
    "path":     req.Path,
    "entry":    statEntry(absPath, rel, fi),     // 新增
})

// create folder 成功后返回
fi, _ := os.Stat(absPath)
rel, _ := filepath.Rel(projectRoot, absPath)
writeJSON(w, http.StatusOK, map[string]interface{}{
    "message":  "Folder created",
    "abs_path": absPath,
    "path":     req.Path,
    "entry":    statEntry(absPath, rel, fi),     // 新增
})

// copy 成功后返回
dstFi, _ := os.Stat(dstAbs)
relDst, _ := filepath.Rel(projectRoot, dstAbs)
writeJSON(w, http.StatusOK, map[string]interface{}{
    "message":    "Copied successfully",
    "src_path":   req.SrcPath,
    "dst_path":   req.DstPath,
    "entry":      statEntry(dstAbs, relDst, dstFi),  // 新增：目标节点的完整 stat
})

// rename 成功后返回
newFi, _ := os.Stat(newAbs)
relNew, _ := filepath.Rel(projectRoot, newAbs)
writeJSON(w, http.StatusOK, map[string]interface{}{
    "message":  "Renamed successfully",
    "old_path": req.OldPath,
    "new_path": req.NewPath,
    "entry":    statEntry(newAbs, relNew, newFi),     // 新增：新路径的完整 stat
})
```

> **v2 变更**：原方案仅提到 copy/rename 需增强，**遗漏了 create**。现统一所有写操作（create/copy/rename/trash/restore）均返回 `entry` 字段。对于目录操作，`entry.children` 需包含完整子树。

#### 4.3.2a 目录操作返回子树

当前后端 `statEntry()` 只返回单个节点的 stat，不填充 `children`。对于目录类操作（create folder、copy folder），前端增量更新需要完整的子树信息。

**实现方式**：在写操作成功后，若目标是目录，额外调用 `listDir` 构建子树：

```go
// create folder 成功后返回（含子树——新建目录通常为空，但保持一致）
fi, _ := os.Stat(absPath)
rel, _ := filepath.Rel(projectRoot, absPath)
entry := statEntry(absPath, rel, fi)
if fi.IsDir() {
    children, _ := listDir(absPath, projectRoot, true, -1)
    entry.Children = children
}

// copy 成功后返回（目录需含子树）
dstFi, _ := os.Stat(dstAbs)
relDst, _ := filepath.Rel(projectRoot, dstAbs)
entry := statEntry(dstAbs, relDst, dstFi)
if dstFi.IsDir() {
    children, _ := listDir(dstAbs, projectRoot, true, -1)
    entry.Children = children
}

// rename 成功后返回（目录需含子树——路径变了但子树结构不变）
newFi, _ := os.Stat(newAbs)
relNew, _ := filepath.Rel(projectRoot, newAbs)
entry := statEntry(newAbs, relNew, newFi)
if newFi.IsDir() {
    children, _ := listDir(newAbs, projectRoot, true, -1)
    entry.Children = children
}
```

> **v4 新增**：明确目录操作需返回子树的实现方式——复用已有的 `listDir()` 函数。create folder 通常是空目录，但仍调用 `listDir` 以保持逻辑统一和边界安全（如模板目录初始化）。

#### 4.3.3 前端 api.ts 返回值改造

现有前端 API 函数均返回 `void`，需改为返回后端响应体：

```ts
// 改造前
export async function createFile(path: string, projectId: string, content = ''): Promise<void>

// 改造后
export async function createFile(path: string, projectId: string, content = ''): Promise<{
  message: string; path: string; abs_path: string; entry: FileTreeEntry;
}>

// 改造前
export async function copyEntry(srcPath: string, dstPath: string, projectId: string): Promise<void>

// 改造后
export async function copyEntry(srcPath: string, dstPath: string, projectId: string): Promise<{
  message: string; src_path: string; dst_path: string; entry: FileTreeEntry;
}>

// 改造前
export async function renameEntry(oldPath: string, newPath: string, projectId: string): Promise<void>

// 改造后
export async function renameEntry(oldPath: string, newPath: string, projectId: string): Promise<{
  message: string; old_path: string; new_path: string; entry: FileTreeEntry;
}>
```

> **v2 新增**：原方案未提及前端 api.ts 返回值改造，但增量更新和 undo 入栈都需要这些返回值。所有写操作函数改为返回包含 `entry` 的响应体。

#### 4.3.4 前端增量更新函数

```ts
/** 从 entries 树中移除指定路径的节点 */
function removeEntryFromTree(entries: FileTreeEntry[], path: string): FileTreeEntry[] {
  return entries.filter(e => {
    if (e.path === path) return false;
    if (e.children) e.children = removeEntryFromTree(e.children, path);
    return true;
  });
}

/** 向指定目录插入新节点 */
function insertEntryToTree(
  entries: FileTreeEntry[],
  parentDir: string,
  newEntry: FileTreeEntry,
): FileTreeEntry[] {
  if (parentDir === '.' || parentDir === '') {
    return [...entries, newEntry].sort(dirFirstAlphaAsc);
  }
  return entries.map(e => {
    if (e.path === parentDir && e.is_dir) {
      return { ...e, children: [...(e.children ?? []), newEntry].sort(dirFirstAlphaAsc) };
    }
    if (e.children) return { ...e, children: insertEntryToTree(e.children, parentDir, newEntry) };
    return e;
  });
}

/** 移动节点：从 oldPath 移除，插入到 newPath 父目录 */
function moveEntryInTree(entries: FileTreeEntry[], oldPath: string, newEntry: FileTreeEntry): FileTreeEntry[] {
  const parentDir = newEntry.path.includes('/') ? newEntry.path.split('/').slice(0, -1).join('/') : '.';
  let result = removeEntryFromTree(entries, oldPath);
  result = insertEntryToTree(result, parentDir, newEntry);
  return result;
}

/** 排序比较函数：目录优先、按字母升序。与后端 listDir 排序逻辑一致 */
function dirFirstAlphaAsc(a: FileTreeEntry, b: FileTreeEntry): number {
  if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;  // 目录在前
  return a.name.toLowerCase().localeCompare(b.name.toLowerCase());
}
```

> **v4 新增**：明确 `dirFirstAlphaAsc` 排序函数的定义。此函数与后端 [`listDir`](../internal/api/handlers_filetree.go:148) 的排序逻辑一致（目录优先 + 字母升序），确保增量插入的节点位置与全量 `loadTree` 后的位置相同。

#### 4.3.5 各操作改为增量更新

| 操作 | 原方式 | 新方式 |
|------|--------|--------|
| createFile/Folder | `loadTree()` | `insertEntryToTree` + 后端返回 stat |
| deleteFile/Folder | `loadTree()` | `removeEntryFromTree` |
| rename | `loadTree()` | `moveEntryInTree` + 后端返回 stat |
| copy | `loadTree()` | `insertEntryToTree` + 后端返回 stat |
| trash | `loadTree()` | `removeEntryFromTree` |
| restore | `loadTree()` | `insertEntryToTree` + 后端返回 stat |

**保留 `loadTree` 作为 fallback**：增量更新失败时（如路径解析异常），降级为全量刷新。

#### 4.3.6 一致性校验机制

**不采用自动定时校验**。理由：增量操作本身是可信的（前端知道做了什么），真正需要校验的是外部变更（用户用终端 `mv`/`rm` 文件），但无论比数量还是比路径，都要求全量拉树，成本等于 `loadTree` 全量刷新，没有意义。

**采用以下策略替代**：

1. **保留 `loadTree` 作为 fallback**：增量更新失败时（如路径解析异常），降级为全量刷新
2. **外部变更靠文件系统 watcher 推送**：后续接入 `fsnotify`，后端检测到项目文件变更后通过 SSE 推 `filetree:changed` 事件，前端收到后做全量刷新（主流IDE的做法）
3. **用户随时可手动 Refresh**：现有按钮已支持，作为兜底

## 5 边界情况处理

### 5.1 文件被外部修改

Undo 执行时目标路径可能已被外部工具修改/删除：
- `undo copy`：如果 dstPath 已不存在，通过 `preValidateUndo` 检测，弹出确认对话框
- `undo move`：如果 newPath 已不存在，弹出确认对话框；如果 oldPath 已被占用，后端 rename API 返回 CONFLICT → **栈正常 pop/push（视为 undo 已完成），前端降级为 `loadTree()` 全量刷新**。理由：CONFLICT 说明 oldPath 处已有新文件，此时 undo move 的语义（"把文件放回原位"）已无法达成，但文件本身还在 newPath 处，磁盘状态与 undo 前一致，无需特殊处理
- `undo delete(restore)`：通过 trash-bridge 调用系统回收站恢复；如果用户已在回收站中清空了该文件，restore 失败，弹出错误提示

> **v2 变更**：原方案对失效路径"静默跳过"，改为弹出确认对话框，避免栈与实际状态不一致。

### 5.1a undo create/copy 时文件已被编辑

undo create（删除刚创建的文件）或 undo copy（删除副本）时，如果用户在 CodePanel 中已经编辑并保存了该文件，直接 undo 会丢失内容。

**策略**：操作入栈时记录 `mod_time`（来自后端返回的 entry），undo 前通过 `/api/filetree/stat` 比对当前 `mod_time`。

操作类型定义增加 `modTime` 字段：

```ts
type FileOperation =
  | { type: 'create'; path: string; isDir: boolean; modTime: string }  // v4: 新增 modTime
  | { type: 'copy';   srcPath: string; dstPath: string; isDir: boolean; modTime: string }  // v4: 新增 modTime
  | { type: 'move';   oldPath: string; newPath: string }
  | { type: 'delete'; path: string; isDir: boolean; trashId: string }
```

`preValidateUndo` 返回三态结果，增加内容变更检测：

```ts
type ValidateResult = 'valid' | 'modified' | 'missing';

async function preValidateUndo(op: FileOperation): Promise<ValidateResult> {
  switch (op.type) {
    case 'create':
    case 'copy': {
      const checkPath = op.type === 'create' ? op.path : op.dstPath;
      const stat = await statEntry(checkPath, projectId);
      if (!stat.exists) return 'missing';
      // 文件（非目录）被修改过 → 警告用户
      if (!op.isDir && stat.mod_time !== op.modTime) return 'modified';
      return 'valid';
    }
    case 'move': {
      const stat = await statEntry(op.newPath, projectId);
      if (!stat.exists) return 'missing';
      return 'valid';
    }
    case 'delete':
      return 'valid';  // Phase 2 中由 restore API 校验
  }
}
```

**用户交互**：

| preValidate 结果 | 行为 |
|------------------|------|
| `valid` | 正常 undo |
| `missing` | 弹出确认对话框：目标已不存在，跳过此操作 |
| `modified` | 弹出**警告**对话框：**"该文件已被修改，撤销将丢失所有编辑内容。确认继续？"** |

> **v4 新增**：undo create/copy 时，如果文件内容已被修改，直接删除会导致数据丢失。通过比较 `mod_time` 检测变更，修改过时弹出警告对话框，防止用户无意丢失编辑内容。

### 5.2 Undo/Redo 执行期间的用户操作

`isUndoRedoing` 标志位防止 undo/redo 执行期间的新操作入栈。`isOperating` 标志位防止操作执行期间的重复触发。

**被丢弃的操作必须有用户反馈**（v4 明确）：不能静默丢弃，否则用户以为操作成功了但实际未执行。

```ts
async function executeAndTrack(op: UndoableOperation) {
  if (isOperating || isUndoRedoing) {
    showToast({
      type: 'warning',
      message: t('fileTree.operationInProgress'),
    });
    return;
  }
  // ... 正常执行
}
```

| 场景 | 反馈 |
|------|------|
| undo 执行中用户触发 copy/paste | Toast 提示"操作正在执行中，请稍后" |
| 文件操作执行中用户按 ⌘Z | Toast 提示"操作正在执行中，请稍后" |

> **v4 变更**：原方案中 `isUndoRedoing` / `isOperating` 期间的操作被静默丢弃。现改为弹出 Toast 提示，避免用户困惑。

### 5.3 系统剪贴板粘贴的 Undo

外部文件 copy 进项目后，undo 只能删除项目内的副本，无法影响源文件（源在项目外）。

### 5.4 跨项目操作

Undo 栈与项目绑定。切换项目时通过 `useEffect` 监听 `projectId` 变化显式清空 undo/redo 栈。

> **v2 变更**：原方案仅说"切换项目时清空"，未考虑组件不 unmount 的场景。现用 `useEffect([projectId])` 显式清空。

### 5.5 批量操作的原子性

`compound` 操作中的子操作可能部分失败：

**执行阶段**：已完成的子操作不回滚（与当前 `handlePaste` 行为一致）。

**执行阶段的入栈策略**（v4 明确）：只将**成功执行的子操作**包装为 compound 入栈。失败的不入栈。

```ts
async function executeAndTrackCompound(ops: FileOperation[]) {
  const succeeded: FileOperation[] = [];
  const failed: string[] = [];

  for (const op of ops) {
    try {
      await executeSingleOp(op);         // 执行单个操作
      succeeded.push(op);                 // 成功的收集起来
      updateEntriesFromOp(op);            // 增量更新
    } catch (e) {
      failed.push(`${opDescription(op)}: ${(e as Error).message}`);
    }
  }

  // 只有成功的子操作入栈
  if (succeeded.length > 0) {
    pushUndoStack(succeeded.length === 1 ? succeeded[0] : { type: 'compound', ops: succeeded });
    clearRedoStack();
  }

  // 部分失败时提示用户
  if (failed.length > 0) {
    showToast({
      type: 'warning',
      message: t('fileTree.partialFailure', { count: failed.length }),
      detail: failed.join('\n'),
    });
  }
}
```

**Undo 阶段**：部分失败的子操作跳过（经 `preValidateUndo` 校验），继续 undo 其余项。

> **v4 变更**：明确"执行阶段部分失败时的入栈策略"——只入栈成功的子操作，而非全部。这样 undo 只会撤销实际完成的操作，不会尝试撤销一个从未成功的操作。

### 5.6 快捷键焦点冲突

⌘Z / ⌘Shift+Z 仅在 FileTree 面板获得焦点时生效。焦点在 CodePanel 时由编辑器处理 undo。判断依据：`document.activeElement.closest('[data-panel="filetree"]')`。

> **v2 新增**

### 5.7 Undo/Redo 执行中的 API 错误

undo/redo 执行中后端 API 返回错误时：
- 非 compound 操作：栈保持不变，向用户提示错误信息
- compound 操作：已执行的子操作不回滚，剩余子操作跳过，栈正常 pop/push，向用户提示部分失败

```ts
async function undoSingle(op: FileOperation): Promise<boolean> {
  try {
    switch (op.type) {
      // ... 原有逻辑
    }
    return true;
  } catch (e) {
    showToast({ type: 'error', message: `${t('fileTree.undoFailed')}: ${(e as Error).message}` });
    return false;
  }
}
```

> **v2 新增**

### 5.8 组件膨胀治理

当前 [`FileTreePanel.tsx`](../ui/src/components/FileTreePanel.tsx) 已有 958 行。Phase 3（增量更新）和 Phase 1（Undo/Redo）将新增约 ~250 行逻辑（undo/redo 状态管理、preValidate、executeAndTrack、增量更新函数、快捷键扩展），组件会膨胀到 ~1200 行，可维护性下降。

**建议**：Phase 1 实现时，将 undo/redo 相关逻辑抽取为独立的 custom hook：

```ts
// ui/src/hooks/useFileTreeUndoRedo.ts
export function useFileTreeUndoRedo(
  entries: FileTreeEntry[],
  setEntries: (entries: FileTreeEntry[]) => void,
  projectId: string,
) {
  const [undoStack, setUndoStack] = useState<UndoableOperation[]>([]);
  const [redoStack, setRedoStack] = useState<UndoableOperation[]>([]);
  const [isUndoRedoing, setIsUndoRedoing] = useState(false);
  const [isOperating, setIsOperating] = useState(false);

  const executeAndTrack = ...;
  const executeUndo = ...;
  const executeRedo = ...;

  // projectId 变化时清空栈
  useEffect(() => { setUndoStack([]); setRedoStack([]); }, [projectId]);

  return { undoStack, redoStack, isUndoRedoing, isOperating, executeAndTrack, executeUndo, executeRedo };
}
```

增量更新函数抽取为独立的工具模块：

```ts
// ui/src/utils/fileTreeUpdate.ts
export function removeEntryFromTree(entries: FileTreeEntry[], path: string): FileTreeEntry[] { ... }
export function insertEntryToTree(entries: FileTreeEntry[], parentDir: string, newEntry: FileTreeEntry): FileTreeEntry[] { ... }
export function moveEntryInTree(entries: FileTreeEntry[], oldPath: string, newEntry: FileTreeEntry): FileTreeEntry[] { ... }
export function dirFirstAlphaAsc(a: FileTreeEntry, b: FileTreeEntry): number { ... }
```

**预期效果**：FileTreePanel.tsx 维持 ~960 行，新增逻辑分散到 `useFileTreeUndoRedo` hook（~100 行）和 `fileTreeUpdate.ts`（~50 行）。

> **v4 新增**：组件膨胀治理建议。958 行组件再加入 ~250 行 undo/redo 逻辑会严重影响可维护性，应在 Phase 1 编码时同步拆分。

## 6 工作量与风险汇总

| 阶段 | 工作量 | 后端改动 | 存量风险 | 依赖 |
|------|--------|---------|---------|------|
| Phase 3: 增量更新优化 | 6-8h | API 返回值增强（create/copy/rename + api.ts 返回值改造） | 低 | 无 |
| Phase 1: Undo/Redo (Copy/Move/Create/Rename) | 8-10h | 无 | 低 | Phase 3 |
| Phase 2: Delete Undo (系统回收站) | 4-6h | 零后端改动（trash-bridge 插件） | 低 | Phase 1 |
| **合计** | **~18-24h** | — | — | — |

**建议实施顺序**：Phase 3 → Phase 1 → Phase 2

理由：先做增量更新优化（独立价值高，改善所有操作体感），再做 Undo/Redo（依赖增量更新才流畅），最后做 Trash（最复杂但用户体验最完整）。

> **v2 变更**：工作量从 16.5h 调整为 24-30h，主要增加项：
> - api.ts 返回值改造（所有写操作 void → 响应体）
> - `preValidateUndo` 校验 + 确认对话框
> - `isFileTreeFocused` 焦点判断
> - 一致性校验 debounce
> - Trash UUID + restore 冲突规则
> - 并发操作防护
>
> **v3 变更**：工作量从 24-30h 降至 18-24h，主要减少项：
> - 移除自建 trash 目录（-6h 后端 handler + ignoredDirs）
> - 移除一致性校验 debounce（-1h）
> - trash-bridge 插件复用 exec-command-bridge 框架（-2h 独立实现）

## 7 性能对比

| 场景 | 当前 | 优化后 |
|------|------|--------|
| 单次 copy 操作 | ~300ms（全量 loadTree） | ~20ms（增量更新） |
| 连续 ⌘Z 5 次 | ~1500ms（5 次全量刷新） | ~100ms（5 次增量更新） |
| 删除大目录（1000 文件） | ~500ms（RemoveAll + loadTree） | ~20ms（系统回收站 + 增量更新） |
| Undo 内存占用 | 0 | < 10KB（100 步操作栈，有上限） |

## 9 v5 安全实施策略：对存量系统零影响保证

### 9.1 首次实施失败的根因分析

首次按 v1-v4 方案实施后出现了以下问题：

#### 9.1.1 Bug：复制/粘贴出现两个相同副本

**症状**：Copy → Paste 后，前端 entries 中出现两个相同节点，但后端目录里只有一个文件。

**根因**：方案要求将 `loadTree()` 全量刷新替换为增量更新（`insertEntryToTree`），但增量更新函数与 `loadTree` 之间存在**双写竞争**：

1. `handlePaste` 中 `copyEntry` API 调用成功后，增量更新函数 `insertEntryToTree` 将新节点插入 entries
2. 但如果任何代码路径中仍然残留了 `loadTree()` 调用（或者某个操作同时触发了增量更新 + loadTree），就会导致节点重复
3. 更关键的是：**增量更新的排序函数 `dirFirstAlphaAsc` 必须与后端 `listDir` 的排序逻辑完全一致**，否则 `insertEntryToTree` 插入的位置与 `loadTree` 返回的位置不同，React 的 key-based reconciliation 会认为是不同的节点，导致视觉上的"两个副本"

**另一个可能的根因**：方案中 `handlePaste` 的系统剪贴板检测逻辑——`readSystemClipboardFiles()` 在某些情况下会误将内部 copy 操作当作系统剪贴板粘贴，导致内部 copy 和系统剪贴板 copy 两条路径都被执行。

#### 9.1.2 Bug：撤销（Undo）无反应

**症状**：⌘Z 后视觉上无任何变化。

**根因**：

1. **undo 只做了增量更新但缺少 loadTree 兜底**：`undoSingle` 中 `removeEntryFromState`/`moveEntryInState` 是纯前端树操作，如果树结构与磁盘状态不一致（如增量更新时漏掉了子节点），undo 的增量更新可能静默失败
2. **快捷键焦点判断 `isFileTreeFocused()` 失效**：如果 `data-panel="filetree"` 属性未正确挂载到根 DOM 元素，或事件冒泡被其他组件拦截，⌘Z 根本不会触发 `executeUndo`
3. **undo 操作执行异常被静默吞掉**：如果 `preValidateUndo` 返回 `'missing'` 但确认对话框因某些原因未显示，undo 就静默跳过了

#### 9.1.3 核心教训

**增量更新和 Undo/Redo 是两个独立的、可解耦的能力，不应绑定交付**。增量更新是优化，Undo/Redo 是功能。增量更新出错会导致所有操作（包括正常操作和 undo）的前端状态与后端不一致。

---

### 9.2 安全实施的核心原则

#### 原则 1：增量更新（Phase 3）必须可降级，降级路径 = 现有 loadTree

**做法**：

- 增量更新成功 → 使用增量结果
- 增量更新失败或校验不一致 → **立即降级为 `loadTree()`**，与现有行为完全相同
- 每个增量更新操作后追加一个**轻量校验**：检查操作后的 entries 节点数是否合理（如 copy 一个文件后，目标目录的 children 数应 +1），若不一致则降级 loadTree

```ts
// 安全增量更新模式
function safeUpdateEntries(
  updater: () => FileTreeEntry[],   // 增量更新函数
  fallback: () => Promise<void>,     // 降级函数 = loadTree
): Promise<void> {
  const result = updater();
  // 轻量校验：如果结果看起来不对，降级
  if (!isValidTreeStructure(result)) {
    console.warn('Incremental update sanity check failed, falling back to loadTree');
    return fallback();
  }
  setEntries(result);
  return Promise.resolve();
}
```

#### 原则 2：Undo/Redo 不依赖增量更新，操作后始终 loadTree

**关键变更**：首次实施 Undo/Redo 时，**放弃增量更新**，每个操作完成后仍然调用 `loadTree()` 全量刷新。

**理由**：

- `loadTree` 是经过验证的、可靠的刷新方式
- Undo/Redo 本身不频繁（用户主动触发），性能开销可接受
- 消除了增量更新与 Undo/Redo 的耦合风险
- 只有在增量更新独立验证通过后，才在 Undo/Redo 中替换 loadTree

#### 原则 3：后端 API 返回值增强必须向后兼容

**做法**：

- 后端所有写操作的响应体**只增不改**：在现有字段基础上新增 `entry` 字段
- 现有不读 `entry` 字段的前端代码不受影响
- 前端 `api.ts` 的函数签名从 `Promise<void>` 改为 `Promise<ApiResponse>` 是**向前兼容的**：调用方不使用返回值时行为不变

#### 原则 4：每个变更点必须可独立验证

**做法**：

- 每个 Phase 内的改动点，逐一实施、逐一验证
- 不允许一个 PR 同时包含增量更新 + Undo/Redo + API 改造
- 每个 PR 合入前，必须验证所有现有功能（copy/paste/rename/delete/create）正常工作

---

### 9.3 修订后的实施顺序（v5）

#### Step 0：前置准备（零功能改动，零风险）

| 改动 | 文件 | 验证点 |
|------|------|--------|
| 后端 API 返回值增强（create/copy/rename 返回 entry） | `handlers_filetree.go` | 现有前端不受影响（忽略新字段） |
| 前端 `api.ts` 返回值类型改造（void → 响应体） | `api.ts` | 现有调用方不使用返回值，行为不变 |
| 新增 `statEntry` API 封装 | `api.ts` | 新增函数，零影响 |
| 根元素添加 `data-panel="filetree"` | `FileTreePanel.tsx` | 纯属性添加，无行为变更 |
| 创建 `fileTreeUpdate.ts` 工具模块 | `ui/src/utils/` | 新增文件，零影响 |
| 创建 `useFileTreeUndoRedo.ts` hook 骨架 | `ui/src/hooks/` | 新增文件，零影响 |

**验证标准**：此 Step 完成后，现有所有功能（copy/paste/rename/delete/create）必须与改动前完全一致，所有操作后仍走 `loadTree()` 刷新。

#### Step 1：增量更新（Phase 3），但保留 loadTree 兜底

| 改动 | 验证点 |
|------|--------|
| 各操作点（handlePaste/handleRenameCommit/handleNewItemCommit/handleDeleteConfirm）改为增量更新 + loadTree 兜底 | 每个操作后检查树结构是否正确 |
| 增量更新失败时降级为 loadTree | 手动触发异常，验证降级路径 |

**关键代码模式**：

```ts
// handleNewItemCommit 改造
const handleNewItemCommit = async (name: string) => {
  if (!newItem || !projectId) return;
  const fullPath = newItem.parentPath === '.' ? name : `${newItem.parentPath}/${name}`;
  try {
    const result = newItem.isDir
      ? await createFolder(fullPath, projectId)
      : await createFile(fullPath, projectId);
    setNewItem(null);

    // 尝试增量更新
    try {
      const newEntry = result.entry as FileTreeEntry;
      if (newEntry) {
        const updated = insertEntryToTree(entries, newItem.parentPath, newEntry);
        if (isValidTreeStructure(updated)) {
          setEntries(updated);
          return; // 增量更新成功，无需 loadTree
        }
      }
    } catch {
      // 增量更新失败，降级
    }
    await loadTree(); // 兜底
  } catch (e: unknown) {
    alert((e as Error).message);
  }
};
```

**验证标准**：此 Step 完成后，所有操作应正常工作。如果增量更新有问题，自动降级到 loadTree，用户不会感知到任何异常。

#### Step 2：Undo/Redo（Phase 1），操作后仍用 loadTree

| 改动 | 验证点 |
|------|--------|
| 集成 `useFileTreeUndoRedo` hook | undo/redo 栈管理正确 |
| handlePaste/handleNewItemCommit/handleRenameCommit 中压入 undoStack | 操作后 ⌘Z 可撤回 |
| ⌘Z / ⌘Shift+Z 快捷键 | 仅在 FileTree 焦点时生效 |
| undo/redo 执行后 loadTree 刷新 | 撤回后树结构正确 |

**关键约束**：此阶段 undo/redo 执行完成后**始终调用 `loadTree()`**，不使用增量更新。

```ts
async function executeUndo(op: UndoableOperation) {
  setIsUndoRedoing(true);
  try {
    // ... preValidate + undoSingle ...
    await loadTree(); // 安全兜底：undo 后全量刷新
  } finally {
    setIsUndoRedoing(false);
  }
}
```

**验证标准**：所有现有功能 + undo/redo 正常工作。undo 后全量刷新确保状态一致。

#### Step 3：Undo/Redo 改用增量更新（性能优化）

| 改动 | 验证点 |
|------|--------|
| undo/redo 执行后改用增量更新 | undo/redo 后树结构正确 |
| 保留 loadTree 兜底 | 增量更新异常时降级 |

**这是可选优化**。只有当 Step 1 的增量更新稳定运行后，才执行此步。

#### Step 4：Delete Undo（Phase 2）

与 v4 方案相同，依赖 trash-bridge 插件。

---

### 9.4 防止"两个相同副本"Bug 的专项措施

**根因**：增量更新的 `insertEntryToTree` 插入节点后，如果同时触发了 `loadTree()`，或排序函数与后端不一致，会出现重复节点。

**专项措施**：

1. **确保每个操作点只有一个刷新路径**：
   - 增量更新成功 → `setEntries(updated)` → **return**（不走 loadTree）
   - 增量更新失败 → `await loadTree()` → **return**
   - 绝不同时执行增量更新和 loadTree

2. **排序函数与后端严格一致**：
   - 前端 `dirFirstAlphaAsc` 必须与后端 `listDir` 中的 `sort.Slice` 逻辑完全匹配
   - 后端排序：目录优先 → 字母升序（`strings.ToLower`）
   - 前端排序：`a.is_dir !== b.is_dir ? (a.is_dir ? -1 : 1) : a.name.toLowerCase().localeCompare(b.name.toLowerCase())`
   - **注意**：`localeCompare` 和 Go 的 `<` 比较在 Unicode 字符上可能有差异，对纯 ASCII 文件名一致

3. **key 稳定性**：TreeNodeItem 使用 `entry.path` 作为 React key，增量更新不会改变已有节点的 path，确保 React reconciliation 正确

4. **`handlePaste` 中的 siblings 追踪**：
   - 当前代码在 copy 循环中 `siblings.push({ name: newName, ... })` 用于避免重名冲突
   - 这个 siblings 是对 `findEntriesAtPath` 返回的**引用**的修改，会修改原 entries 树中的对象
   - 增量更新模式下，这个 siblings push 会导致 entries 被意外修改
   - **修复**：增量更新模式下不再使用 siblings push，而是每次 copy 前重新 `findEntriesAtPath` 获取最新 siblings

### 9.5 防止"撤销无反应"Bug 的专项措施

**根因**：快捷键焦点判断失效、undo 静默失败、增量更新静默失败。

**专项措施**：

1. **快捷键注册在 window 上 + 使用 capture 阶段**：
   ```ts
   window.addEventListener('keydown', handleKeyDown, true); // capture = true
   ```
   确保在事件冒泡前拦截 ⌘Z

2. **undo 执行后始终 loadTree 兜底**（Step 2 的核心约束）

3. **undo 失败时给用户明确反馈**：
   - 不静默跳过，弹出 Toast
   - 栈操作（pop/push）在 undo API 调用成功后才执行

4. **开发期间添加调试日志**：
   ```ts
   console.log('[UndoRedo]', 'executeUndo', op.type, 'stack size:', undoStack.length);
   ```
   便于定位问题

### 9.6 对存量影响的逐项审计

v5 方案中，以下改动点**直接修改了存量代码**，必须逐一审计风险：

#### Step 0：前置准备

| 改动点 | 存量文件 | 修改内容 | 风险 | 能否做到零影响 |
|--------|---------|---------|------|--------------|
| 后端 create/copy/rename 返回 entry | `handlers_filetree.go` | `writeJSON` 响应体新增 `entry` 字段 | **极低**：JSON 新增字段，前端不读则忽略 | **是** |
| 前端 `createFile` 返回值 void→响应体 | `api.ts:803` | 函数签名变更 | **低**：调用方不使用返回值时行为不变 | **是**（TypeScript 类型变更不影响运行时） |
| 前端 `createFolder` 返回值 void→响应体 | `api.ts:826` | 同上 | **低** | **是** |
| 前端 `copyEntry` 返回值 void→响应体 | `api.ts:851` | 同上 | **低** | **是** |
| 前端 `renameEntry` 返回值 void→响应体 | `api.ts:864` | 同上 | **低** | **是** |
| 新增 `statEntry` 函数 | `api.ts` | 新增函数 | **零** | **是** |
| 根元素添加 `data-panel` | `FileTreePanel.tsx:843` | JSX 属性 | **零** | **是** |
| 新增 `fileTreeUpdate.ts` | 新文件 | — | **零** | **是** |
| 新增 `useFileTreeUndoRedo.ts` | 新文件 | — | **零** | **是** |

**Step 0 结论：可以做到对存量零影响。**

#### Step 1：增量更新

| 改动点 | 存量文件 | 修改内容 | 风险 |
|--------|---------|---------|------|
| `handleNewItemCommit` | `FileTreePanel.tsx:787-798` | 将 `await loadTree()` 替换为增量更新 + loadTree 兜底 | **中**：修改了核心操作路径 |
| `handleRenameCommit` | `FileTreePanel.tsx:750-761` | 同上 | **中** |
| `handlePaste` | `FileTreePanel.tsx:521-541` | 同上 | **中**：paste 逻辑最复杂 |
| `handleDeleteConfirm` | `FileTreePanel.tsx:765-783` | 同上 | **中** |

**Step 1 结论：无法做到零影响——必须修改 4 个操作函数的核心路径。但通过 loadTree 兜底，最坏情况等价于现有行为。**

#### Step 2：Undo/Redo

| 改动点 | 存量文件 | 修改内容 | 风险 |
|--------|---------|---------|------|
| `handleNewItemCommit` | `FileTreePanel.tsx:787-798` | 新增 `executeAndTrack()` 调用 | **中**：在操作成功后追加逻辑 |
| `handleRenameCommit` | `FileTreePanel.tsx:750-761` | 同上 | **中** |
| `handlePaste` | `FileTreePanel.tsx:521-541` | 同上 | **中** |
| 键盘事件监听 | `FileTreePanel.tsx:700-746` | 新增 ⌘Z/⌘Shift+Z 分支 | **中**：在现有 keydown handler 中追加条件分支 |

**Step 2 结论：无法做到零影响——必须修改 4 个操作函数 + 键盘监听。但所有修改都是"追加逻辑"，不改变现有分支的执行路径。**

---

### 9.7 如何真正将对存量影响降到零

**核心洞察**：Step 1（增量更新）和 Step 2（Undo/Redo）都**必须修改存量代码**，无法绕过。但可以通过以下策略确保存量功能不受影响：

#### 策略 1：Step 1 用 Feature Flag 控制

```ts
const USE_INCREMENTAL_UPDATE = false; // 开关，默认关闭

const handleNewItemCommit = async (name: string) => {
  // ... 现有代码完全不变 ...
  if (USE_INCREMENTAL_UPDATE) {
    // 尝试增量更新，失败则 loadTree
  } else {
    await loadTree(); // 现有行为
  }
};
```

**效果**：合入主分支后默认关闭增量更新，存量行为完全不变。通过开关逐步灰度验证。

#### 策略 2：Step 2 的 undo 入栈逻辑放在操作成功后的"追加段"

```ts
const handleNewItemCommit = async (name: string) => {
  // ... 现有代码（createFile + loadTree）完全不变 ...
  // ↓ 新增：undo 入栈（追加在现有逻辑之后，不影响现有执行路径）
  if (result?.entry) {
    pushUndoStack({ type: 'create', path: fullPath, isDir: newItem.isDir, modTime: result.entry.mod_time });
  }
};
```

**关键**：undo 入栈是**幂等追加**，不改变现有代码的执行路径和返回值。即使入栈逻辑有 bug，最坏情况是 undo 不工作，不会影响正常的 create/rename/copy/delete。

#### 策略 3：⌘Z 快捷键用最早返回模式

```ts
// 在现有 keydown handler 的最前面追加，不修改现有分支
if (isMod && e.key === 'z' && isFileTreeFocused()) {
  if (!isShift && undoStack.length > 0) {
    e.preventDefault();
    executeUndo(undoStack[undoStack.length - 1]);
    return; // 最早返回，不进入后续逻辑
  }
  if (isShift && redoStack.length > 0) {
    e.preventDefault();
    executeRedo(redoStack[redoStack.length - 1]);
    return;
  }
}
// ... 现有 ⌘C/⌘X/⌘V/F2/Delete 逻辑完全不变 ...
```

**效果**：⌘Z 只在 FileTree 焦点时拦截，其他快捷键完全不受影响。

#### 策略 4：undo 执行后始终 loadTree

这是最关键的安全网。undo 执行后全量刷新，**保证前端状态与后端一致**，即使 undo 的增量操作有 bug，loadTree 也会修正。

#### 策略 5：改动存量文件前必须备份

**每改动一个存量文件前，先创建备份副本**，确保出问题时可秒级回滚到改动前状态。

**备份规范**：

| 规则 | 说明 |
|------|------|
| 备份位置 | 在文件同级目录下创建 `.{filename}.bak`，如 `FileTreePanel.tsx` → `.FileTreePanel.tsx.bak` |
| 备份时机 | 修改存量文件**之前**立即备份，不延后 |
| 备份内容 | 完整复制原文件，不做任何修改 |
| 保留策略 | 当前 Step 验证通过后**不删除备份**，保留至整个 Undo/Redo 功能全部稳定上线后再清理 |
| 回滚方式 | `cp .{filename}.bak {filename}` 即可恢复到改动前状态 |

**需要备份的存量文件清单**：

| Step | 文件 | 备份文件名 |
|------|------|-----------|
| Step 0 | `ui/src/services/api.ts` | `ui/src/services/.api.ts.bak` |
| Step 0 | `ui/src/components/FileTreePanel.tsx` | `ui/src/components/.FileTreePanel.tsx.bak` |
| Step 1 | 同上（已在 Step 0 备份） | — |
| Step 2 | 同上（已在 Step 0 备份） | — |
| Step 0 | `internal/api/handlers_filetree.go` | `internal/api/.handlers_filetree.go.bak` |

**注意**：`FileTreePanel.tsx` 和 `api.ts` 在多个 Step 中都会被修改，只需在首次修改前备份一次。备份记录的是 Step 0 之前（即当前线上版本）的完整内容，这是最终回滚锚点。

**Git 辅助**：除文件备份外，每个 Step 开始前应在 git 中打 tag，如 `git tag pre-undo-step0`、`git tag pre-undo-step1`，提供双重回滚能力。

#### 综合结论

| Step | 对存量影响 | 能否做到零影响 | 安全网 |
|------|-----------|--------------|--------|
| **0** | 零 | **是** | — |
| **1** | 中（修改 4 个操作函数） | **否，但可用 Feature Flag 关闭** | 开关关闭 = 现有行为 |
| **2** | 中（追加逻辑 + 快捷键） | **否，但所有修改是"追加"不改变现有路径** | undo 入栈失败不影响正常操作；⌘Z 用最早返回模式 |
| **3** | 低（可选优化） | **否** | loadTree 兜底 |
| **4** | 低（新增 trash API） | **否，但降级为永久删除 = 现有行为** | trash 失败走永久删除 |

**最终结论**：Step 0 可以做到绝对零影响。Step 1-4 无法做到零修改，但可以通过 **Feature Flag + 追加逻辑 + loadTree 兜底 + 最早返回 + 存量文件备份** 五重保护，确保存量功能在**任何情况下**不劣于现有行为。最坏情况是新增功能（增量更新/undo）不工作，但现有 copy/paste/rename/delete/create 一定正常。且任何 Step 出问题均可秒级回滚到改动前版本。

### 9.8 每步的回归测试清单

每个 Step 完成后，必须逐项验证：

| 序号 | 测试场景 | 预期结果 |
|------|---------|---------|
| 1 | 创建文件 | 文件出现在正确位置，无重复 |
| 2 | 创建文件夹 | 文件夹出现在正确位置，可展开 |
| 3 | 重命名文件 | 文件名更新，路径正确 |
| 4 | 重命名文件夹 | 文件夹名更新，子文件不受影响 |
| 5 | 复制文件（⌘C → ⌘V） | 副本出现，原文保留，无重复 |
| 6 | 复制文件夹 | 副本（含子文件）出现，原文保留 |
| 7 | 剪切文件（⌘X → ⌘V） | 文件移动到新位置，原位置消失 |
| 8 | 剪切文件夹 | 文件夹移动到新位置，含子文件 |
| 9 | 删除文件 | 文件消失 |
| 10 | 删除文件夹 | 文件夹及内容消失 |
| 11 | 复制重名文件 | 自动加 "copy" 后缀 |
| 12 | 外部文件粘贴 | 从系统剪贴板粘贴正常 |
| 13 | 多选操作 | Shift/Cmd 多选后批量操作正常 |
| 14 | 刷新按钮 | 手动刷新后树结构正确 |

Step 2 额外测试：

| 序号 | 测试场景 | 预期结果 |
|------|---------|---------|
| 15 | ⌘Z 撤销创建 | 文件/文件夹被删除 |
| 16 | ⌘Z 撤销复制 | 副本被删除，原文保留 |
| 17 | ⌘Z 撤销剪切 | 文件回到原位置 |
| 18 | ⌘Z 撤销重命名 | 文件名恢复 |
| 19 | ⌘Shift+Z 重做 | 重做撤销的操作 |
| 20 | 多次 ⌘Z | 连续撤回多步 |
| 21 | 新操作清空 redo | 操作后 ⌘Shift+Z 无效 |
| 22 | CodePanel ⌘Z 不受影响 | 焦点在编辑器时 ⌘Z 不触发 FileTree undo |

## 10 v5 实施状态：已实现与未实现清单

### 10.1 已实现（Step 0 + Step 1 + Step 2）

| 文档条目 | 改动文件 | 说明 |
|---------|---------|------|
| **4.1.1** 操作类型定义 | `useFileTreeUndoRedo.ts` | FileOperation / CompoundOperation / ValidateResult |
| **4.1.2** 状态管理 | `useFileTreeUndoRedo.ts` | undoStack / redoStack / isUndoRedoing / isOperating + MAX 100 + projectId 清空 |
| **4.1.3** 操作执行与入栈 | `FileTreePanel.tsx` | pushUndoStack 在 create/copy/cut/rename 后入栈，compound 批量包装 |
| **4.1.4** Undo 执行 | `useFileTreeUndoRedo.ts` | executeUndo + preValidateUndo + undoSingle，undo 后 loadTree 兜底 |
| **4.1.5** Redo 执行 | `useFileTreeUndoRedo.ts` | executeRedo + redoSingle，redo 后 loadTree 兜底 |
| **4.1.6** 快捷键 | `FileTreePanel.tsx` | ⌘Z / ⌘Shift+Z，panelRef + :hover 焦点判断（比文档的 data-panel 方案更可靠） |
| **4.1.7** 涉及修改的调用点 | `FileTreePanel.tsx` | handlePaste(copy/cut) / handleNewItemCommit / handleRenameCommit 均入栈 |
| **4.1.4a** statEntry 封装 | `api.ts` | 新增 statEntry 函数 |
| **4.1.4a** data-panel 属性 | `FileTreePanel.tsx` | 根元素添加 data-panel="filetree" + ref |
| **4.3.2** 后端 API 返回值增强 | `handlers_filetree.go` | create/copy/rename 均返回 entry，目录含子树 |
| **4.3.3** 前端 api.ts 返回值改造 | `api.ts` | createFile / createFolder / copyEntry / renameEntry 均改为返回响应体 |
| **4.3.4** 前端增量更新函数 | `fileTreeUpdate.ts` | insert / remove / move / isValidTreeStructure / dirFirstAlphaAsc（不可变版本） |
| **4.3.5** 各操作改为增量更新 | `FileTreePanel.tsx` | 4 个操作函数均增量更新 + loadTree 兜底 + isValidTreeStructure 校验 |
| **5.1a** modTime 比对 | `useFileTreeUndoRedo.ts` | preValidateUndo 中 create/copy 检测 modified |
| **5.4** 跨项目操作 | `useFileTreeUndoRedo.ts` | useEffect([projectId]) 清空栈 |

### 10.2 未实现

| 文档条目 | 优先级 | 说明 | 预计工作量 |
|---------|--------|------|-----------|
| **4.1.4** missing/modified 确认对话框 | **高** | preValidateUndo 返回 missing/modified 时，文档要求弹出 ConfirmDialog 让用户确认，当前只 console.warn 直接跳过或继续 | 1-2h |
| **4.1.4a** Toast 替换 alert | **中** | 文档要求 FileTreePanel 中所有 `alert()` 替换为非阻塞 Toast，当前仍用 alert() 阻塞交互 | 1h |
| **4.1.8** 并发操作防护 isOperating | **中** | hook 中有 isOperating/setIsOperating state，但 FileTreePanel 中未使用。文档要求操作执行期间防止重复触发 | 0.5h |
| **5.2** 操作被丢弃时 Toast 提示 | **中** | isOperating/isUndoRedoing 期间操作应弹 Toast 而非静默丢弃 | 0.5h |
| **5.7** undo/redo API 错误 Toast | **低** | undo/redo 执行失败时应弹 Toast 错误提示，当前只 console.error | 0.5h |
| **4.2** Delete Undo（Phase 2） | **低** | 需要 trash-bridge 插件，当前 delete 不入栈。涉及新增 trash API + handleDeleteConfirm 改造 | 4-6h |
| **Step 3** undo/redo 改用增量更新 | **低** | 当前 undo/redo 后 loadTree 全量刷新（安全但慢），可优化为增量更新 | 1-2h |
| **i18n** 国际化 | **低** | 文档中引用的 t('fileTree.undoConflictTitle') 等 i18n key 未添加到 locale 文件 | 0.5h |

## 8 修订记录

| 版本 | 日期 | 变更摘要 |
|------|------|---------|
| v1 | — | 初始方案 |
| v2 | 2026-05-26 | 评审后修订：新增 undo 栈上限、preValidate 校验、焦点冲突处理、一致性校验机制、并发防护、trashId UUID、create API 返回 stat、api.ts 返回值改造、工作量复核 |
| v3 | 2026-05-26 | 评审后修订：移除自建 .axons-trash/ 目录，改为系统回收站（trash-bridge 插件）；移除 scheduleConsistencyCheck，改为 fallback loadTree + fsnotify 事件驱动；Delete 不可逆时不入 undo 栈；工作量从 24-30h 降至 18-24h |
| v4 | 2026-05-26 | 生产落地评审修订：1) Phase 3 补齐目录操作返回子树实现（listDir）、dirFirstAlphaAsc 排序函数定义；2) Phase 1 明确部分失败入栈策略（只入栈成功子操作）；3) 新增 undo create/copy 时文件已修改保护（modTime 比对 + 三态 preValidate）；4) 明确 undo move CONFLICT 时栈处理（正常 pop/push + loadTree 降级）；5) isUndoRedoing/isOperating 期间操作丢弃改为 Toast 提示；6) 新增前端前置改动清单（api.ts statEntry 封装、data-panel 属性、Toast 升级）；7) 新增组件膨胀治理建议（拆分 useFileTreeUndoRedo hook + fileTreeUpdate.ts） |
| v5 | 2026-05-26 | 安全实施策略修订：1) 分析首次实施失败根因（双写竞争导致副本重复、undo 静默失败）；2) 核心原则：增量更新可降级、Undo/Redo 不依赖增量更新、后端 API 只增不改、每步独立验证；3) 实施顺序改为 Step 0→1→2→3→4，Undo/Redo 阶段仍用 loadTree 兜底；4) 专项措施防止副本重复（单刷新路径、排序一致、key 稳定）；5) 专项措施防止撤销无反应（capture 模式、loadTree 兜底、Toast 反馈）；6) 新增每步回归测试清单；7) 存量影响逐项审计（Step 0 零影响，Step 1-4 不可零影响但有五重保护）；8) 五重保护：Feature Flag + 追加逻辑 + loadTree 兜底 + 最早返回 + 存量文件备份；9) 改动存量文件前必须备份 + git tag 双重回滚 |
| v6 | 2026-05-26 | 实施状态核对：新增 Section 10 已实现/未实现清单。未实现项：missing/modified 确认对话框（高）、Toast 替换 alert（中）、isOperating 并发防护（中）、操作丢弃 Toast（中）、undo 错误 Toast（低）、Delete Undo Phase 2（低）、undo 增量更新优化（低）、i18n（低） |
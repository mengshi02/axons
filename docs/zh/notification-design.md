# Axons 消息通知系统设计方案

> 版本: v1.0 | 日期: 2026-05-20 | 状态: 已实现

## 一、背景与动机

### 1.1 当前痛点

Axons 缺少统一的消息通知机制。构建完成、插件崩溃、模型下载进度等关键事件仅通过 SSE 事件在各自组件中分散处理，用户无法获得全局视角：

- **构建完成**：仅在 GraphCanvas 静默刷新，无显式通知
- **插件崩溃**：SSE 推送 `plugin.crashed`，仅在前端日志中可见
- **长时任务进度**（安装插件、下载模型）：进度信息分散在各个面板内，切换面板后丢失

### 1.2 需求

1. **统一入口**：铃铛图标 + 通知面板，聚合所有来源的消息
2. **双源支持**：宿主（daemon）和插件均可发送通知
3. **实时性**：新通知通过 SSE 实时推送到前端，铃铛即时显示未读计数
4. **持久化**：通知存储在 SQLite，刷新页面不丢失
5. **前薄后厚**：后端承担 CRUD + 推送逻辑，前端仅渲染

### 1.3 设计原则

遵循项目既有架构原则（参见 [plugin-system-design.md](plugin-system-design.md) §2.1 和 [plugin-ui-isolation-design.md](plugin-ui-isolation-design.md) §1.4）：

| 层级 | 职责 | 不做的事 |
|------|------|---------|
| **前端（薄）** | 渲染铃铛/面板/toast、订阅 SSE 通知事件、调用 API 标记已读 | 不做通知存储、不做去重、不做分发 |
| **后端（厚）** | 通知 CRUD、权限校验、SSE 推送、自动清理 | — |

---

## 二、数据模型

### 2.1 Notification 结构

```go
// Notification 代表一条通知消息
type Notification struct {
    ID        string    `json:"id"`         // UUID v4
    Source     string    `json:"source"`     // "host" | pluginId（如 "com.axons.huggingface"）
    Type      string    `json:"type"`       // info | warning | error | success | progress
    Title     string    `json:"title"`      // 通知标题
    Message   string    `json:"message"`    // 通知正文（可选）
    Group     string    `json:"group"`      // 分组标识（可选，三期通知分组功能预留）
    Actions   []Action  `json:"actions"`    // 可选操作按钮（二期）
    Read      bool      `json:"read"`       // 是否已读
    Timestamp time.Time `json:"timestamp"`  // 创建时间
}

// Action 代表通知上的可交互按钮
type Action struct {
    ID    string `json:"id"`     // 操作标识
    Label string `json:"label"`  // 按钮文字
    URL   string `json:"url"`    // 点击跳转地址（可选，如 "panel://huggingface"）
}
```

### 2.2 Type 定义

| Type | 图标 | 场景 |
|------|------|------|
| `info` | ℹ️ 蓝色 | 一般信息通知（如：插件已启动） |
| `success` | ✓ 绿色 | 操作成功（如：构建完成、模型下载完成） |
| `warning` | ⚠ 黄色 | 需要注意的情况（如：插件重启 2 次） |
| `error` | ✕ 红色 | 操作失败（如：构建失败、插件崩溃） |
| `progress` | ⟳ 紫色 | 进行中的操作（如：模型下载 67%） |

**progress 特殊处理**：同一条 progress 通知会不断更新（同 `source` + 同 `title` 的 progress 通知复用同一条记录，更新 `message` 字段），而非创建新记录。前端收到 progress 类型通知时可展示进度条。当进度完成时，由发送方再次 POST 同 `title` 的通知，将 `type` 设为 `success` 或 `error`——daemon 会自动将同 source+title 的已有 progress 记录更新为终态（见 §3.6）。

**progress→终态转换规则**：当 POST 请求的 `type` 为非 `progress`（如 `success`/`error`），且同 `source` + `title` 已存在一条 `progress` 记录时，daemon 将该 progress 记录的 type 更新为请求中的 type，message 更新为请求中的 message，而非创建新记录。这样发送方无需记住通知 ID，只需继续 POST 同 title 的通知即可完成终态转换。

### 2.3 Source 规则

| 来源 | source 值 | 填充方式 |
|------|----------|---------|
| 宿主（daemon 内部） | `"host"` | Go 代码硬编码 |
| 插件后端 | 插件 ID（如 `"com.axons.huggingface"`） | daemon 从 `AXONS_PLUGIN_TOKEN` 鉴权后自动填充 |

**防伪造**：插件无法自行指定 source 字段。POST `/v1/notifications` 请求体中的 `source` 字段（如存在）将被忽略，由 daemon 根据请求的 Token 查找对应插件 ID 后覆盖填充。未携带 Token 的请求 source 固定为 `"host"`（一期仅限 localhost 访问，安全性由网络层保证）。

### 2.4 数据库 Schema

通知存储在主数据库（`axons.db`），作为 `migrateMainV7` 迁移新增：

```sql
CREATE TABLE IF NOT EXISTS notifications (
    id         TEXT PRIMARY KEY,
    source     TEXT    NOT NULL,
    type       TEXT    NOT NULL DEFAULT 'info',
    title      TEXT    NOT NULL,
    message    TEXT    DEFAULT '',
    actions    TEXT    DEFAULT '[]',         -- JSON array (SQLite 3.38+ 可改用 json 类型以支持 json_each() 查询)
    "group"    TEXT    DEFAULT '',            -- 分组标识（三期预留，当前为空）
    read       INTEGER NOT NULL DEFAULT 0,   -- 0=未读, 1=已读
    timestamp  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 未读查询高频索引
CREATE INDEX IF NOT EXISTS idx_notifications_unread
    ON notifications(read, timestamp DESC);

-- 按 source 过滤索引（如：查看某插件的所有通知）
CREATE INDEX IF NOT EXISTS idx_notifications_source
    ON notifications(source, timestamp DESC);

-- 按 group 分组索引（三期预留）
CREATE INDEX IF NOT EXISTS idx_notifications_group
    ON notifications("group", timestamp DESC);
```

**容量策略**（两级清理）：

1. **已读通知软上限**：保留最近 150 条已读通知。每次创建新通知时，daemon 检查已读通知总数，超出则自动清理最旧的已读记录（同步执行，单次 SQL）：

```sql
DELETE FROM notifications
WHERE read = 1 AND id NOT IN (
    SELECT id FROM notifications WHERE read = 1
    ORDER BY timestamp DESC LIMIT 150
);
```

2. **总量硬上限**：无论已读未读，总量不超过 500 条。超出时强制清理最旧的记录（即使未读也清理，按 timestamp 排序）：

```sql
DELETE FROM notifications
WHERE id NOT IN (
    SELECT id FROM notifications
    ORDER BY timestamp DESC LIMIT 450
);
```

> 保留 150 条已读 + 50 条未读（软上限）+ 硬上限 500 条防止未读无限增长。阈值可通过配置调整。

**TTL 自动已读**（二期）：`info` 类型通知在 24 小时后自动标记为已读，减少信息噪音。实现方式：daemon 启动时执行一次批量更新，后续可通过定时任务或创建通知时顺带清理。

---

## 三、后端 API 设计

### 3.1 API 路由

所有通知 API 注册在 `httprouter` 的 `/v1/notifications` 前缀下。

| 路由 | 方法 | 说明 | 鉴权 |
|------|------|------|------|
| `/v1/notifications` | GET | 获取通知列表 | 无 |
| `/v1/notifications` | POST | 发送通知 | 插件需 Token |
| `/v1/notifications/:id/read` | PUT | 标记单条已读 | 无 |
| `/v1/notifications/read-all` | PUT | 全部标记已读 | 无 |
| `/v1/notifications/:id` | DELETE | 删除单条通知 | 无 |
| `/v1/notifications/unread-count` | GET | 获取未读数量 | 无 |

### 3.2 请求/响应格式

#### GET /v1/notifications

查询参数：

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `unread` | bool | false | 仅返回未读通知 |
| `source` | string | — | 按来源过滤（如 `host` 或插件 ID） |
| `type` | string | — | 按类型过滤 |
| `limit` | int | 50 | 每页数量（最大 100） |
| `offset` | int | 0 | 偏移量 |

响应：

```json
{
  "notifications": [
    {
      "id": "a1b2c3d4-...",
      "source": "com.axons.huggingface",
      "type": "success",
      "title": "Model Download Complete",
      "message": "llama3 is ready to use",
      "actions": [
        {"id": "open-manager", "label": "Open Hugging Face", "url": "panel://huggingface"}
      ],
      "read": false,
      "timestamp": "2026-05-20T10:30:00Z"
    }
  ],
  "total": 42,
  "unreadCount": 5
}
```

#### POST /v1/notifications

请求体：

```json
{
  "type": "success",
  "title": "Model Download Complete",
  "message": "llama3 is ready to use",
  "actions": [
    {"id": "open-manager", "label": "Open Hugging Face", "url": "panel://huggingface"}
  ]
}
```

- `source` 字段**不需要**由调用方填写，daemon 自动填充
- `type` 默认 `"info"`
- `actions` 可选，默认空数组
- 对于 `progress` 类型，若同 `source` + `title` 已存在 progress 通知，则更新已有记录而非创建新记录

响应：

```json
{
  "id": "a1b2c3d4-...",
  "source": "com.axons.huggingface",
  "type": "success",
  "title": "Model Download Complete",
  "message": "llama3 is ready to use",
  "actions": [...],
  "read": false,
  "timestamp": "2026-05-20T10:30:00Z"
}
```

#### PUT /v1/notifications/:id/read

响应：`204 No Content`

#### PUT /v1/notifications/read-all

响应：`204 No Content`

#### DELETE /v1/notifications/:id

响应：`204 No Content`

#### GET /v1/notifications/unread-count

响应：

```json
{
  "count": 5
}
```

### 3.3 插件鉴权

插件后端通过 `POST /v1/notifications` 发送通知时，需在请求头中携带 Token：

```
Authorization: Bearer <AXONS_PLUGIN_TOKEN>
```

daemon 收到请求后：

1. 提取 `Authorization` 头中的 Token
2. 在 `PluginManager` 中查找 Token 对应的插件 ID
3. 若找到 → `source` 填充为该插件 ID
4. 若未找到 → 返回 `401 Unauthorized`
5. 无 `Authorization` 头 → `source` 填充为 `"host"`（一期信任 localhost，二期增加宿主 API Key）

**输入校验**：
- `title`：必填，最大 200 字符
- `message`：可选，最大 1000 字符
- `actions`：最多 3 个，每个 `label` 最大 50 字符，`url` 最大 500 字符
- 超出限制返回 `400 Bad Request`

**鉴权边界**（一期→二期规划）：
- **一期**：除 POST 需插件 Token + 权限校验外，GET/PUT/DELETE 端点无鉴权。安全性由 localhost 网络层保证
- **二期**：引入宿主 API Key，所有写操作（POST/PUT/DELETE）需鉴权；GET 端点维持无鉴权（通知为用户级数据）

### 3.4 SSE 推送

复用现有 [`EventBroker`](../internal/api/events.go:71)，新增事件类型：

```go
// 在 internal/api/events.go 新增
EventNotification EventType = "notification"
```

当 `NotificationService.Create()` 创建或更新通知后，通过 `EventBroker` 广播 `notification` 事件。事件 `data` 中包含 `action` 字段，区分新建（`"created"`）和更新（`"updated"`）：

```go
func (s *NotificationService) broadcastNotification(n *Notification, action string) {
    s.eventBroker.Broadcast(Event{
        Type:      EventNotification,
        Timestamp: time.Now(),
        Data: map[string]interface{}{
            "action":    action,                      // "created" | "updated"
            "id":        n.ID,
            "source":    n.Source,
            "type":      n.Type,
            "title":     n.Title,
            "message":   n.Message,
            "actions":   n.Actions,
            "read":      n.Read,
            "timestamp": n.Timestamp,
        },
    })
}
```

> 前端根据 `action` 字段决定处理方式：`created` → prepend 到列表 + unreadCount++ + 弹 Toast；`updated` → 查找并更新已有条目（如 progress 进度变化），不弹 Toast、不增加 unreadCount。

**前端订阅**：通过已有的 `/v1/events` SSE 连接（[`useEventStream`](../ui/src/hooks/useEventStream.ts:178)），新增 `notification` 事件监听。无需新建 SSE 连接。

**PluginEventBus 同步**：新通知同时通过 [`PluginEventBus.Emit()`](../internal/plugin/eventbus.go:78) 广播，使 iframe 内的插件也能通过 `/v1/plugins/events/stream` 收到通知事件：

```go
// 通知事件同时推送到 PluginEventBus
GetGlobalBus().Emit(Event{
    PluginID: n.Source,
    Type:     "notification",
    Payload:  notificationPayload,
})
```

### 3.5 宿主内部调用

daemon 内部代码直接调用 `NotificationService`，无需走 HTTP：

```go
// 示例：构建完成时发送通知
func (s *Server) onBuildComplete(projectID string, stats BuildStats) {
    s.notificationService.Create(context.Background(), &Notification{
        Source:  "host",
        Type:    "success",
        Title:   "Build Complete",
        Message: fmt.Sprintf("%d nodes, %d edges created", stats.NodesCreated, stats.EdgesCreated),
    })
}
```

### 3.6 progress 类型更新机制

progress 通知采用"查找-更新"模式，避免重复创建。扩展 POST 语义支持 progress→终态转换：

```go
func (s *NotificationService) Create(ctx context.Context, n *Notification) error {
    // 查找同 source + title 的现有 progress 通知
    existing, _ := s.repo.FindProgressBySourceAndTitle(ctx, n.Source, n.Title)

    if existing != nil {
        // 情况1：新通知也是 progress → 更新已有记录的 message
        // 情况2：新通知是 success/error → 将 progress 记录更新为终态
        if err := s.repo.Update(ctx, existing.ID, n.Message, n.Type); err != nil {
            return err
        }
        n.ID = existing.ID // 复用原 ID，前端可据此更新已有条目

        // SSE 广播更新事件（区分 created/updated）
        s.broadcastNotification(n, "updated")
        return nil
    }

    // 无已有 progress 记录 → 创建新记录
    if err := s.repo.Create(ctx, n); err != nil {
        return err
    }

    s.broadcastNotification(n, "created")

    // 同步清理超出容量的已读通知
    if err := s.repo.Cleanup(ctx); err != nil {
        // 清理失败不影响通知创建，记录日志即可
        log.Printf("notification cleanup failed: %v", err)
    }

    return nil
}
```

> **设计决策**：采用扩展 POST 语义而非新增 PATCH 端点。理由：progress→终态是 progress 机制的自然延伸，发送方无需记住通知 ID，只需继续 POST 同 title 的通知、type 换成 success/error 即可。

---

## 四、前端设计

### 4.1 UI 布局

铃铛图标放置在 [`TopSearchBar`](../ui/src/components/TopSearchBar.tsx) 右侧：

```
改造前：[spacer(macos traffic lights) | search-bar | spacer(window controls)]
改造后：[spacer(macos traffic lights) | search-bar | bell-icon | spacer(window controls)]
```

通知面板从铃铛下方弹出（下拉面板），Toast 在右下角浮动。

### 4.2 组件结构

#### NotificationBell

铃铛按钮组件，放置在 TopSearchBar 右侧：

```tsx
interface NotificationBellProps {
  unreadCount: number;
  onClick: () => void;
}

function NotificationBell({ unreadCount, onClick }: NotificationBellProps) {
  return (
    <button
      onClick={onClick}
      className="relative p-1.5 rounded-md hover:bg-hover transition-colors"
      title="Notifications"
    >
      <Bell className="w-4 h-4 text-text-muted" />
      {unreadCount > 0 && (
        <span className="absolute -top-0.5 -right-0.5 min-w-[14px] h-[14px]
                         flex items-center justify-center rounded-full
                         bg-red-500 text-white text-[9px] font-bold leading-none px-0.5">
          {unreadCount > 99 ? '99+' : unreadCount}
        </span>
      )}
    </button>
  );
}
```

#### NotificationPanel

点击铃铛弹出的通知列表面板：

```
┌─────────────────────────────────────┐
│  Notifications         [Mark all read] │
├─────────────────────────────────────┤
│  ● Model Download Complete     ✓     │
│    llama3 is ready to use            │
│    [Open Hugging Face]              │
│    Hugging Face · 2m ago            │
├─────────────────────────────────────┤
│  ○ Build Complete                    │
│    128 nodes, 256 edges created      │
│    Axons · 5m ago                    │
├─────────────────────────────────────┤
│  ○ Plugin Crashed                    │
│    Ollama Manager has crashed        │
│    Hugging Face · 1h ago            │
├─────────────────────────────────────┤
│         [Load more...]               │
└─────────────────────────────────────┘
```

- `●` 未读标记（圆点），`○` 已读
- 每条通知显示：**source 名称** + 相对时间。`source` 为 `"host"` 时显示 "Axons"；插件 source 显示插件 `name`（而非 ID），便于用户理解来源
- 点击通知 → 标记已读 + 执行 action（如有）
- 空 state：显示 "No notifications"
- 不同 type 使用不同图标颜色，便于快速区分
- **点击外部关闭**：通过 `useEffect` 注册 `document.addEventListener('mousedown', ...)` 检测面板外点击，关闭面板
- **滚动分页**：列表底部显示 "Load more..." 按钮（非无限滚动，避免性能问题）。点击后调用 `fetchNotifications({ offset: currentCount })` 追加加载

#### NotificationToast

新通知到达时，右下角弹出 toast 提示（3s 自动消失）：

```
┌─────────────────────────────────┐
│  ✓ Model Download Complete      │
│    llama3 is ready to use       │
│                       [Dismiss] │
└─────────────────────────────────┘
```

- 仅对新到达的未读通知显示 toast（`action="created"` 时）
- **面板打开时抑制 Toast**：当 `NotificationPanel` 已展开时，新通知不弹 Toast（避免遮挡面板），仅在面板关闭时弹出
- `error` / `warning` 类型 toast 不自动消失，需手动关闭
- 最多同时显示 3 条 toast，超出时最早的自动消失

### 4.3 前端数据流

```
SSE /v1/events ──→ useEventStream (notification 事件)
                        │
                        ▼
                useNotifications hook
                   ├── notifications: Notification[]     (全量列表)
                   ├── unreadCount: number               (未读数)
                   ├── fetchNotifications()              (初次加载/翻页)
                   ├── markAsRead(id)                    (PUT API)
                   ├── markAllAsRead()                   (PUT API)
                   ├── deleteNotification(id)            (DELETE API)
                   └── onNewNotification callback        (SSE → 更新列表 + 未读数)
```

**`useNotifications` hook**：

```typescript
interface UseNotificationsReturn {
  notifications: Notification[];
  unreadCount: number;
  loading: boolean;
  fetchNotifications: (options?: { unread?: boolean; limit?: number }) => Promise<void>;
  markAsRead: (id: string) => Promise<void>;
  markAllAsRead: () => Promise<void>;
  deleteNotification: (id: string) => Promise<void>;
  refresh: () => Promise<void>;  // SSE 重连后全量刷新
}

function useNotifications(): UseNotificationsReturn {
  // 1. 初次加载：GET /v1/notifications?limit=50
  // 2. SSE 实时：useEventStream 的 onNotification 回调
  //    - action="created" → prepend 到列表 + unreadCount++
  //    - action="updated" → 查找并更新已有条目（progress 进度变化）
  // 3. SSE 重连：onConnect 回调中调用 refresh() → 重新 fetch 全量列表 + unreadCount
  //    确保断连期间遗漏的通知和已读状态变化被补齐
  // 4. 操作：调用 API 后更新本地状态
}
```

**SSE 重连状态恢复**：`useEventStream` 的 `onConnect` 回调中调用 `refresh()`，重新从后端获取全量通知列表和 unreadCount，避免断连期间状态不一致。

### 4.4 SSE 事件集成

在 [`useEventStream.ts`](../ui/src/hooks/useEventStream.ts) 中新增 notification 事件类型：

```typescript
// EventType 新增
| 'notification'

// 事件数据接口
export interface NotificationEvent {
  id: string;
  source: string;
  type: 'info' | 'warning' | 'error' | 'success' | 'progress';
  title: string;
  message: string;
  actions: Action[];
  read: boolean;
  timestamp: string;
}

// UseEventStreamOptions 新增
onNotification?: (data: NotificationEvent) => void;

// SSE 监听新增
eventSource.addEventListener('notification', (e: MessageEvent) => {
  try {
    const event = JSON.parse(e.data) as Event;
    callbacksRef.current.onNotification?.(event.data as unknown as NotificationEvent);
  } catch (err) {
    console.error('Failed to parse notification event:', err);
  }
});
```

### 4.5 API 服务层

在 [`services/api.ts`](../ui/src/services/api.ts) 中新增：

```typescript
// 获取通知列表
export async function fetchNotifications(options?: {
  unread?: boolean;
  source?: string;
  type?: string;
  limit?: number;
  offset?: number;
}): Promise<{ notifications: Notification[]; total: number; unreadCount: number }>

// 发送通知（宿主前端场景，一般不使用，通知应由后端发起）
export async function createNotification(n: Partial<Notification>): Promise<Notification>

// 标记已读
export async function markNotificationRead(id: string): Promise<void>

// 全部标记已读
export async function markAllNotificationsRead(): Promise<void>

// 删除通知
export async function deleteNotification(id: string): Promise<void>

// 获取未读数
export async function fetchUnreadCount(): Promise<{ count: number }>
```

---

## 五、插件使用方式

### 5.1 接入流程

插件开发者接入通知系统需完成以下步骤：

```
Step 1: manifest.json 声明权限
        ↓
Step 2: 插件后端通过 HTTP API 发送通知
        ↓
Step 3:（可选）插件 iframe 内监听通知事件
```

**Step 1：声明权限**

在 `manifest.json` 的 `permissions` 数组中添加 `"notification:send"`：

```jsonc
{
  "id": "com.axons.huggingface",
  "permissions": [
    "graph:read",
    "panel:create",
    "notification:send"    // ← 必须声明，否则 POST 返回 403
  ]
}
```

> 未声明 `notification:send` 的插件调用 `POST /v1/notifications` 会返回 `403 Forbidden`。

**Step 2：后端发送通知**

插件后端启动时，Axons 注入三个环境变量（参见 [plugin-developer-guide.md §5.1](plugin-developer-guide.md)）：

| 变量 | 用途 |
|------|------|
| `AXONS_API_URL` | Axons API 地址，如 `http://127.0.0.1:8080` |
| `AXONS_PLUGIN_PORT` | 插件应绑定的端口 |
| `AXONS_PLUGIN_TOKEN` | 插件鉴权 Token（用于通知 API 的 `Authorization` 头） |

发送通知时：
1. **请求头**：`Authorization: Bearer <AXONS_PLUGIN_TOKEN>`
2. **请求体**：`type`、`title`、`message`（可选）、`actions`（可选）
3. **source 自动填充**：daemon 根据 Token 识别插件 ID 并覆盖 `source`，插件无需（也无法）自行指定
4. **桌面端/Web 端无差异**：插件后端与 daemon 在同一机器上通过 localhost HTTP 通信，无 CORS 问题

**Step 3：iframe 内监听通知（可选）**

插件 iframe 可通过 `pluginApi.onEvent('notification', callback)` 监听通知事件，用于：
- 自己发出的通知到达后做 UI 响应（如更新面板内状态）
- 监听其他来源的通知做联动

> 注意：iframe 接收的通知事件与铃铛/面板的通知来源相同，不需要重复展示。

### 5.2 通知类型选择指南

| 场景 | 推荐类型 | 示例 |
|------|---------|------|
| 操作完成 | `success` | 模型下载完成、安装成功 |
| 操作失败 | `error` | 下载失败、安装失败 |
| 需要用户注意 | `warning` | 配置异常、资源即将耗尽 |
| 一般信息 | `info` | 插件已启动、配置已更新 |
| 进行中的操作 | `progress` | 下载进度 67%、安装中 |

**progress 使用规则**：
- 同 `source` + `title` 的 progress 通知会**复用同一条记录**，更新 `message` 字段
- 进度完成时，再次 POST 同 `title` 的通知，`type` 改为 `success` 或 `error`，daemon 自动更新已有记录
- 前端不会为 progress 更新弹 Toast，只更新进度条

### 5.3 代码示例（Python 后端）

```python
# Python 插件后端示例
import os
import requests

AXONS_API_URL = os.environ["AXONS_API_URL"]
AXONS_PLUGIN_TOKEN = os.environ["AXONS_PLUGIN_TOKEN"]

def notify_download_progress(model_name: str, progress: int):
    """发送进度通知 — 同 title 的 progress 通知会复用同一条记录"""
    requests.post(f"{AXONS_API_URL}/v1/notifications", json={
        "type": "progress",
        "title": f"Downloading {model_name}",
        "message": f"{progress}% complete"
    }, headers={"Authorization": f"Bearer {AXONS_PLUGIN_TOKEN}"})

def notify_download_complete(model_name: str):
    """发送完成通知 — 同 title 的 progress 通知自动更新为终态"""
    requests.post(f"{AXONS_API_URL}/v1/notifications", json={
        "type": "success",
        "title": f"Download Complete: {model_name}",
        "message": f"{model_name} is ready to use",
        "actions": [
            {"id": "open", "label": "Open Hugging Face", "url": "panel://huggingface"}
        ]
    }, headers={"Authorization": f"Bearer {AXONS_PLUGIN_TOKEN}"})
```

### 5.4 代码示例（Go 后端）

```go
// Go 插件后端示例
func sendNotification(nType, title, message string) error {
    payload := map[string]string{
        "type":    nType,
        "title":   title,
        "message": message,
    }
    body, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST",
        os.Getenv("AXONS_API_URL")+"/v1/notifications",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+os.Getenv("AXONS_PLUGIN_TOKEN"))
    resp, err := http.DefaultClient.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("notification API returned %d", resp.StatusCode)
    }
    return nil
}
```

### 5.5 权限声明

manifest.json 新增 `notification:send` 权限：

```jsonc
{
  "id": "com.axons.huggingface",
  "permissions": [
    "graph:read",
    "notification:send"    // ← 新增：允许发送通知
  ]
}
```

未声明 `notification:send` 的插件调用 `POST /v1/notifications` 返回 `403 Forbidden`。

### 5.6 ValidPermissions 新增

在 [`internal/plugin/manifest.go`](../internal/plugin/manifest.go:137) 的 `ValidPermissions` 中新增：

```go
var ValidPermissions = map[string]bool{
    "graph:read":         true,
    "project:read":       true,
    "model:register":     true,
    "panel:create":       true,
    "state:read":         true,
    "state:write":        true,
    "notification:send":  true,  // ← 新增
}
```

### 5.7 插件 iframe 内监听通知

iframe 内的 PluginApi 已通过 SSE `/v1/plugins/events/stream` 订阅事件（参见 [iframe-adapter.ts](../ui/src/plugin-sdk/iframe-adapter.ts)）。新通知作为 `type: "notification"` 事件通过此通道推送，插件 iframe 内可直接消费：

```javascript
// iframe 内监听通知事件
const adapter = new AxonsPluginIframe.IframePluginApiAdapter();

adapter.init().then(() => {
  adapter.onEvent('notification', (notification) => {
    if (notification.source === adapter.pluginId) {
      // 自己发出的通知，可做特殊处理
      console.log('My notification was delivered:', notification.title);
    }
  });
});
```

---

## 六、与现有系统的集成

### 6.1 宿主通知场景

以下宿主内部事件应自动生成通知：

| 事件 | 通知 type | 通知 title | 触发点 |
|------|----------|-----------|--------|
| 构建完成 | `success` | Build Complete | [`handlers.go`](../internal/api/handlers.go) build 完成 |
| 构建失败 | `error` | Build Failed | build 错误 |
| 插件启动 | `info` | Plugin Started | [`manager.go`](../internal/plugin/manager.go) 启动成功 |
| 插件崩溃 | `error` | Plugin Crashed | 崩溃超出重启限制 |
| 插件安装完成 | `success` | Plugin Installed | 安装成功 |
| 插件安装失败 | `error` | Plugin Install Failed | 安装失败 |
| Embedding 完成 | `success` | Embedding Complete | embedding 完成 |

### 6.2 集成点汇总

| 现有机制 | 集成方式 |
|---------|---------|
| [`EventBroker`](../internal/api/events.go:71) | 新增 `EventNotification` 事件类型，SSE 推送新通知 |
| [`PluginEventBus`](../internal/plugin/eventbus.go:14) | 通知事件同步广播，iframe 内可订阅 |
| [`useEventStream`](../ui/src/hooks/useEventStream.ts:178) | 新增 `onNotification` 回调 |
| [`TopSearchBar`](../ui/src/components/TopSearchBar.tsx) | 右侧插入铃铛图标 |
| [`Footer`](../ui/src/components/Footer.tsx) `footerSlot='center'` | 预留：可在 Footer 中间区域显示最新通知摘要 |
| [`migrateMainV7`](../internal/db/migrations_main.go) | 新建 `notifications` 表 |

---

## 七、文件改动清单

### 7.1 后端（Go）

| 文件 | 改动类型 | 内容 |
|------|---------|------|
| `internal/notification/model.go` | **新增** | `Notification` + `Action` 结构体定义 |
| `internal/notification/repository.go` | **新增** | SQLite CRUD + progress 查找更新 + 自动清理 |
| `internal/notification/service.go` | **新增** | `NotificationService`：创建/查询/已读/删除 + EventBroker 广播 |
| `internal/api/handlers_notification.go` | **新增** | API handler（6 个端点） |
| `internal/db/migrations_main.go` | **修改** | 新增 `migrateMainV7`：创建 `notifications` 表 + 索引，`MainSchemaVersion` 升至 7 |
| `internal/api/events.go` | **修改** | 新增 `EventNotification` 常量 |
| `internal/api/server.go` | **修改** | 注入 `notificationService`，注册 `/v1/notifications` 路由 + catch-all dispatcher |
| `internal/plugin/manifest.go` | **修改** | `ValidPermissions` 新增 `"notification:send"` |
| `internal/plugin/manager.go` | **修改** | 新增 `FindPluginByToken()` 方法，供通知鉴权使用 |

### 7.2 前端（React）

| 文件 | 改动类型 | 内容 |
|------|---------|------|
| `ui/src/components/NotificationBell.tsx` | **新增** | 铃铛图标 + badge 未读计数 |
| `ui/src/components/NotificationPanel.tsx` | **新增** | 通知列表面板（弹出式） |
| `ui/src/components/NotificationToast.tsx` | **新增** | 右下角 toast 提示 |
| `ui/src/hooks/useNotifications.ts` | **新增** | 通知状态管理 hook |
| `ui/src/components/TopSearchBar.tsx` | **修改** | 右侧插入 `NotificationBell` |
| `ui/src/App.tsx` | **修改** | 包裹 `NotificationToast`（全局浮动），将 `onNotification` 回调传给 `useNotifications` |
| `ui/src/hooks/useEventStream.ts` | **修改** | 新增 `notification` 事件类型 + `onNotification` 回调 |
| `ui/src/services/api.ts` | **修改** | 新增 6 个通知 API 函数 |
| `ui/src/i18n/en/notifications.json` | **新增** | 通知相关 i18n key（独立 namespace） |
| `ui/src/i18n/index.ts`` | **修改** | 注册 `notifications` namespace + import |

### 7.3 i18n 新增 Key

通知 i18n 使用独立 namespace `notifications`（而非塞入 `common`），与项目现有的按功能拆分文件（settings/panels/chat/...）风格一致：

```json
// ui/src/i18n/en/notifications.json
{
  "title": "Notifications",
  "markAllRead": "Mark all as read",
  "noNotifications": "No notifications",
  "unreadCount": "{{count}} unread",
  "justNow": "Just now",
  "minutesAgo": "{{count}}m ago",
  "hoursAgo": "{{count}}h ago",
  "daysAgo": "{{count}}d ago"
}
```

在 [`ui/src/i18n/index.ts`](../ui/src/i18n/index.ts) 中注册新 namespace：

```typescript
import notifications from './en/notifications.json';

const enResources = {
  common, settings, panels, chat, activitybar, dropzone, extensions, notifications,
};

// ns 数组中也需新增 'notifications'
ns: ['common', 'settings', 'panels', 'chat', 'activitybar', 'dropzone', 'extensions', 'notifications'],
```

### 7.4 依赖注入与初始化顺序

`NotificationService` 的初始化需要在 `Server` 构造过程中完成，依赖链如下：

```
DB (sql.DB) → Repository → NotificationService → Server
```

在 [`NewServer()`](../internal/api/server.go) 中，`notificationService` 应在 `eventBroker` 和 `pluginManager` 之后创建：

```go
// internal/api/server.go — NewServer() 中新增
notificationService := notification.NewService(mainDB, s.eventBroker, s.pluginManager)
s.notificationService = notificationService
```

### 7.5 API 路径兼容

项目现有路由混用 `/v1/...` 和 `/api/...` 两种前缀。通知 API 统一使用 `/v1/notifications` 前缀，不额外注册 `/api/v1/notifications` 兼容路由。理由：通知系统是新功能，无历史客户端兼容需求。

---

## 八、详细实现

### 8.1 NotificationRepository

```go
// internal/notification/repository.go

type Repository struct {
    db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
    return &Repository{db: db}
}

// Create 插入新通知
func (r *Repository) Create(ctx context.Context, n *Notification) error {
    actionsJSON, _ := json.Marshal(n.Actions)
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO notifications (id, source, type, title, message, actions, read, timestamp)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
        n.ID, n.Source, n.Type, n.Title, n.Message, string(actionsJSON), n.Read, n.Timestamp,
    )
    return err
}

// FindProgressBySourceAndTitle 查找同 source + title 的 progress 通知
func (r *Repository) FindProgressBySourceAndTitle(ctx context.Context, source, title string) (*Notification, error) {
    row := r.db.QueryRowContext(ctx, `
        SELECT id, source, type, title, message, actions, read, timestamp
        FROM notifications
        WHERE source = ? AND title = ? AND type = 'progress'
        ORDER BY timestamp DESC LIMIT 1`, source, title)
    // ... scan and return
}

// Update 更新通知的 message 和 type
func (r *Repository) Update(ctx context.Context, id, message, nType string) error {
    _, err := r.db.ExecContext(ctx, `
        UPDATE notifications SET message = ?, type = ?, timestamp = CURRENT_TIMESTAMP
        WHERE id = ?`, message, nType, id)
    return err
}

// List 查询通知列表
func (r *Repository) List(ctx context.Context, opts ListOptions) ([]Notification, int, int, error) {
    // 构造 WHERE 子句：unread / source / type 过滤
    // 返回 notifications, total, unreadCount
}

// MarkRead 标记单条已读
func (r *Repository) MarkRead(ctx context.Context, id string) error { ... }

// MarkAllRead 全部标记已读
func (r *Repository) MarkAllRead(ctx context.Context) error { ... }

// Delete 删除单条
func (r *Repository) Delete(ctx context.Context, id string) error { ... }

// UnreadCount 获取未读数量
func (r *Repository) UnreadCount(ctx context.Context) (int, error) { ... }

// Cleanup 清理超出容量的已读通知
func (r *Repository) Cleanup(ctx context.Context) error { ... }
```

### 8.2 NotificationService

```go
// internal/notification/service.go

type Service struct {
    repo         *Repository
    eventBroker  *api.EventBroker
    pluginMgr    *plugin.Manager
}

func NewService(db *sql.DB, broker *api.EventBroker, pluginMgr *plugin.Manager) *Service {
    return &Service{
        repo:        NewRepository(db),
        eventBroker: broker,
        pluginMgr:   pluginMgr,
    }
}

// Create 创建或更新通知 + SSE 广播 + 自动清理
// 详见 §3.6 的完整逻辑说明
func (s *Service) Create(ctx context.Context, n *Notification) error {
    // 查找同 source + title 的现有 progress 通知
    existing, _ := s.repo.FindProgressBySourceAndTitle(ctx, n.Source, n.Title)

    if existing != nil {
        // 更新已有记录（progress 进度更新 或 progress→终态转换）
        if err := s.repo.Update(ctx, existing.ID, n.Message, n.Type); err != nil {
            return err
        }
        n.ID = existing.ID
        s.broadcastNotification(n, "updated")
        return nil
    }

    if err := s.repo.Create(ctx, n); err != nil {
        return err
    }

    // 同步清理超出容量的通知（已读软上限 + 总量硬上限）
    if err := s.repo.Cleanup(ctx); err != nil {
        log.Printf("notification cleanup failed: %v", err) // 清理失败不影响通知创建
    }

    s.broadcastNotification(n, "created")

    return nil
}

// broadcastNotification 通过 EventBroker 和 PluginEventBus 广播通知事件
func (s *Service) broadcastNotification(n *Notification, action string) {
    payload := map[string]interface{}{
        "action":    action,
        "id":        n.ID,
        "source":    n.Source,
        "type":      n.Type,
        "title":     n.Title,
        "message":   n.Message,
        "actions":   n.Actions,
        "read":      n.Read,
        "timestamp": n.Timestamp,
    }

    s.eventBroker.Broadcast(api.Event{
        Type:      api.EventNotification,
        Timestamp: time.Now(),
        Data:      payload,
    })

    plugin.GetGlobalBus().Emit(plugin.Event{
        PluginID: n.Source,
        Type:     "notification",
        Payload:  payload,
    })
}

// IdentifySource 从请求中识别通知来源
func (s *Service) IdentifySource(r *http.Request) (string, error) {
    auth := r.Header.Get("Authorization")
    if auth == "" {
        return "host", nil
    }
    token := strings.TrimPrefix(auth, "Bearer ")
    pluginID, ok := s.pluginMgr.FindPluginByToken(token)
    if !ok {
        return "", fmt.Errorf("invalid token")
    }
    return pluginID, nil
}
```

> **FindPluginByToken 注意事项**：此方法需在 `internal/plugin/manager.go` 中新增。Token 仅存在于运行时 `PluginInstance` 中（非持久化），插件停止后 Token 失效。鉴权时应注意：
> - 插件已停止 → Token 查找不到 → 返回 `401 Unauthorized`（符合预期）
> - 插件重启后获得新 Token → 旧 Token 自动失效（符合预期）
> - 宿主内部调用不走此方法，直接 `source = "host"`
```

### 8.3 API Handlers

```go
// internal/api/handlers_notification.go

func (s *Server) handleGetNotifications(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
    // 解析 query params: unread, source, type, limit, offset
    // 调用 notificationService.repo.List()
    // 返回 JSON
}

func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
    // 1. 识别 source
    source, err := s.notificationService.IdentifySource(r)
    if err != nil {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }

    // 2. 权限校验（插件需声明 notification:send）
    if source != "host" {
        if !s.pluginMgr.HasPermission(source, "notification:send") {
            http.Error(w, "forbidden: missing notification:send permission", http.StatusForbidden)
            return
        }
    }

    // 3. 解析请求体
    var req CreateNotificationRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request", http.StatusBadRequest)
        return
    }

    // 4. 构造 Notification（source 由后端填充，忽略请求体中的 source）
    n := &Notification{
        ID:        uuid.NewString(),
        Source:    source,
        Type:      req.Type,
        Title:     req.Title,
        Message:   req.Message,
        Actions:   req.Actions,
        Read:      false,
        Timestamp: time.Now(),
    }

    // 5. 创建 + 广播
    if err := s.notificationService.Create(r.Context(), n); err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }

    // 6. 返回创建的通知
    writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request, ps httprouter.Params) { ... }
func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request, _ httprouter.Params) { ... }
func (s *Server) handleDeleteNotification(w http.ResponseWriter, r *http.Request, ps httprouter.Params) { ... }
func (s *Server) handleGetUnreadCount(w http.ResponseWriter, r *http.Request, _ httprouter.Params) { ... }
```

### 8.4 路由注册

采用 catch-all dispatcher 模式（与 [`handlePluginDispatch`](../internal/plugin/handlers.go:47) 风格一致），避免 httprouter 静态/参数段冲突：

```go
// 在 internal/api/server.go 的 registerRoutes() 中新增：
s.router.GET("/v1/notifications", s.handleGetNotifications)
s.router.POST("/v1/notifications", s.handleCreateNotification)
s.router.GET("/v1/notifications/unread-count", s.handleGetUnreadCount)

// Catch-all dispatcher 处理含 :id 的子路由
s.router.PUT("/v1/notifications/:path", s.handleNotificationDispatch)
s.router.DELETE("/v1/notifications/:path", s.handleNotificationDispatch)
```

```go
// internal/api/handlers_notification.go

func (s *Server) handleNotificationDispatch(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
    path := ps.ByName("path")
    method := r.Method

    switch {
    case method == "PUT" && path == "read-all":
        s.handleMarkAllNotificationsRead(w, r, ps)
    case method == "PUT" && strings.HasSuffix(path, "/read"):
        // path = "<id>/read" → 提取 id
        id := strings.TrimSuffix(path, "/read")
        s.handleMarkNotificationRead(w, r, id)
    case method == "DELETE" && !strings.Contains(path, "/"):
        // path = "<id>"
        s.handleDeleteNotification(w, r, path)
    default:
        http.NotFound(w, r)
    }
}
```

> **设计决策**：采用 catch-all dispatcher 而非改路径避免冲突。理由：项目已有 `handlePluginDispatch` 先例，风格一致；且 dispatcher 模式扩展性更好，后续新增子路由无需担心 httprouter 冲突。

### 8.5 TopSearchBar 改造

在 [`TopSearchBar.tsx`](../ui/src/components/TopSearchBar.tsx) 的右侧 spacer 区域插入铃铛：

```tsx
{/* 原有：右侧 spacer */}
{/* <div className="flex-1" /> */}

{/* 改造后：铃铛 + spacer */}
<div style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}>
  <NotificationBell
    unreadCount={unreadCount}
    onClick={() => setPanelOpen(prev => !prev)}
  />
</div>
<div className="flex-1" />

{/* 通知面板（条件渲染） */}
{isPanelOpen && (
  <NotificationPanel
    notifications={notifications}
    onMarkRead={markAsRead}
    onMarkAllRead={markAllAsRead}
    onDelete={deleteNotification}
    onClose={() => setPanelOpen(false)}
  />
)}
```

### 8.6 useNotifications Hook

```typescript
// ui/src/hooks/useNotifications.ts

export function useNotifications(isPanelOpen: boolean) {
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [unreadCount, setUnreadCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [total, setTotal] = useState(0);

  // 初次加载
  useEffect(() => {
    fetchNotifications({ limit: 50 }).then(data => {
      setNotifications(data.notifications);
      setUnreadCount(data.unreadCount);
      setTotal(data.total);
      setLoading(false);
    });
  }, []);

  // SSE 重连后全量刷新
  const refresh = useCallback(async () => {
    const data = await fetchNotifications({ limit: 50 });
    setNotifications(data.notifications);
    setUnreadCount(data.unreadCount);
    setTotal(data.total);
  }, []);

  // SSE 实时更新 — 通过 useEventStream 的 onNotification 回调
  // 由 App.tsx 调用 useEventStream({ onNotification: handleNewNotification }) 触发
  // 注意：useEventStream 内部用 callbacksRef 存回调引用，handleNewNotification 引用变化
  // 不会导致 EventSource 重建。同时 handleNewNotification 内只使用 setState（函数式更新），
  // 不依赖外部 state，因此 useCallback([]) 空依赖不会产生过期闭包问题。
  const handleNewNotification = useCallback((n: NotificationEvent) => {
    if (n.action === 'updated') {
      // 更新已有条目（progress 进度变化 或 progress→终态转换）
      setNotifications(prev => prev.map(item =>
        item.id === n.id
          ? { ...item, message: n.message, type: n.type, timestamp: n.timestamp }
          : item
      ));
      // 不弹 Toast、不增加 unreadCount
    } else {
      // 新通知 prepend
      setNotifications(prev => [n as Notification, ...prev]);
      setUnreadCount(prev => prev + 1);
      // Toast 仅在面板关闭时弹出（由 NotificationToast 组件自行判断 isPanelOpen）
    }
  }, []);

  const markAsRead = async (id: string) => {
    await markNotificationRead(id);
    setNotifications(prev => prev.map(n => n.id === id ? { ...n, read: true } : n));
    setUnreadCount(prev => Math.max(0, prev - 1));
  };

  const markAllAsRead = async () => {
    await markAllNotificationsRead();
    setNotifications(prev => prev.map(n => ({ ...n, read: true })));
    setUnreadCount(0);
  };

  const deleteNotification = async (id: string) => {
    await deleteNotificationApi(id);
    setNotifications(prev => {
      const deleted = prev.find(n => n.id === id);
      if (deleted && !deleted.read) setUnreadCount(c => Math.max(0, c - 1));
      return prev.filter(n => n.id !== id);
    });
  };

  return {
    notifications, unreadCount, loading, total,
    markAsRead, markAllAsRead, deleteNotification,
    handleNewNotification, refresh,
  };
}
```

---

## 九、数据流

### 9.1 完整通知生命周期

```
1. 来源（宿主/插件）→ NotificationService.Create()
2. → 写入 SQLite
3. → 同步清理超出容量的已读通知
4. → EventBroker.Broadcast(notification, action)   → SSE /v1/events → 前端 useEventStream
5. → PluginEventBus.Emit(notification)             → SSE /v1/plugins/events/stream → iframe
6. 前端收到 SSE 通知事件 → useNotifications.handleNewNotification()
7. → action="created"：prepend 到列表 + unreadCount++ + 弹 Toast
   → action="updated"：查找并更新已有条目（progress 进度变化），不弹 Toast
8. → NotificationBell badge 更新
9. → NotificationToast 弹出（面板关闭时 + action="created"）
```

### 9.3 一期与现有 SSE 事件的共存策略

> **设计决策**：一期通知系统与现有 SSE 事件共存，职责分离：
> - **原有 SSE 事件**（`build_complete`、`plugin.crashed` 等）→ 驱动实时 UI 更新（GraphCanvas 刷新、面板状态变化）
> - **notification SSE 事件**→ 驱动铃铛/面板/toast 展示 + SQLite 持久化
>
> 两者内容可能重叠，但职责不同。一期不做去重或迁移，原因：
> 1. 最小化一期改动范围
> 2. 原有事件有特定的前端处理逻辑（如 build_complete 触发 GraphCanvas 重建），通知系统不应替代
> 3. 可在三期统一考虑事件系统整合
>
> 宿主内部代码在触发原有 SSE 事件的同时，调用 `NotificationService.Create()` 生成通知，例如：

```go
// 构建完成：既有 SSE 事件，又生成通知
func (s *Server) onBuildComplete(projectID string, stats BuildStats) {
    // 原有：SSE 广播 build_complete 事件 → 前端 GraphCanvas 刷新
    s.eventBroker.Broadcast(api.Event{Type: api.EventBuildComplete, ...})

    // 新增：创建持久化通知 → 铃铛/面板展示
    s.notificationService.Create(ctx, &Notification{
        Source:  "host",
        Type:    "success",
        Title:   "Build Complete",
        Message: fmt.Sprintf("%d nodes, %d edges created", stats.NodesCreated, stats.EdgesCreated),
    })
}
```

### 9.2 用户交互流程

```
点击铃铛 → NotificationPanel 展开
  ├── 点击通知 → markAsRead() + 执行 action URL（如有）
  ├── 点击 "Mark all read" → markAllAsRead()
  ├── 点击通知 × 按钮 → deleteNotification()
  └── 点击面板外部 → 关闭面板
```

---

## 十、桌面端与 Web 端兼容性

Axons 同时运行在桌面端（Wails webview）和 Web 端（浏览器），通知系统需在两种环境下均正常工作。

### 10.1 两种运行模式对比

| 维度 | 桌面端（Wails） | Web 端（浏览器） |
|------|----------------|-----------------|
| 前端加载方式 | webview 加载 `http://127.0.0.1:PORT` | 浏览器访问远程 daemon URL |
| API 请求 | same-origin，无 CORS 问题 | same-origin，无 CORS 问题 |
| SSE 连接 | `http://127.0.0.1:PORT/v1/events` | `http://<daemon-host>/v1/events` |
| 插件后端→daemon | localhost HTTP，无 CORS | localhost HTTP（same-machine），无 CORS |
| 用户数 | 单用户 | 潜在多用户（远程部署） |
| 标签页 | 单窗口（Wails webview） | 可能多标签页 |

### 10.2 兼容性分析

**无差异的部分**（可直接运行）：
- 后端 API + SQLite CRUD：纯 Go 代码，与运行模式无关
- SSE 推送：前端通过 `useEventStream` 订阅 `/v1/events`，桌面端和 Web 端路径一致
- 插件后端发送通知：通过 `AXONS_API_URL` + `AXONS_PLUGIN_TOKEN` 调用 HTTP API，两种模式下均正常
- 铃铛/面板/Toast UI：React 组件，与运行模式无关

**需注意差异的部分**：

| 差异点 | 说明 | 处理方式 |
|--------|------|---------|
| **桌面端原生通知** | 桌面端可通过 Wails API 或 `Notification` Web API 发送操作系统级通知，用户无需聚焦窗口即可感知 | 二期实现：检测 `getRuntimeMode() === 'desktop'` + `Notification.permission` 时发送原生通知 |
| **Web 端多标签页** | Web 端用户可能打开多个标签页，每个标签页独立接收 SSE 事件，操作同一份通知数据 | 后端无状态，SQLite 写操作天然序列化；前端多标签页各自维护本地状态，操作 API 后通过 SSE 同步，无需额外处理 |
| **Web 端离线场景** | Web 端网络断开时 SSE 断连，通知无法实时推送 | 已有 SSE 重连 + `refresh()` 机制（§4.3），重连后自动补齐 |
| **Web 端多用户** | 远程部署场景下多个用户访问同一 daemon，通知数据不隔离 | 一期不处理（单用户场景）。三期可考虑通知按用户隔离（需引入用户身份体系） |
| **桌面端窗口最小化** | 用户最小化窗口时无法看到 Toast | 二期：桌面端原生通知作为补充 |

### 10.3 桌面端原生通知（二期）

```typescript
// 桌面端且浏览器授权时，发送操作系统原生通知
if (getRuntimeMode() === 'desktop' && Notification.permission === 'granted') {
  new Notification(notification.title, {
    body: notification.message,
    icon: '/favicon.ico',
    tag: notification.id, // 同 ID 通知复用，避免重复弹出
  });
}
```

> 一期不实现原生通知，仅用 Toast。理由：原生通知需用户授权交互，增加一期复杂度；Toast 在桌面端已足够（窗口始终在前台）。

---

## 十一、实施分期

### 一期（核心功能，预计 3-4 天）

1. 后端 `NotificationRepository` + `Service` + API handlers
2. 数据库 `migrateMainV7`
3. SSE 推送集成（`EventBroker` + `PluginEventBus`）
4. 前端 `NotificationBell` + `NotificationPanel`
5. 前端 `useNotifications` hook + `useEventStream` 集成
6. 宿主内部通知发送（build complete、plugin crashed 等）
7. 插件 `notification:send` 权限

### 二期（增强功能，预计 2-3 天）

1. `NotificationToast` 右下角弹窗提示
2. 通知分类过滤（按 source / type 标签切换）
3. `panel://xxx` 协议支持：点击通知跳转到指定面板
4. Footer center 区域简短通知摘要
5. 桌面端通知声音提示

### 三期（可选扩展）

1. 通知分组（同 source 的通知合并折叠）
2. 通知优先级（高优先级 toast 不自动消失）
3. 通知订阅偏好（用户可关闭某 source 的通知）
4. 通知批量操作（全部清除、按类型清除）
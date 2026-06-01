# Axons Notification System Design

> Version: v1.0 | Date: 2026-05-20 | Status: Implemented

## 1. Background & Motivation

### 1.1 Current Pain Points

Axons lacks a unified notification mechanism. Key events such as build completion, plugin crashes, and model download progress are handled in a scattered manner through SSE events in individual components, preventing users from getting a global perspective:

- **Build completion**: Only silently refreshes GraphCanvas, no explicit notification
- **Plugin crash**: SSE pushes `plugin.crashed`, only visible in frontend logs
- **Long-running task progress** (installing plugins, downloading models): Progress info is scattered across individual panels and lost when switching panels

### 1.2 Requirements

1. **Unified entry point**: Bell icon + notification panel, aggregating messages from all sources
2. **Dual-source support**: Both the host (daemon) and plugins can send notifications
3. **Real-time**: New notifications are pushed to the frontend via SSE in real time, and the bell immediately displays the unread count
4. **Persistence**: Notifications are stored in SQLite, surviving page refreshes
5. **Thin frontend, thick backend**: The backend handles CRUD + push logic, the frontend only renders

### 1.3 Design Principles

Following the project's existing architectural principles (see [plugin-system-design.md](plugin-system-design.md) §2.1 and [plugin-ui-isolation-design.md](plugin-ui-isolation-design.md) §1.4):

| Layer | Responsibility | What it doesn't do |
|-------|---------------|-------------------|
| **Frontend (thin)** | Render bell/panel/toast, subscribe to SSE notification events, call API to mark as read | No notification storage, no deduplication, no dispatch |
| **Backend (thick)** | Notification CRUD, permission validation, SSE push, auto-cleanup | — |

---

## 2. Data Model

### 2.1 Notification Structure

```go
// Notification represents a notification message
type Notification struct {
    ID        string    `json:"id"`         // UUID v4
    Source     string    `json:"source"`     // "host" | pluginId (e.g. "com.axons.huggingface")
    Type      string    `json:"type"`       // info | warning | error | success | progress
    Title     string    `json:"title"`      // Notification title
    Message   string    `json:"message"`    // Notification body (optional)
    Group     string    `json:"group"`      // Group identifier (optional, reserved for phase 3 notification grouping)
    Actions   []Action  `json:"actions"`    // Optional action buttons (phase 2)
    Read      bool      `json:"read"`       // Whether read
    Timestamp time.Time `json:"timestamp"`  // Creation time
}

// Action represents an interactive button on a notification
type Action struct {
    ID    string `json:"id"`     // Action identifier
    Label string `json:"label"`  // Button text
    URL   string `json:"url"`    // Click-through URL (optional, e.g. "panel://huggingface")
}
```

### 2.2 Type Definitions

| Type | Icon | Scenario |
|------|------|----------|
| `info` | ℹ️ Blue | General information notifications (e.g. plugin started) |
| `success` | ✓ Green | Operation succeeded (e.g. build complete, model download complete) |
| `warning` | ⚠ Yellow | Situations requiring attention (e.g. plugin restarted 2 times) |
| `error` | ✕ Red | Operation failed (e.g. build failed, plugin crashed) |
| `progress` | ⟳ Purple | In-progress operation (e.g. model download 67%) |

**progress special handling**: The same progress notification is continuously updated (progress notifications with the same `source` + `title` reuse the same record, updating the `message` field), rather than creating a new record. When the frontend receives a progress type notification, it can display a progress bar. When progress completes, the sender POSTs another notification with the same `title`, setting `type` to `success` or `error` — the daemon automatically updates the existing progress record with the same source+title to a terminal state (see §3.6).

**progress→terminal state transition rule**: When the POST request's `type` is not `progress` (e.g. `success`/`error`), and a `progress` record with the same `source` + `title` already exists, the daemon updates that progress record's type to the type in the request and message to the message in the request, rather than creating a new record. This way, the sender doesn't need to remember the notification ID — they can simply continue POSTing notifications with the same title to complete the terminal state transition.

### 2.3 Source Rules

| Source | source value | How it's populated |
|--------|-------------|-------------------|
| Host (daemon internal) | `"host"` | Hardcoded in Go code |
| Plugin backend | Plugin ID (e.g. `"com.axons.huggingface"`) | Daemon auto-populates after authenticating via `AXONS_PLUGIN_TOKEN` |

**Anti-spoofing**: Plugins cannot specify the source field themselves. The `source` field in the POST `/v1/notifications` request body (if present) will be ignored — the daemon looks up the corresponding plugin ID based on the request's Token and overwrites it. Requests without a Token have their source fixed as `"host"` (phase 1 is limited to localhost access; security is guaranteed by the network layer).

### 2.4 Database Schema

Notifications are stored in the main database (`axons.db`), added as the `migrateMainV7` migration:

```sql
CREATE TABLE IF NOT EXISTS notifications (
    id         TEXT PRIMARY KEY,
    source     TEXT    NOT NULL,
    type       TEXT    NOT NULL DEFAULT 'info',
    title      TEXT    NOT NULL,
    message    TEXT    DEFAULT '',
    actions    TEXT    DEFAULT '[]',         -- JSON array (SQLite 3.38+ can use json type for json_each() queries)
    "group"    TEXT    DEFAULT '',            -- Group identifier (reserved for phase 3, currently empty)
    read       INTEGER NOT NULL DEFAULT 0,   -- 0=unread, 1=read
    timestamp  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- High-frequency index for unread queries
CREATE INDEX IF NOT EXISTS idx_notifications_unread
    ON notifications(read, timestamp DESC);

-- Index for filtering by source (e.g. viewing all notifications from a plugin)
CREATE INDEX IF NOT EXISTS idx_notifications_source
    ON notifications(source, timestamp DESC);

-- Index for grouping by group (reserved for phase 3)
CREATE INDEX IF NOT EXISTS idx_notifications_group
    ON notifications("group", timestamp DESC);
```

**Capacity policy** (two-level cleanup):

1. **Read notification soft limit**: Retain the most recent 150 read notifications. Each time a new notification is created, the daemon checks the total number of read notifications and automatically cleans up the oldest read records if exceeded (executed synchronously, single SQL):

```sql
DELETE FROM notifications
WHERE read = 1 AND id NOT IN (
    SELECT id FROM notifications WHERE read = 1
    ORDER BY timestamp DESC LIMIT 150
);
```

2. **Total hard limit**: Regardless of read/unread status, the total count must not exceed 500. When exceeded, the oldest records are forcibly cleaned up (even if unread, sorted by timestamp):

```sql
DELETE FROM notifications
WHERE id NOT IN (
    SELECT id FROM notifications
    ORDER BY timestamp DESC LIMIT 450
);
```

> Retain 150 read + 50 unread (soft limit) + hard limit of 500 to prevent unbounded unread growth. Thresholds are adjustable via configuration.

**TTL auto-read** (phase 2): `info` type notifications are automatically marked as read after 24 hours to reduce information noise. Implementation: daemon performs a batch update on startup, and subsequently via scheduled tasks or as a side effect when creating notifications.

---

## 3. Backend API Design

### 3.1 API Routes

All notification APIs are registered under the `httprouter` `/v1/notifications` prefix.

| Route | Method | Description | Auth |
|-------|--------|-------------|------|
| `/v1/notifications` | GET | Get notification list | None |
| `/v1/notifications` | POST | Send notification | Plugins require Token |
| `/v1/notifications/:id/read` | PUT | Mark single notification as read | None |
| `/v1/notifications/read-all` | PUT | Mark all as read | None |
| `/v1/notifications/:id` | DELETE | Delete single notification | None |
| `/v1/notifications/unread-count` | GET | Get unread count | None |

### 3.2 Request/Response Format

#### GET /v1/notifications

Query parameters:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `unread` | bool | false | Only return unread notifications |
| `source` | string | — | Filter by source (e.g. `host` or plugin ID) |
| `type` | string | — | Filter by type |
| `limit` | int | 50 | Per-page count (max 100) |
| `offset` | int | 0 | Offset |

Response:

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

Request body:

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

- The `source` field **does not need** to be provided by the caller; the daemon auto-populates it
- `type` defaults to `"info"`
- `actions` is optional, defaults to empty array
- For `progress` type, if a progress notification with the same `source` + `title` already exists, the existing record is updated instead of creating a new one

Response:

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

Response: `204 No Content`

#### PUT /v1/notifications/read-all

Response: `204 No Content`

#### DELETE /v1/notifications/:id

Response: `204 No Content`

#### GET /v1/notifications/unread-count

Response:

```json
{
  "count": 5
}
```

### 3.3 Plugin Authentication

When plugin backends send notifications via `POST /v1/notifications`, they must include a Token in the request header:

```
Authorization: Bearer <AXONS_PLUGIN_TOKEN>
```

After the daemon receives the request:

1. Extract the Token from the `Authorization` header
2. Look up the corresponding plugin ID in `PluginManager` using the Token
3. If found → `source` is populated with that plugin ID
4. If not found → return `401 Unauthorized`
5. No `Authorization` header → `source` is populated as `"host"` (phase 1 trusts localhost; phase 2 adds host API Key)

**Input validation**:
- `title`: Required, max 200 characters
- `message`: Optional, max 1000 characters
- `actions`: Max 3 items, each `label` max 50 characters, `url` max 500 characters
- Returns `400 Bad Request` if limits are exceeded

**Auth boundary** (phase 1 → phase 2 plan):
- **Phase 1**: Apart from POST requiring plugin Token + permission check, GET/PUT/DELETE endpoints have no auth. Security is guaranteed by the localhost network layer
- **Phase 2**: Introduce host API Key; all write operations (POST/PUT/DELETE) require auth; GET endpoints remain unauthenticated (notifications are user-level data)

### 3.4 SSE Push

Reuse the existing [`EventBroker`](../internal/api/events.go:71), adding a new event type:

```go
// Added in internal/api/events.go
EventNotification EventType = "notification"
```

When `NotificationService.Create()` creates or updates a notification, it broadcasts a `notification` event via `EventBroker`. The event `data` includes an `action` field to distinguish between creation (`"created"`) and update (`"updated"`):

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

> The frontend uses the `action` field to determine handling: `created` → prepend to list + unreadCount++ + show Toast; `updated` → find and update existing entry (e.g. progress change), no Toast, no increment to unreadCount.

**Frontend subscription**: Via the existing `/v1/events` SSE connection ([`useEventStream`](../ui/src/hooks/useEventStream.ts:178)), add `notification` event listener. No new SSE connection needed.

**PluginEventBus sync**: New notifications are also broadcast via [`PluginEventBus.Emit()`](../internal/plugin/eventbus.go:78), so plugins in iframes can also receive notification events through `/v1/plugins/events/stream`:

```go
// Notification events are also pushed to PluginEventBus
GetGlobalBus().Emit(Event{
    PluginID: n.Source,
    Type:     "notification",
    Payload:  notificationPayload,
})
```

### 3.5 Host Internal Calls

Daemon internal code calls `NotificationService` directly, without going through HTTP:

```go
// Example: Send notification when build completes
func (s *Server) onBuildComplete(projectID string, stats BuildStats) {
    s.notificationService.Create(context.Background(), &Notification{
        Source:  "host",
        Type:    "success",
        Title:   "Build Complete",
        Message: fmt.Sprintf("%d nodes, %d edges created", stats.NodesCreated, stats.EdgesCreated),
    })
}
```

### 3.6 Progress Type Update Mechanism

Progress notifications use a "find-and-update" pattern to avoid duplicate creation. The POST semantics are extended to support progress→terminal state transition:

```go
func (s *NotificationService) Create(ctx context.Context, n *Notification) error {
    // Find existing progress notification with the same source + title
    existing, _ := s.repo.FindProgressBySourceAndTitle(ctx, n.Source, n.Title)

    if existing != nil {
        // Case 1: New notification is also progress → update existing record's message
        // Case 2: New notification is success/error → update progress record to terminal state
        if err := s.repo.Update(ctx, existing.ID, n.Message, n.Type); err != nil {
            return err
        }
        n.ID = existing.ID // Reuse original ID, frontend can use this to update existing entry

        // SSE broadcast update event (distinguish created/updated)
        s.broadcastNotification(n, "updated")
        return nil
    }

    // No existing progress record → create new record
    if err := s.repo.Create(ctx, n); err != nil {
        return err
    }

    s.broadcastNotification(n, "created")

    // Synchronously clean up read notifications that exceed capacity
    if err := s.repo.Cleanup(ctx); err != nil {
        // Cleanup failure doesn't affect notification creation, just log it
        log.Printf("notification cleanup failed: %v", err)
    }

    return nil
}
```

> **Design decision**: Use extended POST semantics rather than adding a new PATCH endpoint. Rationale: progress→terminal state is a natural extension of the progress mechanism; the sender doesn't need to remember the notification ID — they can simply continue POSTing notifications with the same title, changing type to success/error.

---

## 4. Frontend Design

### 4.1 UI Layout

The bell icon is placed to the right of [`TopSearchBar`](../ui/src/components/TopSearchBar.tsx):

```
Before: [spacer(macos traffic lights) | search-bar | spacer(window controls)]
After:  [spacer(macos traffic lights) | search-bar | bell-icon | spacer(window controls)]
```

The notification panel pops up below the bell (dropdown panel), and the Toast floats in the bottom-right corner.

### 4.2 Component Structure

#### NotificationBell

Bell button component, placed to the right of TopSearchBar:

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

Notification list panel that pops up when the bell is clicked:

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

- `●` Unread marker (dot), `○` Read
- Each notification displays: **source name** + relative time. When `source` is `"host"`, display "Axons"; plugin source displays the plugin `name` (not ID), making it easier for users to understand the origin
- Click notification → mark as read + execute action (if any)
- Empty state: Display "No notifications"
- Different types use different icon colors for quick visual distinction
- **Click outside to close**: Register `document.addEventListener('mousedown', ...)` via `useEffect` to detect clicks outside the panel and close it
- **Paginated scrolling**: Display a "Load more..." button at the bottom of the list (not infinite scroll, to avoid performance issues). Click to call `fetchNotifications({ offset: currentCount })` for additional loading

#### NotificationToast

When a new notification arrives, a toast pops up in the bottom-right corner (auto-dismisses after 3s):

```
┌─────────────────────────────────┐
│  ✓ Model Download Complete      │
│    llama3 is ready to use       │
│                       [Dismiss] │
└─────────────────────────────────┘
```

- Only show toast for newly arrived unread notifications (when `action="created"`)
- **Suppress toast when panel is open**: When `NotificationPanel` is already expanded, new notifications don't show Toast (to avoid obscuring the panel); only show when panel is closed
- `error` / `warning` type toasts don't auto-dismiss and must be manually closed
- Display at most 3 toasts simultaneously; the oldest auto-dismisses when exceeded

### 4.3 Frontend Data Flow

```
SSE /v1/events ──→ useEventStream (notification event)
                        │
                        ▼
                useNotifications hook
                   ├── notifications: Notification[]     (full list)
                   ├── unreadCount: number               (unread count)
                   ├── fetchNotifications()              (initial load/pagination)
                   ├── markAsRead(id)                    (PUT API)
                   ├── markAllAsRead()                   (PUT API)
                   ├── deleteNotification(id)            (DELETE API)
                   └── onNewNotification callback        (SSE → update list + unread count)
```

**`useNotifications` hook**:

```typescript
interface UseNotificationsReturn {
  notifications: Notification[];
  unreadCount: number;
  loading: boolean;
  fetchNotifications: (options?: { unread?: boolean; limit?: number }) => Promise<void>;
  markAsRead: (id: string) => Promise<void>;
  markAllAsRead: () => Promise<void>;
  deleteNotification: (id: string) => Promise<void>;
  refresh: () => Promise<void>;  // Full refresh after SSE reconnection
}

function useNotifications(): UseNotificationsReturn {
  // 1. Initial load: GET /v1/notifications?limit=50
  // 2. SSE real-time: useEventStream's onNotification callback
  //    - action="created" → prepend to list + unreadCount++
  //    - action="updated" → find and update existing entry (progress change)
  // 3. SSE reconnection: call refresh() in onConnect callback → re-fetch full list + unreadCount
  //    Ensure missed notifications and read state changes during disconnection are reconciled
  // 4. Operations: update local state after calling API
}
```

**SSE reconnection state recovery**: Call `refresh()` in the `onConnect` callback of `useEventStream` to re-fetch the full notification list and unreadCount from the backend, avoiding state inconsistency during disconnection.

### 4.4 SSE Event Integration

Add a notification event type in [`useEventStream.ts`](../ui/src/hooks/useEventStream.ts):

```typescript
// EventType addition
| 'notification'

// Event data interface
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

// UseEventStreamOptions addition
onNotification?: (data: NotificationEvent) => void;

// SSE listener addition
eventSource.addEventListener('notification', (e: MessageEvent) => {
  try {
    const event = JSON.parse(e.data) as Event;
    callbacksRef.current.onNotification?.(event.data as unknown as NotificationEvent);
  } catch (err) {
    console.error('Failed to parse notification event:', err);
  }
});
```

### 4.5 API Service Layer

Add the following in [`services/api.ts`](../ui/src/services/api.ts):

```typescript
// Get notification list
export async function fetchNotifications(options?: {
  unread?: boolean;
  source?: string;
  type?: string;
  limit?: number;
  offset?: number;
}): Promise<{ notifications: Notification[]; total: number; unreadCount: number }>

// Send notification (host frontend scenario, generally not used; notifications should originate from backend)
export async function createNotification(n: Partial<Notification>): Promise<Notification>

// Mark as read
export async function markNotificationRead(id: string): Promise<void>

// Mark all as read
export async function markAllNotificationsRead(): Promise<void>

// Delete notification
export async function deleteNotification(id: string): Promise<void>

// Get unread count
export async function fetchUnreadCount(): Promise<{ count: number }>
```

---

## 5. Plugin Usage

### 5.1 Integration Flow

Plugin developers need to complete the following steps to integrate with the notification system:

```
Step 1: Declare permission in manifest.json
        ↓
Step 2: Plugin backend sends notifications via HTTP API
        ↓
Step 3: (Optional) Plugin iframe listens for notification events
```

**Step 1: Declare Permission**

Add `"notification:send"` to the `permissions` array in `manifest.json`:

```jsonc
{
  "id": "com.axons.huggingface",
  "permissions": [
    "graph:read",
    "panel:create",
    "notification:send"    // ← Must be declared, otherwise POST returns 403
  ]
}
```

> Plugins that have not declared `notification:send` will receive `403 Forbidden` when calling `POST /v1/notifications`.

**Step 2: Backend Sends Notifications**

When the plugin backend starts, Axons injects three environment variables (see [plugin-developer-guide.md §5.1](plugin-developer-guide.md)):

| Variable | Purpose |
|----------|---------|
| `AXONS_API_URL` | Axons API address, e.g. `http://127.0.0.1:8080` |
| `AXONS_PLUGIN_PORT` | Port the plugin should bind to |
| `AXONS_PLUGIN_TOKEN` | Plugin auth Token (used for the `Authorization` header in notification API) |

When sending notifications:
1. **Request header**: `Authorization: Bearer <AXONS_PLUGIN_TOKEN>`
2. **Request body**: `type`, `title`, `message` (optional), `actions` (optional)
3. **Source auto-population**: The daemon identifies the plugin ID from the Token and overwrites `source`; plugins don't need to (and can't) specify it themselves
4. **No difference between desktop/web**: The plugin backend communicates with the daemon via localhost HTTP on the same machine, no CORS issues

**Step 3: Listen for Notifications in iframe (Optional)**

Plugin iframes can listen for notification events via `pluginApi.onEvent('notification', callback)` to:
- Perform UI response after their own notifications arrive (e.g. update state within the panel)
- Listen for notifications from other sources for coordination

> Note: Notification events received by iframes come from the same source as those in the bell/panel; there's no need to display them redundantly.

### 5.2 Notification Type Selection Guide

| Scenario | Recommended Type | Example |
|----------|-----------------|---------|
| Operation completed | `success` | Model download complete, installation succeeded |
| Operation failed | `error` | Download failed, installation failed |
| User attention needed | `warning` | Configuration error, resource nearly exhausted |
| General information | `info` | Plugin started, configuration updated |
| In-progress operation | `progress` | Download progress 67%, installing |

**progress usage rules**:
- Progress notifications with the same `source` + `title` will **reuse the same record**, updating the `message` field
- When progress completes, POST another notification with the same `title`, changing `type` to `success` or `error`; the daemon automatically updates the existing record
- The frontend won't show a Toast for progress updates, only updating the progress bar

### 5.3 Code Example (Python Backend)

```python
# Python plugin backend example
import os
import requests

AXONS_API_URL = os.environ["AXONS_API_URL"]
AXONS_PLUGIN_TOKEN = os.environ["AXONS_PLUGIN_TOKEN"]

def notify_download_progress(model_name: str, progress: int):
    """Send progress notification — progress notifications with the same title reuse the same record"""
    requests.post(f"{AXONS_API_URL}/v1/notifications", json={
        "type": "progress",
        "title": f"Downloading {model_name}",
        "message": f"{progress}% complete"
    }, headers={"Authorization": f"Bearer {AXONS_PLUGIN_TOKEN}"})

def notify_download_complete(model_name: str):
    """Send completion notification — progress notifications with the same title are automatically updated to terminal state"""
    requests.post(f"{AXONS_API_URL}/v1/notifications", json={
        "type": "success",
        "title": f"Download Complete: {model_name}",
        "message": f"{model_name} is ready to use",
        "actions": [
            {"id": "open", "label": "Open Hugging Face", "url": "panel://huggingface"}
        ]
    }, headers={"Authorization": f"Bearer {AXONS_PLUGIN_TOKEN}"})
```

### 5.4 Code Example (Go Backend)

```go
// Go plugin backend example
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

### 5.5 Permission Declaration

Add `notification:send` permission in manifest.json:

```jsonc
{
  "id": "com.axons.huggingface",
  "permissions": [
    "graph:read",
    "notification:send"    // ← New: allow sending notifications
  ]
}
```

Plugins that have not declared `notification:send` will receive `403 Forbidden` when calling `POST /v1/notifications`.

### 5.6 ValidPermissions Addition

Add to `ValidPermissions` in [`internal/plugin/manifest.go`](../internal/plugin/manifest.go:137):

```go
var ValidPermissions = map[string]bool{
    "graph:read":         true,
    "project:read":       true,
    "model:register":     true,
    "panel:create":       true,
    "state:read":         true,
    "state:write":        true,
    "notification:send":  true,  // ← New
}
```

### 5.7 Listening for Notifications in Plugin iframe

The PluginApi within iframes already subscribes to events via SSE `/v1/plugins/events/stream` (see [iframe-adapter.ts](../ui/src/plugin-sdk/iframe-adapter.ts)). New notifications are pushed as `type: "notification"` events through this channel and can be consumed directly within plugin iframes:

```javascript
// Listen for notification events in iframe
const adapter = new AxonsPluginIframe.IframePluginApiAdapter();

adapter.init().then(() => {
  adapter.onEvent('notification', (notification) => {
    if (notification.source === adapter.pluginId) {
      // Own notification, can perform special handling
      console.log('My notification was delivered:', notification.title);
    }
  });
});
```

---

## 6. Integration with Existing Systems

### 6.1 Host Notification Scenarios

The following host internal events should automatically generate notifications:

| Event | Notification type | Notification title | Trigger point |
|-------|------------------|-------------------|---------------|
| Build completed | `success` | Build Complete | [`handlers.go`](../internal/api/handlers.go) build completed |
| Build failed | `error` | Build Failed | Build error |
| Plugin started | `info` | Plugin Started | [`manager.go`](../internal/plugin/manager.go) startup succeeded |
| Plugin crashed | `error` | Plugin Crashed | Crash exceeds restart limit |
| Plugin installation completed | `success` | Plugin Installed | Installation succeeded |
| Plugin installation failed | `error` | Plugin Install Failed | Installation failed |
| Embedding completed | `success` | Embedding Complete | Embedding completed |

### 6.2 Integration Points Summary

| Existing mechanism | Integration method |
|-------------------|-------------------|
| [`EventBroker`](../internal/api/events.go:71) | Add `EventNotification` event type, SSE push for new notifications |
| [`PluginEventBus`](../internal/plugin/eventbus.go:14) | Notification events synchronously broadcast, subscribable in iframes |
| [`useEventStream`](../ui/src/hooks/useEventStream.ts:178) | Add `onNotification` callback |
| [`TopSearchBar`](../ui/src/components/TopSearchBar.tsx) | Insert bell icon on the right side |
| [`Footer`](../ui/src/components/Footer.tsx) `footerSlot='center'` | Reserved: Can display latest notification summary in Footer center area |
| [`migrateMainV7`](../internal/db/migrations_main.go) | Create `notifications` table |

---

## 7. File Change List

### 7.1 Backend (Go)

| File | Change type | Content |
|------|------------|---------|
| `internal/notification/model.go` | **New** | `Notification` + `Action` struct definitions |
| `internal/notification/repository.go` | **New** | SQLite CRUD + progress find-and-update + auto-cleanup |
| `internal/notification/service.go` | **New** | `NotificationService`: create/query/read/delete + EventBroker broadcast |
| `internal/api/handlers_notification.go` | **New** | API handlers (6 endpoints) |
| `internal/db/migrations_main.go` | **Modified** | Add `migrateMainV7`: create `notifications` table + indexes, `MainSchemaVersion` bumped to 7 |
| `internal/api/events.go` | **Modified** | Add `EventNotification` constant |
| `internal/api/server.go` | **Modified** | Inject `notificationService`, register `/v1/notifications` routes + catch-all dispatcher |
| `internal/plugin/manifest.go` | **Modified** | Add `"notification:send"` to `ValidPermissions` |
| `internal/plugin/manager.go` | **Modified** | Add `FindPluginByToken()` method for notification auth |

### 7.2 Frontend (React)

| File | Change type | Content |
|------|------------|---------|
| `ui/src/components/NotificationBell.tsx` | **New** | Bell icon + badge unread count |
| `ui/src/components/NotificationPanel.tsx` | **New** | Notification list panel (popup style) |
| `ui/src/components/NotificationToast.tsx` | **New** | Bottom-right toast notification |
| `ui/src/hooks/useNotifications.ts` | **New** | Notification state management hook |
| `ui/src/components/TopSearchBar.tsx` | **Modified** | Insert `NotificationBell` on the right side |
| `ui/src/App.tsx` | **Modified** | Wrap `NotificationToast` (global floating), pass `onNotification` callback to `useNotifications` |
| `ui/src/hooks/useEventStream.ts` | **Modified** | Add `notification` event type + `onNotification` callback |
| `ui/src/services/api.ts` | **Modified** | Add 6 notification API functions |
| `ui/src/i18n/en/notifications.json` | **New** | Notification-related i18n keys (separate namespace) |
| `ui/src/i18n/index.ts`` | **Modified** | Register `notifications` namespace + import |

### 7.3 New i18n Keys

Notification i18n uses a separate `notifications` namespace (rather than stuffing into `common`), consistent with the project's existing per-feature file splitting style (settings/panels/chat/...):

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

Register the new namespace in [`ui/src/i18n/index.ts`](../ui/src/i18n/index.ts):

```typescript
import notifications from './en/notifications.json';

const enResources = {
  common, settings, panels, chat, activitybar, dropzone, extensions, notifications,
};

// Also need to add 'notifications' to the ns array
ns: ['common', 'settings', 'panels', 'chat', 'activitybar', 'dropzone', 'extensions', 'notifications'],
```

### 7.4 Dependency Injection & Initialization Order

`NotificationService` initialization must be completed during `Server` construction. The dependency chain is as follows:

```
DB (sql.DB) → Repository → NotificationService → Server
```

In [`NewServer()`](../internal/api/server.go), `notificationService` should be created after `eventBroker` and `pluginManager`:

```go
// internal/api/server.go — Added in NewServer()
notificationService := notification.NewService(mainDB, s.eventBroker, s.pluginManager)
s.notificationService = notificationService
```

### 7.5 API Path Compatibility

The project's existing routes mix `/v1/...` and `/api/...` prefixes. Notification APIs uniformly use the `/v1/notifications` prefix, without additionally registering `/api/v1/notifications` compatibility routes. Rationale: The notification system is a new feature with no legacy client compatibility requirements.

---

## 8. Detailed Implementation

### 8.1 NotificationRepository

```go
// internal/notification/repository.go

type Repository struct {
    db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
    return &Repository{db: db}
}

// Create inserts a new notification
func (r *Repository) Create(ctx context.Context, n *Notification) error {
    actionsJSON, _ := json.Marshal(n.Actions)
    _, err := r.db.ExecContext(ctx, `
        INSERT INTO notifications (id, source, type, title, message, actions, read, timestamp)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
        n.ID, n.Source, n.Type, n.Title, n.Message, string(actionsJSON), n.Read, n.Timestamp,
    )
    return err
}

// FindProgressBySourceAndTitle finds a progress notification with the same source + title
func (r *Repository) FindProgressBySourceAndTitle(ctx context.Context, source, title string) (*Notification, error) {
    row := r.db.QueryRowContext(ctx, `
        SELECT id, source, type, title, message, actions, read, timestamp
        FROM notifications
        WHERE source = ? AND title = ? AND type = 'progress'
        ORDER BY timestamp DESC LIMIT 1`, source, title)
    // ... scan and return
}

// Update updates a notification's message and type
func (r *Repository) Update(ctx context.Context, id, message, nType string) error {
    _, err := r.db.ExecContext(ctx, `
        UPDATE notifications SET message = ?, type = ?, timestamp = CURRENT_TIMESTAMP
        WHERE id = ?`, message, nType, id)
    return err
}

// List queries the notification list
func (r *Repository) List(ctx context.Context, opts ListOptions) ([]Notification, int, int, error) {
    // Build WHERE clause: unread / source / type filtering
    // Return notifications, total, unreadCount
}

// MarkRead marks a single notification as read
func (r *Repository) MarkRead(ctx context.Context, id string) error { ... }

// MarkAllRead marks all notifications as read
func (r *Repository) MarkAllRead(ctx context.Context) error { ... }

// Delete deletes a single notification
func (r *Repository) Delete(ctx context.Context, id string) error { ... }

// UnreadCount gets the unread count
func (r *Repository) UnreadCount(ctx context.Context) (int, error) { ... }

// Cleanup cleans up read notifications that exceed capacity
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

// Create creates or updates a notification + SSE broadcast + auto-cleanup
// See §3.6 for the complete logic description
func (s *Service) Create(ctx context.Context, n *Notification) error {
    // Find existing progress notification with the same source + title
    existing, _ := s.repo.FindProgressBySourceAndTitle(ctx, n.Source, n.Title)

    if existing != nil {
        // Update existing record (progress update or progress→terminal state transition)
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

    // Synchronously clean up notifications that exceed capacity (read soft limit + total hard limit)
    if err := s.repo.Cleanup(ctx); err != nil {
        log.Printf("notification cleanup failed: %v", err) // Cleanup failure doesn't affect notification creation
    }

    s.broadcastNotification(n, "created")

    return nil
}

// broadcastNotification broadcasts notification events via EventBroker and PluginEventBus
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

// IdentifySource identifies the notification source from the request
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

> **FindPluginByToken notes**: This method needs to be added in `internal/plugin/manager.go`. The Token only exists in the runtime `PluginInstance` (not persisted) and becomes invalid after the plugin stops. During auth, note:
> - Plugin stopped → Token not found → return `401 Unauthorized` (expected behavior)
> - Plugin restarted with new Token → old Token automatically invalidated (expected behavior)
> - Host internal calls bypass this method, using `source = "host"` directly
```

### 8.3 API Handlers

```go
// internal/api/handlers_notification.go

func (s *Server) handleGetNotifications(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
    // Parse query params: unread, source, type, limit, offset
    // Call notificationService.repo.List()
    // Return JSON
}

func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
    // 1. Identify source
    source, err := s.notificationService.IdentifySource(r)
    if err != nil {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }

    // 2. Permission check (plugins must declare notification:send)
    if source != "host" {
        if !s.pluginMgr.HasPermission(source, "notification:send") {
            http.Error(w, "forbidden: missing notification:send permission", http.StatusForbidden)
            return
        }
    }

    // 3. Parse request body
    var req CreateNotificationRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request", http.StatusBadRequest)
        return
    }

    // 4. Construct Notification (source is populated by backend, ignoring source in request body)
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

    // 5. Create + broadcast
    if err := s.notificationService.Create(r.Context(), n); err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }

    // 6. Return created notification
    writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request, ps httprouter.Params) { ... }
func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request, _ httprouter.Params) { ... }
func (s *Server) handleDeleteNotification(w http.ResponseWriter, r *http.Request, ps httprouter.Params) { ... }
func (s *Server) handleGetUnreadCount(w http.ResponseWriter, r *http.Request, _ httprouter.Params) { ... }
```

### 8.4 Route Registration

Using the catch-all dispatcher pattern (consistent with [`handlePluginDispatch`](../internal/plugin/handlers.go:47) style) to avoid httprouter static/param segment conflicts:

```go
// Added in internal/api/server.go's registerRoutes():
s.router.GET("/v1/notifications", s.handleGetNotifications)
s.router.POST("/v1/notifications", s.handleCreateNotification)
s.router.GET("/v1/notifications/unread-count", s.handleGetUnreadCount)

// Catch-all dispatcher handles sub-routes with :id
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
        // path = "<id>/read" → extract id
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

> **Design decision**: Use catch-all dispatcher instead of changing paths to avoid conflicts. Rationale: The project already has a `handlePluginDispatch` precedent with consistent style; the dispatcher pattern is more extensible, and adding new sub-routes later doesn't risk httprouter conflicts.

### 8.5 TopSearchBar Modification

Insert the bell into the right spacer area of [`TopSearchBar.tsx`](../ui/src/components/TopSearchBar.tsx):

```tsx
{/* Original: right spacer */}
{/* <div className="flex-1" /> */}

{/* Modified: bell + spacer */}
<div style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}>
  <NotificationBell
    unreadCount={unreadCount}
    onClick={() => setPanelOpen(prev => !prev)}
  />
</div>
<div className="flex-1" />

{/* Notification panel (conditional render) */}
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

  // Initial load
  useEffect(() => {
    fetchNotifications({ limit: 50 }).then(data => {
      setNotifications(data.notifications);
      setUnreadCount(data.unreadCount);
      setTotal(data.total);
      setLoading(false);
    });
  }, []);

  // Full refresh after SSE reconnection
  const refresh = useCallback(async () => {
    const data = await fetchNotifications({ limit: 50 });
    setNotifications(data.notifications);
    setUnreadCount(data.unreadCount);
    setTotal(data.total);
  }, []);

  // SSE real-time updates — triggered by App.tsx calling useEventStream({ onNotification: handleNewNotification })
  // Note: useEventStream internally uses callbacksRef to store callback references; handleNewNotification reference changes
  // won't cause EventSource to be recreated. Also, handleNewNotification only uses setState (functional updates) internally,
  // not depending on external state, so useCallback([]) with empty deps won't cause stale closure issues.
  const handleNewNotification = useCallback((n: NotificationEvent) => {
    if (n.action === 'updated') {
      // Update existing entry (progress change or progress→terminal state transition)
      setNotifications(prev => prev.map(item =>
        item.id === n.id
          ? { ...item, message: n.message, type: n.type, timestamp: n.timestamp }
          : item
      ));
      // No Toast, no unreadCount increment
    } else {
      // New notification prepend
      setNotifications(prev => [n as Notification, ...prev]);
      setUnreadCount(prev => prev + 1);
      // Toast only pops when panel is closed (handled by NotificationToast component checking isPanelOpen)
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

## 9. Data Flow

### 9.1 Complete Notification Lifecycle

```
1. Source (host/plugin) → NotificationService.Create()
2. → Write to SQLite
3. → Synchronously clean up read notifications that exceed capacity
4. → EventBroker.Broadcast(notification, action)   → SSE /v1/events → Frontend useEventStream
5. → PluginEventBus.Emit(notification)             → SSE /v1/plugins/events/stream → iframe
6. Frontend receives SSE notification event → useNotifications.handleNewNotification()
7. → action="created": prepend to list + unreadCount++ + show Toast
   → action="updated": find and update existing entry (progress change), no Toast
8. → NotificationBell badge updates
9. → NotificationToast pops (when panel is closed + action="created")
```

### 9.3 Phase 1 Coexistence Strategy with Existing SSE Events

> **Design decision**: The phase 1 notification system coexists with existing SSE events, with separated responsibilities:
> - **Original SSE events** (`build_complete`, `plugin.crashed`, etc.) → Drive real-time UI updates (GraphCanvas refresh, panel state changes)
> - **notification SSE events** → Drive bell/panel/toast display + SQLite persistence
>
> The two may overlap in content but have different responsibilities. Phase 1 doesn't deduplicate or migrate, because:
> 1. Minimize phase 1 change scope
> 2. Original events have specific frontend handling logic (e.g. build_complete triggers GraphCanvas rebuild) that the notification system shouldn't replace
> 3. Event system consolidation can be considered in phase 3
>
> Host internal code calls `NotificationService.Create()` to generate notifications alongside triggering original SSE events, for example:

```go
// Build complete: both existing SSE event and notification generated
func (s *Server) onBuildComplete(projectID string, stats BuildStats) {
    // Existing: SSE broadcast build_complete event → Frontend GraphCanvas refresh
    s.eventBroker.Broadcast(api.Event{Type: api.EventBuildComplete, ...})

    // New: Create persistent notification → Bell/panel display
    s.notificationService.Create(ctx, &Notification{
        Source:  "host",
        Type:    "success",
        Title:   "Build Complete",
        Message: fmt.Sprintf("%d nodes, %d edges created", stats.NodesCreated, stats.EdgesCreated),
    })
}
```

### 9.2 User Interaction Flow

```
Click bell → NotificationPanel expands
  ├── Click notification → markAsRead() + execute action URL (if any)
  ├── Click "Mark all read" → markAllAsRead()
  ├── Click notification × button → deleteNotification()
  └── Click outside panel → Close panel
```

---

## 10. Desktop & Web Compatibility

Axons runs on both desktop (Wails webview) and web (browser), and the notification system must work correctly in both environments.

### 10.1 Comparison of Two Runtime Modes

| Dimension | Desktop (Wails) | Web (Browser) |
|-----------|----------------|---------------|
| Frontend loading | webview loads `http://127.0.0.1:PORT` | Browser accesses remote daemon URL |
| API requests | same-origin, no CORS issues | same-origin, no CORS issues |
| SSE connection | `http://127.0.0.1:PORT/v1/events` | `http://<daemon-host>/v1/events` |
| Plugin backend→daemon | localhost HTTP, no CORS | localhost HTTP (same-machine), no CORS |
| Users | Single user | Potentially multiple users (remote deployment) |
| Tabs | Single window (Wails webview) | Possibly multiple tabs |

### 10.2 Compatibility Analysis

**No-difference parts** (work as-is):
- Backend API + SQLite CRUD: Pure Go code, independent of runtime mode
- SSE push: Frontend subscribes to `/v1/events` via `useEventStream`, same path for desktop and web
- Plugin backend sending notifications: Calls HTTP API via `AXONS_API_URL` + `AXONS_PLUGIN_TOKEN`, works in both modes
- Bell/Panel/Toast UI: React components, independent of runtime mode

**Parts requiring attention for differences**:

| Difference | Description | Handling method |
|------------|-------------|----------------|
| **Desktop native notifications** | Desktop can send OS-level notifications via Wails API or `Notification` Web API, allowing users to perceive them without focusing the window | Phase 2: Detect `getRuntimeMode() === 'desktop'` + `Notification.permission` to send native notifications |
| **Web multi-tab** | Web users may open multiple tabs, each independently receiving SSE events, operating on the same notification data | Backend is stateless, SQLite writes are naturally serialized; frontend tabs each maintain local state, and operations on APIs sync via SSE without additional handling |
| **Web offline scenario** | When web network disconnects, SSE drops, and notifications can't be pushed in real time | Already handled by SSE reconnection + `refresh()` mechanism (§4.3), auto-reconciles after reconnection |
| **Web multi-user** | In remote deployment scenarios, multiple users access the same daemon with non-isolated notification data | Not handled in phase 1 (single-user scenario). Phase 3 can consider per-user notification isolation (requires introducing a user identity system) |
| **Desktop window minimized** | Users can't see Toast when window is minimized | Phase 2: Desktop native notifications as supplement |

### 10.3 Desktop Native Notifications (Phase 2)

```typescript
// Send OS native notification when on desktop and browser has granted permission
if (getRuntimeMode() === 'desktop' && Notification.permission === 'granted') {
  new Notification(notification.title, {
    body: notification.message,
    icon: '/favicon.ico',
    tag: notification.id, // Same ID notifications reuse, avoiding duplicate popups
  });
}
```

> Phase 1 doesn't implement native notifications, using only Toast. Rationale: Native notifications require user authorization interaction, increasing phase 1 complexity; Toast is sufficient on desktop (window is always in the foreground).

---

## 11. Implementation Phases

### Phase 1 (Core Features, estimated 3-4 days)

1. Backend `NotificationRepository` + `Service` + API handlers
2. Database `migrateMainV7`
3. SSE push integration (`EventBroker` + `PluginEventBus`)
4. Frontend `NotificationBell` + `NotificationPanel`
5. Frontend `useNotifications` hook + `useEventStream` integration
6. Host internal notification sending (build complete, plugin crashed, etc.)
7. Plugin `notification:send` permission

### Phase 2 (Enhanced Features, estimated 2-3 days)

1. `NotificationToast` bottom-right popup notification
2. Notification category filtering (toggle by source/type tabs)
3. `panel://xxx` protocol support: Click notification to navigate to a specific panel
4. Footer center area brief notification summary
5. Desktop notification sound

### Phase 3 (Optional Extensions)

1. Notification grouping (merge and collapse notifications from the same source)
2. Notification priority (high-priority toasts don't auto-dismiss)
3. Notification subscription preferences (users can disable notifications from a specific source)
4. Notification batch operations (clear all, clear by type)
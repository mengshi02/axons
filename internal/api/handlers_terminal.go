package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/terminal"
	"go.uber.org/zap"
)

// TerminalManager manages terminal sessions (set by server initialization)
var terminalMgr *terminal.Manager
var terminalMgrOnce sync.Once

// WebSocket upgrader with improved settings
var upgrader = websocket.Upgrader{
	ReadBufferSize:   4096,
	WriteBufferSize:  4096,
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Add proper origin checking for production
		return true
	},
}

// wsWriter provides thread-safe WebSocket message writing.
// It uses a single goroutine to serialize all write operations,
// preventing "concurrent write to websocket connection" errors.
// Uses atomic.Bool + WaitGroup to avoid "send on closed channel" panics.
type wsWriter struct {
	conn    *websocket.Conn
	writeCh chan []byte
	errCh   chan error
	closed  atomic.Bool // atomic flag: set before closing channels
	once    sync.Once
	wg      sync.WaitGroup // tracks in-flight Write calls
}

// newWsWriter creates a new thread-safe WebSocket writer.
func newWsWriter(conn *websocket.Conn) *wsWriter {
	w := &wsWriter{
		conn:    conn,
		writeCh: make(chan []byte, 4096),
		errCh:   make(chan error, 1),
	}
	go w.run()
	return w
}

// run is the single goroutine that handles all WebSocket writes.
func (w *wsWriter) run() {
	for data := range w.writeCh {
		if w.closed.Load() {
			return
		}
		if err := w.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			select {
			case w.errCh <- err:
			default:
			}
			return
		}
	}
}

// Write queues a message to be written to the WebSocket.
// It blocks for up to writeTimeout when the internal buffer is full, applying
// backpressure to the caller instead of silently dropping data.
func (w *wsWriter) Write(data []byte) {
	if w.closed.Load() {
		return
	}
	w.wg.Add(1)
	defer w.wg.Done()
	// Double-check after WaitGroup.Add to prevent race with Close
	if w.closed.Load() {
		return
	}
	select {
	case w.writeCh <- data:
		return
	default:
	}
	// Buffer full — block with timeout instead of dropping.
	// This propagates backpressure to the terminal output loop.
	const writeTimeout = 5 * time.Second
	select {
	case w.writeCh <- data:
	case <-time.After(writeTimeout):
		// Truly stuck — log and drop rather than block forever.
		zap.L().Warn("wsWriter buffer full after timeout, dropping message")
	}
}

// WriteAndWait writes a message and waits for it to be sent.
// This is useful for critical messages like exit notifications.
func (w *wsWriter) WriteAndWait(data []byte) error {
	if w.closed.Load() {
		return errors.New("writer closed")
	}
	w.wg.Add(1)
	defer w.wg.Done()
	if w.closed.Load() {
		return errors.New("writer closed")
	}
	select {
	case w.writeCh <- data:
		// Wait a short time for the write to complete
		time.Sleep(10 * time.Millisecond)
		select {
		case err := <-w.errCh:
			return err
		default:
			return nil
		}
	default:
		return errors.New("writer buffer full")
	}
}

// Close stops the writer and closes the WebSocket connection.
// It waits for all in-flight Write calls to complete before closing channels.
// It is safe to call multiple times.
func (w *wsWriter) Close() {
	w.once.Do(func() {
		w.closed.Store(true) // Mark closed first - no new Write calls will proceed
		w.wg.Wait()          // Wait for all in-flight Write calls to finish
		close(w.writeCh)     // Safe to close now - no one is sending
		w.conn.Close()
	})
}

// Err returns a channel that receives write errors.
func (w *wsWriter) Err() <-chan error {
	return w.errCh
}

// InitTerminalManager initializes the terminal manager.
func InitTerminalManager(cfg terminal.ManagerConfig) {
	terminalMgrOnce.Do(func() {
		terminalMgr = terminal.NewManager(cfg)
	})
}

// GetTerminalManager returns the terminal manager instance.
func GetTerminalManager() *terminal.Manager {
	return terminalMgr
}

// handleTerminalCreate creates a new terminal session.
func (s *Server) handleTerminalCreate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if terminalMgr == nil {
		zap.L().Error("Terminal manager not initialized")
		http.Error(w, "terminal feature not enabled", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		CWD   string `json:"cwd"`
		Shell string `json:"shell"`
		Cols  uint16 `json:"cols"`
		Rows  uint16 `json:"rows"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.CWD == "" {
		req.CWD = "/"
	}
	if req.Cols == 0 {
		req.Cols = 120
	}
	if req.Rows == 0 {
		req.Rows = 40
	}

	// Create session
	session, err := terminalMgr.CreateSession(req.CWD, req.Shell, req.Cols, req.Rows)
	if err != nil {
		zap.L().Error("Failed to create terminal session", zap.Error(err))

		// Return structured JSON error for better frontend handling
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)

		userMsg := "Failed to create terminal session"
		if strings.Contains(err.Error(), "PTY resources exhausted") {
			userMsg = "Terminal device unavailable — system PTY resources exhausted. Try closing other terminal applications (e.g. VS Code terminals) and retry."
		} else if strings.Contains(err.Error(), "device not configured") {
			userMsg = "Terminal device temporarily unavailable. Please try again."
		} else if strings.Contains(err.Error(), "maximum number of sessions") {
			userMsg = err.Error()
		}

		json.NewEncoder(w).Encode(map[string]string{
			"error":   userMsg,
			"details": err.Error(),
		})
		return
	}

	// Return session info
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         session.ID,
		"pid":        session.PID,
		"cwd":        session.CWD,
		"shell":      session.Shell,
		"created_at": session.CreatedAt,
		"status":     session.Status,
	}); err != nil {
		zap.L().Error("Failed to encode response", zap.Error(err))
	}
}

// handleTerminalWS handles WebSocket connection for terminal.
// WebSocket is treated as a temporary consumer of session output.
// Disconnection does NOT kill the session - the client can reconnect and resume.
func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if terminalMgr == nil {
		http.Error(w, "terminal feature not enabled", http.StatusServiceUnavailable)
		return
	}

	sessionID := params.ByName("id")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Get session
	session, err := terminalMgr.GetSession(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.L().Error("Failed to upgrade WebSocket", zap.String("sessionID", sessionID), zap.Error(err))
		return
	}

	// Set read deadline for ping/pong timeout detection
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Create thread-safe writer
	writer := newWsWriter(conn)

	zap.L().Debug("WebSocket connected", zap.String("sessionID", sessionID))

	// Subscribe to session output
	subID := fmt.Sprintf("ws-%d", time.Now().UnixNano())
	subCh, _ := session.Subscribe(subID)

	// Context for coordinating goroutine shutdown
	ctx, cancel := context.WithCancel(context.Background())

	// WaitGroup to track the output reader goroutine
	var outputWg sync.WaitGroup

	// Read from session output subscription and send to WebSocket
	outputWg.Add(1)
	go func() {
		defer outputWg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-subCh:
				if !ok {
					// Channel closed (session exited or unsubscribed)
					cancel()
					return
				}
				msg := terminal.NewOutputMessage(string(entry.Data))
				data, err := terminal.EncodeMessage(msg)
				if err != nil {
					zap.L().Error("Failed to encode output message",
						zap.String("sessionID", sessionID), zap.Error(err))
					continue
				}
				writer.Write(data)
			}
		}
	}()

	// Set up exit handler to notify this WebSocket connection
	session.AddOnExit(func(code int) {
		// Send exit message to client
		msg := terminal.NewExitMessage(code)
		data, _ := terminal.EncodeMessage(msg)
		if err := writer.WriteAndWait(data); err != nil {
			zap.L().Debug("Failed to send exit message",
				zap.String("sessionID", sessionID), zap.Error(err))
		}
		// Cancel context to signal output goroutine to stop
		cancel()
	})

	// Track if user explicitly closed the terminal (vs accidental disconnect)
	userInitiatedClose := false

	// Read from WebSocket and write to terminal
	for {
		select {
		case <-ctx.Done():
			goto cleanup
		case err := <-writer.Err():
			zap.L().Debug("WebSocket write error", zap.String("sessionID", sessionID), zap.Error(err))
			goto cleanup
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
					zap.L().Debug("WebSocket read error", zap.String("sessionID", sessionID), zap.Error(err))
				}
				// WebSocket disconnected - only kill session if user initiated close
				if !userInitiatedClose {
					zap.L().Debug("WebSocket disconnected unexpectedly, keeping session alive for reconnect",
						zap.String("sessionID", sessionID))
				}
				goto cleanup
			}

			// Reset read deadline on successful read
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))

			// Decode message
			msg, err := terminal.DecodeMessage(message)
			if err != nil {
				zap.L().Error("Failed to decode message", zap.String("sessionID", sessionID), zap.Error(err))
				continue
			}

			// Handle message
			switch msg.Type {
			case terminal.MessageTypeInput:
				if err := session.Write([]byte(msg.Data)); err != nil {
					zap.L().Debug("Failed to write to terminal", zap.String("sessionID", sessionID), zap.Error(err))
				}

			case terminal.MessageTypeResize:
				if err := session.Resize(msg.Cols, msg.Rows); err != nil {
					zap.L().Debug("Failed to resize terminal", zap.String("sessionID", sessionID), zap.Error(err))
				}

			case terminal.MessageTypePing:
				// Respond to ping with pong
				pong := terminal.Message{Type: terminal.MessageTypePong}
				data, _ := terminal.EncodeMessage(pong)
				writer.Write(data)

			case terminal.MessageTypeClose:
				// User explicitly closed the terminal - mark and kill session
				userInitiatedClose = true
				zap.L().Debug("User initiated close, killing session", zap.String("sessionID", sessionID))
				terminalMgr.KillSession(sessionID)
				goto cleanup

			case terminal.MessageTypeResume:
				// Client requests replay from a sequence number
				sinceSeq := msg.Seq
				entries := session.ReplaySince(sinceSeq)
				if len(entries) > 0 {
					// Send replayed output as a single batch message
					var replayData strings.Builder
					for _, entry := range entries {
						replayData.Write(entry.Data)
					}
					replayMsg := terminal.NewReplayMessage(replayData.String())
					data, _ := terminal.EncodeMessage(replayMsg)
					writer.Write(data)
				}
				// Send sync message with current sequence number
				syncMsg := terminal.NewSyncMessage(session.LatestSeq())
				data, _ := terminal.EncodeMessage(syncMsg)
				writer.Write(data)
			}
		}
	}

cleanup:
	// Unsubscribe from session output first (stops the output goroutine from receiving)
	session.Unsubscribe(subID)
	// Cancel context to ensure output goroutine exits
	cancel()
	// Wait for output goroutine to finish before closing writer
	outputWg.Wait()
	// Now safe to close writer (no more Write calls in flight)
	writer.Close()
}

// handleTerminalKill kills a terminal session.
func (s *Server) handleTerminalKill(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if terminalMgr == nil {
		http.Error(w, "terminal feature not enabled", http.StatusServiceUnavailable)
		return
	}

	sessionID := params.ByName("id")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	if err := terminalMgr.KillSession(sessionID); err != nil {
		// Session already killed (e.g., via WebSocket close message) - treat as idempotent success
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleTerminalList lists all terminal sessions.
func (s *Server) handleTerminalList(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if terminalMgr == nil {
		http.Error(w, "terminal feature not enabled", http.StatusServiceUnavailable)
		return
	}

	sessions := terminalMgr.ListSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sessions); err != nil {
		zap.L().Error("Failed to encode sessions", zap.Error(err))
	}
}

// handleTerminalGet returns a single terminal session by ID.
func (s *Server) handleTerminalGet(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if terminalMgr == nil {
		http.Error(w, "terminal feature not enabled", http.StatusServiceUnavailable)
		return
	}

	sessionID := params.ByName("id")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	session, err := terminalMgr.GetSession(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         session.ID,
		"pid":        session.PID,
		"cwd":        session.CWD,
		"shell":      session.Shell,
		"created_at": session.CreatedAt,
		"status":     session.Status,
	}); err != nil {
		zap.L().Error("Failed to encode session", zap.Error(err))
	}
}

// handleTerminalResize resizes a terminal session.
func (s *Server) handleTerminalResize(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if terminalMgr == nil {
		http.Error(w, "terminal feature not enabled", http.StatusServiceUnavailable)
		return
	}

	sessionID := params.ByName("id")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Cols == 0 || req.Rows == 0 {
		http.Error(w, "invalid terminal size", http.StatusBadRequest)
		return
	}

	session, err := terminalMgr.GetSession(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := session.Resize(req.Cols, req.Rows); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleTerminalKillAll kills all terminal sessions.
func (s *Server) handleTerminalKillAll(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if terminalMgr == nil {
		http.Error(w, "terminal feature not enabled", http.StatusServiceUnavailable)
		return
	}

	count := terminalMgr.KillAllSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"killed": count,
	}); err != nil {
		zap.L().Error("Failed to encode response", zap.Error(err))
	}
}
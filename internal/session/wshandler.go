package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/auth"
)

// WSSendService is the subset of *Service consumed by the WebSocket handler.
// Declared locally so tests can supply a mock.
type WSSendService interface {
	GetSession(ctx context.Context, tenantID, userID, id uuid.UUID) (*Session, error)
	SendMessage(ctx context.Context, tenantID, userID, sid uuid.UUID,
		content string, onEvent func(agent.Event) error) error
}

// WSHandler upgrades /sessions/:id/ws to a WebSocket and bridges client frames
// to Service.SendMessage. Each connection runs an independent reader; one
// SendMessage is allowed in-flight at a time per connection.
type WSHandler struct {
	svc      WSSendService
	upgrader websocket.Upgrader
}

// NewWSHandler constructs a WSHandler. allowedOrigins controls the WS handshake
// Origin check: pass {"*"} to allow any origin (development); pass a list of
// fully-qualified origins to restrict to those exact strings.
func NewWSHandler(svc WSSendService, allowedOrigins []string) *WSHandler {
	allowAll := false
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = struct{}{}
	}
	return &WSHandler{
		svc: svc,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				if allowAll {
					return true
				}
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // non-browser client (e.g. wscat without Origin)
				}
				_, ok := allowed[origin]
				return ok
			},
		},
	}
}

// Register mounts the WS route on rg. rg should already have auth.Middleware
// applied so the JWT is parsed during the HTTP handshake.
func (h *WSHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/sessions/:id/ws", h.serve)
}

const (
	wsReadDeadline  = 70 * time.Second
	wsWriteDeadline = 10 * time.Second
	wsPingPeriod    = 25 * time.Second
	wsMaxMessage    = 64 * 1024 // 64 KB user content cap
)

func (h *WSHandler) serve(c *gin.Context) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	sid, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: id"})
		return
	}
	// Pre-upgrade check: session must exist for this caller; cross-tenant
	// access returns 404 (no existence leak) and never upgrades.
	if _, err := h.svc.GetSession(c.Request.Context(), cl.TenantID, cl.UserID, sid); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // Upgrader already wrote an HTTP error.
	}
	defer conn.Close()

	conn.SetReadLimit(wsMaxMessage)
	_ = conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	})

	c2 := newWSConn(conn)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Server-driven ping goroutine to keep the connection alive.
	go c2.pingLoop(ctx)

	c2.readLoop(ctx, cl.TenantID, cl.UserID, sid, h.svc)
}

// wsConn is a thin wrapper that serializes writes via a mutex (gorilla allows
// only one concurrent writer per connection).
type wsConn struct {
	c     *websocket.Conn
	wmu   sync.Mutex
	busy  bool // true while a SendMessage is in flight; one-in-flight rule
	busmu sync.Mutex
}

func newWSConn(c *websocket.Conn) *wsConn { return &wsConn{c: c} }

func (w *wsConn) writeJSON(v any) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	_ = w.c.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
	return w.c.WriteJSON(v)
}

func (w *wsConn) writeClose(code int, reason string) {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	_ = w.c.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
	_ = w.c.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason))
}

// pingLoop sends a websocket ping every wsPingPeriod. It exits when ctx is
// cancelled, which happens when the read loop returns (client disconnect or
// read error).
func (w *wsConn) pingLoop(ctx context.Context) {
	t := time.NewTicker(wsPingPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.wmu.Lock()
			_ = w.c.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
			err := w.c.WriteMessage(websocket.PingMessage, nil)
			w.wmu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// inboundFrame is the union of client → server frame types. type=user_message
// requires content; type=ping carries no payload.
type inboundFrame struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
}

// outEvent is the {"type":"event", "event": ...} server → client frame.
type outEvent struct {
	Type  string      `json:"type"`
	Event agent.Event `json:"event"`
}

func (w *wsConn) readLoop(ctx context.Context, tid, uid, sid uuid.UUID, svc WSSendService) {
	for {
		// One ReadMessage per iteration; auto-renewed ReadDeadline through
		// pong handler.
		_, raw, err := w.c.ReadMessage()
		if err != nil {
			return
		}
		var f inboundFrame
		if jerr := json.Unmarshal(raw, &f); jerr != nil {
			w.writeClose(websocket.CloseUnsupportedData, "invalid_json")
			return
		}
		switch f.Type {
		case FrameTypePing:
			_ = w.writeJSON(map[string]string{"type": FrameTypePong})
		case FrameTypeUserMessage:
			if !w.tryAcquire() {
				_ = w.writeJSON(map[string]any{
					"type": FrameTypeError, "message": "busy: another message in flight",
				})
				continue
			}
			// Run in a goroutine so the read loop can keep noticing client
			// disconnects (which cancel ctx and abort SendMessage).
			go func(content string) {
				defer w.release()
				w.handleUserMessage(ctx, tid, uid, sid, content, svc)
			}(f.Content)
		default:
			w.writeClose(websocket.CloseUnsupportedData, "unknown_frame_type")
			return
		}
	}
}

func (w *wsConn) tryAcquire() bool {
	w.busmu.Lock()
	defer w.busmu.Unlock()
	if w.busy {
		return false
	}
	w.busy = true
	return true
}

func (w *wsConn) release() {
	w.busmu.Lock()
	defer w.busmu.Unlock()
	w.busy = false
}

// handleUserMessage drives one SendMessage call, streaming each Event as a
// server → client frame. On engine error an error frame is written; on success
// a done frame is written. Synchronous within the read loop so we keep the
// one-in-flight rule trivially correct.
func (w *wsConn) handleUserMessage(
	ctx context.Context, tid, uid, sid uuid.UUID, content string,
	svc WSSendService,
) {
	var writeErr error
	err := svc.SendMessage(ctx, tid, uid, sid, content, func(ev agent.Event) error {
		if writeErr != nil {
			return writeErr
		}
		if werr := w.writeJSON(outEvent{Type: FrameTypeEvent, Event: ev}); werr != nil {
			writeErr = werr
			return werr
		}
		return nil
	})
	if writeErr != nil {
		// Client gone; nothing more to send.
		return
	}
	if err != nil {
		code := "internal"
		switch {
		case errors.Is(err, ErrSessionArchived):
			code = "archived"
		case errors.Is(err, ErrEmptyContent):
			code = "empty_content"
		case errors.Is(err, ErrSessionNotFound):
			code = "not_found"
		}
		_ = w.writeJSON(map[string]any{
			"type": FrameTypeError, "code": code, "message": err.Error(),
		})
		return
	}
	_ = w.writeJSON(map[string]string{"type": FrameTypeDone})
}

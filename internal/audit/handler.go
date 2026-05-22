package audit

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// HandlerService is the subset of *Service consumed by the HTTP handler.
// Declared locally so handler tests can inject mocks without importing pgxpool.
type HandlerService interface {
	List(ctx context.Context, f ListFilter) ([]Entry, int, error)
	Verify(ctx context.Context, fromID int64) (*VerifyResult, error)
}

// TenantReader returns the tenant ID for the current request. main.go provides
// the production impl that decodes auth.Claims; tests can pass a fixed UUID.
// Declared here (rather than importing auth) to keep the audit package free of
// an audit ↔ auth import cycle (auth.Handler instruments with audit.Sink).
type TenantReader func(c *gin.Context) (uuid.UUID, bool)

// Handler exposes admin-only audit query endpoints. Mounted onto a
// gin.RouterGroup wrapped with both auth.Middleware AND auth.RequireAdmin so
// the handler can assume Claims are present AND Role == "admin".
type Handler struct {
	svc      HandlerService
	tenantOf TenantReader
}

// NewHandler constructs an audit Handler. tenantOf must extract the tenant ID
// from the gin request context (typically wrapping auth.FromCtx); it's never
// expected to return false in production because RequireAdmin upstream already
// guarantees Claims are present, but the contract is explicit for tests.
func NewHandler(svc HandlerService, tenantOf TenantReader) *Handler {
	return &Handler{svc: svc, tenantOf: tenantOf}
}

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/audit", h.list)
	rg.GET("/audit/verify", h.verify)
}

type entryDTO struct {
	OccurredAt time.Time      `json:"occurred_at"`
	TenantID   *uuid.UUID     `json:"tenant_id,omitempty"`
	UserID     *uuid.UUID     `json:"user_id,omitempty"`
	Action     string         `json:"action"`
	Target     string         `json:"target"`
	Method     string         `json:"method"`
	Path       string         `json:"path"`
	Status     int            `json:"status"`
	DurationMS int            `json:"duration_ms"`
	Metadata   map[string]any `json:"metadata"`
}

type listResp struct {
	Entries []entryDTO `json:"entries"`
	Total   int        `json:"total"`
	Limit   int        `json:"limit"`
	Offset  int        `json:"offset"`
}

func (h *Handler) list(c *gin.Context) {
	tid, ok := h.tenantOf(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	f := ListFilter{
		TenantID: tid,
		Action:   c.Query("action"),
	}
	if v := c.Query("user_id"); v != "" {
		uid, err := uuid.Parse(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: user_id"})
			return
		}
		f.UserID = &uid
	}
	if v := c.Query("from"); v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: from"})
			return
		}
		f.From = &ts
	}
	if v := c.Query("to"); v != "" {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: to"})
			return
		}
		f.To = &ts
	}
	if v := c.Query("min_status"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: min_status"})
			return
		}
		f.MinStatus = n
	}
	if v := c.Query("max_status"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: max_status"})
			return
		}
		f.MaxStatus = n
	}
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Offset = n
		}
	}

	entries, total, err := h.svc.List(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	dto := make([]entryDTO, 0, len(entries))
	for _, e := range entries {
		dto = append(dto, entryDTO{
			OccurredAt: e.OccurredAt, TenantID: e.TenantID, UserID: e.UserID,
			Action: e.Action, Target: e.Target, Method: e.Method, Path: e.Path,
			Status: e.Status, DurationMS: e.DurationMS, Metadata: e.Metadata,
		})
	}
	c.JSON(http.StatusOK, listResp{
		Entries: dto, Total: total, Limit: limit, Offset: f.Offset,
	})
}

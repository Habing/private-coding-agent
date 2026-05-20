package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// HandlerService is the subset of *Service consumed by the REST handler.
// Declared locally so handler_test.go can supply a mock without standing up a
// real Repo.
type HandlerService interface {
	Create(ctx context.Context, tenantID, userID uuid.UUID, req CreateRequest) (*Memory, error)
	Get(ctx context.Context, tenantID, userID, id uuid.UUID) (*Memory, error)
	List(ctx context.Context, tenantID, userID uuid.UUID, f ListFilter) ([]Memory, error)
	Update(ctx context.Context, tenantID, userID, id uuid.UUID, req UpdateRequest) (*Memory, error)
	Delete(ctx context.Context, tenantID, userID, id uuid.UUID) error
}

// Handler exposes /memories/* REST endpoints. Mounted onto a gin.RouterGroup
// that has been wrapped with auth.Middleware by main.
type Handler struct {
	svc HandlerService
}

func NewHandler(svc HandlerService) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/memories")
	g.POST("", h.create)
	g.GET("", h.list)
	g.GET("/:id", h.get)
	g.PUT("/:id", h.update)
	g.DELETE("/:id", h.delete)
}

func (h *Handler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return cl, true
}

func (h *Handler) parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: id"})
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) mapErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrMemoryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, ErrEmptyContent):
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: content"})
	case errors.Is(err, ErrInvalidType):
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: type"})
	case errors.Is(err, ErrEmptySearch):
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: empty_search"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
	}
}

func (h *Handler) create(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: body"})
		return
	}
	m, err := h.svc.Create(c.Request.Context(), cl.TenantID, cl.UserID, req)
	if err != nil {
		h.mapErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, m)
}

type listResp struct {
	Memories []Memory `json:"memories"`
}

func (h *Handler) list(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	f := ListFilter{
		Type:  c.Query("type"),
		Query: c.Query("q"),
	}
	if tag := c.Query("tag"); tag != "" {
		f.Tags = []string{tag}
	}
	if tags := c.QueryArray("tags"); len(tags) > 0 {
		f.Tags = append(f.Tags, tags...)
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
	rows, err := h.svc.List(c.Request.Context(), cl.TenantID, cl.UserID, f)
	if err != nil {
		h.mapErr(c, err)
		return
	}
	c.JSON(http.StatusOK, listResp{Memories: rows})
}

func (h *Handler) get(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	m, err := h.svc.Get(c.Request.Context(), cl.TenantID, cl.UserID, id)
	if err != nil {
		h.mapErr(c, err)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) update(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	raw, err := readUpdateBody(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: body"})
		return
	}
	req, err := parseUpdate(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: " + err.Error()})
		return
	}
	m, err := h.svc.Update(c.Request.Context(), cl.TenantID, cl.UserID, id, req)
	if err != nil {
		h.mapErr(c, err)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) delete(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), cl.TenantID, cl.UserID, id); err != nil {
		h.mapErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func readUpdateBody(c *gin.Context) (map[string]json.RawMessage, error) {
	raw := map[string]json.RawMessage{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func parseUpdate(raw map[string]json.RawMessage) (UpdateRequest, error) {
	var req UpdateRequest
	if v, ok := raw["type"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return req, errors.New("type")
		}
		req.Type = &s
	}
	if v, ok := raw["content"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return req, errors.New("content")
		}
		req.Content = &s
	}
	if v, ok := raw["tags"]; ok {
		req.TagsSet = true
		if strings.TrimSpace(string(v)) != "null" {
			if err := json.Unmarshal(v, &req.Tags); err != nil {
				return req, errors.New("tags")
			}
		}
	}
	return req, nil
}

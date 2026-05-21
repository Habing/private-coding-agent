package skills

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// Handler exposes read-only /skills/* endpoints. The registry is shared across
// tenants — Skills are global, scoped only by auth (any authenticated caller
// can list / fetch). Body is opt-in via ?include=body to keep List light.
type Handler struct {
	reg *Registry
}

func NewHandler(reg *Registry) *Handler { return &Handler{reg: reg} }

func (h *Handler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/skills")
	g.GET("", h.list)
	g.GET("/:id", h.get)
}

func (h *Handler) authed(c *gin.Context) bool {
	if auth.FromCtx(c.Request.Context()) == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return false
	}
	return true
}

func (h *Handler) list(c *gin.Context) {
	if !h.authed(c) {
		return
	}
	if h.reg == nil {
		c.JSON(http.StatusOK, gin.H{"skills": []SkillMeta{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"skills": h.reg.List()})
}

// skillView is the response shape for GET /skills/:id. Body present only when
// include=body. SourcePath is never exposed (audit/debug only).
type skillView struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Version     string `json:"version"`
	CharCount   int    `json:"char_count"`
	Body        string `json:"body,omitempty"`
}

func (h *Handler) get(c *gin.Context) {
	if !h.authed(c) {
		return
	}
	id := c.Param("id")
	if h.reg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	sk, ok := h.reg.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	view := skillView{
		ID:          sk.ID,
		Description: sk.Description,
		Version:     sk.Version,
		CharCount:   sk.CharCount,
	}
	if c.Query("include") == "body" {
		view.Body = sk.Body
	}
	c.JSON(http.StatusOK, view)
}

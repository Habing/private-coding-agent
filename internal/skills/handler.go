package skills

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// Handler exposes read-only /skills/* endpoints. The filesystem registry is
// global; tenant DB skills (12b) are layered on top when a DBLookup is wired.
// Body is opt-in via ?include=body to keep List light.
type Handler struct {
	reg *Registry
	db  DBLookup
}

func NewHandler(reg *Registry) *Handler { return &Handler{reg: reg} }

// WithDBLookup wires the tenant Skills DB; nil resets to FS-only.
func (h *Handler) WithDBLookup(db DBLookup) *Handler {
	if h != nil {
		h.db = db
	}
	return h
}

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
	cl := auth.FromCtx(c.Request.Context())
	combined := map[string]SkillMeta{}
	if h.reg != nil {
		for _, m := range h.reg.List() {
			combined[m.ID] = m
		}
	}
	if h.db != nil && cl != nil {
		if rows, err := h.db.ListEnabled(c.Request.Context(), cl.TenantID); err == nil {
			for _, row := range rows {
				sk := row.ToSkill()
				combined[sk.ID] = SkillMeta{
					ID:          sk.ID,
					Description: sk.Description,
					Version:     sk.Version,
					CharCount:   sk.CharCount,
				}
			}
		}
	}
	out := make([]SkillMeta, 0, len(combined))
	for _, m := range combined {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.JSON(http.StatusOK, gin.H{"skills": out})
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
	cl := auth.FromCtx(c.Request.Context())
	var sk *Skill
	if h.db != nil && cl != nil {
		// DB wins over FS on duplicate key, matching Resolver semantics.
		if row, err := h.db.ListEnabled(c.Request.Context(), cl.TenantID); err == nil {
			for i := range row {
				if row[i].SkillKey == id {
					sk = row[i].ToSkill()
					break
				}
			}
		}
	}
	if sk == nil && h.reg != nil {
		if s, ok := h.reg.Get(id); ok {
			sk = s
		}
	}
	if sk == nil {
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

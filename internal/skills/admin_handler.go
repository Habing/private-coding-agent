package skills

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
)

// AdminHandler exposes tenant Skills CRUD + per-profile binding under
// /admin/skills and /admin/profiles/:name/skills. The router group must
// already apply auth.Middleware + auth.RequireAdmin before mounting.
type AdminHandler struct {
	db    *DBRepo
	audit audit.Sink
	// maxBodyChars is the upper bound on Skill body size. 0 means no limit.
	maxBodyChars int
}

func NewAdminHandler(db *DBRepo) *AdminHandler {
	return &AdminHandler{db: db, maxBodyChars: 64 * 1024}
}

// WithAuditSink wires a sink for "skill.admin.*" entries. Optional.
func (h *AdminHandler) WithAuditSink(s audit.Sink) *AdminHandler {
	if h != nil {
		h.audit = s
	}
	return h
}

func (h *AdminHandler) Register(rg *gin.RouterGroup) {
	skills := rg.Group("/admin/skills")
	skills.POST("", h.create)
	skills.GET("", h.list)
	skills.GET("/:key", h.get)
	skills.PUT("/:key", h.update)
	skills.DELETE("/:key", h.delete)

	profiles := rg.Group("/admin/profiles")
	profiles.GET("/:name/skills", h.getProfileSkills)
	profiles.PUT("/:name/skills", h.setProfileSkills)
}

var skillKeyRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$|^[a-z0-9]$`)

type createReq struct {
	SkillKey    string `json:"skill_key"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

type updateReq struct {
	Description *string `json:"description,omitempty"`
	Body        *string `json:"body,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

type setProfileReq struct {
	SkillKeys []string `json:"skill_keys"`
}

func (h *AdminHandler) tenant(c *gin.Context) (uuid.UUID, *auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return uuid.Nil, nil, false
	}
	return cl.TenantID, cl, true
}

func (h *AdminHandler) create(c *gin.Context) {
	tid, cl, ok := h.tenant(c)
	if !ok {
		return
	}
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if !skillKeyRE.MatchString(req.SkillKey) || len(req.SkillKey) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_skill_key"})
		return
	}
	if req.Body == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body_required"})
		return
	}
	if h.maxBodyChars > 0 && len(req.Body) > h.maxBodyChars {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body_too_large", "max": h.maxBodyChars})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	in := &DBSkill{
		ID: uuid.New(), TenantID: tid, SkillKey: req.SkillKey,
		Description: req.Description, Body: req.Body, Enabled: enabled,
	}
	out, err := h.db.Insert(c.Request.Context(), in)
	if err != nil {
		if errors.Is(err, ErrSkillKeyConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "skill_key_conflict"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	h.auditAdmin(cl, "skill.admin.create", out.SkillKey, map[string]any{
		"skill_id": out.ID.String(), "enabled": out.Enabled, "chars": len(out.Body),
	})
	c.JSON(http.StatusCreated, out)
}

func (h *AdminHandler) list(c *gin.Context) {
	tid, _, ok := h.tenant(c)
	if !ok {
		return
	}
	rows, err := h.db.List(c.Request.Context(), tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	// Strip body from list view by default to keep responses small.
	includeBody := c.Query("include") == "body"
	if !includeBody {
		for i := range rows {
			rows[i].Body = ""
		}
	}
	c.JSON(http.StatusOK, gin.H{"skills": rows})
}

func (h *AdminHandler) get(c *gin.Context) {
	tid, _, ok := h.tenant(c)
	if !ok {
		return
	}
	key := c.Param("key")
	row, err := h.db.GetByKey(c.Request.Context(), tid, key)
	if err != nil {
		if errors.Is(err, ErrSkillNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *AdminHandler) update(c *gin.Context) {
	tid, cl, ok := h.tenant(c)
	if !ok {
		return
	}
	key := c.Param("key")
	var req updateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if req.Body != nil && h.maxBodyChars > 0 && len(*req.Body) > h.maxBodyChars {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body_too_large", "max": h.maxBodyChars})
		return
	}
	out, err := h.db.Update(c.Request.Context(), tid, key, req.Description, req.Body, req.Enabled)
	if err != nil {
		if errors.Is(err, ErrSkillNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	h.auditAdmin(cl, "skill.admin.update", out.SkillKey, map[string]any{
		"skill_id":          out.ID.String(),
		"enabled":           out.Enabled,
		"updated_body":      req.Body != nil,
		"updated_enabled":   req.Enabled != nil,
		"updated_descr":     req.Description != nil,
	})
	c.JSON(http.StatusOK, out)
}

func (h *AdminHandler) delete(c *gin.Context) {
	tid, cl, ok := h.tenant(c)
	if !ok {
		return
	}
	key := c.Param("key")
	err := h.db.Delete(c.Request.Context(), tid, key)
	if err != nil {
		if errors.Is(err, ErrSkillNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	h.auditAdmin(cl, "skill.admin.delete", key, nil)
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) getProfileSkills(c *gin.Context) {
	tid, _, ok := h.tenant(c)
	if !ok {
		return
	}
	name := c.Param("name")
	keys, err := h.db.GetForProfile(c.Request.Context(), tid, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"profile": name, "skill_keys": keys})
}

func (h *AdminHandler) setProfileSkills(c *gin.Context) {
	tid, cl, ok := h.tenant(c)
	if !ok {
		return
	}
	name := c.Param("name")
	var req setProfileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	for _, k := range req.SkillKeys {
		if !skillKeyRE.MatchString(k) || len(k) > 64 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_skill_key", "key": k})
			return
		}
	}
	if err := h.db.SetForProfile(c.Request.Context(), tid, name, req.SkillKeys); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	h.auditAdmin(cl, "skill.admin.profile_bind", name, map[string]any{
		"skill_keys": req.SkillKeys,
	})
	c.JSON(http.StatusOK, gin.H{"profile": name, "skill_keys": req.SkillKeys})
}

func (h *AdminHandler) auditAdmin(cl *auth.Claims, action, target string, meta map[string]any) {
	if h.audit == nil || cl == nil {
		return
	}
	tid := cl.TenantID
	uid := cl.UserID
	audit.Detached(h.audit, audit.Entry{
		TenantID: &tid, UserID: &uid,
		Action: action, Target: target,
		Metadata: meta,
	}, nil)
}

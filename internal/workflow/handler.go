package workflow

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// AdminHandler exposes /admin/workflows CRUD + publish/unpublish + invoke +
// runs. The router group must already apply auth.Middleware + auth.RequireAdmin
// before mounting — handler trusts auth.FromCtx claims for tenant scoping.
type AdminHandler struct {
	svc          *Service
	maxBodyChars int
}

func NewAdminHandler(svc *Service) *AdminHandler {
	return &AdminHandler{svc: svc, maxBodyChars: 128 * 1024}
}

func (h *AdminHandler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/admin/workflows")
	g.POST("", h.create)
	g.POST("/graph-preview", h.graphPreview)
	g.GET("", h.list)
	g.GET("/:slug/graph", h.graph)
	g.GET("/:slug", h.get)
	g.PUT("/:slug", h.update)
	g.DELETE("/:slug", h.delete)
	g.POST("/:slug/publish", h.publish)
	g.POST("/:slug/unpublish", h.unpublish)
	g.POST("/:slug/invoke", h.invoke)
	g.GET("/:slug/runs", h.runs)
}

var slugRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$|^[a-z0-9]$`)

type createReq struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DSLYAML     string `json:"dsl_yaml"`
}

type updateReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DSLYAML     string `json:"dsl_yaml"`
}

type invokeReq struct {
	Inputs map[string]any `json:"inputs"`
	DryRun bool           `json:"dry_run"`
}

func (h *AdminHandler) claims(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return uuid.Nil, uuid.Nil, false
	}
	return cl.TenantID, cl.UserID, true
}

func (h *AdminHandler) create(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if !slugRE.MatchString(req.Slug) || len(req.Slug) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_slug"})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name_required"})
		return
	}
	if req.DSLYAML == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dsl_required"})
		return
	}
	if h.maxBodyChars > 0 && len(req.DSLYAML) > h.maxBodyChars {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dsl_too_large", "max": h.maxBodyChars})
		return
	}
	wf, err := h.svc.Create(c.Request.Context(), tid, req.Slug, req.Name, req.Description, req.DSLYAML)
	if err != nil {
		if errors.Is(err, ErrSlugTaken) {
			c.JSON(http.StatusConflict, gin.H{"error": "slug_taken"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "validate", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, wf)
}

func (h *AdminHandler) list(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	rows, err := h.svc.List(c.Request.Context(), tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflows": rows})
}

func (h *AdminHandler) get(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	wf, err := h.svc.Get(c.Request.Context(), tid, c.Param("slug"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, wf)
}

func (h *AdminHandler) update(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	var req updateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if req.DSLYAML == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dsl_required"})
		return
	}
	if h.maxBodyChars > 0 && len(req.DSLYAML) > h.maxBodyChars {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dsl_too_large", "max": h.maxBodyChars})
		return
	}
	wf, err := h.svc.Update(c.Request.Context(), tid, c.Param("slug"), req.Name, req.Description, req.DSLYAML)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "validate", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, wf)
}

func (h *AdminHandler) delete(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), tid, c.Param("slug")); err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) publish(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	if err := h.svc.Publish(c.Request.Context(), tid, c.Param("slug")); err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "publish", "detail": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) unpublish(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	if err := h.svc.Unpublish(c.Request.Context(), tid, c.Param("slug")); err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) invoke(c *gin.Context) {
	tid, uid, ok := h.claims(c)
	if !ok {
		return
	}
	var req invokeReq
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	// ?dry_run=true query arg also flips DryRun so curl-style probes don't need
	// to build a body.
	if v := c.Query("dry_run"); v == "true" || v == "1" {
		req.DryRun = true
	}
	res, err := h.svc.Invoke(c.Request.Context(), tid, uid, c.Param("slug"), req.Inputs, req.DryRun)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *AdminHandler) runs(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	rows, err := h.svc.ListRuns(c.Request.Context(), tid, c.Param("slug"), limit)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": rows})
}

type graphPreviewReq struct {
	DSLYAML string `json:"dsl_yaml"`
}

func (h *AdminHandler) graphPreview(c *gin.Context) {
	if _, _, ok := h.claims(c); !ok {
		return
	}
	var req graphPreviewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if req.DSLYAML == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dsl_required"})
		return
	}
	if h.maxBodyChars > 0 && len(req.DSLYAML) > h.maxBodyChars {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dsl_too_large", "max": h.maxBodyChars})
		return
	}
	g, err := GraphFromYAML(req.DSLYAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, g)
}

func (h *AdminHandler) graph(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	wf, err := h.svc.Get(c.Request.Context(), tid, c.Param("slug"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "detail": err.Error()})
		return
	}
	g, err := GraphFromYAML(wf.DSLYAML)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "parse", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, g)
}

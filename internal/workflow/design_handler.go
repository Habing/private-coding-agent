package workflow

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// ToolLister supplies registered tools for the design editor picker.
type ToolLister interface {
	ListTools(ctx context.Context, tenantID uuid.UUID) []toolbus.ToolDef
}

// WithToolLister enables GET /admin/workflows/tool-schemas.
func (h *AdminHandler) WithToolLister(l ToolLister) *AdminHandler {
	h.toolLister = l
	return h
}

func (h *AdminHandler) registerDesignRoutes(g *gin.RouterGroup) {
	g.POST("/design/compile", h.designCompile)
	g.POST("/design/decompile", h.designDecompile)
	g.GET("/tool-schemas", h.toolSchemas)
}

type designBody struct {
	Design *WorkflowDesign `json:"design"`
	// Slug is the workflow URL slug; when set, compile rejects DSL that cannot be saved via PUT.
	Slug string `json:"slug,omitempty"`
}

type decompileBody struct {
	DSLYAML string `json:"dsl_yaml"`
}

func (h *AdminHandler) designCompile(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	var req designBody
	if err := c.ShouldBindJSON(&req); err != nil || req.Design == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if h.toolLister != nil {
		allow := toolNameSet(h.toolLister.ListTools(c.Request.Context(), tid))
		if err := ValidateDesign(req.Design, allow); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validate", "detail": err.Error()})
			return
		}
	} else if err := ValidateDesign(req.Design, nil); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validate", "detail": err.Error()})
		return
	}
	res, err := CompileDesign(req.Design)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "compile", "detail": err.Error()})
		return
	}
	if req.Slug != "" {
		if req.Design.ID != req.Slug {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "validate",
				"detail": fmt.Sprintf("design id %q does not match slug %q", req.Design.ID, req.Slug),
			})
			return
		}
		if _, err := h.svc.ParseValidateDSL(res.DSLYAML, req.Slug); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validate", "detail": err.Error()})
			return
		}
	} else if _, err := Parse(res.DSLYAML); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *AdminHandler) designDecompile(c *gin.Context) {
	if _, _, ok := h.claims(c); !ok {
		return
	}
	var req decompileBody
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
	res, err := DecompileDesign(req.DSLYAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decompile", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *AdminHandler) toolSchemas(c *gin.Context) {
	tid, _, ok := h.claims(c)
	if !ok {
		return
	}
	if h.toolLister == nil {
		c.JSON(http.StatusOK, gin.H{"tools": []toolbus.ToolDef{}})
		return
	}
	tools := dedupeToolDefs(h.toolLister.ListTools(c.Request.Context(), tid))
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

func dedupeToolDefs(defs []toolbus.ToolDef) []toolbus.ToolDef {
	seen := make(map[string]struct{}, len(defs))
	out := make([]toolbus.ToolDef, 0, len(defs))
	for _, d := range defs {
		if _, ok := seen[d.Name]; ok {
			continue
		}
		seen[d.Name] = struct{}{}
		out = append(out, d)
	}
	return out
}

func toolNameSet(defs []toolbus.ToolDef) map[string]struct{} {
	m := make(map[string]struct{}, len(defs))
	for _, d := range defs {
		m[d.Name] = struct{}{}
	}
	return m
}

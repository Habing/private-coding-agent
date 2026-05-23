package connectors

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/mcp"
)

// AdminHandler exposes connector catalog under /admin/connectors.
type AdminHandler struct {
	mcpRepo          *mcp.Repo
	httpFetchEnabled bool
}

// NewAdminHandler returns a catalog handler. mcpRepo may be nil when MCP is disabled.
func NewAdminHandler(mcpRepo *mcp.Repo, httpFetchEnabled bool) *AdminHandler {
	return &AdminHandler{mcpRepo: mcpRepo, httpFetchEnabled: httpFetchEnabled}
}

// Register wires GET /admin/connectors/catalog.
func (h *AdminHandler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/admin/connectors")
	g.GET("/catalog", h.catalog)
}

func (h *AdminHandler) catalog(c *gin.Context) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var servers []MCPServerView
	if h.mcpRepo != nil {
		rows, err := h.mcpRepo.List(c.Request.Context(), cl.TenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "list mcp servers", "detail": err.Error()})
			return
		}
		servers = mcpRowsToViews(rows)
	}
	c.JSON(http.StatusOK, gin.H{
		"recipes": BuildCatalog(servers, h.httpFetchEnabled),
	})
}

func mcpRowsToViews(rows []mcp.Server) []MCPServerView {
	out := make([]MCPServerView, 0, len(rows))
	for _, s := range rows {
		v := MCPServerView{
			ID:      s.ID.String(),
			Slug:    s.Slug,
			Enabled: s.Enabled,
		}
		for _, t := range s.ToolsCache {
			v.ToolsCache = append(v.ToolsCache, ToolView{Name: t.Name})
		}
		out = append(out, v)
	}
	return out
}

// ListInstalledNotifyTools returns bus tool names suitable for workflow notify slots.
func ListInstalledNotifyTools(servers []MCPServerView, httpFetchEnabled bool) []string {
	cat := BuildCatalog(servers, httpFetchEnabled)
	var out []string
	seen := map[string]struct{}{}
	for _, r := range cat {
		if !r.Enabled {
			continue
		}
		for _, t := range r.Tools {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

package connectors

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/mcp"
	tools "github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

// AdminHandler exposes connector catalog and http.fetch settings under /admin/connectors.
type AdminHandler struct {
	mcpRepo         *mcp.Repo
	httpFetch       *tools.HTTPFetch
	httpFetchRepo   *HTTPFetchRepo
	blockPrivateIPs bool
}

// NewAdminHandler returns a catalog handler. mcpRepo may be nil when MCP is disabled.
func NewAdminHandler(
	mcpRepo *mcp.Repo,
	httpFetch *tools.HTTPFetch,
	httpFetchRepo *HTTPFetchRepo,
	blockPrivateIPs bool,
) *AdminHandler {
	return &AdminHandler{
		mcpRepo:         mcpRepo,
		httpFetch:       httpFetch,
		httpFetchRepo:   httpFetchRepo,
		blockPrivateIPs: blockPrivateIPs,
	}
}

// Register wires connector admin routes.
func (h *AdminHandler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/admin/connectors")
	g.GET("/catalog", h.catalog)
	g.GET("/http-fetch", h.getHTTPFetch)
	g.PUT("/http-fetch", h.putHTTPFetch)
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
	allowHosts := h.currentAllowHosts(c)
	c.JSON(http.StatusOK, gin.H{
		"recipes": BuildCatalog(servers, h.httpFetch != nil, allowHosts),
	})
}

type httpFetchSettingsResponse struct {
	Enabled         bool     `json:"enabled"`
	AllowHosts      []string `json:"allow_hosts"`
	BlockPrivateIPs bool     `json:"block_private_ips"`
}

type httpFetchSettingsRequest struct {
	AllowHosts []string `json:"allow_hosts"`
}

func (h *AdminHandler) getHTTPFetch(c *gin.Context) {
	if auth.FromCtx(c.Request.Context()) == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.httpFetch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "http_fetch_disabled"})
		return
	}
	c.JSON(http.StatusOK, httpFetchSettingsResponse{
		Enabled:         true,
		AllowHosts:      h.currentAllowHosts(c),
		BlockPrivateIPs: h.blockPrivateIPs,
	})
}

func (h *AdminHandler) putHTTPFetch(c *gin.Context) {
	if auth.FromCtx(c.Request.Context()) == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.httpFetch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "http_fetch_disabled"})
		return
	}
	var req httpFetchSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_json", "detail": err.Error()})
		return
	}
	norm, err := ValidateAllowHosts(req.AllowHosts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_allow_hosts", "detail": err.Error()})
		return
	}
	saved := norm
	if h.httpFetchRepo != nil {
		var saveErr error
		saved, saveErr = h.httpFetchRepo.UpsertAllowHosts(c.Request.Context(), norm)
		if saveErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "save_failed", "detail": saveErr.Error()})
			return
		}
	}
	h.httpFetch.SetAllowHosts(saved)
	c.JSON(http.StatusOK, httpFetchSettingsResponse{
		Enabled:         true,
		AllowHosts:      saved,
		BlockPrivateIPs: h.blockPrivateIPs,
	})
}

func (h *AdminHandler) currentAllowHosts(c *gin.Context) []string {
	if h.httpFetch == nil {
		return nil
	}
	if h.httpFetchRepo != nil {
		if hosts, ok, err := h.httpFetchRepo.GetAllowHosts(c.Request.Context()); err == nil && ok {
			return hosts
		}
	}
	return h.httpFetch.AllowHosts()
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
func ListInstalledNotifyTools(servers []MCPServerView, httpFetch *tools.HTTPFetch) []string {
	allow := []string{}
	if httpFetch != nil {
		allow = httpFetch.AllowHosts()
	}
	return listInstalledNotifyTools(servers, httpFetch != nil, allow)
}

func listInstalledNotifyTools(servers []MCPServerView, httpFetchEnabled bool, httpAllowHosts []string) []string {
	cat := BuildCatalog(servers, httpFetchEnabled, httpAllowHosts)
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

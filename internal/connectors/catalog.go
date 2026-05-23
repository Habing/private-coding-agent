package connectors

// Recipe describes a built-in connector type (Slice 25b). Recipes are static;
// live install status is joined from tenant MCP servers or http.fetch config.
type Recipe struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Kind          string   `json:"kind"` // mcp | http_fetch
	MCPSlug       string   `json:"mcp_slug,omitempty"`
	SuggestTools  []string `json:"suggest_tools"`
	SetupURLHint  string   `json:"setup_url_hint,omitempty"`
	DocsPath      string   `json:"docs_path,omitempty"`
	AuthType      string   `json:"auth_type_default,omitempty"`
}

// RecipeStatus is a recipe plus tenant-specific install state.
type RecipeStatus struct {
	Recipe
	Installed bool     `json:"installed"`
	ServerID  string   `json:"server_id,omitempty"`
	Enabled   bool     `json:"enabled"`
	Tools     []string `json:"tools"`
}

var recipes = []Recipe{
	{
		ID:           "slack",
		Name:         "Slack",
		Description:  "通过 MCP 向 Slack 频道发送消息（需部署 Slack MCP 服务）",
		Kind:         "mcp",
		MCPSlug:      "slack",
		SuggestTools: []string{"mcp.slack.post_message", "mcp.slack.post"},
		SetupURLHint: "https://your-slack-mcp.example/mcp",
		DocsPath:     "docs/CONNECTORS.md#slack",
		AuthType:     "bearer",
	},
	{
		ID:           "github",
		Name:         "GitHub",
		Description:  "通过 MCP 创建 Issue / 评论等（需部署 GitHub MCP 服务）",
		Kind:         "mcp",
		MCPSlug:      "github",
		SuggestTools: []string{"mcp.github.create_issue", "mcp.github.add_comment"},
		SetupURLHint: "https://your-github-mcp.example/mcp",
		DocsPath:     "docs/CONNECTORS.md#github",
		AuthType:     "bearer",
	},
	{
		ID:           "dev-mock",
		Name:         "开发 Mock MCP",
		Description:  "Compose 内置 mock-mcp（echo 工具），用于 E2E 与本地验证",
		Kind:         "mcp",
		MCPSlug:      "e2e-mock",
		SuggestTools: []string{"mcp.e2e-mock.echo"},
		SetupURLHint: "http://mock-mcp:8083",
		DocsPath:     "docs/CONNECTORS.md#dev-mock",
		AuthType:     "none",
	},
	{
		ID:           "http-fetch",
		Name:         "HTTP 拉取",
		Description:  "Server 侧 http.fetch（25a）；无需 MCP，在 config 中启用并配置 allow_hosts",
		Kind:         "http_fetch",
		SuggestTools: []string{"http.fetch"},
		DocsPath:     "docs/CONNECTORS.md#http-fetch",
	},
}

// ListRecipes returns a copy of built-in connector recipes.
func ListRecipes() []Recipe {
	out := make([]Recipe, len(recipes))
	copy(out, recipes)
	return out
}

// MCPServerView is the subset of MCP server fields needed for status join.
type MCPServerView struct {
	ID         string
	Slug       string
	Enabled    bool
	ToolsCache []ToolView
}

// ToolView names one tool from an MCP server cache.
type ToolView struct {
	Name string
}

// BuildCatalog joins static recipes with tenant MCP rows and http.fetch enable flag.
func BuildCatalog(servers []MCPServerView, httpFetchEnabled bool) []RecipeStatus {
	bySlug := map[string]MCPServerView{}
	for _, s := range servers {
		bySlug[s.Slug] = s
	}
	out := make([]RecipeStatus, 0, len(recipes))
	for _, r := range recipes {
		st := RecipeStatus{Recipe: r}
		switch r.Kind {
		case "mcp":
			if srv, ok := bySlug[r.MCPSlug]; ok {
				st.Installed = true
				st.ServerID = srv.ID
				st.Enabled = srv.Enabled
				for _, tool := range srv.ToolsCache {
					st.Tools = append(st.Tools, "mcp."+srv.Slug+"."+tool.Name)
				}
			}
		case "http_fetch":
			st.Installed = httpFetchEnabled
			st.Enabled = httpFetchEnabled
			if httpFetchEnabled {
				st.Tools = append(st.Tools, "http.fetch")
			}
		}
		out = append(out, st)
	}
	return out
}

// PickNotifyTool returns the first suggested tool that appears in availableTools,
// or fallback when none match.
func PickNotifyTool(suggested, available []string, fallback string) string {
	avail := map[string]struct{}{}
	for _, t := range available {
		avail[t] = struct{}{}
	}
	for _, s := range suggested {
		if _, ok := avail[s]; ok {
			return s
		}
	}
	return fallback
}

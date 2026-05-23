package connectors_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/connectors"
)

func TestBuildCatalog_MCPInstalled(t *testing.T) {
	cat := connectors.BuildCatalog([]connectors.MCPServerView{
		{
			ID: "srv-1", Slug: "e2e-mock", Enabled: true,
			ToolsCache: []connectors.ToolView{{Name: "echo"}},
		},
	}, false)
	var mock, httpRecipe connectors.RecipeStatus
	for _, r := range cat {
		switch r.ID {
		case "dev-mock":
			mock = r
		case "http-fetch":
			httpRecipe = r
		}
	}
	require.True(t, mock.Installed)
	require.True(t, mock.Enabled)
	require.Equal(t, []string{"mcp.e2e-mock.echo"}, mock.Tools)
	require.False(t, httpRecipe.Installed)
}

func TestBuildCatalog_HTTPFetchEnabled(t *testing.T) {
	cat := connectors.BuildCatalog(nil, true)
	var httpRecipe connectors.RecipeStatus
	for _, r := range cat {
		if r.ID == "http-fetch" {
			httpRecipe = r
		}
	}
	require.True(t, httpRecipe.Installed)
	require.Equal(t, []string{"http.fetch"}, httpRecipe.Tools)
}

func TestPickNotifyTool(t *testing.T) {
	got := connectors.PickNotifyTool(
		[]string{"mcp.slack.post", "llm.chat"},
		[]string{"mcp.e2e-mock.echo", "llm.chat"},
		"llm.chat",
	)
	require.Equal(t, "llm.chat", got)

	got = connectors.PickNotifyTool(
		[]string{"mcp.e2e-mock.echo", "llm.chat"},
		[]string{"mcp.e2e-mock.echo", "llm.chat"},
		"llm.chat",
	)
	require.Equal(t, "mcp.e2e-mock.echo", got)
}

func TestListRecipes_HasSlackAndGitHub(t *testing.T) {
	list := connectors.ListRecipes()
	ids := map[string]struct{}{}
	for _, r := range list {
		ids[r.ID] = struct{}{}
	}
	require.Contains(t, ids, "slack")
	require.Contains(t, ids, "github")
	require.Contains(t, ids, "dev-mock")
}

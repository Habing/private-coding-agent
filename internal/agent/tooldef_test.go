package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

func TestBuildModelTools_ConvertsAndFiltersByAllowlist(t *testing.T) {
	bus := []toolbus.ToolDef{
		{Name: "fs.read", Description: "read file", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "shell.exec", Description: "exec", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "secret.tool", Description: "denied", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	out := agent.BuildModelTools(bus, []string{"fs.read", "shell.exec"})
	require.Len(t, out, 2)
	require.Equal(t, "function", out[0].Type)
	require.Equal(t, "fs.read", out[0].Function.Name)
	require.Equal(t, "read file", out[0].Function.Description)
	require.JSONEq(t, `{"type":"object"}`, string(out[0].Function.Parameters))
	require.Equal(t, "shell.exec", out[1].Function.Name)
}

func TestBuildModelTools_EmptyBusReturnsEmpty(t *testing.T) {
	out := agent.BuildModelTools(nil, []string{"fs.read"})
	require.Empty(t, out)
}

func TestBuildModelTools_EmptyAllowlistFiltersAll(t *testing.T) {
	bus := []toolbus.ToolDef{{Name: "fs.read"}}
	out := agent.BuildModelTools(bus, nil)
	require.Empty(t, out)
}

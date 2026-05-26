package workflow_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow"
)

func TestCompileDecompile_E2EMockChainGolden(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	yamlPath := filepath.Join(root, "deploy", "compose", "examples", "e2e-mock-chain.yaml")
	raw, err := os.ReadFile(yamlPath)
	require.NoError(t, err)

	dec, err := workflow.DecompileDesign(string(raw))
	require.NoError(t, err)
	require.NotNil(t, dec.Design)
	require.Equal(t, "e2e-mock-chain", dec.Design.ID)
	require.GreaterOrEqual(t, len(dec.Design.Steps), 3)

	out, err := workflow.CompileDesign(dec.Design)
	require.NoError(t, err)
	require.Contains(t, out.DSLYAML, "mcp.e2e-mock.fetch_status")
	require.Contains(t, out.DSLYAML, "mcp.e2e-mock.record_event")
	require.Contains(t, out.DSLYAML, `vars.health == "degraded"`)

	dec2, err := workflow.DecompileDesign(out.DSLYAML)
	require.NoError(t, err)
	out2, err := workflow.CompileDesign(dec2.Design)
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(out2.DSLYAML), "e2e-mock")
}

func TestCompileIfCondition_LiteralLeftQuoted(t *testing.T) {
	d := &workflow.WorkflowDesign{
		ID:   "t-if",
		Name: "If",
		Steps: []workflow.DesignStep{{
			ID: "gate", Kind: "if",
			Condition: &workflow.DesignCondition{
				Left: "ok", LeftKind: "literal", Op: "eq", Right: "degraded", RightKind: "literal",
			},
			Then: []workflow.DesignStep{{ID: "t", Kind: "tool", Tool: "mcp.e2e-mock.echo", Args: []workflow.ArgField{{Name: "text", Value: "x", ValueKind: "literal"}}}},
		}},
	}
	out, err := workflow.CompileDesign(d)
	require.NoError(t, err)
	require.Contains(t, out.DSLYAML, `"ok" == "degraded"`)
}

func TestDecompileIfCondition_VarsHealth(t *testing.T) {
	yaml := `id: x
name: X
steps:
  - id: gate
    if: ${vars.health == "degraded"}
    then:
      - id: t
        use: mcp.e2e-mock.echo
        args:
          text: hi
`
	dec, err := workflow.DecompileDesign(yaml)
	require.NoError(t, err)
	gate := dec.Design.Steps[0]
	require.Equal(t, "${vars.health}", gate.Condition.Left)
	require.Equal(t, "degraded", gate.Condition.Right)
}

func TestCompile_MinimalToolStep(t *testing.T) {
	d := &workflow.WorkflowDesign{
		ID:   "t1",
		Name: "T1",
		Steps: []workflow.DesignStep{{
			ID: "s1", Kind: "tool", Tool: "mcp.e2e-mock.echo",
			Args: []workflow.ArgField{{Name: "text", Value: "hi", ValueKind: "literal"}},
		}},
	}
	out, err := workflow.CompileDesign(d)
	require.NoError(t, err)
	require.Contains(t, out.DSLYAML, "mcp.e2e-mock.echo")
	require.Contains(t, out.DSLYAML, "hi")
}

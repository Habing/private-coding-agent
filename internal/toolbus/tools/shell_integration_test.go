//go:build docker_integration

package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

func TestShellExec_Integration(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDriverForToolsTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	tool := tools.NewShellExec(d)
	in, _ := json.Marshal(map[string]any{
		"sandbox_id": sb.ID.String(),
		"cmd":        []string{"echo", "hello-from-shell"},
	})
	out, err := tool.Invoke(ctx, tid, uid, in)
	require.NoError(t, err)
	require.Contains(t, string(out), "hello-from-shell")
	require.Contains(t, string(out), `"exit_code":0`)
}

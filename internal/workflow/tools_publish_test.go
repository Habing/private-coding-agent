package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
	"github.com/yourorg/private-coding-agent/internal/workflow"
)

func proposalToolsByName(psvc *workflow.ProposalService) map[string]toolbus.Tool {
	out := map[string]toolbus.Tool{}
	for _, t := range workflow.NewProposalTools(psvc) {
		out[t.Name()] = t
	}
	return out
}

func TestProposalTools_RegisteredAndMutating(t *testing.T) {
	psvc, _, _, _, _, _, _ := newProposalService(t)
	tools := proposalToolsByName(psvc)
	require.Contains(t, tools, "workflow.propose")
	require.Contains(t, tools, "workflow.publish")
	require.True(t, tools["workflow.propose"].(toolbus.Mutating).IsMutating())
	require.True(t, tools["workflow.publish"].(toolbus.Mutating).IsMutating())
}

func TestProposalTools_ProposeAndPublish_Admin(t *testing.T) {
	psvc, _, bus, _, tid, uid, _ := newProposalService(t)
	tools := proposalToolsByName(psvc)
	ctx := ctxWithRole(tid, uid, "admin")

	raw, err := tools["workflow.propose"].Invoke(ctx, tid, uid, json.RawMessage(`{
		"slug": "tool-propose",
		"name": "Tool Propose",
		"dsl_yaml": "`+escapeJSON(replaceDSLID(svcDSL, "tool-propose"))+`"
	}`))
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, true, env["ok"])
	require.Equal(t, true, env["dry_run_ok"])
	pid, _ := env["proposal_id"].(string)
	require.NotEmpty(t, pid)

	pubRaw, err := tools["workflow.publish"].Invoke(ctx, tid, uid, json.RawMessage(`{"proposal_id":"`+pid+`"}`))
	require.NoError(t, err)
	pub := decodeEnvelope(t, pubRaw)
	require.Equal(t, true, pub["ok"])
	require.Equal(t, true, pub["published"])
	require.True(t, bus.has("workflow.tool-propose"))
}

func TestProposalTools_Propose_MemberAllowed(t *testing.T) {
	psvc, _, _, _, tid, _, p := newProposalService(t)
	memberID := seedUser(t, p, tid, "member")
	tools := proposalToolsByName(psvc)
	ctx := ctxWithRole(tid, memberID, "member")

	raw, err := tools["workflow.propose"].Invoke(ctx, tid, memberID, json.RawMessage(`{
		"slug": "member-propose",
		"name": "Member",
		"template_id": "tool-chain",
		"slots": {"steps":[{"id":"s1","use":"llm.chat","args":{"model":"default-mock:text","messages":[{"role":"user","content":"hi"}]}}]}
	}`))
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, true, env["ok"])
}

func TestProposalTools_Publish_MemberDenied(t *testing.T) {
	psvc, _, _, _, tid, adminID, p := newProposalService(t)
	memberID := seedUser(t, p, tid, "member")
	tools := proposalToolsByName(psvc)

	adminCtx := ctxWithRole(tid, adminID, "admin")
	raw, err := tools["workflow.propose"].Invoke(adminCtx, tid, adminID, json.RawMessage(`{
		"slug": "deny-pub",
		"name": "Deny",
		"dsl_yaml": "`+escapeJSON(replaceDSLID(svcDSL, "deny-pub"))+`"
	}`))
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	pid := env["proposal_id"].(string)

	memberCtx := ctxWithRole(tid, memberID, "member")
	pubRaw, err := tools["workflow.publish"].Invoke(memberCtx, tid, memberID, json.RawMessage(`{"proposal_id":"`+pid+`"}`))
	require.NoError(t, err)
	pub := decodeEnvelope(t, pubRaw)
	require.Equal(t, false, pub["ok"])
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}

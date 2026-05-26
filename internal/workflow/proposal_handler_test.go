package workflow_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow"
)

func newProposalHandlerSetup(t *testing.T, role string) (*gin.Engine, *workflow.ProposalService, uuid.UUID, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	psvc, _, _, _, tid, uid, _ := newProposalService(t)
	r := gin.New()
	r.Use(claimsMiddleware(tid, uid, role))
	workflow.NewProposalHandler(psvc).RegisterAgent(&r.RouterGroup)
	return r, psvc, tid, uid
}

func newProposalAdminSetup(t *testing.T) (*gin.Engine, *workflow.ProposalService, uuid.UUID, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	psvc, _, _, _, tid, adminID, p := newProposalService(t)
	memberID := seedUser(t, p, tid, "member")
	r := gin.New()
	ag := r.Group("/")
	ag.Use(claimsMiddleware(tid, adminID, "admin"))
	workflow.NewProposalHandler(psvc).RegisterAdmin(ag)
	_ = memberID
	return r, psvc, tid, adminID
}

func TestProposalHandler_ListTemplates(t *testing.T) {
	r, _, _, _ := newProposalHandlerSetup(t, "member")
	rr := doJSON(t, r, http.MethodGet, "/agent/workflow/templates", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Templates []struct{ ID string `json:"id"` } `json:"templates"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Templates, 6)
}

func TestProposalHandler_CreateAndConfirmAdmin(t *testing.T) {
	r, _, _, _ := newProposalHandlerSetup(t, "admin")
	rr := doJSON(t, r, http.MethodPost, "/agent/workflow/proposals", map[string]any{
		"slug": "api-greet", "name": "API Greet", "dsl_yaml": replaceDSLID(svcDSL, "api-greet"),
	})
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	var created struct {
		Proposal workflow.Proposal `json:"proposal"`
		Summary  string            `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &created))
	require.True(t, created.Proposal.DryRunOK)

	rr = doJSON(t, r, http.MethodPost, "/agent/workflow/proposals/"+created.Proposal.ID.String()+"/confirm", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var confirmed struct {
		Proposal workflow.Proposal `json:"proposal"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &confirmed))
	require.Equal(t, workflow.ProposalPublished, confirmed.Proposal.Status)
}

func TestProposalHandler_MemberConfirmAdminApprove(t *testing.T) {
	gin.SetMode(gin.TestMode)
	psvc, _, bus, _, tid, adminID, p := newProposalService(t)
	memberID := seedUser(t, p, tid, "member")

	memberR := gin.New()
	memberR.Use(claimsMiddleware(tid, memberID, "member"))
	workflow.NewProposalHandler(psvc).RegisterAgent(&memberR.RouterGroup)

	rr := doJSON(t, memberR, http.MethodPost, "/agent/workflow/proposals", map[string]any{
		"slug": "pending-api", "name": "Pending", "dsl_yaml": replaceDSLID(svcDSL, "pending-api"),
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	var created struct {
		Proposal workflow.Proposal `json:"proposal"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &created))

	rr = doJSON(t, memberR, http.MethodPost, "/agent/workflow/proposals/"+created.Proposal.ID.String()+"/confirm", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var pending struct {
		Proposal workflow.Proposal `json:"proposal"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &pending))
	require.Equal(t, workflow.ProposalPendingApproval, pending.Proposal.Status)

	adminR := gin.New()
	adminR.Use(claimsMiddleware(tid, adminID, "admin"))
	workflow.NewProposalHandler(psvc).RegisterAdmin(&adminR.RouterGroup)

	rr = doJSON(t, adminR, http.MethodPost, "/admin/workflow/proposals/"+created.Proposal.ID.String()+"/approve", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	require.True(t, bus.has("workflow.pending-api"))
}

func TestProposalHandler_PreviewTemplate(t *testing.T) {
	r, _, _, _ := newProposalHandlerSetup(t, "member")
	rr := doJSON(t, r, http.MethodPost, "/agent/workflow/templates/cron-notify/preview", map[string]any{
		"slug": "weekly-report", "name": "Weekly Report",
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body struct {
		DSLYAML string `json:"dsl_yaml"`
		Slug    string `json:"slug"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, "weekly-report", body.Slug)
	require.Contains(t, body.DSLYAML, "weekly-report")
	require.Contains(t, body.DSLYAML, "triggers:")
}

func TestProposalHandler_ListProposalsAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	psvc, _, _, _, tid, adminID, p := newProposalService(t)
	memberID := seedUser(t, p, tid, "member")

	memberR := gin.New()
	memberR.Use(claimsMiddleware(tid, memberID, "member"))
	workflow.NewProposalHandler(psvc).RegisterAgent(&memberR.RouterGroup)

	rr := doJSON(t, memberR, http.MethodPost, "/agent/workflow/proposals", map[string]any{
		"slug": "list-api", "name": "List API", "dsl_yaml": replaceDSLID(svcDSL, "list-api"),
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	var created struct {
		Proposal workflow.Proposal `json:"proposal"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &created))

	rr = doJSON(t, memberR, http.MethodPost, "/agent/workflow/proposals/"+created.Proposal.ID.String()+"/confirm", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	adminR := gin.New()
	adminR.Use(claimsMiddleware(tid, adminID, "admin"))
	workflow.NewProposalHandler(psvc).RegisterAdmin(&adminR.RouterGroup)

	rr = doJSON(t, adminR, http.MethodGet, "/admin/workflow/proposals?status=pending_approval", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var list struct {
		Proposals []workflow.Proposal `json:"proposals"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	require.NotEmpty(t, list.Proposals)
}

func TestProposalHandler_ProposalGraph(t *testing.T) {
	r, _, _, _ := newProposalHandlerSetup(t, "member")
	rr := doJSON(t, r, http.MethodPost, "/agent/workflow/proposals", map[string]any{
		"slug": "graph-wf", "name": "Graph WF", "dsl_yaml": replaceDSLID(svcDSL, "graph-wf"),
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	var created struct {
		Proposal struct {
			ID uuid.UUID `json:"id"`
		} `json:"proposal"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &created))

	rr = doJSON(t, r, http.MethodGet, "/agent/workflow/proposals/"+created.Proposal.ID.String()+"/graph", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var g workflow.Graph
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &g))
	require.Equal(t, "graph-wf", g.Meta.ID)
	require.NotEmpty(t, g.Nodes)
}

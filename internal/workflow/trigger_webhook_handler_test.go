package workflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow"
)

func TestWebhookHandler_ValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, repo, _, _, tid := newService(t)
	p := newPool(t)
	uid := seedUser(t, p, tid, "admin")
	svc = svc.SetTenantAdminLookup(adminLookup{uid})

	ctx := context.Background()
	triggers := workflow.NewTriggerRepo(repo)
	dsl := `
id: hook-svc
name: Hook
triggers:
  - id: inbound
    webhook: {}
steps:
  - id: a
    assign:
      msg: ${inputs.payload}
outputs:
  out: ${vars.msg}
`
	w, err := repo.Create(ctx, tid, "hook-svc", "Hook", "", dsl)
	require.NoError(t, err)
	doc, err := workflow.Parse(dsl)
	require.NoError(t, err)
	require.NoError(t, triggers.SyncTriggersFromDoc(ctx, tid, w.ID, doc))
	rows, err := triggers.ListByWorkflow(ctx, tid, w.ID)
	require.NoError(t, err)
	token := rows[0].WebhookToken
	require.NoError(t, repo.SetPublished(ctx, tid, "hook-svc", true))

	r := gin.New()
	workflow.NewWebhookHandler(svc, 60).Register(r)
	body := map[string]any{"payload": "hi"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/hooks/workflow/"+token, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	wrec := httptest.NewRecorder()
	r.ServeHTTP(wrec, req)
	require.Equal(t, http.StatusCreated, wrec.Code)
}

func TestWebhookHandler_BadToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _, _, _ := newService(t)
	r := gin.New()
	workflow.NewWebhookHandler(svc, 60).Register(r)
	req := httptest.NewRequest(http.MethodPost, "/hooks/workflow/not-a-real-token", nil)
	wrec := httptest.NewRecorder()
	r.ServeHTTP(wrec, req)
	require.Equal(t, http.StatusNotFound, wrec.Code)
}

func TestWebhookHandler_Unpublished(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, repo, _, _, tid := newService(t)
	ctx := context.Background()
	triggers := workflow.NewTriggerRepo(repo)
	dsl := `
id: hook-draft
name: Hook
triggers:
  - id: inbound
    webhook: {}
steps:
  - id: a
    wait: 1ms
`
	w, err := repo.Create(ctx, tid, "hook-draft", "Hook", "", dsl)
	require.NoError(t, err)
	doc, err := workflow.Parse(dsl)
	require.NoError(t, err)
	require.NoError(t, triggers.SyncTriggersFromDoc(ctx, tid, w.ID, doc))
	rows, err := triggers.ListByWorkflow(ctx, tid, w.ID)
	require.NoError(t, err)
	token := rows[0].WebhookToken

	r := gin.New()
	workflow.NewWebhookHandler(svc, 60).Register(r)
	req := httptest.NewRequest(http.MethodPost, "/hooks/workflow/"+token, nil)
	wrec := httptest.NewRecorder()
	r.ServeHTTP(wrec, req)
	require.Equal(t, http.StatusConflict, wrec.Code)
}

func TestTriggerAdminHandler_ListAndRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, bus, _, tid := newService(t)
	uid := seedUser(t, newPool(t), tid, "admin")
	ctx := context.Background()

	dsl := `
id: trig-admin
name: T
triggers:
  - id: inbound
    webhook: {}
steps:
  - id: a
    assign:
      x: "1"
outputs:
  y: ${vars.x}
`
	_, err := svc.Create(ctx, tid, "trig-admin", "T", "", dsl)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tid, "trig-admin"))
	require.True(t, bus.has("workflow.trig-admin"))

	r := gin.New()
	r.Use(claimsMiddleware(tid, uid, "admin"))
	workflow.NewTriggerAdminHandler(svc).Register(&r.RouterGroup)

	wrec := doJSON(t, r, http.MethodGet, "/admin/workflows/trig-admin/triggers", nil)
	require.Equal(t, http.StatusOK, wrec.Code)
	var listResp struct {
		Triggers []map[string]any `json:"triggers"`
	}
	require.NoError(t, json.Unmarshal(wrec.Body.Bytes(), &listResp))
	require.Len(t, listResp.Triggers, 1)

	wrec = doJSON(t, r, http.MethodPost, "/admin/workflows/trig-admin/triggers/inbound/run", nil)
	require.Equal(t, http.StatusOK, wrec.Code)
}

type adminLookup struct{ id uuid.UUID }

func (a adminLookup) FirstAdminID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, error) {
	return a.id, nil
}

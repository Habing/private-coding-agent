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

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/workflow"
)

// claimsMiddleware injects an admin Claims for the given tenant/user so the
// handler's auth.FromCtx call works without a real JWT.
func claimsMiddleware(tenantID, userID uuid.UUID, role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := &auth.Claims{TenantID: tenantID, UserID: userID, Role: role}
		ctx := auth.WithClaims(c.Request.Context(), cl)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func newHandlerSetup(t *testing.T) (*gin.Engine, *workflow.Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc, _, _, _, tid := newService(t)
	uid := uuid.New()
	r := gin.New()
	r.Use(claimsMiddleware(tid, uid, "admin"))
	workflow.NewAdminHandler(svc).Register(&r.RouterGroup)
	return r, svc, tid, uid
}

func doJSON(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// TestHandler_CreateGetList walks the basic CRUD: POST → GET → list.
func TestHandler_CreateGetList(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)

	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "greet", "name": "Greet", "description": "say hi", "dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

	rr = doJSON(t, r, http.MethodGet, "/admin/workflows/greet", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var got workflow.Workflow
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, "greet", got.Slug)
	require.NotEmpty(t, got.DSLYAML)

	rr = doJSON(t, r, http.MethodGet, "/admin/workflows", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var listed struct {
		Workflows []workflow.Workflow `json:"workflows"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &listed))
	require.Len(t, listed.Workflows, 1)
	require.Empty(t, listed.Workflows[0].DSLYAML, "list view omits DSL body")
}

// TestHandler_Create_InvalidSlug checks the slug regex gate (400 before DB).
func TestHandler_Create_InvalidSlug(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "BAD SLUG", "name": "X", "dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestHandler_Create_BadDSL surfaces validate errors as 400.
func TestHandler_Create_BadDSL(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "broken", "name": "B", "dsl_yaml": "::: not yaml :::",
	})
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestHandler_Create_SlugConflict returns 409 on UNIQUE violation.
func TestHandler_Create_SlugConflict(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	body := map[string]any{"slug": "greet", "name": "G", "dsl_yaml": svcDSL}
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", body)
	require.Equal(t, http.StatusCreated, rr.Code)
	rr2 := doJSON(t, r, http.MethodPost, "/admin/workflows", body)
	require.Equal(t, http.StatusConflict, rr2.Code)
}

// TestHandler_Get_NotFound 404s for an unknown slug.
func TestHandler_Get_NotFound(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodGet, "/admin/workflows/missing", nil)
	require.Equal(t, http.StatusNotFound, rr.Code)
}

// TestHandler_UpdateDelete: PUT bumps version + drops published, DELETE removes.
func TestHandler_UpdateDelete(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "greet", "name": "G", "dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	rr = doJSON(t, r, http.MethodPut, "/admin/workflows/greet", map[string]any{
		"name": "Greet2", "description": "edited", "dsl_yaml": svcDSL + "\n# bump",
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var updated workflow.Workflow
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &updated))
	require.Equal(t, 2, updated.Version)
	require.False(t, updated.Published)

	rr = doJSON(t, r, http.MethodDelete, "/admin/workflows/greet", nil)
	require.Equal(t, http.StatusNoContent, rr.Code)

	rr = doJSON(t, r, http.MethodGet, "/admin/workflows/greet", nil)
	require.Equal(t, http.StatusNotFound, rr.Code)
}

// TestHandler_PublishUnpublish flips the published flag round-trip.
func TestHandler_PublishUnpublish(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "greet", "name": "G", "dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	rr = doJSON(t, r, http.MethodPost, "/admin/workflows/greet/publish", nil)
	require.Equal(t, http.StatusNoContent, rr.Code)
	rr = doJSON(t, r, http.MethodGet, "/admin/workflows/greet", nil)
	var got workflow.Workflow
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.True(t, got.Published)

	rr = doJSON(t, r, http.MethodPost, "/admin/workflows/greet/unpublish", nil)
	require.Equal(t, http.StatusNoContent, rr.Code)
	rr = doJSON(t, r, http.MethodGet, "/admin/workflows/greet", nil)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.False(t, got.Published)
}

// TestHandler_Invoke runs a workflow through the admin endpoint and asserts
// outputs come back resolved.
func TestHandler_Invoke(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "greet", "name": "G", "dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	rr = doJSON(t, r, http.MethodPost, "/admin/workflows/greet/invoke", map[string]any{
		"inputs": map[string]any{"who": "Carol"},
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var res workflow.InvokeResult
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
	require.Equal(t, "ok", res.Status)
	require.Equal(t, "hello Carol", res.Outputs["said"])
	require.False(t, res.DryRun)
}

// TestHandler_Invoke_DryRunQuery picks up dry_run from the query string when
// the body omits it.
func TestHandler_Invoke_DryRunQuery(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "greet", "name": "G", "dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	rr = doJSON(t, r, http.MethodPost, "/admin/workflows/greet/invoke?dry_run=true", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var res workflow.InvokeResult
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
	require.True(t, res.DryRun)
}

// TestHandler_Runs lists the workflow_runs rows after an invoke.
func TestHandler_Runs(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "greet", "name": "G", "dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusCreated, rr.Code)
	rr = doJSON(t, r, http.MethodPost, "/admin/workflows/greet/invoke", map[string]any{})
	require.Equal(t, http.StatusOK, rr.Code)

	rr = doJSON(t, r, http.MethodGet, "/admin/workflows/greet/runs", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var got struct {
		Runs []workflow.Run `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got.Runs, 1)
	require.Equal(t, "ok", got.Runs[0].Status)
}

// TestHandler_Unauthorized refuses requests without claims (no middleware →
// FromCtx returns nil → 401).
func TestHandler_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, _, _, _ := newService(t)
	r := gin.New() // no claimsMiddleware
	workflow.NewAdminHandler(svc).Register(&r.RouterGroup)
	rr := doJSON(t, r, http.MethodGet, "/admin/workflows", nil)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestHandler_GraphPreview(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows/graph-preview", map[string]any{
		"dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusOK, rr.Code)
	var g workflow.Graph
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &g))
	require.Equal(t, "greet", g.Meta.ID)
	require.NotEmpty(t, g.Nodes)
}

func TestHandler_GraphPreview_BadYAML(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows/graph-preview", map[string]any{
		"dsl_yaml": "[[[",
	})
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandler_Graph_Get(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows", map[string]any{
		"slug": "greet", "name": "G", "dsl_yaml": svcDSL,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	rr = doJSON(t, r, http.MethodGet, "/admin/workflows/greet/graph", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var g workflow.Graph
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &g))
	require.Equal(t, "greet", g.Meta.ID)
}

func TestHandler_DesignDecompileCompile(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	raw := `id: mini
name: Mini
steps:
  - id: echo
    use: mcp.e2e-mock.echo
    args:
      text: hi
`
	rr := doJSON(t, r, http.MethodPost, "/admin/workflows/design/decompile", map[string]any{
		"dsl_yaml": raw,
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var dec map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &dec))
	designObj := dec["design"].(map[string]any)
	require.Equal(t, "mini", designObj["id"])

	rr = doJSON(t, r, http.MethodPost, "/admin/workflows/design/compile", map[string]any{
		"design": designObj,
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var comp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &comp))
	require.Contains(t, comp["dsl_yaml"].(string), "mcp.e2e-mock.echo")
}

func TestHandler_Graph_NotFound(t *testing.T) {
	r, _, _, _ := newHandlerSetup(t)
	rr := doJSON(t, r, http.MethodGet, "/admin/workflows/missing/graph", nil)
	require.Equal(t, http.StatusNotFound, rr.Code)
}

// keep the auth import meaningful even if helpers above shift around.
var _ = context.Background

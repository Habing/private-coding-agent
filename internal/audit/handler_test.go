package audit_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
)

type mockHandlerSvc struct {
	captured       audit.ListFilter
	entries        []audit.Entry
	total          int
	err            error
	verifyFromID   int64
	verifyResult   *audit.VerifyResult
	verifyErr      error
	verifyCalls    int
}

func (m *mockHandlerSvc) List(_ context.Context, f audit.ListFilter) ([]audit.Entry, int, error) {
	m.captured = f
	return m.entries, m.total, m.err
}

func (m *mockHandlerSvc) Verify(_ context.Context, fromID int64) (*audit.VerifyResult, error) {
	m.verifyFromID = fromID
	m.verifyCalls++
	return m.verifyResult, m.verifyErr
}

func newAuditHandlerRouter(t *testing.T, svc audit.HandlerService, role string) (*gin.Engine, string, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	tid := uuid.New()
	tok, _ := j.Issue(uuid.New(), tid, role)
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	g.Use(auth.RequireAdmin())
	audit.NewHandler(svc, func(c *gin.Context) (uuid.UUID, bool) {
		cl := auth.FromCtx(c.Request.Context())
		if cl == nil {
			return uuid.Nil, false
		}
		return cl.TenantID, true
	}).Register(g)
	return r, "Bearer " + tok, tid
}

func TestAuditHandler_List_OK_DefaultsApplied(t *testing.T) {
	svc := &mockHandlerSvc{
		entries: []audit.Entry{
			{Action: "auth.login.success", Status: 200, Method: "POST", Path: "/auth/login"},
		},
		total: 1,
	}
	r, tok, tid := newAuditHandlerRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	require.Equal(t, tid, svc.captured.TenantID, "tenant must come from auth claims")
	require.Empty(t, svc.captured.Action)

	var resp struct {
		Entries []map[string]any `json:"entries"`
		Total   int              `json:"total"`
		Limit   int              `json:"limit"`
		Offset  int              `json:"offset"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Total)
	require.Equal(t, audit.DefaultListLimit, resp.Limit)
	require.Equal(t, 0, resp.Offset)
	require.Len(t, resp.Entries, 1)
	require.Equal(t, "auth.login.success", resp.Entries[0]["action"])
}

func TestAuditHandler_List_AllQueryParamsPropagated(t *testing.T) {
	svc := &mockHandlerSvc{}
	r, tok, tid := newAuditHandlerRouter(t, svc, "admin")
	uid := uuid.New()
	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)
	u := "/audit?action=auth.login&user_id=" + uid.String() +
		"&from=" + from + "&to=" + to +
		"&min_status=400&max_status=499&limit=10&offset=5"
	req := httptest.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, tid, svc.captured.TenantID)
	require.Equal(t, "auth.login", svc.captured.Action)
	require.NotNil(t, svc.captured.UserID)
	require.Equal(t, uid, *svc.captured.UserID)
	require.NotNil(t, svc.captured.From)
	require.NotNil(t, svc.captured.To)
	require.Equal(t, 400, svc.captured.MinStatus)
	require.Equal(t, 499, svc.captured.MaxStatus)
	require.Equal(t, 10, svc.captured.Limit)
	require.Equal(t, 5, svc.captured.Offset)
}

func TestAuditHandler_List_RejectsMemberWith403(t *testing.T) {
	svc := &mockHandlerSvc{}
	r, tok, _ := newAuditHandlerRouter(t, svc, "member")
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestAuditHandler_List_BadUserID(t *testing.T) {
	r, tok, _ := newAuditHandlerRouter(t, &mockHandlerSvc{}, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit?user_id=not-a-uuid", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "validation: user_id")
}

func TestAuditHandler_List_BadTime(t *testing.T) {
	r, tok, _ := newAuditHandlerRouter(t, &mockHandlerSvc{}, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit?from=yesterday", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "validation: from")
}

func TestAuditHandler_List_RepoError500(t *testing.T) {
	svc := &mockHandlerSvc{err: context.Canceled}
	r, tok, _ := newAuditHandlerRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAuditHandler_Verify_OK(t *testing.T) {
	svc := &mockHandlerSvc{verifyResult: &audit.VerifyResult{
		OK: true, RowsChecked: 7, ChainStartID: 1, ChainEndID: 7,
	}}
	r, tok, _ := newAuditHandlerRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit/verify", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, svc.verifyCalls)
	require.Equal(t, int64(0), svc.verifyFromID)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, true, resp["ok"])
	require.EqualValues(t, 7, resp["rows_checked"])
}

func TestAuditHandler_Verify_FromIDPassedThrough(t *testing.T) {
	svc := &mockHandlerSvc{verifyResult: &audit.VerifyResult{OK: true}}
	r, tok, _ := newAuditHandlerRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit/verify?from_id=42", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(42), svc.verifyFromID)
}

func TestAuditHandler_Verify_BadFromID(t *testing.T) {
	svc := &mockHandlerSvc{verifyResult: &audit.VerifyResult{OK: true}}
	r, tok, _ := newAuditHandlerRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit/verify?from_id=not-an-int", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Zero(t, svc.verifyCalls, "bad input must short-circuit before Service")
}

func TestAuditHandler_Verify_TamperedSurfacesBrokenRow(t *testing.T) {
	svc := &mockHandlerSvc{verifyResult: &audit.VerifyResult{
		OK: false, RowsChecked: 3, ChainStartID: 1, FirstBrokenID: 4,
		Reason: "entry_hash_mismatch",
		ExpectedHash: "aa", ActualHash: "bb",
	}}
	r, tok, _ := newAuditHandlerRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit/verify", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "tampered chain still returns 200")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, false, resp["ok"])
	require.EqualValues(t, 4, resp["first_broken_id"])
	require.Equal(t, "entry_hash_mismatch", resp["reason"])
	require.Equal(t, "aa", resp["expected_hash"])
	require.Equal(t, "bb", resp["actual_hash"])
}

func TestAuditHandler_Verify_RejectsMemberWith403(t *testing.T) {
	svc := &mockHandlerSvc{}
	r, tok, _ := newAuditHandlerRouter(t, svc, "member")
	req := httptest.NewRequest(http.MethodGet, "/audit/verify", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Zero(t, svc.verifyCalls)
}

func TestAuditHandler_Verify_RejectsUnauthenticatedWith401(t *testing.T) {
	svc := &mockHandlerSvc{}
	r, _, _ := newAuditHandlerRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit/verify", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuditHandler_Verify_RepoError500(t *testing.T) {
	svc := &mockHandlerSvc{verifyErr: context.Canceled}
	r, tok, _ := newAuditHandlerRouter(t, svc, "admin")
	req := httptest.NewRequest(http.MethodGet, "/audit/verify", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

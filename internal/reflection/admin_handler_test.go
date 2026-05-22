package reflection_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/reflection"
)

type adminFakeMem struct {
	calls    int32
	wantErr  error
	memID    uuid.UUID
	dedupHit bool
	lastType string
	lastTags []string
}

func (f *adminFakeMem) CreateForReflection(_ context.Context, _, _ uuid.UUID,
	typ, _ string, tags []string, _ *uuid.UUID) (uuid.UUID, bool, error) {
	atomic.AddInt32(&f.calls, 1)
	f.lastType = typ
	f.lastTags = tags
	if f.wantErr != nil {
		return uuid.Nil, false, f.wantErr
	}
	if f.memID == uuid.Nil {
		f.memID = uuid.New()
	}
	return f.memID, f.dedupHit, nil
}

func newAdminRouter(t *testing.T, pg *pgxpool.Pool, tid uuid.UUID, role string,
	mem reflection.MemoryCreator) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	tok, _ := j.Issue(uuid.New(), tid, role)
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	g.Use(auth.RequireAdmin())
	reflection.NewAdminHandler(reflection.NewRepo(pg), mem).Register(g)
	return r, "Bearer " + tok
}

func seedPending(t *testing.T, pg *pgxpool.Pool, tid, uid uuid.UUID, conf float32) *reflection.MemoryProposal {
	t.Helper()
	repo := reflection.NewRepo(pg)
	p, err := repo.Insert(context.Background(), &reflection.MemoryProposal{
		TenantID:    tid,
		OwnerUserID: uid,
		Type:        reflection.TypePreference,
		Content:     "user loves generics",
		Tags:        []string{"go"},
		Confidence:  conf,
		Status:      reflection.StatusPending,
	})
	require.NoError(t, err)
	return p
}

func do(t *testing.T, r *gin.Engine, tok, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	} else {
		buf = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Authorization", tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminHandler_RejectsNonAdmin(t *testing.T) {
	pg := newPool(t)
	tid, _ := fixtures(t, pg)
	r, tok := newAdminRouter(t, pg, tid, "member", &adminFakeMem{})
	w := do(t, r, tok, http.MethodGet, "/admin/memory-proposals", nil)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminHandler_ListAndGet(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	r, tok := newAdminRouter(t, pg, tid, "admin", &adminFakeMem{})
	p := seedPending(t, pg, tid, uid, 0.5)

	w := do(t, r, tok, http.MethodGet, "/admin/memory-proposals?status=pending", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list struct {
		Proposals []reflection.MemoryProposal `json:"proposals"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Proposals, 1)
	require.Equal(t, p.ID, list.Proposals[0].ID)

	w = do(t, r, tok, http.MethodGet, "/admin/memory-proposals/"+p.ID.String(), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var got reflection.MemoryProposal
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, p.ID, got.ID)

	w = do(t, r, tok, http.MethodGet, "/admin/memory-proposals/"+uuid.NewString(), nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminHandler_List_InvalidStatus(t *testing.T) {
	pg := newPool(t)
	tid, _ := fixtures(t, pg)
	r, tok := newAdminRouter(t, pg, tid, "admin", &adminFakeMem{})
	w := do(t, r, tok, http.MethodGet, "/admin/memory-proposals?status=bogus", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminHandler_Approve_NoOverrides(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	mem := &adminFakeMem{memID: uuid.New()}
	r, tok := newAdminRouter(t, pg, tid, "admin", mem)
	p := seedPending(t, pg, tid, uid, 0.5)

	w := do(t, r, tok, http.MethodPost, "/admin/memory-proposals/"+p.ID.String()+"/approve", map[string]any{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Equal(t, int32(1), atomic.LoadInt32(&mem.calls))
	require.Equal(t, "preference", mem.lastType)
	require.Equal(t, []string{"go"}, mem.lastTags)

	var got reflection.MemoryProposal
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, reflection.StatusApproved, got.Status)
	require.NotNil(t, got.MemoryID)
	require.Equal(t, mem.memID, *got.MemoryID)
	require.NotNil(t, got.DecidedAt)
	require.NotNil(t, got.DecidedBy)
}

func TestAdminHandler_Approve_Overrides(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	mem := &adminFakeMem{memID: uuid.New(), dedupHit: true}
	r, tok := newAdminRouter(t, pg, tid, "admin", mem)
	p := seedPending(t, pg, tid, uid, 0.5)

	w := do(t, r, tok, http.MethodPost, "/admin/memory-proposals/"+p.ID.String()+"/approve", map[string]any{
		"type":    "lesson",
		"content": "rewritten content",
		"tags":    []string{"refined", "go"},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "lesson", mem.lastType)
	require.Equal(t, []string{"refined", "go"}, mem.lastTags)
}

func TestAdminHandler_Approve_InvalidTypeOverride(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	r, tok := newAdminRouter(t, pg, tid, "admin", &adminFakeMem{})
	p := seedPending(t, pg, tid, uid, 0.5)

	w := do(t, r, tok, http.MethodPost, "/admin/memory-proposals/"+p.ID.String()+"/approve",
		map[string]any{"type": "bogus"})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminHandler_Approve_AlreadyDecided(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	mem := &adminFakeMem{memID: uuid.New()}
	r, tok := newAdminRouter(t, pg, tid, "admin", mem)
	p := seedPending(t, pg, tid, uid, 0.5)

	// First approve succeeds.
	w := do(t, r, tok, http.MethodPost, "/admin/memory-proposals/"+p.ID.String()+"/approve", map[string]any{})
	require.Equal(t, http.StatusOK, w.Code)
	// Second approve returns 409.
	w = do(t, r, tok, http.MethodPost, "/admin/memory-proposals/"+p.ID.String()+"/approve", map[string]any{})
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestAdminHandler_Approve_MemoryCreateFailure(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	mem := &adminFakeMem{wantErr: errors.New("embed boom")}
	r, tok := newAdminRouter(t, pg, tid, "admin", mem)
	p := seedPending(t, pg, tid, uid, 0.5)

	w := do(t, r, tok, http.MethodPost, "/admin/memory-proposals/"+p.ID.String()+"/approve", map[string]any{})
	require.Equal(t, http.StatusInternalServerError, w.Code)

	// Proposal should still be pending after a failed memory.Create.
	got, err := reflection.NewRepo(pg).Get(context.Background(), tid, p.ID)
	require.NoError(t, err)
	require.Equal(t, reflection.StatusPending, got.Status)
}

func TestAdminHandler_Reject(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	r, tok := newAdminRouter(t, pg, tid, "admin", &adminFakeMem{})
	p := seedPending(t, pg, tid, uid, 0.5)

	w := do(t, r, tok, http.MethodPost, "/admin/memory-proposals/"+p.ID.String()+"/reject",
		map[string]any{"reason": "duplicate"})
	require.Equal(t, http.StatusOK, w.Code)
	var got reflection.MemoryProposal
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, reflection.StatusRejected, got.Status)
	require.Nil(t, got.MemoryID)
	require.NotNil(t, got.DecidedAt)
}

func TestAdminHandler_CrossTenant_NotFound(t *testing.T) {
	pg := newPool(t)
	tidA, uidA := fixtures(t, pg)
	tidB, _ := fixtures(t, pg)
	p := seedPending(t, pg, tidA, uidA, 0.5)

	// Admin in tenant B should not see tenant A's proposal.
	r, tok := newAdminRouter(t, pg, tidB, "admin", &adminFakeMem{})
	w := do(t, r, tok, http.MethodGet, "/admin/memory-proposals/"+p.ID.String(), nil)
	require.Equal(t, http.StatusNotFound, w.Code)
	w = do(t, r, tok, http.MethodPost, "/admin/memory-proposals/"+p.ID.String()+"/approve", map[string]any{})
	require.Equal(t, http.StatusNotFound, w.Code)
}

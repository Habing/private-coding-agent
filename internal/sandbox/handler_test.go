package sandbox_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

// mockRuntime is a hand-written test double satisfying sandbox.Runtime.
type mockRuntime struct {
	createRet  *sandbox.Sandbox
	createErr  error
	getRet     *sandbox.Sandbox
	getErr     error
	destroyErr error
	execRet    *sandbox.ExecResult
	execErr    error
	readRet    []byte
	readErr    error
	writeErr   error
	snap       *sandbox.Snapshot
	snapErr    error
	restoreRet *sandbox.Sandbox
	restoreErr error

	// last-call inspection
	lastCreateOpts sandbox.CreateOpts
	lastExecOpts   sandbox.ExecOpts
	lastWriteData  []byte
}

func (m *mockRuntime) Create(_ context.Context, opts sandbox.CreateOpts) (*sandbox.Sandbox, error) {
	m.lastCreateOpts = opts
	return m.createRet, m.createErr
}
func (m *mockRuntime) Get(_ context.Context, _, _ uuid.UUID) (*sandbox.Sandbox, error) {
	return m.getRet, m.getErr
}
func (m *mockRuntime) Destroy(_ context.Context, _, _ uuid.UUID) error { return m.destroyErr }
func (m *mockRuntime) Exec(_ context.Context, _, _ uuid.UUID, opts sandbox.ExecOpts) (*sandbox.ExecResult, error) {
	m.lastExecOpts = opts
	return m.execRet, m.execErr
}
func (m *mockRuntime) ReadFile(_ context.Context, _, _ uuid.UUID, _ string) ([]byte, error) {
	return m.readRet, m.readErr
}
func (m *mockRuntime) WriteFile(_ context.Context, _, _ uuid.UUID, _ string, data []byte) error {
	m.lastWriteData = data
	return m.writeErr
}
func (m *mockRuntime) Snapshot(_ context.Context, _, _ uuid.UUID) (*sandbox.Snapshot, error) {
	if m.snapErr != nil {
		return nil, m.snapErr
	}
	if m.snap != nil {
		return m.snap, nil
	}
	return nil, m.snapErr
}
func (m *mockRuntime) RestoreFromSnapshot(_ context.Context, _, _ uuid.UUID, _ uuid.UUID) (*sandbox.Sandbox, error) {
	if m.restoreErr != nil {
		return nil, m.restoreErr
	}
	return m.restoreRet, nil
}

func newRouterWithMock(t *testing.T, m *mockRuntime) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, err := j.Issue(uid, tid, "member")
	require.NoError(t, err)

	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	sandbox.NewHandler(m).Register(g)
	return r, "Bearer " + tok
}

func do(r *gin.Engine, method, path, bearer string, body any) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", bearer)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHandler_Create_OK(t *testing.T) {
	mr := &mockRuntime{
		createRet: &sandbox.Sandbox{
			ID:        uuid.New(),
			Status:    sandbox.StatusRunning,
			Image:     "pca/sandbox:base",
			Network:   sandbox.NetworkInternal,
			Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
		},
	}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions", tok, map[string]any{})
	require.Equal(t, http.StatusCreated, w.Code)
}

func TestHandler_Create_NoAuth(t *testing.T) {
	mr := &mockRuntime{}
	r, _ := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions", "", map[string]any{})
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Get_NotFound(t *testing.T) {
	mr := &mockRuntime{getErr: sandbox.ErrSandboxNotFound}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/sessions/"+uuid.NewString(), tok, nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Get_BadID(t *testing.T) {
	mr := &mockRuntime{}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/sessions/not-a-uuid", tok, nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Destroy_NoContent(t *testing.T) {
	mr := &mockRuntime{}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodDelete, "/sandbox/sessions/"+uuid.NewString(), tok, nil)
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandler_Exec_OK(t *testing.T) {
	mr := &mockRuntime{
		execRet: &sandbox.ExecResult{ExitCode: 0, Stdout: []byte("hi\n")},
	}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+uuid.NewString()+"/exec", tok,
		map[string]any{"cmd": []string{"echo", "hi"}})
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, float64(0), resp["exit_code"])
	dec, _ := base64.StdEncoding.DecodeString(resp["stdout_base64"].(string))
	require.Equal(t, "hi\n", string(dec))
}

func TestHandler_Exec_NotReady(t *testing.T) {
	mr := &mockRuntime{execErr: sandbox.ErrSandboxNotReady}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+uuid.NewString()+"/exec", tok,
		map[string]any{"cmd": []string{"echo"}})
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestHandler_WriteFile_OK(t *testing.T) {
	mr := &mockRuntime{}
	r, tok := newRouterWithMock(t, mr)
	content := base64.StdEncoding.EncodeToString([]byte("hello"))
	w := do(r, http.MethodPut, "/sandbox/sessions/"+uuid.NewString()+"/files?path=a.txt", tok,
		map[string]any{"content_base64": content})
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, []byte("hello"), mr.lastWriteData)
}

func TestHandler_WriteFile_PathOutside(t *testing.T) {
	mr := &mockRuntime{writeErr: sandbox.ErrPathOutsideWorkspace}
	r, tok := newRouterWithMock(t, mr)
	content := base64.StdEncoding.EncodeToString([]byte("x"))
	w := do(r, http.MethodPut, "/sandbox/sessions/"+uuid.NewString()+"/files?path=../x", tok,
		map[string]any{"content_base64": content})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_ListFiles_OK(t *testing.T) {
	sbID := uuid.New()
	mr := &mockRuntime{
		execRet: &sandbox.ExecResult{
			ExitCode: 0,
			Stdout:   []byte("hello.txt\tf\t5\nsubdir\td\t4096\n"),
		},
	}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/sessions/"+sbID.String()+"/files?path=.&list=1", tok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Entries []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Entries, 2)
	require.Equal(t, "hello.txt", resp.Entries[0].Name)
	require.Equal(t, "subdir", resp.Entries[1].Name)
	require.Contains(t, mr.lastExecOpts.Cmd, "find")
}

func TestHandler_ReadFile_TooLarge(t *testing.T) {
	mr := &mockRuntime{readErr: sandbox.ErrTooLarge}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/sessions/"+uuid.NewString()+"/files?path=big.bin", tok, nil)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestHandler_Snapshot_Disabled(t *testing.T) {
	// Driver returns ErrSnapshotDisabled when SetSnapshotDeps was never called
	// (slice-22b gated off via cfg.Snapshot.Enabled=false). Handler maps to 503.
	mr := &mockRuntime{snapErr: sandbox.ErrSnapshotDisabled}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+uuid.NewString()+"/snapshot", tok, nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "snapshot_disabled")
}

func TestHandler_Snapshot_Create_OK(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()
	sid := uuid.New()
	snap := &sandbox.Snapshot{
		ID: uuid.New(), TenantID: tid, UserID: uid,
		SessionID: &sid,
		ObjectKey: tid.String() + "/" + sid.String() + "/2026-05-23T00.tar",
		SizeBytes: 12345,
		ImageRef:  "pca-snapshot-x:1",
		Metadata:  map[string]any{"image_id": "sha256:abc"},
		CreatedAt: time.Now(),
	}
	mr := &mockRuntime{snap: snap}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+sid.String()+"/snapshot", tok, nil)
	require.Equal(t, http.StatusCreated, w.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, snap.ID.String(), got["id"])
	require.Equal(t, snap.ObjectKey, got["object_key"])
	require.Equal(t, float64(snap.SizeBytes), got["size_bytes"])
	require.Equal(t, snap.ImageRef, got["image_ref"])
}

func TestHandler_Snapshot_NotReady(t *testing.T) {
	mr := &mockRuntime{snapErr: sandbox.ErrSandboxNotReady}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+uuid.NewString()+"/snapshot", tok, nil)
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestHandler_Snapshot_NotFound(t *testing.T) {
	mr := &mockRuntime{snapErr: sandbox.ErrSandboxNotFound}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+uuid.NewString()+"/snapshot", tok, nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_SnapshotList_DisabledNoRepo(t *testing.T) {
	// WithSnapshotRepo never called → list route 503 snapshot_disabled.
	mr := &mockRuntime{}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/snapshots", tok, nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "snapshot_disabled")
}

func TestHandler_SnapshotGet_DisabledNoRepo(t *testing.T) {
	mr := &mockRuntime{}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/snapshots/"+uuid.NewString(), tok, nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandler_SnapshotRestore_OK(t *testing.T) {
	sid := uuid.New()
	mr := &mockRuntime{restoreRet: &sandbox.Sandbox{
		ID: sid, Status: sandbox.StatusRunning, Image: "pca/snapshot:restored",
	}}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/snapshots/restore/"+uuid.NewString(), tok, nil)
	require.Equal(t, http.StatusCreated, w.Code)
	require.Contains(t, w.Body.String(), sid.String())
}

func TestHandler_SnapshotRestore_Disabled(t *testing.T) {
	mr := &mockRuntime{restoreErr: sandbox.ErrSnapshotDisabled}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/snapshots/restore/"+uuid.NewString(), tok, nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandler_SnapshotList_NoAuth(t *testing.T) {
	mr := &mockRuntime{}
	r, _ := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/snapshots", "", nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

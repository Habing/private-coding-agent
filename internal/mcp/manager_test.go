package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/mcp"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// newBus builds a Bus with an empty registry, no quota, no audit. The recorder
// is nil-safe because Manager never invokes through the Bus; it only registers
// tools.
func newBus(t *testing.T) *toolbus.Bus {
	t.Helper()
	bus, err := toolbus.NewBus(toolbus.NewRegistry(), &toolbus.InvocationRecorder{})
	require.NoError(t, err)
	return bus
}

// mockMCPServer spins up a JSON-RPC endpoint that exposes one configurable
// tool. callsByMethod is incremented per method so tests can assert request
// counts (e.g. heartbeat fired N times).
type mockMCPServer struct {
	mu            chan struct{}
	tools         []mcp.ToolSchema
	failInit      bool
	callsByMethod map[string]*atomic.Int64
}

func newMockMCPServer(t *testing.T, tools []mcp.ToolSchema) *httptest.Server {
	t.Helper()
	m := &mockMCPServer{
		mu:    make(chan struct{}, 1),
		tools: tools,
		callsByMethod: map[string]*atomic.Int64{
			"initialize": new(atomic.Int64),
			"tools/list": new(atomic.Int64),
			"tools/call": new(atomic.Int64),
		},
	}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	return srv
}

func (m *mockMCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if c, ok := m.callsByMethod[req.Method]; ok {
		c.Add(1)
	}
	w.Header().Set("Content-Type", "application/json")
	switch req.Method {
	case "initialize":
		if m.failInit {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]any{"code": -32000, "message": "init failed"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{
				"protocolVersion": mcp.ProtocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "mock", "version": "1"},
			},
		})
	case "tools/list":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{"tools": m.tools},
		})
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"error": map[string]any{"code": -32601, "message": "method not found"},
		})
	}
}

func defaultCfg() mcp.Config {
	return mcp.Config{
		Enabled:           true,
		HeartbeatInterval: 0, // off by default; tests opt in
		InvokeTimeout:     5 * time.Second,
		ListToolsTimeout:  5 * time.Second,
	}
}

func TestManager_Disabled_ReturnsErr(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	m := mcp.NewManager(repo, bus, nil, mcp.Config{Enabled: false})

	require.ErrorIs(t, m.Start(context.Background()), mcp.ErrManagerDisabled)
	_, err := m.RegisterServer(context.Background(), sampleServer(uuid.New(), "x"))
	require.ErrorIs(t, err, mcp.ErrManagerDisabled)
	_, err = m.RefreshTools(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, mcp.ErrManagerDisabled)
	require.ErrorIs(t, m.TestConnection(context.Background(), sampleServer(uuid.New(), "x")), mcp.ErrManagerDisabled)
}

func TestManager_BootRepublishesCachedTools(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	srv := sampleServer(tid, "boot")
	srv.ToolsCache = []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
		{Name: "wipe", InputSchema: map[string]any{"type": "object"},
			Annotations: map[string]any{"destructiveHint": true}},
	}
	created, err := repo.Insert(ctx, srv)
	require.NoError(t, err)

	m := mcp.NewManager(repo, bus, nil, defaultCfg())
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	// Both cached tools should be on the Bus under the mcp.<slug>. prefix.
	defs := bus.ListTools(ctx, tid)
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["mcp.boot.echo"], "echo should be republished: got %v", names)
	assert.True(t, names["mcp.boot.wipe"], "wipe should be republished: got %v", names)
	assert.Equal(t, 2, m.ToolCount(created.ID))
}

func TestManager_BootSkipsDisabledServers(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	off := sampleServer(tid, "boot-off")
	off.Enabled = false
	off.ToolsCache = []mcp.ToolSchema{{Name: "wont", InputSchema: map[string]any{"type": "object"}}}
	_, err := repo.Insert(ctx, off)
	require.NoError(t, err)

	m := mcp.NewManager(repo, bus, nil, defaultCfg())
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	for _, d := range bus.ListTools(ctx, tid) {
		assert.NotEqual(t, "mcp.boot-off.wont", d.Name,
			"disabled server's tools must not be on the Bus")
	}
}

func TestManager_RegisterServer_FetchesAndRegisters(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", Description: "say hi",
			InputSchema: map[string]any{"type": "object"}},
	})

	s := sampleServer(tid, "reg")
	s.URL = srv.URL
	created, err := repo.Insert(ctx, s)
	require.NoError(t, err)

	m := mcp.NewManager(repo, bus, nil, defaultCfg())

	tools, err := m.RegisterServer(ctx, created)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "echo", tools[0].Name)

	// Bus has it.
	assert.True(t, m.IsRegistered("mcp.reg.echo"))

	// Cache row persisted.
	dbRow, err := repo.Get(ctx, tid, created.ID)
	require.NoError(t, err)
	require.Len(t, dbRow.ToolsCache, 1)
	assert.Equal(t, "echo", dbRow.ToolsCache[0].Name)
	assert.NotNil(t, dbRow.LastSeenAt)
	assert.Empty(t, dbRow.LastError)
}

func TestManager_RegisterServer_BadURLRecordsLastError(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	s := sampleServer(tid, "bad")
	s.URL = "http://127.0.0.1:1" // refused
	created, err := repo.Insert(ctx, s)
	require.NoError(t, err)

	m := mcp.NewManager(repo, bus, nil, mcp.Config{
		Enabled: true, InvokeTimeout: 500 * time.Millisecond,
		ListToolsTimeout: 500 * time.Millisecond,
	})
	_, err = m.RegisterServer(ctx, created)
	require.Error(t, err)

	row, err := repo.Get(ctx, tid, created.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, row.LastError)
	assert.False(t, m.IsRegistered("mcp.bad.echo"),
		"failed RegisterServer must not leak partial Bus entries")
}

func TestManager_RefreshTools_UpdatesBusAndCache(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	initialTools := []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	}
	srv := newMockMCPServer(t, initialTools)

	s := sampleServer(tid, "rfr")
	s.URL = srv.URL
	created, err := repo.Insert(ctx, s)
	require.NoError(t, err)

	m := mcp.NewManager(repo, bus, nil, defaultCfg())
	_, err = m.RegisterServer(ctx, created)
	require.NoError(t, err)
	assert.True(t, m.IsRegistered("mcp.rfr.echo"))
	assert.False(t, m.IsRegistered("mcp.rfr.echo2"))

	// Server now advertises an extra tool.
	srv.Close()
	srv2 := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
		{Name: "echo2", InputSchema: map[string]any{"type": "object"}},
	})
	// Update URL on the cached row so RefreshTools picks the new endpoint.
	newURL := srv2.URL
	_, err = repo.Update(ctx, tid, created.ID, nil, nil, &newURL, nil, nil, nil, nil)
	require.NoError(t, err)
	// Drop the cached Client so RefreshTools rebuilds against the new URL.
	m.UnregisterServer(created.ID)

	got, err := m.RefreshTools(ctx, tid, created.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, m.IsRegistered("mcp.rfr.echo"))
	assert.True(t, m.IsRegistered("mcp.rfr.echo2"))

	dbRow, err := repo.Get(ctx, tid, created.ID)
	require.NoError(t, err)
	require.Len(t, dbRow.ToolsCache, 2)
}

func TestManager_RefreshTools_DisabledServerRejected(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	s := sampleServer(tid, "rfd")
	s.Enabled = false
	created, err := repo.Insert(ctx, s)
	require.NoError(t, err)

	m := mcp.NewManager(repo, bus, nil, defaultCfg())
	_, err = m.RefreshTools(ctx, tid, created.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestManager_UnregisterServer_IsIdempotent(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	})
	s := sampleServer(tid, "unr")
	s.URL = srv.URL
	created, err := repo.Insert(ctx, s)
	require.NoError(t, err)

	m := mcp.NewManager(repo, bus, nil, defaultCfg())
	_, err = m.RegisterServer(ctx, created)
	require.NoError(t, err)
	require.True(t, m.IsRegistered("mcp.unr.echo"))

	m.UnregisterServer(created.ID)
	assert.False(t, m.IsRegistered("mcp.unr.echo"))
	// Calling again must not panic.
	m.UnregisterServer(created.ID)
}

func TestManager_TestConnection_NoSideEffects(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	})
	s := sampleServer(tid, "tst")
	s.URL = srv.URL
	created, err := repo.Insert(ctx, s)
	require.NoError(t, err)

	m := mcp.NewManager(repo, bus, nil, defaultCfg())
	require.NoError(t, m.TestConnection(ctx, created))

	// No Bus registration.
	assert.False(t, m.IsRegistered("mcp.tst.echo"))
	// No cache write.
	row, err := repo.Get(ctx, tid, created.ID)
	require.NoError(t, err)
	assert.Empty(t, row.ToolsCache,
		"TestConnection must not persist a tools_cache snapshot")
}

func TestManager_Heartbeat_UpdatesLastSeenAndError(t *testing.T) {
	p := newPool(t)
	repo := mcp.NewRepo(p)
	bus := newBus(t)
	ctx := context.Background()
	tid := seedTenant(t, p)

	srv := newMockMCPServer(t, []mcp.ToolSchema{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}},
	})
	s := sampleServer(tid, "hb")
	s.URL = srv.URL
	created, err := repo.Insert(ctx, s)
	require.NoError(t, err)

	cfg := defaultCfg()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	m := mcp.NewManager(repo, bus, nil, cfg)
	_, err = m.RegisterServer(ctx, created)
	require.NoError(t, err)
	require.NoError(t, m.Start(ctx))
	defer m.Stop()

	// Wait for at least one heartbeat tick.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		row, err := repo.Get(ctx, tid, created.ID)
		require.NoError(t, err)
		if row.LastSeenAt != nil && !row.LastSeenAt.IsZero() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("heartbeat never updated last_seen_at within 2s")
}

func TestManager_AuthTokenFingerprint(t *testing.T) {
	assert.Equal(t, "", mcp.AuthTokenFingerprint(""))
	fp := mcp.AuthTokenFingerprint("super-secret")
	assert.Len(t, fp, 8)
	assert.NotContains(t, fp, "super",
		"fingerprint must not reveal the original token")
}

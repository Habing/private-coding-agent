package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/yourorg/private-coding-agent/internal/audit"
	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// Config controls Manager lifecycle. Zero values fall back to safe defaults
// when the Manager is constructed.
type Config struct {
	// Enabled gates the whole subsystem. When false the Manager is not built
	// and admin endpoints return ErrManagerDisabled. Used by main.go.
	Enabled bool
	// HeartbeatInterval is the gap between successive Ping rounds against all
	// enabled servers. 0 disables the loop entirely.
	HeartbeatInterval time.Duration
	// InvokeTimeout is the per-request HTTP timeout for tools/call. Falls back
	// to DefaultTimeout when 0.
	InvokeTimeout time.Duration
	// ListToolsTimeout is used by Initialize and tools/list during refresh and
	// boot republish. Tighter than InvokeTimeout because no tool work runs.
	ListToolsTimeout time.Duration
}

// Manager owns the lifecycle of every enabled external MCP server: builds the
// per-server HTTP Client, republishes cached tools to the ToolBus at boot,
// pings them on a heartbeat loop, and exposes admin-facing helpers that mutate
// the underlying mcp_servers rows and the Bus in lockstep.
type Manager struct {
	repo  *Repo
	bus   *toolbus.Bus
	audit audit.Sink
	cfg   Config

	mu      sync.Mutex
	clients map[uuid.UUID]*Client // by server.ID
	toolIDs map[uuid.UUID][]string

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewManager builds a Manager. The caller must hold the Bus pointer until the
// Manager is stopped; the Manager does not own the Bus.
func NewManager(repo *Repo, bus *toolbus.Bus, sink audit.Sink, cfg Config) *Manager {
	if cfg.InvokeTimeout == 0 {
		cfg.InvokeTimeout = DefaultTimeout
	}
	if cfg.ListToolsTimeout == 0 {
		cfg.ListToolsTimeout = 10 * time.Second
	}
	return &Manager{
		repo:    repo,
		bus:     bus,
		audit:   sink,
		cfg:     cfg,
		clients: map[uuid.UUID]*Client{},
		toolIDs: map[uuid.UUID][]string{},
		stopCh:  make(chan struct{}),
	}
}

// Start performs boot-time republish: each enabled mcp_servers row is wrapped
// in a Client and its tools_cache is re-registered on the Bus, so the system
// stays available even if a server is briefly unreachable at startup. Failures
// of individual servers are logged and persisted via UpdateLastError but do
// NOT abort startup — the spec calls boot republish best-effort.
//
// After republish, the heartbeat goroutine (if HeartbeatInterval > 0) is
// started so degraded servers get marked promptly.
func (m *Manager) Start(ctx context.Context) error {
	if !m.cfg.Enabled {
		return ErrManagerDisabled
	}
	servers, err := m.repo.ListAllEnabled(ctx)
	if err != nil {
		return fmt.Errorf("mcp: boot list: %w", err)
	}
	for i := range servers {
		s := &servers[i]
		if err := m.registerLocked(s, s.ToolsCache); err != nil {
			slog.Warn("mcp: boot republish failed",
				"slug", s.Slug, "tenant", s.TenantID, "err", err)
			_ = m.repo.UpdateLastError(ctx, s.TenantID, s.ID, err.Error())
		}
	}
	if m.cfg.HeartbeatInterval > 0 {
		m.wg.Add(1)
		go m.heartbeatLoop()
	}
	return nil
}

// Stop shuts down the heartbeat goroutine. Bus state is left intact so any
// in-flight Agent runs can finish; main.go normally calls Stop after the
// HTTP server stops accepting new requests.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
	m.wg.Wait()
}

// RegisterServer is the create-time path: Initialize + ListTools against the
// remote, persist the snapshot via UpdateToolsCache, then wrap each ToolSchema
// in a Tool and Bus.Register it. Replaces any existing registration for the
// same server (admins changing url/auth/tokens still hit the same path).
//
// Returns the freshly-fetched ToolSchema list so the admin handler can echo it
// back in the create/update response without an extra DB round-trip.
func (m *Manager) RegisterServer(ctx context.Context, s *Server) ([]ToolSchema, error) {
	if !m.cfg.Enabled {
		return nil, ErrManagerDisabled
	}
	client := m.newClient(s)
	initCtx, cancel := context.WithTimeout(ctx, m.cfg.ListToolsTimeout)
	defer cancel()
	if _, err := client.Initialize(initCtx); err != nil {
		_ = m.repo.UpdateLastError(ctx, s.TenantID, s.ID, err.Error())
		return nil, fmt.Errorf("mcp: initialize %s: %w", s.Slug, err)
	}
	tools, err := client.ListTools(initCtx)
	if err != nil {
		_ = m.repo.UpdateLastError(ctx, s.TenantID, s.ID, err.Error())
		return nil, fmt.Errorf("mcp: tools/list %s: %w", s.Slug, err)
	}
	if err := m.repo.UpdateToolsCache(ctx, s.TenantID, s.ID, tools, time.Now()); err != nil {
		return nil, fmt.Errorf("mcp: cache tools for %s: %w", s.Slug, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregisterToolsLocked(s.ID)
	if err := m.registerToolsLocked(s, tools, client); err != nil {
		return nil, err
	}
	return tools, nil
}

// UnregisterServer is the delete/disable path: drop every `mcp.<slug>.<tool>`
// entry from the Bus and forget the per-server Client. Idempotent — calling
// twice is a no-op.
func (m *Manager) UnregisterServer(serverID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregisterToolsLocked(serverID)
}

// RefreshTools re-runs tools/list against a registered server and republishes
// the result. Admin "Refresh" button calls this. Errors are persisted to
// last_error so the WebUI surfaces them.
func (m *Manager) RefreshTools(ctx context.Context, tenantID, serverID uuid.UUID) ([]ToolSchema, error) {
	if !m.cfg.Enabled {
		return nil, ErrManagerDisabled
	}
	s, err := m.repo.Get(ctx, tenantID, serverID)
	if err != nil {
		return nil, err
	}
	if !s.Enabled {
		return nil, fmt.Errorf("mcp: server %s is disabled", s.Slug)
	}
	// Reuse cached Client if present so per-call SSE state (future) is kept.
	m.mu.Lock()
	client := m.clients[s.ID]
	m.mu.Unlock()
	if client == nil {
		client = m.newClient(s)
	}
	listCtx, cancel := context.WithTimeout(ctx, m.cfg.ListToolsTimeout)
	defer cancel()
	if _, err := client.Initialize(listCtx); err != nil {
		_ = m.repo.UpdateLastError(ctx, tenantID, serverID, err.Error())
		return nil, fmt.Errorf("mcp: initialize %s: %w", s.Slug, err)
	}
	tools, err := client.ListTools(listCtx)
	if err != nil {
		_ = m.repo.UpdateLastError(ctx, tenantID, serverID, err.Error())
		return nil, fmt.Errorf("mcp: tools/list %s: %w", s.Slug, err)
	}
	if err := m.repo.UpdateToolsCache(ctx, tenantID, serverID, tools, time.Now()); err != nil {
		return nil, fmt.Errorf("mcp: cache tools for %s: %w", s.Slug, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregisterToolsLocked(serverID)
	if err := m.registerToolsLocked(s, tools, client); err != nil {
		return nil, err
	}
	return tools, nil
}

// TestConnection runs Initialize against a candidate Server without touching
// the Bus or persisting anything. The admin "Test" button uses this to
// validate URL + token combinations before saving.
func (m *Manager) TestConnection(ctx context.Context, s *Server) error {
	if !m.cfg.Enabled {
		return ErrManagerDisabled
	}
	client := m.newClient(s)
	tctx, cancel := context.WithTimeout(ctx, m.cfg.ListToolsTimeout)
	defer cancel()
	_, err := client.Initialize(tctx)
	return err
}

// AuthTokenFingerprint returns a short sha256 prefix of the bearer token for
// audit metadata. Used by the admin handler so audit_log never stores the
// raw secret.
func AuthTokenFingerprint(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:8]
}

// registerLocked is the shared boot/RegisterServer path that wraps the
// register-tools half. The caller has already loaded/persisted `tools`; this
// builds the Client (idempotent) and pushes the Tool adapters into the Bus.
//
// Holds m.mu via the calling locked helpers, so it must not be called from
// outside the mu.Lock()/Unlock() critical section.
func (m *Manager) registerLocked(s *Server, tools []ToolSchema) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	client := m.clients[s.ID]
	if client == nil {
		client = m.newClient(s)
	}
	m.unregisterToolsLocked(s.ID)
	return m.registerToolsLocked(s, tools, client)
}

func (m *Manager) registerToolsLocked(s *Server, tools []ToolSchema, client *Client) error {
	m.clients[s.ID] = client
	names := make([]string, 0, len(tools))
	for _, schema := range tools {
		tool := NewTool(s.ID, s.Slug, s.TenantID, schema, client, m.audit)
		// Bus.Register rejects duplicates with a plain error; cross-tenant slug
		// collisions land here. Bubble the first error so admin sees it; tools
		// registered before the failure stay live (they'll be cleaned up by a
		// later disable/delete). This mirrors workflow.registerTool semantics.
		if err := m.bus.Register(tool); err != nil {
			m.toolIDs[s.ID] = names
			return fmt.Errorf("mcp: bus register %s: %w", tool.Name(), err)
		}
		names = append(names, tool.Name())
	}
	m.toolIDs[s.ID] = names
	return nil
}

func (m *Manager) unregisterToolsLocked(serverID uuid.UUID) {
	for _, name := range m.toolIDs[serverID] {
		_ = m.bus.Unregister(name)
	}
	delete(m.toolIDs, serverID)
	delete(m.clients, serverID)
}

func (m *Manager) newClient(s *Server) *Client {
	httpClient := &http.Client{Timeout: m.cfg.InvokeTimeout}
	return NewClient(s.URL, s.AuthType, s.AuthToken, s.Headers, httpClient)
}

// heartbeatLoop wakes on m.cfg.HeartbeatInterval and pings every currently
// enabled server. Outcome is recorded to last_seen_at / last_error and a
// MCPHeartbeatTotal counter — but NOT to the audit log (high frequency,
// low signal).
func (m *Manager) heartbeatLoop() {
	defer m.wg.Done()
	t := time.NewTicker(m.cfg.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-t.C:
			m.heartbeatOnce()
		}
	}
}

func (m *Manager) heartbeatOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.ListToolsTimeout)
	defer cancel()
	servers, err := m.repo.ListAllEnabled(ctx)
	if err != nil {
		slog.Warn("mcp: heartbeat list failed", "err", err)
		return
	}
	for i := range servers {
		s := &servers[i]
		m.heartbeatOne(ctx, s)
	}
}

func (m *Manager) heartbeatOne(ctx context.Context, s *Server) {
	m.mu.Lock()
	client := m.clients[s.ID]
	m.mu.Unlock()
	if client == nil {
		// Server was enabled after Start; pick it up lazily on next refresh.
		return
	}
	pctx, cancel := context.WithTimeout(ctx, m.cfg.ListToolsTimeout)
	defer cancel()
	err := client.Ping(pctx)
	outcome := "success"
	if err != nil {
		outcome = "fail"
		_ = m.repo.UpdateLastError(ctx, s.TenantID, s.ID, err.Error())
	} else {
		_ = m.repo.UpdateLastSeen(ctx, s.TenantID, s.ID, time.Now())
	}
	if pcametrics.MCPHeartbeatTotal != nil {
		pcametrics.MCPHeartbeatTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("server", s.Slug),
			attribute.String("outcome", outcome),
		))
	}
}

// ToolCount returns the number of `mcp.<slug>.<tool>` names currently
// registered for the given server. Used by tests and the admin handler when
// composing list payloads.
func (m *Manager) ToolCount(serverID uuid.UUID) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.toolIDs[serverID])
}

// IsRegistered reports whether the named tool is currently bound to the Bus
// under this Manager. Tests inspect this without reaching into Bus internals.
func (m *Manager) IsRegistered(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, names := range m.toolIDs {
		for _, n := range names {
			if n == name {
				return true
			}
		}
	}
	return false
}

// sentinel kept for readability of error wrapping above.
var _ = errors.New

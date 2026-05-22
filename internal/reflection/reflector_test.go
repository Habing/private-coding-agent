package reflection_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/reflection"
	"github.com/yourorg/private-coding-agent/internal/session"
)

type fakeGateway struct {
	gotReq *modelgw.ChatRequest
	resp   *modelgw.ChatResponse
	err    error
}

func (f *fakeGateway) ChatCompletion(_ context.Context, _, _ uuid.UUID,
	req modelgw.ChatRequest) (*modelgw.ChatResponse, error) {
	r := req
	f.gotReq = &r
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func chatResp(content string) *modelgw.ChatResponse {
	return &modelgw.ChatResponse{
		Choices: []modelgw.ChatChoice{{
			Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: content},
		}},
	}
}

type fakeMessages struct {
	msgs []session.Message
	err  error
}

func (f *fakeMessages) List(_ context.Context, _, _ uuid.UUID) ([]session.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.msgs, nil
}

type fakeMemSvc struct {
	calls    int32
	lastType string
	dedup    bool
	err      error
	memID    uuid.UUID
}

func (f *fakeMemSvc) CreateForReflection(_ context.Context, _, _ uuid.UUID,
	typ, _ string, _ []string, _ *uuid.UUID) (uuid.UUID, bool, error) {
	atomic.AddInt32(&f.calls, 1)
	f.lastType = typ
	if f.err != nil {
		return uuid.Nil, false, f.err
	}
	if f.memID == uuid.Nil {
		f.memID = uuid.New()
	}
	return f.memID, f.dedup, nil
}

// dockertest helper reused from repo_test.go (TestMain + fixtures + newPool).

func mkSession(t *testing.T, pg *pgxpool.Pool, tid, uid uuid.UUID) uuid.UUID {
	t.Helper()
	sid := uuid.New()
	_, err := pg.Exec(context.Background(),
		`INSERT INTO sessions (id, tenant_id, owner_user_id, model) VALUES ($1,$2,$3,'mock')`,
		sid, tid, uid)
	require.NoError(t, err)
	return sid
}

func userMsg(tid, sid uuid.UUID, seq int64, content string) session.Message {
	return session.Message{
		ID: uuid.New(), TenantID: tid, SessionID: sid,
		Seq: seq, Role: "user", Content: content,
	}
}

func TestReflector_AutoApprove_HitsMemoryServiceAndStoresProposal(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	sid := mkSession(t, pg, tid, uid)
	gw := &fakeGateway{resp: chatResp(
		`[{"type":"preference","content":"loves Go","tags":["go"],"confidence":0.95}]`,
	)}
	mem := &fakeMemSvc{memID: uuid.New()}
	msgs := &fakeMessages{msgs: []session.Message{userMsg(tid, sid, 1, "I love Go generics")}}

	r := reflection.NewReflector(gw, mem, msgs, repo, nil, reflection.Config{
		Model: "mock", AutoApproveThreshold: 0.85,
	})
	require.NoError(t, r.Reflect(ctx, reflection.ReflectionJob{
		TenantID: tid, UserID: uid, SessionID: sid,
	}))

	// memory.Service.Create called once.
	require.Equal(t, int32(1), atomic.LoadInt32(&mem.calls))
	require.Equal(t, "preference", mem.lastType)

	// Proposal stored as auto_approved with memory_id filled.
	list, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, reflection.StatusAutoApproved, list[0].Status)
	require.NotNil(t, list[0].MemoryID)
	require.Equal(t, mem.memID, *list[0].MemoryID)
	require.NotNil(t, list[0].DecidedAt)

	// LLM saw the reflection marker in the system prompt.
	require.NotNil(t, gw.gotReq)
	require.GreaterOrEqual(t, len(gw.gotReq.Messages), 2)
	require.Equal(t, modelgw.RoleSystem, gw.gotReq.Messages[0].Role)
	require.True(t, strings.Contains(gw.gotReq.Messages[0].Content, reflection.ReflectionMarker))
}

func TestReflector_LowConfidence_PendingNotAutoApproved(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	sid := mkSession(t, pg, tid, uid)
	gw := &fakeGateway{resp: chatResp(
		`[{"type":"preference","content":"loves Go","tags":["go"],"confidence":0.5}]`,
	)}
	mem := &fakeMemSvc{}
	msgs := &fakeMessages{msgs: []session.Message{userMsg(tid, sid, 1, "hi")}}

	r := reflection.NewReflector(gw, mem, msgs, repo, nil, reflection.Config{
		Model: "mock", AutoApproveThreshold: 0.85,
	})
	require.NoError(t, r.Reflect(ctx, reflection.ReflectionJob{
		TenantID: tid, UserID: uid, SessionID: sid,
	}))
	// memSvc NOT called.
	require.Equal(t, int32(0), atomic.LoadInt32(&mem.calls))

	list, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, reflection.StatusPending, list[0].Status)
	require.Nil(t, list[0].MemoryID)
	require.Nil(t, list[0].DecidedAt)
}

func TestReflector_EmptyArray_NoProposal(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	sid := uuid.New()
	gw := &fakeGateway{resp: chatResp(`[]`)}
	mem := &fakeMemSvc{}
	msgs := &fakeMessages{msgs: []session.Message{userMsg(tid, sid, 1, "hi")}}

	r := reflection.NewReflector(gw, mem, msgs, repo, nil, reflection.Config{Model: "mock"})
	require.NoError(t, r.Reflect(ctx, reflection.ReflectionJob{TenantID: tid, UserID: uid, SessionID: sid}))

	list, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{})
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestReflector_LLMError_NoProposalNoMemory(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	sid := uuid.New()
	gw := &fakeGateway{err: errors.New("boom")}
	mem := &fakeMemSvc{}
	msgs := &fakeMessages{msgs: []session.Message{userMsg(tid, sid, 1, "hi")}}

	r := reflection.NewReflector(gw, mem, msgs, repo, nil, reflection.Config{Model: "mock"})
	err := r.Reflect(ctx, reflection.ReflectionJob{TenantID: tid, UserID: uid, SessionID: sid})
	require.Error(t, err)

	list, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{})
	require.NoError(t, err)
	require.Empty(t, list)
	require.Equal(t, int32(0), atomic.LoadInt32(&mem.calls))
}

func TestReflector_BadJSON_NoProposal(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	sid := uuid.New()
	gw := &fakeGateway{resp: chatResp(`not json at all`)}
	mem := &fakeMemSvc{}
	msgs := &fakeMessages{msgs: []session.Message{userMsg(tid, sid, 1, "hi")}}

	r := reflection.NewReflector(gw, mem, msgs, repo, nil, reflection.Config{Model: "mock"})
	require.Error(t, r.Reflect(ctx, reflection.ReflectionJob{TenantID: tid, UserID: uid, SessionID: sid}))

	list, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{})
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestReflector_TolerateMarkdownFences(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	sid := mkSession(t, pg, tid, uid)
	gw := &fakeGateway{resp: chatResp(
		"```json\n[{\"type\":\"profile\",\"content\":\"x\",\"tags\":[],\"confidence\":0.3}]\n```",
	)}
	mem := &fakeMemSvc{}
	msgs := &fakeMessages{msgs: []session.Message{userMsg(tid, sid, 1, "hi")}}

	r := reflection.NewReflector(gw, mem, msgs, repo, nil, reflection.Config{Model: "mock"})
	require.NoError(t, r.Reflect(ctx, reflection.ReflectionJob{TenantID: tid, UserID: uid, SessionID: sid}))
	list, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestReflector_DropsInvalidType(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	sid := mkSession(t, pg, tid, uid)
	gw := &fakeGateway{resp: chatResp(
		`[{"type":"bogus","content":"x","tags":[],"confidence":0.5},
		  {"type":"profile","content":"y","tags":[],"confidence":0.5}]`,
	)}
	mem := &fakeMemSvc{}
	msgs := &fakeMessages{msgs: []session.Message{userMsg(tid, sid, 1, "hi")}}

	r := reflection.NewReflector(gw, mem, msgs, repo, nil, reflection.Config{Model: "mock"})
	require.NoError(t, r.Reflect(ctx, reflection.ReflectionJob{TenantID: tid, UserID: uid, SessionID: sid}))
	list, err := repo.ListByTenant(ctx, tid, reflection.ListFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "profile", list[0].Type)
}

func TestReflector_MessageLimitAndTruncation(t *testing.T) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	ctx := context.Background()
	repo := reflection.NewRepo(pg)

	sid := uuid.New()
	gw := &fakeGateway{resp: chatResp(`[]`)}
	mem := &fakeMemSvc{}
	// 30 messages, but cfg cap = 5.
	var ms []session.Message
	for i := int64(0); i < 30; i++ {
		ms = append(ms, userMsg(tid, sid, i+1, strings.Repeat("x", 1000)))
	}
	msgs := &fakeMessages{msgs: ms}

	r := reflection.NewReflector(gw, mem, msgs, repo, nil, reflection.Config{
		Model: "mock", MaxMessagesPerSession: 5, MaxCharsPerMessage: 100,
	})
	require.NoError(t, r.Reflect(ctx, reflection.ReflectionJob{TenantID: tid, UserID: uid, SessionID: sid}))

	require.NotNil(t, gw.gotReq)
	// 1 system + 5 user
	require.Equal(t, 6, len(gw.gotReq.Messages))
	require.Equal(t, modelgw.RoleSystem, gw.gotReq.Messages[0].Role)
	require.Equal(t, 100, len(gw.gotReq.Messages[1].Content))
}

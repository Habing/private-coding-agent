package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/memory"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

type mockMemSvc struct {
	createRet  *memory.Memory
	createErr  error
	searchRet  []memory.Memory
	searchErr  error
	listRet    []memory.Memory
	listErr    error
	delErr     error
	lastCreate memory.CreateRequest
	lastSearch memory.SearchRequest
	lastList   memory.ListFilter
	lastDelID  uuid.UUID
}

func (m *mockMemSvc) Create(_ context.Context, _, _ uuid.UUID, r memory.CreateRequest) (*memory.Memory, error) {
	m.lastCreate = r
	return m.createRet, m.createErr
}
func (m *mockMemSvc) Search(_ context.Context, _, _ uuid.UUID, r memory.SearchRequest) ([]memory.Memory, error) {
	m.lastSearch = r
	return m.searchRet, m.searchErr
}
func (m *mockMemSvc) List(_ context.Context, _, _ uuid.UUID, f memory.ListFilter) ([]memory.Memory, error) {
	m.lastList = f
	return m.listRet, m.listErr
}
func (m *mockMemSvc) Delete(_ context.Context, _, _, id uuid.UUID) error {
	m.lastDelID = id
	return m.delErr
}

func TestMemorySave_OK(t *testing.T) {
	id := uuid.New()
	svc := &mockMemSvc{createRet: &memory.Memory{ID: id, Type: memory.TypeKnowledge}}
	tool := tools.NewMemorySave(svc)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"type":"knowledge","content":"x"}`))
	require.NoError(t, err)
	var got struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, id, got.ID)
	require.Equal(t, memory.SourceAgent, svc.lastCreate.Source, "default source = agent for tool-driven saves")
}

func TestMemorySave_ValidationWraps(t *testing.T) {
	svc := &mockMemSvc{createErr: memory.ErrInvalidType}
	tool := tools.NewMemorySave(svc)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"type":"profile","content":"x"}`))
	require.True(t, errors.Is(err, toolbus.ErrInvalidArguments))
}

func TestMemorySearch_OK(t *testing.T) {
	svc := &mockMemSvc{searchRet: []memory.Memory{
		{ID: uuid.New(), Type: memory.TypeKnowledge, Content: "found", Tags: []string{"a"}},
	}}
	tool := tools.NewMemorySearch(svc)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"query":"foo"}`))
	require.NoError(t, err)
	require.Equal(t, "foo", svc.lastSearch.Query)
	require.Contains(t, string(out), `"content":"found"`)
}

func TestMemorySearch_EmptyParams(t *testing.T) {
	svc := &mockMemSvc{searchErr: memory.ErrEmptySearch}
	tool := tools.NewMemorySearch(svc)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{}`))
	require.True(t, errors.Is(err, toolbus.ErrInvalidArguments))
}

func TestMemoryList_OK(t *testing.T) {
	svc := &mockMemSvc{listRet: []memory.Memory{
		{ID: uuid.New(), Type: memory.TypePreference, Content: "a"},
		{ID: uuid.New(), Type: memory.TypePreference, Content: "b"},
	}}
	tool := tools.NewMemoryList(svc)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"type":"preference","limit":10}`))
	require.NoError(t, err)
	require.Equal(t, memory.TypePreference, svc.lastList.Type)
	require.Equal(t, 10, svc.lastList.Limit)
	require.Contains(t, string(out), `"content":"a"`)
}

func TestMemoryDelete_OK(t *testing.T) {
	svc := &mockMemSvc{}
	id := uuid.New()
	tool := tools.NewMemoryDelete(svc)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"id":"`+id.String()+`"}`))
	require.NoError(t, err)
	require.Equal(t, id, svc.lastDelID)
	require.JSONEq(t, `{"ok":true}`, string(out))
}

func TestMemoryDelete_NotFoundBubbles(t *testing.T) {
	svc := &mockMemSvc{delErr: memory.ErrMemoryNotFound}
	tool := tools.NewMemoryDelete(svc)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"id":"`+uuid.New().String()+`"}`))
	require.ErrorIs(t, err, memory.ErrMemoryNotFound)
	require.False(t, errors.Is(err, toolbus.ErrInvalidArguments), "NotFound should not be wrapped as 4xx")
}

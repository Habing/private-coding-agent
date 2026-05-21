package memory_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/memory"
)

func newService(t *testing.T) (*memory.Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	return memory.NewService(memory.NewRepo(pg), nil, memory.MemoryConfig{}), tid, uid
}

func TestService_Create_Happy(t *testing.T) {
	svc, tid, uid := newService(t)
	res, err := svc.Create(context.Background(), tid, uid, memory.CreateRequest{
		Type: memory.TypePreference, Content: "uses tabs", Tags: []string{"style"},
	})
	require.NoError(t, err)
	require.True(t, res.Created)
	require.Equal(t, "uses tabs", res.Memory.Content)
	require.Equal(t, memory.SourceUser, res.Memory.Source)
}

func TestService_Create_Validation(t *testing.T) {
	svc, tid, uid := newService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, tid, uid, memory.CreateRequest{Type: memory.TypeKnowledge, Content: ""})
	require.ErrorIs(t, err, memory.ErrEmptyContent)

	_, err = svc.Create(ctx, tid, uid, memory.CreateRequest{Type: "bogus", Content: "x"})
	require.ErrorIs(t, err, memory.ErrInvalidType)
}

func TestService_List_TypeValidation(t *testing.T) {
	svc, tid, uid := newService(t)
	_, err := svc.List(context.Background(), tid, uid, memory.ListFilter{Type: "wrong"})
	require.ErrorIs(t, err, memory.ErrInvalidType)
}

func TestService_Update_Validation(t *testing.T) {
	svc, tid, uid := newService(t)
	ctx := context.Background()

	res, err := svc.Create(ctx, tid, uid, memory.CreateRequest{Type: memory.TypeLesson, Content: "orig"})
	require.NoError(t, err)

	wrong := "wrong"
	_, err = svc.Update(ctx, tid, uid, res.Memory.ID, memory.UpdateRequest{Type: &wrong})
	require.ErrorIs(t, err, memory.ErrInvalidType)

	empty := "   "
	_, err = svc.Update(ctx, tid, uid, res.Memory.ID, memory.UpdateRequest{Content: &empty})
	require.ErrorIs(t, err, memory.ErrEmptyContent)
}

func TestService_Search_EmptyParamsRejected(t *testing.T) {
	svc, tid, uid := newService(t)
	_, err := svc.Search(context.Background(), tid, uid, memory.SearchRequest{})
	require.ErrorIs(t, err, memory.ErrEmptySearch)
}

func TestService_Search_InvalidTypeRejected(t *testing.T) {
	svc, tid, uid := newService(t)
	_, err := svc.Search(context.Background(), tid, uid, memory.SearchRequest{Type: "x"})
	require.ErrorIs(t, err, memory.ErrInvalidType)
}

func TestService_Search_HappyRoundTrip(t *testing.T) {
	svc, tid, uid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, uid, memory.CreateRequest{
		Type: memory.TypeKnowledge, Content: "uses pgvector", Tags: []string{"db"},
	})
	require.NoError(t, err)

	hits, err := svc.Search(ctx, tid, uid, memory.SearchRequest{Query: "pgvector"})
	require.NoError(t, err)
	require.Len(t, hits, 1)
}

func TestService_CrossTenant404(t *testing.T) {
	svc, tid, uid := newService(t)
	ctx := context.Background()
	res, err := svc.Create(ctx, tid, uid, memory.CreateRequest{Type: memory.TypeProfile, Content: "x"})
	require.NoError(t, err)

	_, err = svc.Get(ctx, uuid.New(), uid, res.Memory.ID)
	require.ErrorIs(t, err, memory.ErrMemoryNotFound)

	require.ErrorIs(t, svc.Delete(ctx, uuid.New(), uid, res.Memory.ID), memory.ErrMemoryNotFound)

	newContent := "y"
	_, err = svc.Update(ctx, uuid.New(), uid, res.Memory.ID, memory.UpdateRequest{Content: &newContent})
	require.ErrorIs(t, err, memory.ErrMemoryNotFound)
}

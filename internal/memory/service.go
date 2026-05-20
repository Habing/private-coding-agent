package memory

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// Service is the application-layer facade over Repo. Handler / MCP tools call
// into this layer only.
type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

// Create validates the request and inserts a new memory.
func (s *Service) Create(ctx context.Context, tenantID, userID uuid.UUID, req CreateRequest) (*Memory, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, ErrEmptyContent
	}
	if !isValidType(req.Type) {
		return nil, ErrInvalidType
	}
	src := req.Source
	if src == "" {
		src = SourceUser
	}
	m := &Memory{
		ID:          uuid.New(),
		TenantID:    tenantID,
		OwnerUserID: userID,
		Type:        req.Type,
		Content:     req.Content,
		Tags:        req.Tags,
		Source:      src,
		SourceMsgID: req.SourceMsgID,
	}
	return s.repo.Insert(ctx, m)
}

// Get fetches one memory by id, scoped to tenant + owner.
func (s *Service) Get(ctx context.Context, tenantID, userID, id uuid.UUID) (*Memory, error) {
	return s.repo.Get(ctx, tenantID, userID, id)
}

// List returns memories matching the filter. Validation: if Type is set it
// must be a valid type.
func (s *Service) List(ctx context.Context, tenantID, userID uuid.UUID, f ListFilter) ([]Memory, error) {
	if f.Type != "" && !isValidType(f.Type) {
		return nil, ErrInvalidType
	}
	return s.repo.List(ctx, tenantID, userID, f)
}

// Update applies a partial update. Validates Type if provided. Content, if
// provided, must be non-empty.
func (s *Service) Update(ctx context.Context, tenantID, userID, id uuid.UUID, req UpdateRequest) (*Memory, error) {
	if req.Type != nil && !isValidType(*req.Type) {
		return nil, ErrInvalidType
	}
	if req.Content != nil && strings.TrimSpace(*req.Content) == "" {
		return nil, ErrEmptyContent
	}
	return s.repo.Update(ctx, tenantID, userID, id, req)
}

// Delete removes a memory. Returns ErrMemoryNotFound on miss.
func (s *Service) Delete(ctx context.Context, tenantID, userID, id uuid.UUID) error {
	return s.repo.Delete(ctx, tenantID, userID, id)
}

// Search runs the keyword + tag + type filter. At least one of (Query, Type,
// Tags) must be non-empty; otherwise ErrEmptySearch (mapped to 400 by the
// handler / tool). Hits touch last_used_at.
func (s *Service) Search(ctx context.Context, tenantID, userID uuid.UUID, req SearchRequest) ([]Memory, error) {
	if strings.TrimSpace(req.Query) == "" && req.Type == "" && len(req.Tags) == 0 {
		return nil, ErrEmptySearch
	}
	if req.Type != "" && !isValidType(req.Type) {
		return nil, ErrInvalidType
	}
	return s.repo.Search(ctx, tenantID, userID, req)
}

func isValidType(t string) bool {
	switch t {
	case TypeProfile, TypePreference, TypeKnowledge, TypeLesson:
		return true
	}
	return false
}

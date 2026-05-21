package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Service is the application-layer facade over Repo. Handler / MCP tools call
// into this layer only.
type Service struct {
	repo     *Repo
	embedder Embedder
	cfg      MemoryConfig
}

// NewService wires repo + (optional) embedder + config. Passing a nil
// embedder disables the vector pipeline entirely (Create skips embed,
// Search always uses keyword) regardless of cfg.EmbedOnWrite.
func NewService(repo *Repo, embedder Embedder, cfg MemoryConfig) *Service {
	return &Service{repo: repo, embedder: embedder, cfg: cfg}
}

// vectorEnabled reports whether the vector path is active for this Service.
// Both the embedder and the config flag must be set.
func (s *Service) vectorEnabled() bool {
	return s.embedder != nil && s.cfg.EmbedOnWrite
}

// CreateResult carries the inserted-or-deduped memory and whether the call
// hit an existing row (handler maps this to 200 vs 201).
type CreateResult struct {
	Memory  *Memory
	Created bool // true = inserted new row, false = dedup hit existing
}

// Create validates the request, embeds the content, dedups against existing
// rows, and inserts a new row on miss.
//
// Behaviour:
//   - vector disabled → plain insert with no embedding
//   - embed error      → return error (Create rejects; no silent insert)
//   - similarity ≥ DedupThreshold → touch existing row, return it (Created=false)
//   - otherwise        → insert new row with embedding (Created=true)
func (s *Service) Create(ctx context.Context, tenantID, userID uuid.UUID, req CreateRequest) (*CreateResult, error) {
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

	var vec []float32
	if s.vectorEnabled() {
		vecs, err := s.embedder.Embed(ctx, []string{req.Content})
		if err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
		if len(vecs) != 1 || len(vecs[0]) != s.embedder.Dim() {
			return nil, ErrEmbedDimMismatch
		}
		vec = vecs[0]

		if s.cfg.DedupThreshold > 0 {
			existing, _, err := s.repo.FindSimilar(ctx, tenantID, userID, vec, s.cfg.DedupThreshold)
			if err == nil {
				if terr := s.repo.TouchLastUsed(ctx, tenantID, userID, existing.ID); terr != nil {
					return nil, terr
				}
				return &CreateResult{Memory: existing, Created: false}, nil
			}
			if !errors.Is(err, ErrMemoryNotFound) {
				return nil, err
			}
		}
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
	inserted, err := s.repo.Insert(ctx, m, vec)
	if err != nil {
		return nil, err
	}
	return &CreateResult{Memory: inserted, Created: true}, nil
}

// Get fetches one memory by id, scoped to tenant + owner.
func (s *Service) Get(ctx context.Context, tenantID, userID, id uuid.UUID) (*Memory, error) {
	return s.repo.Get(ctx, tenantID, userID, id)
}

// List returns memories matching the filter.
func (s *Service) List(ctx context.Context, tenantID, userID uuid.UUID, f ListFilter) ([]Memory, error) {
	if f.Type != "" && !isValidType(f.Type) {
		return nil, ErrInvalidType
	}
	return s.repo.List(ctx, tenantID, userID, f)
}

// Update applies a partial update. If Content changes and vector is enabled,
// the embedding is recomputed. Update never dedups — explicit overwrites
// must not be silently merged into other rows.
func (s *Service) Update(ctx context.Context, tenantID, userID, id uuid.UUID, req UpdateRequest) (*Memory, error) {
	if req.Type != nil && !isValidType(*req.Type) {
		return nil, ErrInvalidType
	}
	if req.Content != nil && strings.TrimSpace(*req.Content) == "" {
		return nil, ErrEmptyContent
	}
	var vec []float32
	if req.Content != nil && s.vectorEnabled() {
		vecs, err := s.embedder.Embed(ctx, []string{*req.Content})
		if err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
		if len(vecs) != 1 || len(vecs[0]) != s.embedder.Dim() {
			return nil, ErrEmbedDimMismatch
		}
		vec = vecs[0]
	}
	return s.repo.Update(ctx, tenantID, userID, id, req, vec)
}

// Delete removes a memory.
func (s *Service) Delete(ctx context.Context, tenantID, userID, id uuid.UUID) error {
	return s.repo.Delete(ctx, tenantID, userID, id)
}

// Search dispatches keyword vs vector backends per req.Mode + query state.
//
// Resolution:
//
//	mode=""        + query="" → keyword (legacy filter-only behaviour)
//	mode=""        + query!="" + vector enabled → vector
//	mode=""        + query!="" + vector disabled → keyword
//	mode="vector"  + query!="" + vector enabled → vector
//	mode="vector"  + (query="" || vector disabled) → error
//	mode="keyword" → keyword
//
// At least one of (Query, Type, Tags) must be non-empty (mirrors slice 7).
func (s *Service) Search(ctx context.Context, tenantID, userID uuid.UUID, req SearchRequest) ([]SearchResult, error) {
	q := strings.TrimSpace(req.Query)
	if q == "" && req.Type == "" && len(req.Tags) == 0 {
		return nil, ErrEmptySearch
	}
	if req.Type != "" && !isValidType(req.Type) {
		return nil, ErrInvalidType
	}

	mode := req.Mode
	switch mode {
	case SearchModeKeyword:
		return s.repo.SearchKeyword(ctx, tenantID, userID, req)
	case SearchModeVector:
		if q == "" {
			return nil, ErrEmptySearch
		}
		if !s.vectorEnabled() {
			return nil, ErrVectorDisabled
		}
		return s.runVector(ctx, tenantID, userID, req)
	case SearchModeAuto:
		if q != "" && s.vectorEnabled() {
			return s.runVector(ctx, tenantID, userID, req)
		}
		return s.repo.SearchKeyword(ctx, tenantID, userID, req)
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidSearchMode, mode)
	}
}

func (s *Service) runVector(ctx context.Context, tenantID, userID uuid.UUID, req SearchRequest) ([]SearchResult, error) {
	vecs, err := s.embedder.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != s.embedder.Dim() {
		return nil, ErrEmbedDimMismatch
	}
	return s.repo.SearchVector(ctx, tenantID, userID, vecs[0], req)
}

func isValidType(t string) bool {
	switch t {
	case TypeProfile, TypePreference, TypeKnowledge, TypeLesson:
		return true
	}
	return false
}

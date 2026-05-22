package audit

import "context"

// Service is a thin pass-through over Repo that the HTTP handler depends on by
// interface. Keeping a Service layer (rather than handler -> Repo directly) lets
// us inject a mock in handler_test without spinning up Postgres, and gives us a
// natural seam if we later add caching, redaction, or fan-out without changing
// callers.
type Service struct {
	repo repoIface
}

// Lister is the slice of Repo behavior Service needs for /audit. Kept as a
// separate interface so callers that only need read access can pass a narrower
// fake. Production *Repo satisfies both Lister and repoIface.
type Lister interface {
	List(ctx context.Context, f ListFilter) ([]Entry, int, error)
}

// repoIface is the full Service ↔ Repo contract (List + Verify). Service
// accepts this so the verify handler can route through the same Service mock
// already used in handler tests.
type repoIface interface {
	List(ctx context.Context, f ListFilter) ([]Entry, int, error)
	Verify(ctx context.Context, fromID int64) (*VerifyResult, error)
}

func NewService(repo repoIface) *Service { return &Service{repo: repo} }

func (s *Service) List(ctx context.Context, f ListFilter) ([]Entry, int, error) {
	return s.repo.List(ctx, f)
}

func (s *Service) Verify(ctx context.Context, fromID int64) (*VerifyResult, error) {
	return s.repo.Verify(ctx, fromID)
}

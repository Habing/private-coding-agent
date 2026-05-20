package audit

import "context"

// Service is a thin pass-through over Repo that the HTTP handler depends on by
// interface. Keeping a Service layer (rather than handler -> Repo directly) lets
// us inject a mock in handler_test without spinning up Postgres, and gives us a
// natural seam if we later add caching, redaction, or fan-out without changing
// callers.
type Service struct {
	repo Lister
}

// Lister is the slice of Repo behavior Service needs. Satisfied by *Repo in
// production and by fakes in tests.
type Lister interface {
	List(ctx context.Context, f ListFilter) ([]Entry, int, error)
}

func NewService(repo Lister) *Service { return &Service{repo: repo} }

func (s *Service) List(ctx context.Context, f ListFilter) ([]Entry, int, error) {
	return s.repo.List(ctx, f)
}

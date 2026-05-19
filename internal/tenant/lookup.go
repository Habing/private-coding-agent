package tenant

import (
	"context"

	"github.com/google/uuid"
)

// Lookup is a thin adapter over *Repo that exposes only the slug-to-ID lookup
// used by the auth handler. Keeping the surface narrow lets the handler depend
// on an interface it can substitute in tests.
type Lookup struct{ r *Repo }

// NewLookup returns a Lookup backed by the given Repo.
func NewLookup(r *Repo) *Lookup { return &Lookup{r: r} }

// GetBySlug resolves a tenant slug to its UUID, propagating Repo errors
// (including ErrNotFound) unchanged.
func (l *Lookup) GetBySlug(ctx context.Context, slug string) (uuid.UUID, error) {
	t, err := l.r.GetBySlug(ctx, slug)
	if err != nil {
		return uuid.UUID{}, err
	}
	return t.ID, nil
}

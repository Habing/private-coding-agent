package user

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var ErrBadCredentials = errors.New("bad credentials")

// dummyHash is a bcrypt hash of a fixed throwaway password computed once at
// package init. It is used in the unknown-user branch of Authenticate so that
// path performs an equivalent bcrypt comparison and matches the cost of the
// wrong-password path. Without this, response-time differences would let an
// attacker enumerate valid emails.
var dummyHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)
	if err != nil {
		// Failing loud at startup is preferable to silently degrading the
		// timing-attack mitigation in production.
		panic("bcrypt init: " + err.Error())
	}
	dummyHash = h
}

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service { return &Service{repo: repo} }

// Register hashes the supplied password with bcrypt and inserts a new user
// with the member role into the tenant. The returned user contains the
// generated hash, never the plaintext password.
func (s *Service) Register(ctx context.Context, tenantID uuid.UUID, email, password, name string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, CreateInput{
		TenantID:     tenantID,
		Email:        email,
		PasswordHash: string(hash),
		Name:         name,
		Role:         RoleMember,
	})
}

// Authenticate looks up the user by tenant + email and compares the supplied
// password against the stored bcrypt hash. Unknown users and wrong passwords
// both return ErrBadCredentials so callers cannot distinguish the two. The
// unknown-user branch also runs a bcrypt comparison against a dummy hash so
// the two paths have indistinguishable timing.
func (s *Service) Authenticate(ctx context.Context, tenantID uuid.UUID, email, password string) (*User, error) {
	u, err := s.repo.GetByEmail(ctx, tenantID, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Timing-attack mitigation: run a bcrypt compare even when the
			// user doesn't exist so the response time matches the
			// wrong-password path. The result is intentionally discarded.
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			return nil, ErrBadCredentials
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrBadCredentials
	}
	return u, nil
}

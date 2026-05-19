package user

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var ErrBadCredentials = errors.New("bad credentials")

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
// both return ErrBadCredentials so callers cannot distinguish the two.
func (s *Service) Authenticate(ctx context.Context, tenantID uuid.UUID, email, password string) (*User, error) {
	u, err := s.repo.GetByEmail(ctx, tenantID, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrBadCredentials
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrBadCredentials
	}
	return u, nil
}

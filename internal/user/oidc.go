package user

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// FindOrCreateOIDC resolves a user by (tenant, iss, sub) or JIT-registers one.
// Email falls back to oidc:<sub>@oidc.local when the IdP omits email.
func (s *Service) FindOrCreateOIDC(ctx context.Context, tenantID uuid.UUID, iss, sub, email, name string) (*User, error) {
	if iss == "" || sub == "" {
		return nil, fmt.Errorf("oidc: iss and sub required")
	}
	u, err := s.repo.GetByOIDC(ctx, tenantID, iss, sub)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if email == "" {
		email = fmt.Sprintf("oidc:%s@oidc.local", sub)
	}
	hash, err := randomPasswordHash()
	if err != nil {
		return nil, err
	}
	return s.repo.CreateOIDC(ctx, CreateOIDCInput{
		TenantID:     tenantID,
		Email:        email,
		PasswordHash: hash,
		Name:         name,
		Role:         RoleMember,
		OIDCIss:      iss,
		OIDCSub:      sub,
	})
}

func randomPasswordHash() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Unusable random password — OIDC users never authenticate locally.
	h, err := bcrypt.GenerateFromPassword([]byte(base64.RawURLEncoding.EncodeToString(b)), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/quota"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

// sandboxRuntime is the subset of sandbox.Runtime used for session binding.
type sandboxRuntime interface {
	Create(ctx context.Context, opts sandbox.CreateOpts) (*sandbox.Sandbox, error)
	Destroy(ctx context.Context, tenantID, id uuid.UUID) error
}

// activeSandboxCounter matches sandbox.SessionRepo.CountActiveByTenant for
// quota gating at session create time (same rule as POST /sandbox/sessions).
type activeSandboxCounter interface {
	CountActiveByTenant(ctx context.Context, tenantID uuid.UUID) (int, error)
}

func (s *Service) checkSandboxQuota(ctx context.Context, tenantID uuid.UUID) error {
	if s.quota == nil || s.activeCnt == nil {
		return nil
	}
	cap := s.quota.SandboxCap()
	if cap <= 0 {
		return nil
	}
	n, err := s.activeCnt.CountActiveByTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("quota check: %w", err)
	}
	if n >= cap {
		return quota.ErrQuotaExceeded
	}
	return nil
}

func (s *Service) provisionSandbox(ctx context.Context, tenantID, userID uuid.UUID) (uuid.UUID, error) {
	if s.sandbox == nil {
		return uuid.Nil, ErrSandboxNotConfigured
	}
	if err := s.checkSandboxQuota(ctx, tenantID); err != nil {
		return uuid.Nil, err
	}
	sb, err := s.sandbox.Create(ctx, sandbox.CreateOpts{
		TenantID:    tenantID,
		OwnerUserID: userID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrSandboxCreateFailed, err)
	}
	return sb.ID, nil
}

func (s *Service) releaseSandbox(ctx context.Context, tenantID uuid.UUID, sandboxID *uuid.UUID) {
	if s.sandbox == nil || sandboxID == nil {
		return
	}
	if err := s.sandbox.Destroy(ctx, tenantID, *sandboxID); err != nil &&
		!errors.Is(err, sandbox.ErrSandboxNotFound) {
		slog.Warn("session.sandbox_destroy",
			"sandbox_id", sandboxID.String(), "err", err.Error())
	}
}

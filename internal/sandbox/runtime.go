package sandbox

import (
	"context"

	"github.com/google/uuid"
)

// Runtime is the sandbox abstraction. All methods must be safe for concurrent
// use across multiple goroutines.
//
// Implementations:
//   - DockerDriver (slice 2)
//   - K8sDriver (future)
type Runtime interface {
	// Create allocates and starts a new sandbox. Returns when status=Running.
	// Honors ctx cancellation up to start; once started, the container lives
	// until Destroy.
	Create(ctx context.Context, opts CreateOpts) (*Sandbox, error)

	// Get returns the sandbox by id scoped to tenant. Returns ErrSandboxNotFound
	// when the id doesn't exist OR belongs to a different tenant (no
	// distinction is exposed, to prevent enumeration).
	Get(ctx context.Context, tenantID, id uuid.UUID) (*Sandbox, error)

	// Destroy stops and removes the sandbox. Idempotent: destroying an
	// already-destroyed sandbox returns nil.
	Destroy(ctx context.Context, tenantID, id uuid.UUID) error

	// Exec runs a command synchronously inside the sandbox. ctx cancellation
	// or ExecOpts.TimeoutSec kill the process (TimedOut=true). Stdout/Stderr
	// each capped at MaxStreamBytes (excess truncated, Truncated=true).
	Exec(ctx context.Context, tenantID, id uuid.UUID, opts ExecOpts) (*ExecResult, error)

	// ReadFile reads a file under /workspace. Path is validated by
	// ResolveWorkspacePath. Files larger than MaxFileSize return ErrTooLarge.
	ReadFile(ctx context.Context, tenantID, id uuid.UUID, path string) ([]byte, error)

	// WriteFile writes a file under /workspace, creating intermediate
	// directories as needed. Size capped at MaxFileSize.
	WriteFile(ctx context.Context, tenantID, id uuid.UUID, path string, data []byte) error

	// Snapshot exports the workspace state. Slice 2 returns ErrNotImplemented;
	// implementation lands when MinIO support is added.
	Snapshot(ctx context.Context, tenantID, id uuid.UUID) (string, error)
}

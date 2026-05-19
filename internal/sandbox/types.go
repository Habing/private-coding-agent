// Package sandbox provides a Runtime abstraction for ephemeral, isolated
// execution environments where the agent reads files, writes files, and
// runs commands. DockerDriver is the Slice 2 implementation; future
// K8sDriver satisfies the same interface.
package sandbox

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status 是沙箱生命周期状态。
type Status string

const (
	StatusPending    Status = "pending"
	StatusRunning    Status = "running"
	StatusDestroying Status = "destroying"
	StatusDestroyed  Status = "destroyed"
	StatusFailed     Status = "failed"
)

// NetworkMode 决定沙箱网络隔离强度。
type NetworkMode string

const (
	NetworkInternal NetworkMode = "internal" // 共享 internal 网络,可与其他沙箱通信但无外网
	NetworkBridge   NetworkMode = "bridge"   // 默认 bridge,能上外网(仅 dev 用)
	NetworkNone     NetworkMode = "none"     // 无网络
)

// 默认值与上限(包级常量,便于 validate.go 引用)。
const (
	DefaultImage          = "pca/sandbox:base"
	DefaultNetwork        = NetworkInternal
	DefaultCPUs           = 1.0
	DefaultMemoryMB       = int64(512)
	DefaultPIDsLimit      = int64(256)
	DefaultExecTimeoutSec = 60

	MaxCPUs           = 4.0
	MaxMemoryMB       = int64(4096)
	MaxPIDsLimit      = int64(1024)
	MaxExecTimeoutSec = 600
	MaxFileSize       = 1 << 20  // 1 MB
	MaxStreamBytes    = 128 * 1024 // 每个 stream (stdout/stderr) 上限
)

// Sandbox 是沙箱的领域对象。
type Sandbox struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ProjectID   *uuid.UUID
	OwnerUserID uuid.UUID
	Status      Status
	Image       string
	Network     NetworkMode
	Resources   ResourceLimits
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DestroyedAt *time.Time
}

// ResourceLimits 是资源约束。零值会被 validate 替换为默认。
type ResourceLimits struct {
	CPUs      float64 // 例如 1.0
	MemoryMB  int64   // 例如 512
	PIDsLimit int64   // 例如 256
}

// CreateOpts 是创建沙箱的请求参数。
type CreateOpts struct {
	TenantID    uuid.UUID
	OwnerUserID uuid.UUID
	ProjectID   *uuid.UUID
	Image       string
	Resources   ResourceLimits
	Network     NetworkMode
	Env         map[string]string
	Labels      map[string]string
}

// ExecOpts 是单次命令执行参数。
type ExecOpts struct {
	Cmd        []string
	WorkingDir string
	Env        map[string]string
	Stdin      []byte
	TimeoutSec int
}

// ExecResult 是命令执行结果。
type ExecResult struct {
	ExitCode   int
	Stdout     []byte
	Stderr     []byte
	Truncated  bool
	DurationMS int64
	TimedOut   bool
}

// 错误哨兵
var (
	ErrSandboxNotFound      = errors.New("sandbox not found")
	ErrSandboxNotReady      = errors.New("sandbox not running")
	ErrPathOutsideWorkspace = errors.New("path outside /workspace")
	ErrNotImplemented       = errors.New("not implemented")
	ErrTooLarge             = errors.New("payload too large")
)

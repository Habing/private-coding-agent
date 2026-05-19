package sandbox

import (
	"fmt"

	"github.com/google/uuid"
)

// NormalizeCreateOpts validates and fills defaults for CreateOpts.
// Returns a fully populated copy; original input is unchanged.
func NormalizeCreateOpts(o CreateOpts) (CreateOpts, error) {
	if o.TenantID == uuid.Nil {
		return o, fmt.Errorf("validation: tenant_id required")
	}
	if o.OwnerUserID == uuid.Nil {
		return o, fmt.Errorf("validation: owner_user_id required")
	}
	if o.Image == "" {
		o.Image = DefaultImage
	}
	if o.Network == "" {
		o.Network = DefaultNetwork
	}
	switch o.Network {
	case NetworkInternal, NetworkBridge, NetworkNone:
	default:
		return o, fmt.Errorf("validation: unknown network mode %q", string(o.Network))
	}
	if o.Resources.CPUs == 0 {
		o.Resources.CPUs = DefaultCPUs
	}
	if o.Resources.MemoryMB == 0 {
		o.Resources.MemoryMB = DefaultMemoryMB
	}
	if o.Resources.PIDsLimit == 0 {
		o.Resources.PIDsLimit = DefaultPIDsLimit
	}
	if o.Resources.CPUs < 0 || o.Resources.CPUs > MaxCPUs {
		return o, fmt.Errorf("validation: cpus %g out of [0, %g]", o.Resources.CPUs, MaxCPUs)
	}
	if o.Resources.MemoryMB < 0 || o.Resources.MemoryMB > MaxMemoryMB {
		return o, fmt.Errorf("validation: memory_mb %d out of [0, %d]", o.Resources.MemoryMB, MaxMemoryMB)
	}
	if o.Resources.PIDsLimit < 0 || o.Resources.PIDsLimit > MaxPIDsLimit {
		return o, fmt.Errorf("validation: pids_limit %d out of [0, %d]", o.Resources.PIDsLimit, MaxPIDsLimit)
	}
	return o, nil
}

// NormalizeExecOpts validates and fills defaults for ExecOpts.
func NormalizeExecOpts(o ExecOpts) (ExecOpts, error) {
	if len(o.Cmd) == 0 {
		return o, fmt.Errorf("validation: cmd required")
	}
	for i, a := range o.Cmd {
		if a == "" {
			return o, fmt.Errorf("validation: cmd[%d] is empty", i)
		}
	}
	if o.WorkingDir == "" {
		o.WorkingDir = workspaceRoot
	}
	if o.TimeoutSec == 0 {
		o.TimeoutSec = DefaultExecTimeoutSec
	}
	if o.TimeoutSec < 0 || o.TimeoutSec > MaxExecTimeoutSec {
		return o, fmt.Errorf("validation: timeout_sec %d out of [0, %d]", o.TimeoutSec, MaxExecTimeoutSec)
	}
	return o, nil
}

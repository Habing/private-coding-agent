package toolbus

import "errors"

// Sentinel errors. Callers use errors.Is to map to HTTP status codes.
var (
	ErrToolNotFound      = errors.New("toolbus: tool not found")
	ErrInvalidArguments  = errors.New("toolbus: invalid arguments")
	ErrSandboxIDRequired = errors.New("toolbus: sandbox_id required")
	ErrToolFailed        = errors.New("toolbus: tool execution failed")
)

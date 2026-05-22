package reflection

import "errors"

// ErrProposalNotFound is returned when a Get/Approve/Reject call cannot find
// a matching row scoped to the tenant.
var ErrProposalNotFound = errors.New("proposal not found")

// ErrInvalidStatus is returned by ListByTenant when filter.Status is set to
// an unknown value.
var ErrInvalidStatus = errors.New("invalid status")

// ErrNotPending is returned by Approve/Reject when the proposal has already
// been decided (idempotency guard so two admins cannot double-approve).
var ErrNotPending = errors.New("proposal not pending")

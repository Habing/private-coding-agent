package audit

import (
	"context"
	"time"
)

// Detached writes e to sink with a fresh 5s timeout derived from
// context.Background() — independent of any per-request ctx. Returns
// immediately (synchronously) so callers see audit errors via log only if
// they supply onErr; passing onErr=nil silently drops errors.
//
// This is the canonical helper for domain-level instrumentation points
// (auth.login.success, sandbox.create, ...) so audit writes survive client
// disconnects and don't propagate request cancellation. Mirrors the
// Middleware's detached-ctx behavior.
//
// sink == nil is a no-op so handlers can be constructed without auditing
// (e.g. unit tests) and the instrumentation calls remain in the code path.
func Detached(sink Sink, e Entry, onErr func(error)) {
	if sink == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sink.Append(ctx, e); err != nil && onErr != nil {
		onErr(err)
	}
}

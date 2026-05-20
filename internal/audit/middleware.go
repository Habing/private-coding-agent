package audit

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Sink accepts audit entries produced by Middleware. Implementations must be
// safe for concurrent use and must respect ctx cancellation for IO operations.
type Sink interface {
	Append(ctx context.Context, e Entry) error
}

// ClaimsExtractor pulls (tenantID, userID) from a request context, returning
// nil pointers when the request was unauthenticated. Injected by main.go so
// the audit package does not depend on the auth package (would create an
// import cycle with the domain instrumentation calls in auth.Handler).
type ClaimsExtractor func(c *gin.Context) (tenantID, userID *uuid.UUID)

// auditWriteTimeout caps how long the audit append call may take. Independent
// of any per-request deadline so that audit records survive client disconnects.
const auditWriteTimeout = 5 * time.Second

// Middleware writes an audit entry per request. Failure to write is logged via
// the optional onErr callback but does not block the request.
//
// The audit append uses a context derived from context.Background() with a
// 5s timeout (auditWriteTimeout) rather than the request context, so that
// records are written even if the client disconnects mid-response.
//
// extract may be nil (entries are recorded without tenant/user IDs), but in
// production callers should pass a function that decodes auth.Claims from ctx.
func Middleware(s Sink, extract ClaimsExtractor, onErr func(error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		e := Entry{
			OccurredAt: start,
			Method:     c.Request.Method,
			Path:       c.FullPath(),
			Status:     c.Writer.Status(),
			DurationMS: int(time.Since(start).Milliseconds()),
			Action:     "http_request",
		}
		if extract != nil {
			tid, uid := extract(c)
			e.TenantID, e.UserID = tid, uid
		}

		appendCtx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
		defer cancel()
		if err := s.Append(appendCtx, e); err != nil && onErr != nil {
			onErr(err)
		}
	}
}

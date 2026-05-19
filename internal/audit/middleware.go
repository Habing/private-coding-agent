package audit

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// Sink accepts audit entries produced by Middleware. Implementations must be
// safe for concurrent use and must respect ctx cancellation for IO operations.
type Sink interface {
	Append(ctx context.Context, e Entry) error
}

// auditWriteTimeout caps how long the audit append call may take. Independent
// of any per-request deadline so that audit records survive client disconnects.
const auditWriteTimeout = 5 * time.Second

// Middleware writes an audit entry per request. Failure to write is logged via
// the optional onErr callback but does not block the request.
//
// The audit append uses a context derived from context.Background() with a
// 5s timeout (auditWriteTimeout) rather than the request context, so that
// records are written even if the client disconnects mid-response.
func Middleware(s Sink, onErr func(error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		cl := auth.FromCtx(c.Request.Context())
		e := Entry{
			OccurredAt: start,
			Method:     c.Request.Method,
			Path:       c.FullPath(),
			Status:     c.Writer.Status(),
			DurationMS: int(time.Since(start).Milliseconds()),
			Action:     "http_request",
		}
		if cl != nil {
			t, u := cl.TenantID, cl.UserID
			e.TenantID, e.UserID = &t, &u
		}

		appendCtx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
		defer cancel()
		if err := s.Append(appendCtx, e); err != nil && onErr != nil {
			onErr(err)
		}
	}
}

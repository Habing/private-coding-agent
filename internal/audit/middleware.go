package audit

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// Sink accepts audit entries produced by Middleware.
type Sink interface {
	Append(ctx context.Context, e Entry) error
}

// Middleware writes an audit entry per request. Failure to write is logged via
// the optional onErr callback but does not block the request.
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
		if err := s.Append(c.Request.Context(), e); err != nil && onErr != nil {
			onErr(err)
		}
	}
}

package httpx

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/logx"
)

// HeaderRequestID is the wire-name for the per-request correlation id.
// Clients may set it inbound; we always reflect it on the response so the
// caller can quote it back when reporting an error.
const HeaderRequestID = "X-Request-ID"

// RequestIDMiddleware reads X-Request-ID from the inbound request, falling
// back to a freshly generated UUIDv4, then stores the id on the request
// context (via logx.WithRequestID) and echoes it on the response header.
//
// Mount before otelgin so the request id is available on the root span
// context; otelgin will pick it up via FromCtx-style helpers if it ever
// instruments baggage in the future.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		c.Header(HeaderRequestID, id)
		ctx := logx.WithRequestID(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

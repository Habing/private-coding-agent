package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type ctxKey struct{}

// FromCtx returns the Claims stored in ctx by Middleware, or nil if absent.
func FromCtx(ctx context.Context) *Claims {
	c, _ := ctx.Value(ctxKey{}).(*Claims)
	return c
}

// WithClaims returns a child ctx carrying cl. Exported so packages without
// a real HTTP request (background goroutines, tests, internal callers that
// stand in for an authenticated user) can populate the same slot Middleware
// uses.
func WithClaims(ctx context.Context, cl *Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, cl)
}

// MiddlewareOption configures optional middleware behavior. Use functional
// options so the common form (`Middleware(jwt)`) stays unchanged while
// production can opt into a revoker.
type MiddlewareOption func(*middlewareOpts)

type middlewareOpts struct {
	revoker Revoker
}

// WithRevoker installs r so each request consults it after token parse.
// Revoked or store-error tokens are rejected with HTTP 401. nil r is
// equivalent to not setting the option (no revocation check).
func WithRevoker(r Revoker) MiddlewareOption {
	return func(o *middlewareOpts) { o.revoker = r }
}

// Middleware returns a Gin handler that requires an "Authorization: Bearer <token>"
// header, parses the JWT via j, and injects the resulting Claims into the request
// context for downstream handlers to retrieve with FromCtx. Missing or invalid
// tokens abort the request with HTTP 401. If WithRevoker is provided, the
// jti is checked against the revocation store on every request.
func Middleware(j *JWT, opts ...MiddlewareOption) gin.HandlerFunc {
	mo := middlewareOpts{}
	for _, o := range opts {
		o(&mo)
	}
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing_token"})
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")
		cl, err := j.Parse(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			return
		}
		if mo.revoker != nil && cl.JTI != "" {
			revoked, rerr := mo.revoker.IsRevoked(c.Request.Context(), cl.JTI)
			if rerr != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "revocation_check_failed"})
				return
			}
			if revoked {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token_revoked"})
				return
			}
		}
		ctx := context.WithValue(c.Request.Context(), ctxKey{}, cl)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

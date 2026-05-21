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

// Middleware returns a Gin handler that requires an "Authorization: Bearer <token>"
// header, parses the JWT via j, and injects the resulting Claims into the request
// context for downstream handlers to retrieve with FromCtx. Missing or invalid
// tokens abort the request with HTTP 401.
func Middleware(j *JWT) gin.HandlerFunc {
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
		ctx := context.WithValue(c.Request.Context(), ctxKey{}, cl)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

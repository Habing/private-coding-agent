package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RoleAdmin is the role string carried in JWT Claims for tenant administrators.
// Must stay in sync with user.RoleAdmin.
const RoleAdmin = "admin"

// RequireAdmin returns a Gin handler that aborts with HTTP 403 unless the
// request's auth.Claims (populated by Middleware) carry Role == "admin".
// It MUST be mounted AFTER Middleware on the same route group.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := FromCtx(c.Request.Context())
		if cl == nil || cl.Role != RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}

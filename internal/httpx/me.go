package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// RegisterMe mounts GET /me on r. The route expects auth.Middleware to have
// populated the request context with Claims; it responds 200 with the caller's
// user_id/tenant_id/role, or 401 if no claims are present.
func RegisterMe(r *gin.RouterGroup) {
	r.GET("/me", func(c *gin.Context) {
		cl := auth.FromCtx(c.Request.Context())
		if cl == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"user_id":   cl.UserID,
			"tenant_id": cl.TenantID,
			"role":      cl.Role,
		})
	})
}

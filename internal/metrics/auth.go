package metrics

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// AuthConfig configures the metrics endpoint authentication.
type AuthConfig struct {
	// JWT is the standard JWT parser used as the fallback credential.
	JWT *auth.JWT
	// StaticToken, when non-empty, allows requests to bypass JWT auth by
	// presenting "Authorization: Bearer <StaticToken>". Intended for
	// Prometheus scrape configs (JWT TTL would otherwise expire mid-scrape).
	StaticToken string
}

// Auth returns a gin middleware that allows the request when EITHER the
// static metrics token OR a valid JWT with role=admin is presented. Returns
// 401 on missing/invalid credentials, 403 on a valid non-admin JWT.
//
// Static-token match uses constant-time compare to avoid timing leaks.
func Auth(cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing_token"})
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")

		if cfg.StaticToken != "" &&
			subtle.ConstantTimeCompare([]byte(tok), []byte(cfg.StaticToken)) == 1 {
			c.Next()
			return
		}

		if cfg.JWT == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			return
		}
		cl, err := cfg.JWT.Parse(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			return
		}
		if cl.Role != auth.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin_required"})
			return
		}
		c.Next()
	}
}

package httpx

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// RateLimitConfig caps HTTP requests per (tenant, user) per minute via a
// Redis fixed-window counter. A value of 0 disables the middleware (it
// returns a no-op handler), so production deploys keep working when the
// section is absent from config.
type RateLimitConfig struct {
	PerMinute int
}

// RateLimitMiddleware enforces RateLimitConfig.PerMinute using a fixed-minute
// Redis window keyed by tenant+user from auth.Claims. Requests without
// claims (the auth endpoints themselves, /healthz, /metrics) are passed
// through unconditionally — they have their own gating. On exceed we return
// HTTP 429 with a Retry-After header equal to the seconds remaining in the
// current minute window.
func RateLimitMiddleware(rdb *redis.Client, cfg RateLimitConfig) gin.HandlerFunc {
	if cfg.PerMinute <= 0 || rdb == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		cl := auth.FromCtx(c.Request.Context())
		if cl == nil {
			c.Next()
			return
		}
		now := time.Now().UTC()
		key := rateLimitKey(cl.TenantID, cl.UserID, now)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Millisecond)
		defer cancel()

		pipe := rdb.TxPipeline()
		incrCmd := pipe.Incr(ctx, key)
		pipe.ExpireNX(ctx, key, 2*time.Minute)
		if _, err := pipe.Exec(ctx); err != nil {
			// Fail-open on Redis errors: better to admit than 5xx the
			// entire surface when the throttle store is unhealthy.
			c.Next()
			return
		}
		if int(incrCmd.Val()) > cfg.PerMinute {
			retry := 60 - now.Second()
			if retry < 1 {
				retry = 1
			}
			c.Header("Retry-After", strconv.Itoa(retry))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate_limited",
				"limit": cfg.PerMinute,
				"used":  incrCmd.Val(),
			})
			return
		}
		c.Next()
	}
}

func rateLimitKey(tenantID, userID uuid.UUID, now time.Time) string {
	return fmt.Sprintf("rl:%s:%s:%s", tenantID, userID, now.Format("2006-01-02T15:04"))
}

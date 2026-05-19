package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerHealth(r *gin.Engine, d Deps) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/readyz", func(c *gin.Context) {
		if d.Ready != nil && d.Ready() {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
	})
}

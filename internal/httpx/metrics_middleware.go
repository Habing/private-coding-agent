package httpx

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/yourorg/private-coding-agent/internal/metrics"
)

// MetricsMiddleware records pca_http_requests_total and
// pca_http_request_duration_seconds for every request. Skips paths that would
// generate scraper noise (the metrics endpoint, liveness, readiness).
//
// Route label uses c.FullPath() (the gin template, e.g. "/v1/sandboxes/:id") to
// keep cardinality bounded. Status code is the numeric value as a string.
func MetricsMiddleware() gin.HandlerFunc {
	skip := map[string]struct{}{
		"/metrics": {}, "/healthz": {}, "/readyz": {},
	}
	return func(c *gin.Context) {
		if _, ok := skip[c.Request.URL.Path]; ok {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		attrs := metric.WithAttributes(
			attribute.String("method", c.Request.Method),
			attribute.String("route", route),
			attribute.String("status_code", strconv.Itoa(c.Writer.Status())),
		)
		if metrics.HTTPRequestsTotal != nil {
			metrics.HTTPRequestsTotal.Add(c.Request.Context(), 1, attrs)
		}
		if metrics.HTTPRequestDuration != nil {
			metrics.HTTPRequestDuration.Record(c.Request.Context(),
				time.Since(start).Seconds(),
				metric.WithAttributes(
					attribute.String("method", c.Request.Method),
					attribute.String("route", route),
				),
			)
		}
	}
}

// Package httpx assembles the Gin engine, routes, and middlewares.
package httpx

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Deps holds the dependencies needed to build the HTTP engine.
type Deps struct {
	ServiceName string
	Ready       func() bool
	Register    func(r *gin.Engine)
}

// NewEngine constructs a Gin engine wired with recovery and health routes.
// If Deps.ServiceName is non-empty, the otelgin middleware is installed so each
// request becomes a span on the global tracer provider. If Deps.Register is
// non-nil, it is invoked after health routes so callers can mount additional
// module routes on the engine.
func NewEngine(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	if d.ServiceName != "" {
		r.Use(otelgin.Middleware(d.ServiceName))
	}
	registerHealth(r, d)
	if d.Register != nil {
		d.Register(r)
	}
	return r
}

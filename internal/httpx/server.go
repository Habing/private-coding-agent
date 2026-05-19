// Package httpx assembles the Gin engine, routes, and middlewares.
package httpx

import "github.com/gin-gonic/gin"

// Deps holds the dependencies needed to build the HTTP engine.
type Deps struct {
	Ready func() bool
}

// NewEngine constructs a Gin engine wired with recovery and health routes.
func NewEngine(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	registerHealth(r, d)
	return r
}

package httpx

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegisterSPAFallback wires fsys as the source of static web assets behind a
// single-page-app friendly NoRoute handler:
//
//   - GET requests that match a real file in fsys are served with the file's
//     contents (assets, favicon, etc).
//   - GET requests that do NOT match any registered gin route or static file
//     are served the contents of index.html with status 200, letting client-side
//     routing take over (e.g. /login, /sessions/:id).
//   - Non-GET requests that fall through to NoRoute return 404 JSON; the SPA
//     shell is only served for navigation requests.
//
// It returns an error if index.html cannot be read from fsys at registration
// time, so missing front-end build artifacts fail loudly at boot instead of
// silently 404'ing every page request.
//
// Caller contract: invoke this AFTER all API routes have been registered on r,
// otherwise the gin route tree will not have them and they will be shadowed by
// the catch-all.
func RegisterSPAFallback(r *gin.Engine, fsys fs.FS) error {
	idx, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return fmt.Errorf("spa: read index.html: %w", err)
	}
	fileServer := http.FileServer(http.FS(fsys))

	r.NoRoute(func(c *gin.Context) {
		// HEAD is semantically GET-without-body per RFC 9110; treat it as GET so
		// curl -I / health checks against /, /login, /assets/... return the same
		// Content-Type as the equivalent GET would.
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path != "" {
			if info, err := fs.Stat(fsys, path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", idx)
	})
	return nil
}

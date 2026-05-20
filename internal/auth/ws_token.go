package auth

import "github.com/gin-gonic/gin"

// WSTokenFromQuery is a narrow shim that lets browser WebSocket clients pass
// the JWT in the ?token= query parameter because the browser WebSocket
// constructor cannot set custom headers.
//
// If the request arrives without an Authorization header but with a non-empty
// ?token= query value, this middleware synthesizes "Authorization: Bearer <t>"
// so the existing auth.Middleware can validate it unchanged. If the header is
// already present, the query is ignored — header wins.
//
// Mount this BEFORE auth.Middleware, and ONLY on the WebSocket upgrade route.
// Do not apply it to REST routes: token leakage into reverse-proxy access logs
// is acceptable for the WS handshake (single rare path) but not for the rest
// of the API surface.
func WSTokenFromQuery() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			if tok := c.Query("token"); tok != "" {
				c.Request.Header.Set("Authorization", "Bearer "+tok)
			}
		}
		c.Next()
	}
}

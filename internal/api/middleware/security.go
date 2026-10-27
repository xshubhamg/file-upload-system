package middleware

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeaders applies conservative defaults suitable for a JSON API.
// The documentation route overrides CSP because Swagger UI needs a CDN.
func SecurityHeaders(hsts bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Header("Cross-Origin-Resource-Policy", "same-site")
		if hsts {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.Header("Cache-Control", "no-store")
		}
		c.Next()
	}
}

package middleware

import (
	"github.com/gin-gonic/gin"
)

// Deprecated marks legacy aliases so clients and logs can see the migration
// signal without breaking existing integrations.
func Deprecated(successor string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Deprecation", "true")
		c.Header("Warning", `299 - "Deprecated API; migrate to `+successor+`"`)
		if successor != "" {
			c.Header("Link", "<"+successor+`>; rel="successor-version"`)
		}
		c.Next()
	}
}

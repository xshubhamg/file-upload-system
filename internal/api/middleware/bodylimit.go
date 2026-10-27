package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/api/response"
	"github.com/xshubhamg/file-upload/internal/apperr"
)

// BodyLimit rejects oversized requests before handlers allocate memory and
// makes the decoder return http.MaxBytesError for chunked bodies.
func BodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > max {
			response.Error(c, apperr.PayloadTooLarge("request body is too large"))
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		}
		c.Next()
	}
}

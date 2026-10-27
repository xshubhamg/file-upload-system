package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/api/response"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = newRequestID()
		}
		c.Set(response.RequestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "req-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(buf)
}

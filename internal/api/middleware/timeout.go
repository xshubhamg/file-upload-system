package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/api/response"
	"github.com/xshubhamg/file-upload/internal/apperr"
)

func Timeout(duration time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), duration)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()

		if ctx.Err() == context.DeadlineExceeded && !c.Writer.Written() {
			response.Error(c, apperr.From(context.DeadlineExceeded))
		}
	}
}

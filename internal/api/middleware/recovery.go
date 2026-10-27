package middleware

import (
	"log/slog"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/api/response"
	"github.com/xshubhamg/file-upload/internal/apperr"
)

func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Error("panic recovered",
					slog.Any("panic", recovered),
					slog.String("request_id", response.RequestID(c)),
					slog.String("method", c.Request.Method),
					slog.String("path", c.Request.URL.Path),
					slog.String("stack", string(debug.Stack())),
				)
				if !c.Writer.Written() {
					response.Error(c, apperr.Internal(nil))
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

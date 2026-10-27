package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/api/response"
)

func Logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		elapsed := time.Since(start)

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		attrs := []any{
			slog.String("request_id", response.RequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int64("duration_ms", elapsed.Milliseconds()),
			slog.Int("bytes", c.Writer.Size()),
			slog.String("client_ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
		}
		if userID, ok := c.Get("user_id"); ok {
			attrs = append(attrs, slog.Any("user_id", userID))
		}

		switch {
		case status >= 500:
			if len(c.Errors) > 0 {
				attrs = append(attrs, slog.String("error", c.Errors.String()))
			}
			log.Error("http request", attrs...)
		case status >= 400:
			log.Warn("http request", attrs...)
		default:
			if route == "/healthz" || route == "/readyz" {
				log.Debug("http request", attrs...)
				return
			}
			log.Info("http request", attrs...)
		}
	}
}

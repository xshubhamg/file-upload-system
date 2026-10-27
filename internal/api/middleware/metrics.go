package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/observability"
)

func Metrics(metrics *observability.Metrics) gin.HandlerFunc {
	if metrics == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		start := time.Now()
		metrics.InFlight.Inc()
		c.Next()
		metrics.InFlight.Dec()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		metrics.RequestsTotal.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		metrics.RequestDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}

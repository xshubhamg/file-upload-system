package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS(allowedOrigins []string) gin.HandlerFunc {
	config := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-ID"},
		ExposeHeaders:    []string{"X-Request-ID", "Deprecation", "Warning", "Location"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	if len(allowedOrigins) > 0 {
		config.AllowOrigins = allowedOrigins
	} else {
		config.AllowOriginFunc = func(string) bool { return false }
	}
	return cors.New(config)
}

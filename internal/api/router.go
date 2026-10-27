// Package api wires HTTP middleware and handlers. Business logic lives in
// internal/service; this package only translates HTTP to use-case calls.
package api

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/xshubhamg/file-upload/internal/api/handler"
	"github.com/xshubhamg/file-upload/internal/api/middleware"
	"github.com/xshubhamg/file-upload/internal/api/response"
	"github.com/xshubhamg/file-upload/internal/apperr"
	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/observability"
	"github.com/xshubhamg/file-upload/internal/service"
	"github.com/xshubhamg/file-upload/internal/storage"
)

type Deps struct {
	Config  config.Config
	Logger  *slog.Logger
	Metrics *observability.Metrics
	DB      *sql.DB
	Store   storage.Store
	Auth    *service.AuthService
	Files   *service.FileService
}

func NewRouter(deps Deps) *gin.Engine {
	if deps.Config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.TestMode)
	}

	router := gin.New()
	if len(deps.Config.TrustedProxies) == 0 {
		_ = router.SetTrustedProxies(nil)
	} else {
		if err := router.SetTrustedProxies(deps.Config.TrustedProxies); err != nil {
			deps.Logger.Warn("invalid TRUSTED_PROXIES; proxy headers will be ignored", slog.String("error", err.Error()))
			_ = router.SetTrustedProxies(nil)
		}
	}
	router.HandleMethodNotAllowed = true

	middlewares := []gin.HandlerFunc{
		middleware.RequestID(),
		middleware.Logger(deps.Logger),
		middleware.Recovery(deps.Logger),
		middleware.SecurityHeaders(deps.Config.IsProduction() || deps.Config.CookieSecure),
		middleware.CORS(deps.Config.CORSAllowedOrigins),
	}
	if deps.Config.MetricsEnabled {
		middlewares = append(middlewares, middleware.Metrics(deps.Metrics))
	}
	middlewares = append(middlewares, middleware.Timeout(deps.Config.RequestTimeout))
	router.Use(middlewares...)

	authHandler := handler.NewAuthHandler(deps.Auth, deps.Config)
	fileHandler := handler.NewFileHandler(deps.Files, deps.Config)
	healthHandler := handler.NewHealthHandler(deps.DB, deps.Store, deps.Logger)

	authLimiter := middleware.NewIPLimiter(deps.Config.AuthRateLimitRPS, deps.Config.AuthRateLimitBurst)
	globalLimiter := middleware.NewIPLimiter(deps.Config.RateLimitRPS, deps.Config.RateLimitBurst)
	authMW := middleware.Auth(deps.Auth)
	jsonLimit := middleware.BodyLimit(deps.Config.MaxJSONBodyBytes)

	router.GET("/healthz", healthHandler.Liveness)
	router.GET("/readyz", healthHandler.Readiness)

	v1 := router.Group("/api/v1")
	v1.GET("/openapi.yaml", handler.OpenAPI)
	v1.GET("/docs", handler.SwaggerUI)

	auth := v1.Group("/auth", jsonLimit, authLimiter.Middleware())
	auth.POST("/register", authHandler.Register)
	auth.POST("/login", authHandler.Login)
	auth.POST("/refresh", authHandler.Refresh)
	auth.POST("/logout", authHandler.Logout)
	auth.POST("/logout-all", authMW, authHandler.LogoutAll)
	auth.GET("/me", authMW, authHandler.Me)

	files := v1.Group("/files", jsonLimit, globalLimiter.Middleware())
	files.POST("/uploads", authMW, fileHandler.Initiate)
	files.POST("/:id/complete", authMW, fileHandler.Complete)
	files.GET("", authMW, fileHandler.List)
	files.GET("/:id", authMW, fileHandler.Get)
	files.GET("/:id/download", authMW, fileHandler.Download)
	files.DELETE("/:id", authMW, fileHandler.Delete)

	// Deprecated aliases. They keep the original paths and semantics so old
	// clients can migrate on their own schedule.
	legacyAuth := router.Group("/api/auth", middleware.Deprecated("/api/v1/auth"), jsonLimit, authLimiter.Middleware())
	legacyAuth.POST("/register", authHandler.Register)
	legacyAuth.POST("/login", authHandler.Login)
	legacyAuth.POST("/refresh", authHandler.Refresh)
	legacyAuth.POST("/logout", authHandler.Logout)
	legacyAuth.POST("/logout-all", authMW, authHandler.LogoutAll)
	legacyAuth.GET("/me", authMW, authHandler.Me)

	legacyFiles := router.Group("/api/file", middleware.Deprecated("/api/v1/files"), globalLimiter.Middleware())
	legacyUploadLimit := middleware.BodyLimit(deps.Config.MaxUploadBytes + 1<<20)
	legacyFiles.POST("/upload", authMW, legacyUploadLimit, fileHandler.LegacyUpload)
	legacyFiles.GET("", authMW, fileHandler.LegacyList)
	legacyFiles.GET("/", authMW, fileHandler.LegacyList)
	legacyFiles.GET("/:filename", authMW, fileHandler.LegacyGet)
	legacyFiles.DELETE("/:filename", authMW, fileHandler.LegacyDelete)

	router.NoRoute(func(c *gin.Context) {
		response.Error(c, apperr.New(http.StatusNotFound, "NOT_FOUND", "route not found"))
	})
	router.NoMethod(func(c *gin.Context) {
		response.Error(c, apperr.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed"))
	})

	return router
}

// NewAdminRouter exposes operational endpoints on a separate listener. It has
// no auth by design: bind it to a private interface or restrict it at the
// ingress/network policy layer.
func NewAdminRouter(deps Deps) *gin.Engine {
	if deps.Config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(
		middleware.RequestID(),
		middleware.Logger(deps.Logger),
		middleware.Recovery(deps.Logger),
	)
	healthHandler := handler.NewHealthHandler(deps.DB, deps.Store, deps.Logger)
	router.GET("/healthz", healthHandler.Liveness)
	router.GET("/readyz", healthHandler.Readiness)
	if deps.Config.MetricsEnabled && deps.Metrics != nil {
		router.GET("/metrics", gin.WrapH(promhttp.HandlerFor(deps.Metrics.Registry, promhttp.HandlerOpts{})))
	}
	return router
}

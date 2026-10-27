package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/xshubhamg/file-upload/internal/api"
	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/database"
	"github.com/xshubhamg/file-upload/internal/observability"
	"github.com/xshubhamg/file-upload/internal/repository"
	"github.com/xshubhamg/file-upload/internal/service"
	"github.com/xshubhamg/file-upload/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal startup error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	// Local development convenience; real deployments inject env vars.
	if _, err := os.Stat(".env"); err == nil {
		_ = godotenv.Load()
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	logger := observability.NewLogger(cfg.LogLevel, cfg.LogFormat, os.Stdout)
	slog.SetDefault(logger)
	logger.Info("starting service",
		slog.String("env", cfg.Env),
		slog.String("port", cfg.Port),
		slog.String("admin_port", cfg.AdminPort),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var metrics *observability.Metrics
	if cfg.MetricsEnabled {
		metrics = observability.NewMetrics()
	}

	db, sqlDB, err := database.Open(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := database.Close(sqlDB); err != nil {
			logger.Warn("failed to close database", slog.String("error", err.Error()))
		}
	}()

	if cfg.AutoMigrate {
		if err := database.Migrate(cfg.DatabaseURL, logger); err != nil {
			return err
		}
	} else {
		version, dirty, err := database.MigrationVersion(cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("check migration version: %w", err)
		}
		if dirty {
			return fmt.Errorf("database schema is dirty at version %d", version)
		}
		if version == 0 {
			return fmt.Errorf("database schema is not migrated; run migrations or set AUTO_MIGRATE=true")
		}
		logger.Info("automatic migrations disabled", slog.Uint64("schema_version", uint64(version)))
	}

	store, err := storage.NewS3(cfg, logger, metrics)
	if err != nil {
		return err
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, 20*time.Second)
	err = store.EnsureBucket(startupCtx)
	cancelStartup()
	if err != nil {
		return fmt.Errorf("object storage is not ready: %w", err)
	}

	users := repository.NewUserRepository(db)
	files := repository.NewFileRepository(db)
	refreshTokens := repository.NewRefreshTokenRepository(db)

	authService := service.NewAuthService(users, refreshTokens, cfg)
	fileService := service.NewFileService(files, store, cfg, logger, metrics)

	deps := api.Deps{
		Config:  cfg,
		Logger:  logger,
		Metrics: metrics,
		DB:      sqlDB,
		Store:   store,
		Auth:    authService,
		Files:   fileService,
	}

	apiServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewRouter(deps),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute, // large legacy multipart uploads
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	adminServer := &http.Server{
		Addr:              ":" + cfg.AdminPort,
		Handler:           api.NewAdminRouter(deps),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	janitorCtx, stopJanitor := context.WithCancel(ctx)
	defer stopJanitor()
	go runJanitor(janitorCtx, logger, fileService, authService, cfg.CleanupInterval)

	serverErrors := make(chan error, 2)
	go func() {
		logger.Info("api server listening", slog.String("addr", apiServer.Addr))
		if err := apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- fmt.Errorf("api server: %w", err)
		}
	}()
	go func() {
		logger.Info("admin server listening", slog.String("addr", adminServer.Addr))
		if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- fmt.Errorf("admin server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErrors:
		logger.Error("server failed", slog.String("error", err.Error()))
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()

	shutdownErr := errors.Join(
		apiServer.Shutdown(shutdownCtx),
		adminServer.Shutdown(shutdownCtx),
	)
	if shutdownErr != nil {
		logger.Warn("graceful shutdown incomplete", slog.String("error", shutdownErr.Error()))
		// Last resort so a stuck connection cannot keep the process alive.
		_ = apiServer.Close()
		_ = adminServer.Close()
	}

	stopJanitor()
	logger.Info("service stopped")
	return nil
}

func runJanitor(ctx context.Context, log *slog.Logger, files *service.FileService, auth *service.AuthService, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			workCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			if n, err := files.CleanupPending(workCtx); err != nil {
				log.Warn("pending upload cleanup failed", slog.String("error", err.Error()))
			} else if n > 0 {
				log.Info("expired pending uploads removed", slog.Int("count", n))
			}
			if n, err := auth.CleanupTokens(workCtx); err != nil {
				log.Warn("refresh token cleanup failed", slog.String("error", err.Error()))
			} else if n > 0 {
				log.Info("stale refresh tokens removed", slog.Int64("count", n))
			}
			cancel()
		}
	}
}

package handler

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/storage"
	"github.com/xshubhamg/file-upload/internal/version"
)

type HealthHandler struct {
	db    *sql.DB
	store storage.Store
	log   *slog.Logger
}

func NewHealthHandler(db *sql.DB, store storage.Store, log *slog.Logger) *HealthHandler {
	return &HealthHandler{db: db, store: store, log: log}
}

func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": version.Version,
		"commit":  version.Commit,
	})
}

func (h *HealthHandler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	checks := gin.H{"database": "ok", "storage": "ok"}
	ready := true

	if h.db != nil {
		if err := h.db.PingContext(ctx); err != nil {
			checks["database"] = "error"
			ready = false
			h.log.Warn("readiness: database unavailable", slog.String("error", err.Error()))
		}
	}
	if h.store != nil {
		if err := h.store.Healthy(ctx); err != nil {
			checks["storage"] = "error"
			ready = false
			h.log.Warn("readiness: object storage unavailable", slog.String("error", err.Error()))
		}
	}

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{
		"status": map[bool]string{true: "ready", false: "unavailable"}[ready],
		"checks": checks,
	})
}

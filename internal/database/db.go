// Package database owns the Postgres connection pool and schema migrations.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/xshubhamg/file-upload/internal/config"
)

// Open connects to Postgres, configures the pool and waits until the database
// accepts queries. Failing fast here makes container startup deterministic.
func Open(ctx context.Context, cfg config.Config, log *slog.Logger) (*gorm.DB, *sql.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
		Logger:                                   newGormLogger(log),
		DisableForeignKeyConstraintWhenMigrating: true,
		TranslateError:                           true,
		PrepareStmt:                              true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("access database pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("ping database: %w", err)
	}

	return db, sqlDB, nil
}

func Close(sqlDB *sql.DB) error {
	if sqlDB == nil {
		return nil
	}
	return sqlDB.Close()
}

// gormLogger routes GORM's slow-query and error logs through slog so every log
// line is structured and carries the same fields as HTTP logs.
type gormLogger struct {
	log   *slog.Logger
	level gormlogger.LogLevel
}

func newGormLogger(log *slog.Logger) gormlogger.Interface {
	level := gormlogger.Warn
	return &gormLogger{log: log.With(slog.String("component", "gorm")), level: level}
}

func (l *gormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	copy := *l
	copy.level = level
	return &copy
}

func (l *gormLogger) Info(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Info {
		l.log.Debug(msg, "args", data)
	}
}

func (l *gormLogger) Warn(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Warn {
		l.log.Warn(msg, "args", data)
	}
}

func (l *gormLogger) Error(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Error {
		l.log.Error(msg, "args", data)
	}
}

func (l *gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	if err != nil && err != gorm.ErrRecordNotFound {
		sqlQuery, rows := fc()
		l.log.Error("database query failed",
			slog.String("error", err.Error()),
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sqlQuery),
		)
		return
	}
	if elapsed > 500*time.Millisecond {
		sqlQuery, rows := fc()
		l.log.Warn("slow database query",
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sqlQuery),
		)
	}
}

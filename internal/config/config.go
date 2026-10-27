// Package config loads and validates all runtime configuration. The process
// refuses to start with an invalid configuration rather than failing later.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

type Config struct {
	AppName string
	Env     string
	Port    string

	// AdminPort serves health probes and metrics on a separate listener so
	// metrics can stay off the public API. Health probes are also exposed on the
	// public listener for simple container and load-balancer checks.
	AdminPort string

	LogLevel  slog.Level
	LogFormat string

	DatabaseURL       string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	AutoMigrate       bool

	AccessSecret  string
	RefreshSecret string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	BcryptCost    int

	MaxUploadBytes   int64
	MaxJSONBodyBytes int64
	AllowedMimeTypes map[string]struct{}

	S3Endpoint         string
	S3PublicEndpoint   string
	S3Region           string
	S3Bucket           string
	S3AccessKey        string
	S3SecretKey        string
	S3UseSSL           bool
	S3PublicUseSSL     bool
	S3AutoCreateBucket bool
	PresignUploadTTL   time.Duration
	PresignDownloadTTL time.Duration

	RateLimitRPS       float64
	RateLimitBurst     int
	AuthRateLimitRPS   float64
	AuthRateLimitBurst int

	CORSAllowedOrigins []string
	TrustedProxies     []string
	CookieSecure       bool
	CookieDomain       string

	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration

	PendingUploadTTL time.Duration
	CleanupInterval  time.Duration
	MetricsEnabled   bool
}

func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Load reads configuration from the process environment.
func Load() (Config, error) {
	return Parse(os.LookupEnv)
}

// Parse is Load with an injectable lookup function, which keeps tests hermetic.
func Parse(lookup func(string) (string, bool)) (Config, error) {
	l := &loader{lookup: lookup}
	env := strings.ToLower(l.str("APP_ENV", EnvDevelopment))
	if env != EnvDevelopment && env != EnvTest && env != EnvProduction {
		l.add(fmt.Errorf("APP_ENV must be one of %s, %s, %s", EnvDevelopment, EnvTest, EnvProduction))
		env = EnvDevelopment
	}

	cfg := Config{
		AppName: l.str("APP_NAME", "file-upload"),
		Env:     env,
		Port:    l.str("PORT", "8080"),

		AdminPort: l.str("ADMIN_PORT", "9090"),
		LogLevel:  l.level("LOG_LEVEL", defaultLevel(env)),
		LogFormat: l.str("LOG_FORMAT", defaultLogFormat(env)),

		DatabaseURL:       l.str("DATABASE_URL", ""),
		DBMaxOpenConns:    l.integer("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns:    l.integer("DB_MAX_IDLE_CONNS", 5),
		DBConnMaxLifetime: l.duration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		AutoMigrate:       l.boolean("AUTO_MIGRATE", env == EnvDevelopment),

		AccessSecret:  l.str("JWT_ACCESS_SECRET", ""),
		RefreshSecret: l.str("JWT_REFRESH_SECRET", ""),
		AccessTTL:     l.duration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTTL:    l.duration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		BcryptCost:    l.integer("BCRYPT_COST", 12),

		MaxUploadBytes:   l.int64("MAX_UPLOAD_BYTES", 100*1024*1024),
		MaxJSONBodyBytes: l.int64("MAX_JSON_BODY_BYTES", 1*1024*1024),
		AllowedMimeTypes: l.mimeSet("ALLOWED_MIME_TYPES",
			"image/jpeg,image/png,image/webp,image/gif,application/pdf,text/plain,text/csv,application/json,application/zip,video/mp4,audio/mpeg"),

		S3Endpoint:         l.str("S3_ENDPOINT", "localhost:9091"),
		S3PublicEndpoint:   l.str("S3_PUBLIC_ENDPOINT", ""),
		S3Region:           l.str("S3_REGION", "us-east-1"),
		S3Bucket:           l.str("S3_BUCKET", "file-upload"),
		S3AccessKey:        l.str("S3_ACCESS_KEY", ""),
		S3SecretKey:        l.str("S3_SECRET_KEY", ""),
		S3UseSSL:           l.boolean("S3_USE_SSL", false),
		S3PublicUseSSL:     l.boolean("S3_PUBLIC_USE_SSL", false),
		S3AutoCreateBucket: l.boolean("S3_AUTO_CREATE_BUCKET", env != EnvProduction),
		PresignUploadTTL:   l.duration("PRESIGN_UPLOAD_TTL", 15*time.Minute),
		PresignDownloadTTL: l.duration("PRESIGN_DOWNLOAD_TTL", 5*time.Minute),

		RateLimitRPS:       l.float("RATE_LIMIT_RPS", 20),
		RateLimitBurst:     l.integer("RATE_LIMIT_BURST", 40),
		AuthRateLimitRPS:   l.float("AUTH_RATE_LIMIT_RPS", 5),
		AuthRateLimitBurst: l.integer("AUTH_RATE_LIMIT_BURST", 10),

		CORSAllowedOrigins: l.csv("CORS_ALLOWED_ORIGINS", defaultCORS(env)),
		TrustedProxies:     l.csv("TRUSTED_PROXIES", defaultProxies(env)),
		CookieSecure:       l.boolean("COOKIE_SECURE", env == EnvProduction),
		CookieDomain:       l.str("COOKIE_DOMAIN", ""),

		RequestTimeout:  l.duration("REQUEST_TIMEOUT", 30*time.Second),
		ShutdownTimeout: l.duration("SHUTDOWN_TIMEOUT", 15*time.Second),

		PendingUploadTTL: l.duration("PENDING_UPLOAD_TTL", time.Hour),
		CleanupInterval:  l.duration("CLEANUP_INTERVAL", 10*time.Minute),
		MetricsEnabled:   l.boolean("METRICS_ENABLED", true),
	}

	if cfg.S3PublicEndpoint == "" {
		cfg.S3PublicEndpoint = cfg.S3Endpoint
		cfg.S3PublicUseSSL = cfg.S3UseSSL
	}

	if err := l.err(); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	var problems []error
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}

	if strings.TrimSpace(c.DatabaseURL) == "" {
		add("DATABASE_URL is required")
	} else if u, err := url.Parse(c.DatabaseURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		add("DATABASE_URL must be a postgres:// or postgresql:// URL")
	}
	if c.DBMaxOpenConns < 1 {
		add("DB_MAX_OPEN_CONNS must be at least 1")
	}
	if c.DBMaxIdleConns < 0 || c.DBMaxIdleConns > c.DBMaxOpenConns {
		add("DB_MAX_IDLE_CONNS must be between 0 and DB_MAX_OPEN_CONNS")
	}
	if c.DBConnMaxLifetime <= 0 {
		add("DB_CONN_MAX_LIFETIME must be positive")
	}

	if len(c.AccessSecret) < 32 {
		add("JWT_ACCESS_SECRET must be at least 32 characters")
	}
	if len(c.RefreshSecret) < 32 {
		add("JWT_REFRESH_SECRET must be at least 32 characters")
	}
	if c.AccessSecret != "" && c.AccessSecret == c.RefreshSecret {
		add("JWT_ACCESS_SECRET and JWT_REFRESH_SECRET must be different")
	}
	if c.AccessTTL <= 0 || c.AccessTTL > 24*time.Hour {
		add("ACCESS_TOKEN_TTL must be between 0 and 24h")
	}
	if c.RefreshTTL <= c.AccessTTL {
		add("REFRESH_TOKEN_TTL must be longer than ACCESS_TOKEN_TTL")
	}
	if c.BcryptCost < 10 || c.BcryptCost > 14 {
		add("BCRYPT_COST must be between 10 and 14")
	}

	if c.MaxUploadBytes <= 0 {
		add("MAX_UPLOAD_BYTES must be positive")
	}
	if c.MaxJSONBodyBytes <= 0 {
		add("MAX_JSON_BODY_BYTES must be positive")
	}
	if len(c.AllowedMimeTypes) == 0 {
		add("ALLOWED_MIME_TYPES must contain at least one MIME type")
	}

	if strings.TrimSpace(c.S3Endpoint) == "" {
		add("S3_ENDPOINT is required")
	}
	if strings.TrimSpace(c.S3Bucket) == "" {
		add("S3_BUCKET is required")
	}
	if strings.TrimSpace(c.S3AccessKey) == "" {
		add("S3_ACCESS_KEY is required")
	}
	if strings.TrimSpace(c.S3SecretKey) == "" {
		add("S3_SECRET_KEY is required")
	}
	if c.PresignUploadTTL <= 0 || c.PresignUploadTTL > time.Hour {
		add("PRESIGN_UPLOAD_TTL must be between 0 and 1h")
	}
	if c.PresignDownloadTTL <= 0 || c.PresignDownloadTTL > time.Hour {
		add("PRESIGN_DOWNLOAD_TTL must be between 0 and 1h")
	}

	if c.RateLimitRPS <= 0 || c.RateLimitBurst < 1 {
		add("RATE_LIMIT_RPS must be positive and RATE_LIMIT_BURST at least 1")
	}
	if c.AuthRateLimitRPS <= 0 || c.AuthRateLimitBurst < 1 {
		add("AUTH_RATE_LIMIT_RPS must be positive and AUTH_RATE_LIMIT_BURST at least 1")
	}
	if c.IsProduction() && len(c.CORSAllowedOrigins) == 0 {
		add("CORS_ALLOWED_ORIGINS is required in production")
	}
	for _, origin := range c.CORSAllowedOrigins {
		if origin == "*" {
			add("CORS_ALLOWED_ORIGINS must not contain '*' because credentials are enabled")
			break
		}
	}
	if c.RequestTimeout <= 0 || c.ShutdownTimeout <= 0 {
		add("REQUEST_TIMEOUT and SHUTDOWN_TIMEOUT must be positive")
	}
	if c.PendingUploadTTL <= 0 || c.CleanupInterval <= 0 {
		add("PENDING_UPLOAD_TTL and CLEANUP_INTERVAL must be positive")
	}

	for _, port := range []struct {
		name  string
		value string
	}{{"PORT", c.Port}, {"ADMIN_PORT", c.AdminPort}} {
		n, err := strconv.Atoi(port.value)
		if err != nil || n < 1 || n > 65535 {
			add("%s must be a valid TCP port", port.name)
		}
	}

	return errors.Join(problems...)
}

func defaultLevel(env string) slog.Level {
	if env == EnvProduction {
		return slog.LevelInfo
	}
	return slog.LevelDebug
}

func defaultLogFormat(env string) string {
	if env == EnvProduction {
		return "json"
	}
	return "text"
}

func defaultCORS(env string) string {
	if env == EnvProduction {
		return ""
	}
	return "http://localhost:3000"
}

func defaultProxies(env string) string {
	if env == EnvProduction {
		return ""
	}
	return "127.0.0.1,::1"
}

type loader struct {
	lookup   func(string) (string, bool)
	problems []error
}

func (l *loader) add(err error) { l.problems = append(l.problems, err) }
func (l *loader) err() error    { return errors.Join(l.problems...) }

func (l *loader) str(key, fallback string) string {
	if v, ok := l.lookup(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func (l *loader) integer(key string, fallback int) int {
	raw := l.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		l.add(fmt.Errorf("%s must be an integer: %w", key, err))
		return fallback
	}
	return v
}

func (l *loader) int64(key string, fallback int64) int64 {
	raw := l.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		l.add(fmt.Errorf("%s must be an integer: %w", key, err))
		return fallback
	}
	return v
}

func (l *loader) float(key string, fallback float64) float64 {
	raw := l.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		l.add(fmt.Errorf("%s must be a number: %w", key, err))
		return fallback
	}
	return v
}

func (l *loader) boolean(key string, fallback bool) bool {
	raw := l.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		l.add(fmt.Errorf("%s must be true or false: %w", key, err))
		return fallback
	}
	return v
}

func (l *loader) duration(key string, fallback time.Duration) time.Duration {
	raw := l.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		l.add(fmt.Errorf("%s must be a duration such as 15m: %w", key, err))
		return fallback
	}
	return v
}

func (l *loader) level(key string, fallback slog.Level) slog.Level {
	raw := l.str(key, "")
	if raw == "" {
		return fallback
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		l.add(fmt.Errorf("%s must be debug, info, warn or error: %w", key, err))
		return fallback
	}
	return level
}

func (l *loader) csv(key, fallback string) []string {
	raw := l.str(key, fallback)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (l *loader) mimeSet(key, fallback string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, v := range l.csv(key, fallback) {
		set[strings.ToLower(v)] = struct{}{}
	}
	return set
}

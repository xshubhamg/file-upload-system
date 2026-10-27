package config_test

import (
	"strings"
	"testing"

	"github.com/xshubhamg/file-upload/internal/config"
)

func baseEnv() map[string]string {
	return map[string]string{
		"APP_ENV":              "test",
		"DATABASE_URL":         "postgres://user:pass@localhost:5432/db?sslmode=disable",
		"JWT_ACCESS_SECRET":    "0123456789abcdef0123456789abcdef",
		"JWT_REFRESH_SECRET":   "fedcba9876543210fedcba9876543210",
		"S3_ENDPOINT":          "localhost:9091",
		"S3_BUCKET":            "bucket",
		"S3_ACCESS_KEY":        "key",
		"S3_SECRET_KEY":        "secret",
		"CORS_ALLOWED_ORIGINS": "http://localhost:3000",
	}
}

func parseEnv(env map[string]string) (config.Config, error) {
	return config.Parse(func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	})
}

func TestParseDefaults(t *testing.T) {
	cfg, err := parseEnv(baseEnv())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Port != "8080" || cfg.AdminPort != "9090" {
		t.Fatalf("unexpected ports: %s %s", cfg.Port, cfg.AdminPort)
	}
	if cfg.AccessTTL.String() != "15m0s" {
		t.Fatalf("unexpected access TTL: %s", cfg.AccessTTL)
	}
	if cfg.AutoMigrate {
		t.Fatal("AutoMigrate should default to false outside development")
	}
	if cfg.CookieSecure {
		t.Fatal("CookieSecure should default to false outside production")
	}
	if _, ok := cfg.AllowedMimeTypes["image/png"]; !ok {
		t.Fatal("default MIME allow-list is missing image/png")
	}
	if cfg.S3PublicEndpoint != cfg.S3Endpoint {
		t.Fatalf("public endpoint should default to internal endpoint: %q", cfg.S3PublicEndpoint)
	}
	if !cfg.MetricsEnabled {
		t.Fatal("MetricsEnabled should default to true")
	}
}

func TestParseMetricsDisabled(t *testing.T) {
	env := baseEnv()
	env["METRICS_ENABLED"] = "false"
	cfg, err := parseEnv(env)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.MetricsEnabled {
		t.Fatal("MetricsEnabled should be false when METRICS_ENABLED=false")
	}
}

func TestParseProductionDefaults(t *testing.T) {
	env := baseEnv()
	env["APP_ENV"] = "production"
	cfg, err := parseEnv(env)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.CookieSecure {
		t.Fatal("CookieSecure should default to true in production")
	}
	if cfg.LogFormat != "json" {
		t.Fatalf("production log format = %q, want json", cfg.LogFormat)
	}
	if cfg.AutoMigrate {
		t.Fatal("AutoMigrate should default to false in production")
	}
}

func TestParseRejectsMissingSecrets(t *testing.T) {
	env := baseEnv()
	delete(env, "JWT_ACCESS_SECRET")
	delete(env, "JWT_REFRESH_SECRET")
	_, err := parseEnv(env)
	if err == nil {
		t.Fatal("expected an error for missing secrets")
	}
	msg := err.Error()
	if !strings.Contains(msg, "JWT_ACCESS_SECRET") || !strings.Contains(msg, "JWT_REFRESH_SECRET") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRejectsSharedSecrets(t *testing.T) {
	env := baseEnv()
	env["JWT_REFRESH_SECRET"] = env["JWT_ACCESS_SECRET"]
	_, err := parseEnv(env)
	if err == nil || !strings.Contains(err.Error(), "must be different") {
		t.Fatalf("expected distinct-secret error, got %v", err)
	}
}

func TestParseRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		value  string
		wanted string
	}{
		{name: "duration", key: "ACCESS_TOKEN_TTL", value: "soon", wanted: "ACCESS_TOKEN_TTL"},
		{name: "database scheme", key: "DATABASE_URL", value: "mysql://nope", wanted: "DATABASE_URL"},
		{name: "bcrypt cost", key: "BCRYPT_COST", value: "4", wanted: "BCRYPT_COST"},
		{name: "rate limit", key: "RATE_LIMIT_RPS", value: "0", wanted: "RATE_LIMIT_RPS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv()
			env[tc.key] = tc.value
			_, err := parseEnv(env)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.wanted) {
				t.Fatalf("error %q does not mention %s", err, tc.wanted)
			}
		})
	}
}

func TestParseProductionRequiresCORS(t *testing.T) {
	env := baseEnv()
	env["APP_ENV"] = "production"
	env["CORS_ALLOWED_ORIGINS"] = ""
	_, err := parseEnv(env)
	if err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
		t.Fatalf("expected CORS error, got %v", err)
	}
}

func TestParseRejectsWildcardCORS(t *testing.T) {
	env := baseEnv()
	env["CORS_ALLOWED_ORIGINS"] = "*"
	_, err := parseEnv(env)
	if err == nil || !strings.Contains(err.Error(), "must not contain '*'") {
		t.Fatalf("expected wildcard CORS error, got %v", err)
	}
}

func TestParseRejectsInvalidPorts(t *testing.T) {
	env := baseEnv()
	env["PORT"] = "not-a-port"
	env["ADMIN_PORT"] = "70000"
	_, err := parseEnv(env)
	if err == nil {
		t.Fatal("expected port validation error")
	}
	if !strings.Contains(err.Error(), "PORT") || !strings.Contains(err.Error(), "ADMIN_PORT") {
		t.Fatalf("unexpected error: %v", err)
	}
}

package testutil

import "github.com/xshubhamg/file-upload/internal/config"

// Config returns a valid configuration for in-process tests. It intentionally
// avoids environment variables so tests are hermetic and parallel-safe.
func Config() config.Config {
	env := map[string]string{
		"APP_ENV":               "test",
		"APP_NAME":              "file-upload-test",
		"PORT":                  "8080",
		"ADMIN_PORT":            "9090",
		"LOG_LEVEL":             "error",
		"LOG_FORMAT":            "text",
		"DATABASE_URL":          "postgres://test:test@localhost:5432/file_upload_test?sslmode=disable",
		"JWT_ACCESS_SECRET":     "0123456789abcdef0123456789abcdef",
		"JWT_REFRESH_SECRET":    "fedcba9876543210fedcba9876543210",
		"ACCESS_TOKEN_TTL":      "15m",
		"REFRESH_TOKEN_TTL":     "24h",
		"BCRYPT_COST":           "10",
		"MAX_UPLOAD_BYTES":      "1048576",
		"MAX_JSON_BODY_BYTES":   "65536",
		"S3_ENDPOINT":           "localhost:9091",
		"S3_REGION":             "us-east-1",
		"S3_BUCKET":             "file-upload-test",
		"S3_ACCESS_KEY":         "s3mock-access",
		"S3_SECRET_KEY":         "s3mock-secret",
		"CORS_ALLOWED_ORIGINS":  "http://example.com",
		"COOKIE_SECURE":         "false",
		"RATE_LIMIT_RPS":        "1000",
		"RATE_LIMIT_BURST":      "1000",
		"AUTH_RATE_LIMIT_RPS":   "1000",
		"AUTH_RATE_LIMIT_BURST": "1000",
		"PENDING_UPLOAD_TTL":    "1h",
		"CLEANUP_INTERVAL":      "10m",
		"AUTO_MIGRATE":          "false",
	}
	cfg, err := config.Parse(func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	})
	if err != nil {
		panic("testutil.Config: " + err.Error())
	}
	return cfg
}

package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xshubhamg/file-upload/internal/observability"
	"github.com/xshubhamg/file-upload/internal/storage"
	"github.com/xshubhamg/file-upload/internal/testutil"
)

func TestS3StoreIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT is not set; skipping S3 integration test")
	}

	cfg := testutil.Config()
	cfg.S3Endpoint = endpoint
	cfg.S3PublicEndpoint = endpoint
	cfg.S3Bucket = envOr("TEST_S3_BUCKET", "file-upload-test")
	cfg.S3AccessKey = envOr("TEST_S3_ACCESS_KEY", "s3mock-access")
	cfg.S3SecretKey = envOr("TEST_S3_SECRET_KEY", "s3mock-secret")
	cfg.S3AutoCreateBucket = true
	cfg.S3UseSSL = os.Getenv("TEST_S3_USE_SSL") == "true"
	cfg.S3PublicUseSSL = cfg.S3UseSSL

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := storage.NewS3(cfg, log, observability.NewMetrics())
	if err != nil {
		t.Fatalf("new s3: %v", err)
	}
	ctx := context.Background()
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}

	key := "tests/" + uuid.NewString() + "/hello.txt"
	payload := []byte("hello from the integration test")

	upload, err := store.PresignUpload(ctx, key, "text/plain", 5*time.Minute)
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, upload.URL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new put request: %v", err)
	}
	for name, value := range upload.Headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("presigned put: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("presigned put status = %d", resp.StatusCode)
	}

	info, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("stat size = %d, want %d", info.Size, len(payload))
	}

	downloadURL, err := store.PresignDownload(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	downloadResp, err := http.Get(downloadURL)
	if err != nil {
		t.Fatalf("presigned get: %v", err)
	}
	body, _ := io.ReadAll(downloadResp.Body)
	_ = downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK || !bytes.Equal(body, payload) {
		t.Fatalf("download status = %d, body = %q", downloadResp.StatusCode, body)
	}

	reader, object, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ = io.ReadAll(reader)
	_ = reader.Close()
	if !bytes.Equal(body, payload) || object.Size != int64(len(payload)) {
		t.Fatalf("get returned %q, info %+v", body, object)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Stat(ctx, key); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("stat after delete = %v, want ErrObjectNotFound", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

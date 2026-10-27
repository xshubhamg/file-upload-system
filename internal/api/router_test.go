package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/xshubhamg/file-upload/internal/api"
	"github.com/xshubhamg/file-upload/internal/api/handler"
	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/observability"
	"github.com/xshubhamg/file-upload/internal/service"
	"github.com/xshubhamg/file-upload/internal/storage"
	"github.com/xshubhamg/file-upload/internal/testutil"
)

type testServer struct {
	t      *testing.T
	server *httptest.Server
	users  *testutil.UserRepo
	files  *testutil.FileRepo
	tokens *testutil.RefreshRepo
	store  *storage.Memory
}

func newTestServer(t *testing.T, cfg config.Config) *testServer {
	t.Helper()
	users := testutil.NewUserRepo()
	files := testutil.NewFileRepo()
	tokens := testutil.NewRefreshRepo()
	store := storage.NewMemory()
	metrics := observability.NewMetrics()
	logger := observability.NewLogger(slog.LevelError, "text", io.Discard)

	auth := service.NewAuthService(users, tokens, cfg)
	filesSvc := service.NewFileService(files, store, cfg, logger, metrics)
	router := api.NewRouter(api.Deps{
		Config:  cfg,
		Logger:  logger,
		Metrics: metrics,
		Store:   store,
		Auth:    auth,
		Files:   filesSvc,
	})
	server := httptest.NewServer(router)
	// Presigned URLs point at object storage; do not follow them in tests.
	server.Client().CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	t.Cleanup(server.Close)
	return &testServer{t: t, server: server, users: users, files: files, tokens: tokens, store: store}
}

func (s *testServer) request(method, path, token string, payload any) (*http.Response, []byte) {
	s.t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			s.t.Fatalf("marshal request: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.server.URL+path, body)
	if err != nil {
		s.t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.server.Client().Do(req)
	if err != nil {
		s.t.Fatalf("do request: %v", err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		s.t.Fatalf("read response: %v", err)
	}
	return resp, raw
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode response %q: %v", raw, err)
	}
	return value
}

type loginResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	User         handler.UserDTO `json:"user"`
}

type initiateResponse struct {
	File   handler.FileDTO         `json:"file"`
	Upload storage.PresignedUpload `json:"upload"`
}

func (s *testServer) registerAndLogin(username string) loginResponse {
	s.t.Helper()
	resp, raw := s.request(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": username,
		"email":    username + "@example.com",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		s.t.Fatalf("register status = %d, body = %s", resp.StatusCode, raw)
	}
	resp, raw = s.request(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": username,
		"password": "password123",
	})
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("login status = %d, body = %s", resp.StatusCode, raw)
	}
	login := decode[loginResponse](s.t, raw)
	if login.AccessToken == "" || login.RefreshToken == "" {
		s.t.Fatalf("login did not return tokens: %s", raw)
	}
	return login
}

func TestAuthAndFileLifecycle(t *testing.T) {
	s := newTestServer(t, testutil.Config())
	login := s.registerAndLogin("alice")

	resp, raw := s.request(http.MethodGet, "/api/v1/files", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status = %d, body = %s", resp.StatusCode, raw)
	}
	envelope := decode[struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}](t, raw)
	if envelope.Error.Code != "UNAUTHORIZED" || envelope.Error.RequestID == "" {
		t.Fatalf("unexpected error envelope: %s", raw)
	}
	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID header")
	}

	resp, raw = s.request(http.MethodPost, "/api/v1/files/uploads", login.AccessToken, map[string]any{
		"filename":     "hello.txt",
		"content_type": "text/plain",
		"size_bytes":   5,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("initiate status = %d, body = %s", resp.StatusCode, raw)
	}
	initiated := decode[initiateResponse](t, raw)
	if initiated.File.ID == "" || initiated.Upload.URL == "" {
		t.Fatalf("unexpected initiate response: %s", raw)
	}

	file, err := s.files.FindByID(context.Background(), uuid.MustParse(initiated.File.ID))
	if err != nil {
		t.Fatalf("find pending file: %v", err)
	}
	if err := s.store.Put(context.Background(), file.ObjectKey, "text/plain", strings.NewReader("hello"), 5); err != nil {
		t.Fatalf("put object: %v", err)
	}

	resp, raw = s.request(http.MethodPost, "/api/v1/files/"+initiated.File.ID+"/complete", login.AccessToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d, body = %s", resp.StatusCode, raw)
	}
	completed := decode[struct {
		File handler.FileDTO `json:"file"`
	}](t, raw)
	if completed.File.Status != "active" || completed.File.SizeBytes != 5 {
		t.Fatalf("unexpected completed file: %s", raw)
	}

	resp, raw = s.request(http.MethodGet, "/api/v1/files", login.AccessToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", resp.StatusCode, raw)
	}
	list := decode[struct {
		Items      []handler.FileDTO `json:"items"`
		NextCursor string            `json:"next_cursor"`
	}](t, raw)
	if len(list.Items) != 1 || list.Items[0].Filename != "hello.txt" {
		t.Fatalf("unexpected list: %s", raw)
	}

	resp, raw = s.request(http.MethodGet, "/api/v1/files/"+initiated.File.ID, login.AccessToken, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte("download_url")) {
		t.Fatalf("get status = %d, body = %s", resp.StatusCode, raw)
	}

	resp, raw = s.request(http.MethodGet, "/api/v1/files/"+initiated.File.ID+"/download", login.AccessToken, nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("download status = %d, body = %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("Location") == "" {
		t.Fatal("download did not redirect")
	}

	resp, raw = s.request(http.MethodDelete, "/api/v1/files/"+initiated.File.ID, login.AccessToken, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", resp.StatusCode, raw)
	}
	if _, err := s.store.Stat(context.Background(), file.ObjectKey); err == nil {
		t.Fatal("object still exists after delete")
	}
}

func TestRefreshRotationOverHTTP(t *testing.T) {
	s := newTestServer(t, testutil.Config())
	login := s.registerAndLogin("bob")

	resp, raw := s.request(http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": login.RefreshToken,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", resp.StatusCode, raw)
	}
	rotated := decode[struct {
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token"`
	}](t, raw)
	if rotated.RefreshToken == "" || rotated.RefreshToken == login.RefreshToken {
		t.Fatalf("token was not rotated: %s", raw)
	}

	resp, raw = s.request(http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": login.RefreshToken,
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay status = %d, body = %s", resp.StatusCode, raw)
	}
	replay := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, raw)
	if replay.Error.Code != "TOKEN_REUSE_DETECTED" {
		t.Fatalf("unexpected replay error: %s", raw)
	}

	// The rotated token belongs to the same family and is now revoked too.
	resp, _ = s.request(http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{
		"refresh_token": rotated.RefreshToken,
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("family token still valid, status = %d", resp.StatusCode)
	}
}

func TestLegacyAliases(t *testing.T) {
	s := newTestServer(t, testutil.Config())

	resp, raw := s.request(http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": "legacy",
		"email":    "legacy@example.com",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("legacy register status = %d, body = %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("Deprecation") != "true" {
		t.Fatal("legacy route did not set Deprecation header")
	}

	resp, raw = s.request(http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "legacy",
		"password": "password123",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy login status = %d, body = %s", resp.StatusCode, raw)
	}
	login := decode[loginResponse](t, raw)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "legacy.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("hello"))
	_ = writer.Close()

	req, err := http.NewRequest(http.MethodPost, s.server.URL+"/api/file/upload", &body)
	if err != nil {
		t.Fatalf("new upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+login.AccessToken)
	uploadResp, err := s.server.Client().Do(req)
	if err != nil {
		t.Fatalf("legacy upload: %v", err)
	}
	uploadBody, _ := io.ReadAll(uploadResp.Body)
	_ = uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("legacy upload status = %d, body = %s", uploadResp.StatusCode, uploadBody)
	}
	if uploadResp.Header.Get("Deprecation") != "true" {
		t.Fatal("legacy upload did not set Deprecation header")
	}
	upload := decode[struct {
		URL string `json:"url"`
	}](t, uploadBody)
	if upload.URL != "/api/file/legacy.txt" {
		t.Fatalf("unexpected legacy URL: %s", uploadBody)
	}

	resp, raw = s.request(http.MethodGet, "/api/file", login.AccessToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy list status = %d, body = %s", resp.StatusCode, raw)
	}
	files := decode[[]struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}](t, raw)
	if len(files) != 1 || files[0].Filename != "legacy.txt" || files[0].Size != 5 {
		t.Fatalf("unexpected legacy list: %s", raw)
	}

	resp, raw = s.request(http.MethodGet, "/api/file/legacy.txt", login.AccessToken, nil)
	if resp.StatusCode != http.StatusOK || string(raw) != "hello" {
		t.Fatalf("legacy get status = %d, body = %q", resp.StatusCode, raw)
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "legacy.txt") {
		t.Fatalf("missing content disposition: %q", resp.Header.Get("Content-Disposition"))
	}

	resp, raw = s.request(http.MethodDelete, "/api/file/legacy.txt", login.AccessToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy delete status = %d, body = %s", resp.StatusCode, raw)
	}
	resp, _ = s.request(http.MethodGet, "/api/file/legacy.txt", login.AccessToken, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("legacy file still present, status = %d", resp.StatusCode)
	}
}

func TestCORSAndRateLimit(t *testing.T) {
	s := newTestServer(t, testutil.Config())

	req, _ := http.NewRequest(http.MethodOptions, s.server.URL+"/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	resp, err := s.server.Client().Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_ = resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Fatalf("missing allow-origin: %v", resp.Header)
	}

	req, _ = http.NewRequest(http.MethodOptions, s.server.URL+"/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	resp, err = s.server.Client().Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_ = resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin was reflected: %v", resp.Header)
	}

	cfg := testutil.Config()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1
	limited := newTestServer(t, cfg)
	first, _ := limited.request(http.MethodGet, "/api/v1/files", "", nil)
	if first.StatusCode != http.StatusUnauthorized {
		t.Fatalf("first request status = %d", first.StatusCode)
	}
	second, raw := limited.request(http.MethodGet, "/api/v1/files", "", nil)
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, body = %s", second.StatusCode, raw)
	}
	if second.Header.Get("Retry-After") == "" {
		t.Fatal("rate-limited response is missing Retry-After")
	}
}

func TestOpenAPIAndNotFound(t *testing.T) {
	s := newTestServer(t, testutil.Config())

	resp, raw := s.request(http.MethodGet, "/api/v1/openapi.yaml", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte("openapi: 3.0.3")) {
		t.Fatalf("openapi status = %d, body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "yaml") {
		t.Fatalf("unexpected openapi content type: %q", resp.Header.Get("Content-Type"))
	}

	resp, raw = s.request(http.MethodGet, "/api/v1/docs", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte("SwaggerUIBundle")) {
		t.Fatalf("docs status = %d, body = %s", resp.StatusCode, raw)
	}

	resp, raw = s.request(http.MethodGet, "/does-not-exist", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("not found status = %d, body = %s", resp.StatusCode, raw)
	}
	envelope := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, raw)
	if envelope.Error.Code != "NOT_FOUND" {
		t.Fatalf("unexpected 404 envelope: %s", raw)
	}
}

func TestMetricsEnabledToggle(t *testing.T) {
	cfg := testutil.Config()
	logger := observability.NewLogger(slog.LevelError, "text", io.Discard)
	metrics := observability.NewMetrics()

	cfg.MetricsEnabled = false
	disabled := httptest.NewServer(api.NewAdminRouter(api.Deps{Config: cfg, Logger: logger, Metrics: metrics}))
	defer disabled.Close()

	resp, err := disabled.Client().Get(disabled.URL + "/metrics")
	if err != nil {
		t.Fatalf("disabled metrics request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("metrics should be absent when disabled, status = %d", resp.StatusCode)
	}

	cfg.MetricsEnabled = true
	enabled := httptest.NewServer(api.NewAdminRouter(api.Deps{Config: cfg, Logger: logger, Metrics: metrics}))
	defer enabled.Close()

	resp, err = enabled.Client().Get(enabled.URL + "/metrics")
	if err != nil {
		t.Fatalf("enabled metrics request: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("go_goroutines")) {
		t.Fatalf("metrics should be served when enabled, status = %d, body = %s", resp.StatusCode, body)
	}

	// Metrics are never exposed on the public API listener, regardless of the flag.
	public := newTestServer(t, cfg)
	resp, raw := public.request(http.MethodGet, "/metrics", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("public /metrics status = %d, body = %s", resp.StatusCode, raw)
	}
}

func TestHealthEndpoints(t *testing.T) {
	s := newTestServer(t, testutil.Config())

	resp, raw := s.request(http.MethodGet, "/healthz", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"ok"`)) {
		t.Fatalf("liveness status = %d, body = %s", resp.StatusCode, raw)
	}
	resp, raw = s.request(http.MethodGet, "/readyz", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(`"ready"`)) {
		t.Fatalf("readiness status = %d, body = %s", resp.StatusCode, raw)
	}
}

package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xshubhamg/file-upload/internal/apperr"
	"github.com/xshubhamg/file-upload/internal/domain"
	"github.com/xshubhamg/file-upload/internal/repository"
	"github.com/xshubhamg/file-upload/internal/service"
	"github.com/xshubhamg/file-upload/internal/storage"
	"github.com/xshubhamg/file-upload/internal/testutil"
)

func newFileFixture() (*service.FileService, *testutil.FileRepo, *storage.Memory, uuid.UUID) {
	cfg := testutil.Config()
	repo := testutil.NewFileRepo()
	store := storage.NewMemory()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return service.NewFileService(repo, store, cfg, log, nil), repo, store, uuid.New()
}

func putObject(t *testing.T, store *storage.Memory, file *domain.File, contentType string, data []byte) {
	t.Helper()
	if err := store.Put(context.Background(), file.ObjectKey, contentType, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("put object: %v", err)
	}
}

func createActive(t *testing.T, svc *service.FileService, store *storage.Memory, userID uuid.UUID, name, contentType string, data []byte) *domain.File {
	t.Helper()
	file, _, err := svc.InitiateUpload(context.Background(), userID, service.InitiateUploadInput{
		Filename:    name,
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
	})
	if err != nil {
		t.Fatalf("initiate %s: %v", name, err)
	}
	putObject(t, store, file, contentType, data)
	completed, err := svc.CompleteUpload(context.Background(), userID, file.ID)
	if err != nil {
		t.Fatalf("complete %s: %v", name, err)
	}
	return completed
}

func TestInitiateUploadValidation(t *testing.T) {
	svc, _, _, userID := newFileFixture()
	ctx := context.Background()

	file, upload, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "../report.txt",
		ContentType: "text/plain",
		SizeBytes:   5,
	})
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if file.Filename != "report.txt" {
		t.Fatalf("filename was not sanitized: %q", file.Filename)
	}
	if file.Status != domain.FilePending {
		t.Fatalf("new upload status = %q, want pending", file.Status)
	}
	if !strings.HasPrefix(file.ObjectKey, "users/"+userID.String()+"/") {
		t.Fatalf("object key is not namespaced by user: %q", file.ObjectKey)
	}
	if upload.URL == "" || upload.Method != "PUT" || upload.Headers["Content-Type"] != "text/plain" {
		t.Fatalf("unexpected presigned upload: %+v", upload)
	}

	if _, _, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "report.txt",
		ContentType: "text/plain",
		SizeBytes:   5,
	}); err == nil || apperr.From(err).Code != "FILE_ALREADY_EXISTS" {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	if _, _, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "bad.exe",
		ContentType: "application/x-msdownload",
		SizeBytes:   5,
	}); err == nil || apperr.From(err).Code != "UNSUPPORTED_MEDIA_TYPE" {
		t.Fatalf("expected unsupported media, got %v", err)
	}
	if _, _, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "empty.txt",
		ContentType: "text/plain",
		SizeBytes:   0,
	}); err == nil || apperr.From(err).Code != "VALIDATION_ERROR" {
		t.Fatalf("expected validation error, got %v", err)
	}
	if _, _, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "huge.bin",
		ContentType: "application/octet-stream",
		SizeBytes:   testutil.Config().MaxUploadBytes + 1,
	}); err == nil || apperr.From(err).Code != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("expected payload too large, got %v", err)
	}
}

func TestCompleteUploadChecksObject(t *testing.T) {
	svc, _, store, userID := newFileFixture()
	ctx := context.Background()

	file, _, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "hello.txt",
		ContentType: "text/plain",
		SizeBytes:   5,
	})
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if _, err := svc.CompleteUpload(ctx, userID, file.ID); err == nil || apperr.From(err).Code != "UPLOAD_NOT_FOUND" {
		t.Fatalf("expected missing upload, got %v", err)
	}

	putObject(t, store, file, "text/plain", []byte("hello"))
	completed, err := svc.CompleteUpload(ctx, userID, file.ID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Status != domain.FileActive || completed.SizeBytes != 5 {
		t.Fatalf("unexpected completed file: %+v", completed)
	}
	// Idempotent retry.
	if _, err := svc.CompleteUpload(ctx, userID, file.ID); err != nil {
		t.Fatalf("idempotent complete: %v", err)
	}
}

func TestCompleteUploadRejectsDisallowedActualType(t *testing.T) {
	svc, _, store, userID := newFileFixture()
	ctx := context.Background()

	file, _, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "sneaky.txt",
		ContentType: "text/plain",
		SizeBytes:   5,
	})
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	putObject(t, store, file, "application/x-msdownload", []byte("hello"))

	if _, err := svc.CompleteUpload(ctx, userID, file.ID); err == nil || apperr.From(err).Code != "UNSUPPORTED_MEDIA_TYPE" {
		t.Fatalf("expected unsupported media, got %v", err)
	}
	if _, err := store.Stat(ctx, file.ObjectKey); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("rejected object should be deleted, got %v", err)
	}
}

func TestCompleteUploadRejectsOversizeObject(t *testing.T) {
	svc, _, store, userID := newFileFixture()
	ctx := context.Background()

	file, _, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "big.txt",
		ContentType: "text/plain",
		SizeBytes:   1,
	})
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	putObject(t, store, file, "text/plain", bytes.Repeat([]byte("x"), int(testutil.Config().MaxUploadBytes)+1))

	if _, err := svc.CompleteUpload(ctx, userID, file.ID); err == nil || apperr.From(err).Code != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("expected payload too large, got %v", err)
	}
	if _, err := store.Stat(ctx, file.ObjectKey); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("oversize object should be deleted, got %v", err)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	svc, _, store, owner := newFileFixture()
	ctx := context.Background()

	file := createActive(t, svc, store, owner, "private.txt", "text/plain", []byte("secret"))
	other := uuid.New()

	if _, _, err := svc.Get(ctx, other, file.ID); err == nil || apperr.From(err).Code != "NOT_FOUND" {
		t.Fatalf("expected not found for other user, got %v", err)
	}
	if err := svc.Delete(ctx, other, file.ID); err == nil || apperr.From(err).Code != "NOT_FOUND" {
		t.Fatalf("expected not found deleting other user's file, got %v", err)
	}
}

func TestListPaginationAndDelete(t *testing.T) {
	svc, repo, store, userID := newFileFixture()
	ctx := context.Background()

	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		createActive(t, svc, store, userID, name, "text/plain", []byte("data"))
		time.Sleep(time.Millisecond)
	}

	page, err := svc.List(ctx, userID, "", 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", page)
	}
	next, err := svc.List(ctx, userID, page.NextCursor, 2)
	if err != nil {
		t.Fatalf("list next: %v", err)
	}
	if len(next.Items) != 1 || next.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v", next)
	}
	if next.Items[0].Filename != "a.txt" {
		t.Fatalf("pagination order wrong: %+v", next.Items[0])
	}

	if err := svc.Delete(ctx, userID, page.Items[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.FindByID(ctx, page.Items[0].ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("row should be gone, got %v", err)
	}
}

func TestUploadLegacy(t *testing.T) {
	svc, repo, _, userID := newFileFixture()
	ctx := context.Background()

	file, err := svc.UploadLegacy(ctx, userID, "legacy.txt", "text/plain", 5, strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("legacy upload: %v", err)
	}
	if file.Status != domain.FileActive || file.SizeBytes != 5 {
		t.Fatalf("unexpected legacy file: %+v", file)
	}
	if _, err := svc.UploadLegacy(ctx, userID, "legacy.txt", "text/plain", 5, strings.NewReader("hello")); err == nil || apperr.From(err).Code != "FILE_ALREADY_EXISTS" {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	if repo.Count() != 1 {
		t.Fatalf("unexpected row count: %d", repo.Count())
	}
}

func TestCleanupPendingUploads(t *testing.T) {
	svc, repo, store, userID := newFileFixture()
	ctx := context.Background()

	now := time.Now().UTC()
	svc.SetClock(func() time.Time { return now })
	file, _, err := svc.InitiateUpload(ctx, userID, service.InitiateUploadInput{
		Filename:    "abandoned.txt",
		ContentType: "text/plain",
		SizeBytes:   5,
	})
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	putObject(t, store, file, "text/plain", []byte("hello"))

	svc.SetClock(func() time.Time { return now.Add(2 * time.Hour) })
	deleted, err := svc.CleanupPending(ctx)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 1 || repo.Count() != 0 {
		t.Fatalf("cleanup deleted %d rows; remaining %d", deleted, repo.Count())
	}
	if _, err := store.Stat(ctx, file.ObjectKey); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("pending object should be deleted, got %v", err)
	}
}

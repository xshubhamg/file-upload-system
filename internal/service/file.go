package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/xshubhamg/file-upload/internal/apperr"
	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/domain"
	"github.com/xshubhamg/file-upload/internal/observability"
	"github.com/xshubhamg/file-upload/internal/repository"
	"github.com/xshubhamg/file-upload/internal/storage"
)

type InitiateUploadInput struct {
	Filename    string
	ContentType string
	SizeBytes   int64
}

type FilePage struct {
	Items      []*domain.File `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type FileService struct {
	files   repository.FileRepository
	store   storage.Store
	cfg     config.Config
	log     *slog.Logger
	metrics *observability.Metrics
	now     func() time.Time
}

func NewFileService(files repository.FileRepository, store storage.Store, cfg config.Config, log *slog.Logger, metrics *observability.Metrics) *FileService {
	return &FileService{
		files:   files,
		store:   store,
		cfg:     cfg,
		log:     log.With(slog.String("component", "files")),
		metrics: metrics,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (s *FileService) SetClock(now func() time.Time) { s.now = now }

// InitiateUpload creates a pending row and a presigned PUT URL. The client
// uploads straight to object storage; CompleteUpload validates the object.
func (s *FileService) InitiateUpload(ctx context.Context, userID uuid.UUID, in InitiateUploadInput) (*domain.File, storage.PresignedUpload, error) {
	filename, err := SafeFilename(in.Filename)
	if err != nil {
		return nil, storage.PresignedUpload{}, err
	}
	if in.SizeBytes <= 0 {
		return nil, storage.PresignedUpload{}, apperr.Validation("size_bytes must be positive").
			WithDetail("size_bytes", "must be greater than zero")
	}
	if in.SizeBytes > s.cfg.MaxUploadBytes {
		return nil, storage.PresignedUpload{}, apperr.PayloadTooLarge(fmt.Sprintf("file exceeds the %d byte limit", s.cfg.MaxUploadBytes))
	}
	contentType, err := s.validateContentType(in.ContentType)
	if err != nil {
		return nil, storage.PresignedUpload{}, err
	}

	file := &domain.File{
		ID:          uuid.New(),
		UserID:      userID,
		ObjectKey:   objectKey(userID, filename),
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   in.SizeBytes,
		Status:      domain.FilePending,
	}
	if err := s.files.Create(ctx, file); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, storage.PresignedUpload{}, apperr.Conflict("FILE_ALREADY_EXISTS", "a file with this name already exists")
		}
		return nil, storage.PresignedUpload{}, apperr.Internal(err)
	}

	upload, err := s.store.PresignUpload(ctx, file.ObjectKey, contentType, s.cfg.PresignUploadTTL)
	if err != nil {
		// The pending row is useless without a URL; remove it best-effort.
		_ = s.files.Delete(ctx, file.ID)
		return nil, storage.PresignedUpload{}, apperr.Internal(err)
	}
	s.observeUpload("initiated")
	return file, upload, nil
}

func (s *FileService) CompleteUpload(ctx context.Context, userID, fileID uuid.UUID) (*domain.File, error) {
	file, err := s.ownedFile(ctx, userID, fileID)
	if err != nil {
		return nil, err
	}
	if file.Status == domain.FileActive {
		return file, nil // idempotent retry
	}

	info, err := s.store.Stat(ctx, file.ObjectKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			s.observeUpload("missing")
			return nil, apperr.Conflict("UPLOAD_NOT_FOUND", "the object was not uploaded")
		}
		return nil, apperr.Internal(err)
	}
	if info.Size <= 0 {
		_ = s.store.Delete(ctx, file.ObjectKey)
		s.observeUpload("empty")
		return nil, apperr.Conflict("EMPTY_UPLOAD", "uploaded object is empty")
	}
	if info.Size > s.cfg.MaxUploadBytes {
		_ = s.store.Delete(ctx, file.ObjectKey)
		s.observeUpload("too_large")
		return nil, apperr.PayloadTooLarge(fmt.Sprintf("file exceeds the %d byte limit", s.cfg.MaxUploadBytes))
	}

	actualType := file.ContentType
	if info.ContentType != "" {
		if parsed, parseErr := normalizeContentType(info.ContentType); parseErr == nil {
			actualType = parsed
		}
	}
	if _, ok := s.cfg.AllowedMimeTypes[actualType]; !ok {
		_ = s.store.Delete(ctx, file.ObjectKey)
		s.observeUpload("rejected_type")
		return nil, apperr.UnsupportedMedia("uploaded object has a disallowed content type").
			WithDetail("content_type", actualType+" is not in the allowed list")
	}

	completed := s.now()
	if err := s.files.Complete(ctx, file.ID, info.Size, actualType, completed); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, apperr.NotFound("file")
		}
		return nil, apperr.Internal(err)
	}

	file.Status = domain.FileActive
	file.SizeBytes = info.Size
	file.ContentType = actualType
	file.CompletedAt = &completed
	s.observeUpload("completed")
	return file, nil
}

func (s *FileService) List(ctx context.Context, userID uuid.UUID, cursor string, limit int) (FilePage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	decoded, err := DecodeCursor(cursor)
	if err != nil {
		return FilePage{}, err
	}
	var repoCursor *repository.FileCursor
	if decoded != nil {
		repoCursor = &repository.FileCursor{CreatedAt: decoded.CreatedAt, ID: decoded.ID}
	}

	files, err := s.files.ListByUser(ctx, userID, repoCursor, limit)
	if err != nil {
		return FilePage{}, apperr.Internal(err)
	}
	page := FilePage{Items: files}
	if len(files) == limit {
		last := files[len(files)-1]
		page.NextCursor = EncodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return page, nil
}

func (s *FileService) Get(ctx context.Context, userID, fileID uuid.UUID) (*domain.File, string, error) {
	file, err := s.ownedFile(ctx, userID, fileID)
	if err != nil {
		return nil, "", err
	}
	if file.Status != domain.FileActive {
		return nil, "", apperr.Conflict("UPLOAD_NOT_COMPLETED", "file upload has not been completed")
	}
	url, err := s.store.PresignDownload(ctx, file.ObjectKey, s.cfg.PresignDownloadTTL)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	return file, url, nil
}

func (s *FileService) Download(ctx context.Context, userID, fileID uuid.UUID) (*domain.File, string, error) {
	return s.Get(ctx, userID, fileID)
}

func (s *FileService) Delete(ctx context.Context, userID, fileID uuid.UUID) error {
	file, err := s.ownedFile(ctx, userID, fileID)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, file.ObjectKey); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		return apperr.Internal(err)
	}
	if err := s.files.Delete(ctx, file.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return apperr.NotFound("file")
		}
		return apperr.Internal(err)
	}
	return nil
}

// UploadLegacy keeps the original server-mediated multipart flow working.
func (s *FileService) UploadLegacy(ctx context.Context, userID uuid.UUID, filename, contentType string, size int64, r io.Reader) (*domain.File, error) {
	filename, err := SafeFilename(filename)
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, apperr.Validation("file is empty")
	}
	if size > s.cfg.MaxUploadBytes {
		return nil, apperr.PayloadTooLarge(fmt.Sprintf("file exceeds the %d byte limit", s.cfg.MaxUploadBytes))
	}
	contentType, err = s.validateContentType(contentType)
	if err != nil {
		return nil, err
	}

	file := &domain.File{
		ID:          uuid.New(),
		UserID:      userID,
		ObjectKey:   objectKey(userID, filename),
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   size,
		Status:      domain.FilePending,
	}
	if err := s.store.Put(ctx, file.ObjectKey, contentType, r, size); err != nil {
		s.observeUpload("failed")
		return nil, apperr.Internal(err)
	}

	completed := s.now()
	file.Status = domain.FileActive
	file.CompletedAt = &completed
	if err := s.files.Create(ctx, file); err != nil {
		_ = s.store.Delete(ctx, file.ObjectKey)
		if errors.Is(err, repository.ErrConflict) {
			return nil, apperr.Conflict("FILE_ALREADY_EXISTS", "a file with this name already exists")
		}
		return nil, apperr.Internal(err)
	}
	s.observeUpload("completed")
	return file, nil
}

func (s *FileService) FindByFilename(ctx context.Context, userID uuid.UUID, filename string) (*domain.File, error) {
	file, err := s.files.FindByUserAndFilename(ctx, userID, filename)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, apperr.NotFound("file")
		}
		return nil, apperr.Internal(err)
	}
	return file, nil
}

func (s *FileService) Open(ctx context.Context, file *domain.File) (io.ReadCloser, storage.ObjectInfo, error) {
	reader, info, err := s.store.Get(ctx, file.ObjectKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return nil, storage.ObjectInfo{}, apperr.NotFound("file")
		}
		return nil, storage.ObjectInfo{}, apperr.Internal(err)
	}
	return reader, info, nil
}

func (s *FileService) CleanupPending(ctx context.Context) (int, error) {
	cutoff := s.now().Add(-s.cfg.PendingUploadTTL)
	deleted := 0
	for {
		files, err := s.files.ListPendingBefore(ctx, cutoff, 100)
		if err != nil {
			return deleted, err
		}
		if len(files) == 0 {
			return deleted, nil
		}
		failed := false
		for _, file := range files {
			if err := s.store.Delete(ctx, file.ObjectKey); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
				s.log.Warn("failed to delete pending object", slog.String("error", err.Error()), slog.String("object_key", file.ObjectKey))
				failed = true
				continue
			}
			if err := s.files.Delete(ctx, file.ID); err != nil && !errors.Is(err, repository.ErrNotFound) {
				s.log.Warn("failed to delete pending row", slog.String("error", err.Error()), slog.String("file_id", file.ID.String()))
				failed = true
				continue
			}
			deleted++
		}
		// A failed row would otherwise be selected again forever.
		if failed || len(files) < 100 {
			return deleted, nil
		}
	}
}

func (s *FileService) ownedFile(ctx context.Context, userID, fileID uuid.UUID) (*domain.File, error) {
	file, err := s.files.FindByID(ctx, fileID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, apperr.NotFound("file")
		}
		return nil, apperr.Internal(err)
	}
	if file.UserID != userID {
		// Do not reveal that another user's file exists.
		return nil, apperr.NotFound("file")
	}
	return file, nil
}

func (s *FileService) observeUpload(status string) {
	if s.metrics != nil {
		s.metrics.ObserveUpload(status)
	}
}

func objectKey(userID uuid.UUID, filename string) string {
	return fmt.Sprintf("users/%s/%s/%s", userID, uuid.NewString(), sanitizeKeyPart(filename))
}

func sanitizeKeyPart(name string) string {
	// Filenames are already validated; this is a last line of defence for keys.
	return strings.ReplaceAll(strings.ReplaceAll(name, "/", "_"), `\`, "_")
}

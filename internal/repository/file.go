package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/xshubhamg/file-upload/internal/domain"
)

// FileCursor is a stable keyset-pagination cursor: created_at DESC, id DESC.
type FileCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type FileRepository interface {
	Create(ctx context.Context, file *domain.File) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.File, error)
	FindByUserAndFilename(ctx context.Context, userID uuid.UUID, filename string) (*domain.File, error)
	ListByUser(ctx context.Context, userID uuid.UUID, cursor *FileCursor, limit int) ([]*domain.File, error)
	Complete(ctx context.Context, id uuid.UUID, size int64, contentType string, completedAt time.Time) error
	Delete(ctx context.Context, id uuid.UUID) error
	ListPendingBefore(ctx context.Context, before time.Time, limit int) ([]*domain.File, error)
}

type fileRepository struct {
	db *gorm.DB
}

func NewFileRepository(db *gorm.DB) FileRepository {
	return &fileRepository{db: db}
}

func (r *fileRepository) Create(ctx context.Context, file *domain.File) error {
	if file.ID == uuid.Nil {
		file.ID = uuid.New()
	}
	if err := r.db.WithContext(ctx).Create(file).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (r *fileRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.File, error) {
	var file domain.File
	if err := r.db.WithContext(ctx).First(&file, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &file, nil
}

func (r *fileRepository) FindByUserAndFilename(ctx context.Context, userID uuid.UUID, filename string) (*domain.File, error) {
	var file domain.File
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND lower(filename) = ?", userID, strings.ToLower(filename)).
		First(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *fileRepository) ListByUser(ctx context.Context, userID uuid.UUID, cursor *FileCursor, limit int) ([]*domain.File, error) {
	query := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ?", userID, domain.FileActive)
	if cursor != nil {
		query = query.Where("(created_at, id) < (?, ?)", cursor.CreatedAt, cursor.ID)
	}

	var files []*domain.File
	if err := query.
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (r *fileRepository) Complete(ctx context.Context, id uuid.UUID, size int64, contentType string, completedAt time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&domain.File{}).
		Where("id = ? AND status = ?", id, domain.FilePending).
		Updates(map[string]any{
			"status":       domain.FileActive,
			"size_bytes":   size,
			"content_type": contentType,
			"completed_at": completedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// The row may already be active (idempotent complete) or may not exist.
		var count int64
		if err := r.db.WithContext(ctx).Model(&domain.File{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (r *fileRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Delete(&domain.File{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *fileRepository) ListPendingBefore(ctx context.Context, before time.Time, limit int) ([]*domain.File, error) {
	var files []*domain.File
	if err := r.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", domain.FilePending, before).
		Order("created_at ASC").
		Limit(limit).
		Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

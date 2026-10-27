package handler

import (
	"time"

	"github.com/xshubhamg/file-upload/internal/domain"
)

type UserDTO struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func NewUserDTO(user *domain.User) UserDTO {
	return UserDTO{
		ID:        user.ID.String(),
		Username:  user.Username,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	}
}

type FileDTO struct {
	ID          string     `json:"id"`
	Filename    string     `json:"filename"`
	ContentType string     `json:"content_type"`
	SizeBytes   int64      `json:"size_bytes"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

func NewFileDTO(file *domain.File) FileDTO {
	return FileDTO{
		ID:          file.ID.String(),
		Filename:    file.Filename,
		ContentType: file.ContentType,
		SizeBytes:   file.SizeBytes,
		Status:      string(file.Status),
		CreatedAt:   file.CreatedAt,
		CompletedAt: file.CompletedAt,
	}
}

type legacyFileDTO struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Path        string    `json:"path"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newLegacyFileDTO(file *domain.File) legacyFileDTO {
	return legacyFileDTO{
		ID:          file.ID.String(),
		UserID:      file.UserID.String(),
		Path:        file.ObjectKey,
		Filename:    file.Filename,
		Size:        file.SizeBytes,
		ContentType: file.ContentType,
		CreatedAt:   file.CreatedAt,
		UpdatedAt:   file.UpdatedAt,
	}
}

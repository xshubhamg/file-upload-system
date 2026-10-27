// Package domain holds the persisted entities. GORM tags live here so the
// repository layer stays free of schema duplication.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	Username     string    `gorm:"not null"`
	Email        string    `gorm:"not null"`
	PasswordHash string    `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type FileStatus string

const (
	FilePending FileStatus = "pending"
	FileActive  FileStatus = "active"
)

// File is a stored object owned by exactly one user. ObjectKey is the S3 key;
// it always starts with the owner's ID so accidental cross-tenant reads are
// obvious in storage logs.
type File struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey"`
	UserID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	ObjectKey   string     `gorm:"not null;uniqueIndex"`
	Filename    string     `gorm:"not null"`
	ContentType string     `gorm:"not null"`
	SizeBytes   int64      `gorm:"not null"`
	Status      FileStatus `gorm:"not null;default:pending"`
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RefreshToken is one rotation step in a session family. Only the SHA-256 hash
// of the opaque token is stored, never the token itself.
type RefreshToken struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID     uuid.UUID `gorm:"type:uuid;not null;index"`
	TokenHash  string    `gorm:"not null;uniqueIndex"`
	FamilyID   uuid.UUID `gorm:"type:uuid;not null;index"`
	UserAgent  string
	IPAddress  string
	ExpiresAt  time.Time `gorm:"not null;index"`
	RevokedAt  *time.Time
	ReplacedBy *uuid.UUID `gorm:"type:uuid"`
	CreatedAt  time.Time
}

func (RefreshToken) TableName() string { return "refresh_tokens" }

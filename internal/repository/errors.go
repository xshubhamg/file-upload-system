// Package repository defines persistence interfaces and their GORM/Postgres
// implementations. Services depend on the interfaces, not on GORM.
package repository

import "errors"

var (
	ErrNotFound            = errors.New("record not found")
	ErrConflict            = errors.New("record already exists")
	ErrTokenAlreadyRotated = errors.New("refresh token was already rotated")
)

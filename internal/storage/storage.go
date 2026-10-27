// Package storage abstracts object storage so services never talk to S3
// directly and tests can use an in-memory implementation.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrObjectNotFound = errors.New("object not found")

type PresignedUpload struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type ObjectInfo struct {
	Key         string
	Size        int64
	ContentType string
	ETag        string
}

type Store interface {
	EnsureBucket(ctx context.Context) error
	Healthy(ctx context.Context) error

	PresignUpload(ctx context.Context, key, contentType string, ttl time.Duration) (PresignedUpload, error)
	PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error)

	Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

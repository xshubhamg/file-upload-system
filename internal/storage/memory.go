package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// Memory is a concurrency-safe Store for tests and local development without
// object storage. It is intentionally not used in production.
type Memory struct {
	mu      sync.RWMutex
	objects map[string]memoryObject
}

type memoryObject struct {
	data        []byte
	contentType string
	etag        string
}

func NewMemory() *Memory {
	return &Memory{objects: make(map[string]memoryObject)}
}

func (m *Memory) EnsureBucket(context.Context) error { return nil }
func (m *Memory) Healthy(context.Context) error      { return nil }

func (m *Memory) PresignUpload(_ context.Context, key, contentType string, ttl time.Duration) (PresignedUpload, error) {
	return PresignedUpload{
		URL:       fmt.Sprintf("memory://local/%s", key),
		Method:    "PUT",
		Headers:   map[string]string{"Content-Type": contentType},
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, nil
}

func (m *Memory) PresignDownload(_ context.Context, key string, _ time.Duration) (string, error) {
	return fmt.Sprintf("memory://local/%s", key), nil
}

func (m *Memory) Put(_ context.Context, key, contentType string, r io.Reader, size int64) error {
	data, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("size mismatch: expected %d bytes, received %d", size, len(data))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memoryObject{
		data:        data,
		contentType: contentType,
		etag:        fmt.Sprintf("%d-%d", len(data), time.Now().UnixNano()),
	}
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	object, ok := m.objects[key]
	if !ok {
		return nil, ObjectInfo{}, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(object.data)), ObjectInfo{
		Key:         key,
		Size:        int64(len(object.data)),
		ContentType: object.contentType,
		ETag:        object.etag,
	}, nil
}

func (m *Memory) Stat(_ context.Context, key string) (ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	object, ok := m.objects[key]
	if !ok {
		return ObjectInfo{}, ErrObjectNotFound
	}
	return ObjectInfo{
		Key:         key,
		Size:        int64(len(object.data)),
		ContentType: object.contentType,
		ETag:        object.etag,
	}, nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

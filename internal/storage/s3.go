package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/observability"
)

// S3 is the production Store. It keeps two clients: one that talks to the
// internal endpoint (Put/Stat/Delete) and one that signs URLs for the public
// endpoint. In Docker the internal endpoint is "s3mock:9090" while browsers
// must reach "localhost:9091"; signatures are host-specific, so presigning
// must use the public host.
type S3 struct {
	bucket     string
	client     *minio.Client
	presign    *minio.Client
	log        *slog.Logger
	metrics    *observability.Metrics
	autoCreate bool
}

func NewS3(cfg config.Config, log *slog.Logger, metrics *observability.Metrics) (*S3, error) {
	creds := credentials.NewStaticV4(cfg.S3AccessKey, cfg.S3SecretKey, "")

	// Path-style addressing works with AWS S3, MinIO, S3Mock, and most
	// self-hosted gateways. Virtual-hosted buckets require wildcard DNS.
	internal, err := minio.New(cfg.S3Endpoint, &minio.Options{
		Creds:        creds,
		Secure:       cfg.S3UseSSL,
		Region:       cfg.S3Region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}

	presign, err := minio.New(cfg.S3PublicEndpoint, &minio.Options{
		Creds:        creds,
		Secure:       cfg.S3PublicUseSSL,
		Region:       cfg.S3Region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 presign client: %w", err)
	}

	return &S3{
		bucket:     cfg.S3Bucket,
		client:     internal,
		presign:    presign,
		log:        log.With(slog.String("component", "storage")),
		metrics:    metrics,
		autoCreate: cfg.S3AutoCreateBucket,
	}, nil
}

func (s *S3) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		s.observe("ensure_bucket", err)
		return fmt.Errorf("check bucket %q: %w", s.bucket, err)
	}
	if exists {
		return nil
	}
	if !s.autoCreate {
		err := fmt.Errorf("bucket %q does not exist", s.bucket)
		s.observe("ensure_bucket", err)
		return err
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		s.observe("ensure_bucket", err)
		return fmt.Errorf("create bucket %q: %w", s.bucket, err)
	}
	s.log.Info("created object storage bucket", slog.String("bucket", s.bucket))
	return nil
}

func (s *S3) Healthy(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("bucket %q does not exist", s.bucket)
	}
	return nil
}

func (s *S3) PresignUpload(ctx context.Context, key, contentType string, ttl time.Duration) (PresignedUpload, error) {
	url, err := s.presign.PresignedPutObject(ctx, s.bucket, key, ttl)
	if err != nil {
		s.observe("presign_upload", err)
		return PresignedUpload{}, fmt.Errorf("presign upload: %w", err)
	}
	return PresignedUpload{
		URL:       url.String(),
		Method:    "PUT",
		Headers:   map[string]string{"Content-Type": contentType},
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, nil
}

func (s *S3) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	url, err := s.presign.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		s.observe("presign_download", err)
		return "", fmt.Errorf("presign download: %w", err)
	}
	return url.String(), nil
}

func (s *S3) Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	s.observe("put", err)
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		s.observe("get", err)
		return nil, ObjectInfo{}, mapError(err)
	}
	stat, err := object.Stat()
	if err != nil {
		_ = object.Close()
		s.observe("get", err)
		return nil, ObjectInfo{}, mapError(err)
	}
	return object, ObjectInfo{
		Key:         key,
		Size:        stat.Size,
		ContentType: stat.ContentType,
		ETag:        stat.ETag,
	}, nil
}

func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	s.observe("stat", err)
	if err != nil {
		return ObjectInfo{}, mapError(err)
	}
	return ObjectInfo{
		Key:         key,
		Size:        stat.Size,
		ContentType: stat.ContentType,
		ETag:        stat.ETag,
	}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	s.observe("delete", err)
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (s *S3) observe(operation string, err error) {
	if s.metrics != nil {
		s.metrics.ObserveStorage(operation, err)
	}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	var response minio.ErrorResponse
	if errors.As(err, &response) {
		switch response.Code {
		case "NoSuchKey", "NotFound", "NoSuchBucket":
			return ErrObjectNotFound
		}
	}
	return err
}

// ensure the object returned by Get is closed even if callers forget.
var _ io.ReadCloser = (*minio.Object)(nil)

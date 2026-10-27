package service

import (
	"mime"
	"path/filepath"
	"strings"

	"github.com/xshubhamg/file-upload/internal/apperr"
)

// SafeFilename strips directory components and rejects anything that could
// escape the per-user object prefix. The returned name never contains a path
// separator on any supported platform.
func SafeFilename(raw string) (string, error) {
	name := filepath.Base(strings.TrimSpace(raw))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "", apperr.Validation("invalid filename").
			WithDetail("filename", "a filename is required")
	}
	if strings.ContainsAny(name, `/\`) {
		return "", apperr.Validation("invalid filename").
			WithDetail("filename", "filename must not contain path separators")
	}
	if len(name) > 255 {
		return "", apperr.Validation("filename is too long").
			WithDetail("filename", "must be at most 255 characters")
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return "", apperr.Validation("invalid filename").
				WithDetail("filename", "filename contains control characters")
		}
	}
	return name, nil
}

func normalizeContentType(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return "", apperr.UnsupportedMedia("content_type is required").
			WithDetail("content_type", "a MIME type is required")
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", apperr.UnsupportedMedia("invalid content_type").
			WithDetail("content_type", "must be a valid MIME type")
	}
	return strings.ToLower(mediaType), nil
}

func (s *FileService) validateContentType(raw string) (string, error) {
	contentType, err := normalizeContentType(raw)
	if err != nil {
		return "", err
	}
	if _, ok := s.cfg.AllowedMimeTypes[contentType]; !ok {
		return "", apperr.UnsupportedMedia("content type is not allowed").
			WithDetail("content_type", contentType+" is not in the allowed list")
	}
	return contentType, nil
}

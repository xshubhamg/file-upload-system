package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// UploadDir is the root for all uploaded files.
	UploadDir = "./uploads"
	// MaxUploadBytes caps a single upload at 100MB.
	MaxUploadBytes = 100 * 1024 * 1024
)

// UserDir returns the per-user storage directory keyed by user ID
// (not username) so special characters in usernames can't escape the dir.
func UserDir(userID uint) string {
	return filepath.Join(UploadDir, strconv.FormatUint(uint64(userID), 10))
}

// EnsureUserDir creates the per-user directory if needed.
func EnsureUserDir(userID uint) (string, error) {
	dir := UserDir(userID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// SafeFilename strips any directory components and rejects
// empty, ".", "..", or names still containing separators.
func SafeFilename(raw string) (string, bool) {
	name := filepath.Base(raw)
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	if strings.ContainsAny(name, `/\`) {
		return "", false
	}
	return name, true
}

// FileURL builds the public API URL for a stored filename.
func FileURL(filename string) string {
	return fmt.Sprintf("/api/file/%s", filename)
}

// RemoveFileBestEffort deletes a file, ignoring the error.
// Used to clean up orphaned uploads after a DB failure.
func RemoveFileBestEffort(path string) {
	_ = os.Remove(path)
}

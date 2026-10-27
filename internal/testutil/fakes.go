// Package testutil provides in-memory repository fakes for unit and HTTP tests.
// It is never imported by production code.
package testutil

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/xshubhamg/file-upload/internal/domain"
	"github.com/xshubhamg/file-upload/internal/repository"
)

type UserRepo struct {
	mu    sync.Mutex
	users map[uuid.UUID]*domain.User
}

func NewUserRepo() *UserRepo {
	return &UserRepo{users: make(map[uuid.UUID]*domain.User)}
}

func (r *UserRepo) Create(_ context.Context, user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.users {
		if strings.EqualFold(existing.Username, user.Username) || strings.EqualFold(existing.Email, user.Email) {
			return repository.ErrConflict
		}
	}
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	r.users[user.ID] = copyUser(user)
	return nil
}

func (r *UserRepo) FindByLogin(_ context.Context, identifier string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if strings.EqualFold(user.Username, identifier) || strings.EqualFold(user.Email, identifier) {
			return copyUser(user), nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *UserRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.users[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return copyUser(user), nil
}

func (r *UserRepo) ExistsByUsernameOrEmail(_ context.Context, username, email string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if strings.EqualFold(user.Username, username) || strings.EqualFold(user.Email, email) {
			return true, nil
		}
	}
	return false, nil
}

func copyUser(user *domain.User) *domain.User {
	copy := *user
	return &copy
}

type FileRepo struct {
	mu    sync.Mutex
	files map[uuid.UUID]*domain.File
}

func NewFileRepo() *FileRepo {
	return &FileRepo{files: make(map[uuid.UUID]*domain.File)}
}

func (r *FileRepo) Create(_ context.Context, file *domain.File) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.files {
		if existing.UserID == file.UserID && strings.EqualFold(existing.Filename, file.Filename) {
			return repository.ErrConflict
		}
	}
	if file.ID == uuid.Nil {
		file.ID = uuid.New()
	}
	now := time.Now().UTC()
	if file.CreatedAt.IsZero() {
		file.CreatedAt = now
	}
	file.UpdatedAt = now
	if file.Status == "" {
		file.Status = domain.FilePending
	}
	r.files[file.ID] = copyFile(file)
	return nil
}

func (r *FileRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	file, ok := r.files[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return copyFile(file), nil
}

func (r *FileRepo) FindByUserAndFilename(_ context.Context, userID uuid.UUID, filename string) (*domain.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, file := range r.files {
		if file.UserID == userID && strings.EqualFold(file.Filename, filename) {
			return copyFile(file), nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *FileRepo) ListByUser(_ context.Context, userID uuid.UUID, cursor *repository.FileCursor, limit int) ([]*domain.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []*domain.File
	for _, file := range r.files {
		if file.UserID != userID || file.Status != domain.FileActive {
			continue
		}
		if cursor != nil {
			afterCursor := file.CreatedAt.Before(cursor.CreatedAt) ||
				(file.CreatedAt.Equal(cursor.CreatedAt) && file.ID.String() < cursor.ID.String())
			if !afterCursor {
				continue
			}
		}
		matched = append(matched, copyFile(file))
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].ID.String() > matched[j].ID.String()
		}
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *FileRepo) Complete(_ context.Context, id uuid.UUID, size int64, contentType string, completedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	file, ok := r.files[id]
	if !ok {
		return repository.ErrNotFound
	}
	if file.Status == domain.FileActive {
		return nil
	}
	file.Status = domain.FileActive
	file.SizeBytes = size
	file.ContentType = contentType
	file.CompletedAt = &completedAt
	file.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *FileRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.files[id]; !ok {
		return repository.ErrNotFound
	}
	delete(r.files, id)
	return nil
}

func (r *FileRepo) ListPendingBefore(_ context.Context, before time.Time, limit int) ([]*domain.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []*domain.File
	for _, file := range r.files {
		if file.Status == domain.FilePending && file.CreatedAt.Before(before) {
			matched = append(matched, copyFile(file))
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CreatedAt.Before(matched[j].CreatedAt) })
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (r *FileRepo) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.files)
}

func copyFile(file *domain.File) *domain.File {
	copy := *file
	if file.CompletedAt != nil {
		completed := *file.CompletedAt
		copy.CompletedAt = &completed
	}
	return &copy
}

type RefreshRepo struct {
	mu     sync.Mutex
	tokens map[uuid.UUID]*domain.RefreshToken
	byHash map[string]uuid.UUID
}

func NewRefreshRepo() *RefreshRepo {
	return &RefreshRepo{
		tokens: make(map[uuid.UUID]*domain.RefreshToken),
		byHash: make(map[string]uuid.UUID),
	}
}

func (r *RefreshRepo) Create(_ context.Context, token *domain.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createLocked(token)
}

func (r *RefreshRepo) createLocked(token *domain.RefreshToken) error {
	if _, exists := r.byHash[token.TokenHash]; exists {
		return repository.ErrConflict
	}
	if token.ID == uuid.Nil {
		token.ID = uuid.New()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	r.tokens[token.ID] = copyRefreshToken(token)
	r.byHash[token.TokenHash] = token.ID
	return nil
}

func (r *RefreshRepo) FindByHash(_ context.Context, hash string) (*domain.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byHash[hash]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return copyRefreshToken(r.tokens[id]), nil
}

func (r *RefreshRepo) Rotate(_ context.Context, oldID uuid.UUID, next *domain.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.tokens[oldID]
	if !ok {
		return repository.ErrNotFound
	}
	if old.RevokedAt != nil {
		return repository.ErrTokenAlreadyRotated
	}
	if err := r.createLocked(next); err != nil {
		return err
	}
	revoked := next.CreatedAt
	old.RevokedAt = &revoked
	old.ReplacedBy = &next.ID
	return nil
}

func (r *RefreshRepo) Revoke(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if token, ok := r.tokens[id]; ok && token.RevokedAt == nil {
		now := time.Now().UTC()
		token.RevokedAt = &now
	}
	return nil
}

func (r *RefreshRepo) RevokeFamily(_ context.Context, familyID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	for _, token := range r.tokens {
		if token.FamilyID == familyID && token.RevokedAt == nil {
			token.RevokedAt = &now
		}
	}
	return nil
}

func (r *RefreshRepo) RevokeAllForUser(_ context.Context, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	for _, token := range r.tokens {
		if token.UserID == userID && token.RevokedAt == nil {
			token.RevokedAt = &now
		}
	}
	return nil
}

func (r *RefreshRepo) DeleteStale(_ context.Context, before time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var deleted int64
	for id, token := range r.tokens {
		expired := token.ExpiresAt.Before(before)
		revoked := token.RevokedAt != nil && token.RevokedAt.Before(before)
		if expired || revoked {
			delete(r.tokens, id)
			delete(r.byHash, token.TokenHash)
			deleted++
		}
	}
	return deleted, nil
}

func (r *RefreshRepo) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.tokens)
}

func copyRefreshToken(token *domain.RefreshToken) *domain.RefreshToken {
	copy := *token
	if token.RevokedAt != nil {
		revoked := *token.RevokedAt
		copy.RevokedAt = &revoked
	}
	if token.ReplacedBy != nil {
		replaced := *token.ReplacedBy
		copy.ReplacedBy = &replaced
	}
	return &copy
}

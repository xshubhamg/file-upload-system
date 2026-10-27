package repository_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/database"
	"github.com/xshubhamg/file-upload/internal/domain"
	"github.com/xshubhamg/file-upload/internal/repository"
	"github.com/xshubhamg/file-upload/internal/testutil"
)

func integrationConfig(t *testing.T) config.Config {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping Postgres integration test")
	}
	cfg := testutil.Config()
	cfg.DatabaseURL = dsn
	return cfg
}

func TestPostgresRepositories(t *testing.T) {
	cfg := integrationConfig(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := database.Migrate(cfg.DatabaseURL, log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, sqlDB, err := database.Open(ctx, cfg, log)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if _, err := sqlDB.ExecContext(ctx, "TRUNCATE TABLE refresh_tokens, files, users CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	users := repository.NewUserRepository(db)
	files := repository.NewFileRepository(db)
	tokens := repository.NewRefreshTokenRepository(db)

	user := &domain.User{
		ID:           uuid.New(),
		Username:     "repo_user",
		Email:        "repo@example.com",
		PasswordHash: "not-a-real-hash",
	}
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := users.FindByLogin(ctx, "REPO_USER"); err != nil {
		t.Fatalf("find user by case-insensitive username: %v", err)
	}
	if _, err := users.FindByID(ctx, user.ID); err != nil {
		t.Fatalf("find user by id: %v", err)
	}
	duplicate := &domain.User{ID: uuid.New(), Username: "REPO_USER", Email: "other@example.com", PasswordHash: "x"}
	if err := users.Create(ctx, duplicate); err == nil {
		t.Fatal("expected duplicate username to be rejected")
	} else if err != repository.ErrConflict {
		t.Fatalf("duplicate error = %v, want ErrConflict", err)
	}

	now := time.Now().UTC()
	pending := &domain.File{
		ID:          uuid.New(),
		UserID:      user.ID,
		ObjectKey:   "users/" + user.ID.String() + "/" + uuid.NewString() + "/a.txt",
		Filename:    "a.txt",
		ContentType: "text/plain",
		SizeBytes:   5,
		Status:      domain.FilePending,
		CreatedAt:   now,
	}
	if err := files.Create(ctx, pending); err != nil {
		t.Fatalf("create pending file: %v", err)
	}
	if err := files.Create(ctx, &domain.File{
		ID: uuid.New(), UserID: user.ID, ObjectKey: "users/x/dup/a.txt", Filename: "A.TXT",
		ContentType: "text/plain", SizeBytes: 5, Status: domain.FilePending,
	}); err == nil {
		t.Fatal("expected duplicate filename to be rejected")
	}

	list, err := files.ListByUser(ctx, user.ID, nil, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("pending files must not be listed: %d", len(list))
	}

	if err := files.Complete(ctx, pending.ID, 5, "text/plain", now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	list, err = files.ListByUser(ctx, user.ID, nil, 10)
	if err != nil || len(list) != 1 || list[0].Status != domain.FileActive {
		t.Fatalf("unexpected active list: %+v, err=%v", list, err)
	}

	older := &domain.File{
		ID: uuid.New(), UserID: user.ID, ObjectKey: "users/x/old/b.txt", Filename: "b.txt",
		ContentType: "text/plain", SizeBytes: 3, Status: domain.FileActive,
		CreatedAt: now.Add(-time.Hour), CompletedAt: &now,
	}
	if err := files.Create(ctx, older); err != nil {
		t.Fatalf("create older file: %v", err)
	}
	page, err := files.ListByUser(ctx, user.ID, nil, 1)
	if err != nil || len(page) != 1 || page[0].ID != pending.ID {
		t.Fatalf("first page: %+v, err=%v", page, err)
	}
	next, err := files.ListByUser(ctx, user.ID, &repository.FileCursor{CreatedAt: page[0].CreatedAt, ID: page[0].ID}, 1)
	if err != nil || len(next) != 1 || next[0].ID != older.ID {
		t.Fatalf("second page: %+v, err=%v", next, err)
	}

	family := uuid.New()
	first := &domain.RefreshToken{
		ID: uuid.New(), UserID: user.ID, TokenHash: "hash-1", FamilyID: family,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := tokens.Create(ctx, first); err != nil {
		t.Fatalf("create refresh token: %v", err)
	}
	second := &domain.RefreshToken{
		ID: uuid.New(), UserID: user.ID, TokenHash: "hash-2", FamilyID: family,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Second),
	}
	if err := tokens.Rotate(ctx, first.ID, second); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := tokens.Rotate(ctx, first.ID, &domain.RefreshToken{
		ID: uuid.New(), UserID: user.ID, TokenHash: "hash-3", FamilyID: family,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(2 * time.Second),
	}); err != repository.ErrTokenAlreadyRotated {
		t.Fatalf("second rotate error = %v, want ErrTokenAlreadyRotated", err)
	}

	stored, err := tokens.FindByHash(ctx, "hash-1")
	if err != nil || stored.RevokedAt == nil {
		t.Fatalf("old token should be revoked: %+v, err=%v", stored, err)
	}
	if err := tokens.RevokeFamily(ctx, family); err != nil {
		t.Fatalf("revoke family: %v", err)
	}
	stored, err = tokens.FindByHash(ctx, "hash-2")
	if err != nil || stored.RevokedAt == nil {
		t.Fatalf("family token should be revoked: %+v, err=%v", stored, err)
	}

	if _, err := tokens.DeleteStale(ctx, now.Add(48*time.Hour)); err != nil {
		t.Fatalf("delete stale: %v", err)
	}
}

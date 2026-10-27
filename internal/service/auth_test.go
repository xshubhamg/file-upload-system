package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/xshubhamg/file-upload/internal/apperr"
	"github.com/xshubhamg/file-upload/internal/service"
	"github.com/xshubhamg/file-upload/internal/testutil"
)

func newAuthFixture() (*service.AuthService, *testutil.UserRepo, *testutil.RefreshRepo, service.SessionMeta) {
	cfg := testutil.Config()
	users := testutil.NewUserRepo()
	tokens := testutil.NewRefreshRepo()
	return service.NewAuthService(users, tokens, cfg), users, tokens, service.SessionMeta{
		UserAgent: "go-test",
		IPAddress: "127.0.0.1",
	}
}

func TestRegisterAndLogin(t *testing.T) {
	auth, _, _, meta := newAuthFixture()
	ctx := context.Background()

	user, err := auth.Register(ctx, service.RegisterInput{
		Username: "Alice",
		Email:    "Alice@Example.com",
		Password: "correct horse battery",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.Username != "alice" || user.Email != "alice@example.com" {
		t.Fatalf("normalization failed: %+v", user)
	}
	if user.PasswordHash == "" || user.PasswordHash == "correct horse battery" {
		t.Fatal("password was not hashed")
	}

	if _, err := auth.Register(ctx, service.RegisterInput{
		Username: "ALICE",
		Email:    "other@example.com",
		Password: "correct horse battery",
	}); err == nil || apperr.From(err).Code != "USERNAME_OR_EMAIL_TAKEN" {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}

	loggedIn, pair, err := auth.Login(ctx, "alice", "correct horse battery", meta)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loggedIn.ID != user.ID {
		t.Fatalf("login returned wrong user")
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("login did not return a token pair")
	}
	if pair.TokenType != "Bearer" || pair.ExpiresIn <= 0 || pair.RefreshExpiresIn <= 0 {
		t.Fatalf("unexpected token metadata: %+v", pair)
	}

	userID, err := auth.Authenticate(pair.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if userID != user.ID {
		t.Fatalf("access token subject = %s, want %s", userID, user.ID)
	}

	if _, err := auth.Authenticate("not-a-token"); err == nil || apperr.From(err).Code != "INVALID_ACCESS_TOKEN" {
		t.Fatalf("expected invalid access token, got %v", err)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	auth, _, _, meta := newAuthFixture()
	ctx := context.Background()

	if _, _, err := auth.Login(ctx, "missing", "some-password", meta); err == nil || apperr.From(err).Code != "UNAUTHORIZED" {
		t.Fatalf("expected generic unauthorized, got %v", err)
	}

	if _, err := auth.Register(ctx, service.RegisterInput{
		Username: "bob",
		Email:    "bob@example.com",
		Password: "password123",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, _, err := auth.Login(ctx, "bob", "wrong-password", meta); err == nil || apperr.From(err).Code != "UNAUTHORIZED" {
		t.Fatalf("expected generic unauthorized, got %v", err)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	auth, _, tokens, meta := newAuthFixture()
	ctx := context.Background()

	if _, err := auth.Register(ctx, service.RegisterInput{
		Username: "carol",
		Email:    "carol@example.com",
		Password: "password123",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, first, err := auth.Login(ctx, "carol", "password123", meta)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	second, err := auth.Refresh(ctx, first.RefreshToken, meta)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}

	// Replaying the consumed token must burn the whole family.
	if _, err := auth.Refresh(ctx, first.RefreshToken, meta); err == nil || apperr.From(err).Code != "TOKEN_REUSE_DETECTED" {
		t.Fatalf("expected reuse detection, got %v", err)
	}
	if _, err := auth.Refresh(ctx, second.RefreshToken, meta); err == nil || apperr.From(err).Code != "TOKEN_REUSE_DETECTED" {
		t.Fatalf("expected family revocation, got %v", err)
	}

	// The tokens are retained for forensics, but none remain usable.
	if tokens.Count() == 0 {
		t.Fatal("expected token rows to be retained")
	}
}

func TestExpiredRefreshToken(t *testing.T) {
	auth, _, _, meta := newAuthFixture()
	ctx := context.Background()

	if _, err := auth.Register(ctx, service.RegisterInput{
		Username: "dave",
		Email:    "dave@example.com",
		Password: "password123",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	now := time.Now().UTC()
	auth.SetClock(func() time.Time { return now })
	_, pair, err := auth.Login(ctx, "dave", "password123", meta)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	auth.SetClock(func() time.Time { return now.Add(25 * time.Hour) })
	if _, err := auth.Refresh(ctx, pair.RefreshToken, meta); err == nil || apperr.From(err).Code != "TOKEN_EXPIRED" {
		t.Fatalf("expected expired token, got %v", err)
	}
}

func TestLogoutAllRevokesSessions(t *testing.T) {
	auth, _, _, meta := newAuthFixture()
	ctx := context.Background()

	user, err := auth.Register(ctx, service.RegisterInput{
		Username: "erin",
		Email:    "erin@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	_, pair, err := auth.Login(ctx, "erin", "password123", meta)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := auth.LogoutAll(ctx, user.ID); err != nil {
		t.Fatalf("logout all: %v", err)
	}
	if _, err := auth.Refresh(ctx, pair.RefreshToken, meta); err == nil || apperr.From(err).Code != "TOKEN_REUSE_DETECTED" {
		t.Fatalf("expected revoked session, got %v", err)
	}
}

func TestCleanupTokensRemovesOldRows(t *testing.T) {
	auth, _, tokens, meta := newAuthFixture()
	ctx := context.Background()

	if _, err := auth.Register(ctx, service.RegisterInput{
		Username: "frank",
		Email:    "frank@example.com",
		Password: "password123",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	now := time.Now().UTC()
	auth.SetClock(func() time.Time { return now })
	if _, _, err := auth.Login(ctx, "frank", "password123", meta); err != nil {
		t.Fatalf("login: %v", err)
	}
	auth.SetClock(func() time.Time { return now.Add(15 * 24 * time.Hour) })
	n, err := auth.CleanupTokens(ctx)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n == 0 {
		t.Fatal("expected stale tokens to be removed")
	}
	if tokens.Count() != 0 {
		t.Fatalf("expected no tokens after cleanup, got %d", tokens.Count())
	}
}

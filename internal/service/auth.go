package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/xshubhamg/file-upload/internal/apperr"
	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/domain"
	"github.com/xshubhamg/file-upload/internal/repository"
)

const (
	TokenIssuer   = "file-upload"
	TokenAudience = "file-upload-api"

	accessTokenType = "access"
)

type SessionMeta struct {
	UserAgent string
	IPAddress string
}

type TokenPair struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

type RegisterInput struct {
	Username string
	Email    string
	Password string
}

type AuthService struct {
	users  repository.UserRepository
	tokens repository.RefreshTokenRepository
	cfg    config.Config
	now    func() time.Time
}

func NewAuthService(users repository.UserRepository, tokens repository.RefreshTokenRepository, cfg config.Config) *AuthService {
	return &AuthService{
		users:  users,
		tokens: tokens,
		cfg:    cfg,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (s *AuthService) SetClock(now func() time.Time) { s.now = now }

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*domain.User, error) {
	username, err := normalizeUsername(in.Username)
	if err != nil {
		return nil, err
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}

	exists, err := s.users.ExistsByUsernameOrEmail(ctx, username, email)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if exists {
		return nil, apperr.Conflict("USERNAME_OR_EMAIL_TAKEN", "username or email is already registered")
	}

	cost := s.cfg.BcryptCost
	if cost < bcrypt.MinCost {
		cost = bcrypt.DefaultCost
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), cost)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	user := &domain.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
	}
	if err := s.users.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, apperr.Conflict("USERNAME_OR_EMAIL_TAKEN", "username or email is already registered")
		}
		return nil, apperr.Internal(err)
	}
	return user, nil
}

func (s *AuthService) Login(ctx context.Context, identifier, password string, meta SessionMeta) (*domain.User, TokenPair, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, TokenPair{}, apperr.Validation("username or email is required").
			WithDetail("username", "provide a username or email")
	}
	if password == "" || len(password) > 72 {
		return nil, TokenPair{}, apperr.Unauthorized("invalid credentials")
	}

	user, err := s.users.FindByLogin(ctx, identifier)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, TokenPair{}, apperr.Unauthorized("invalid credentials")
		}
		return nil, TokenPair{}, apperr.Internal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		// Deliberately identical to the unknown-user path.
		return nil, TokenPair{}, apperr.Unauthorized("invalid credentials")
	}

	pair, err := s.issuePair(ctx, user.ID, uuid.New(), meta)
	if err != nil {
		return nil, TokenPair{}, err
	}
	return user, pair, nil
}

func (s *AuthService) Refresh(ctx context.Context, rawToken string, meta SessionMeta) (TokenPair, error) {
	if strings.TrimSpace(rawToken) == "" {
		return TokenPair{}, apperr.New(401, "INVALID_REFRESH_TOKEN", "a refresh token is required")
	}

	current, err := s.tokens.FindByHash(ctx, s.hashToken(rawToken))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TokenPair{}, apperr.New(401, "INVALID_REFRESH_TOKEN", "refresh token is invalid")
		}
		return TokenPair{}, apperr.Internal(err)
	}

	now := s.now()
	if current.RevokedAt != nil {
		// A previously rotated token was replayed: assume theft and burn the
		// whole family so neither the attacker nor the victim can continue.
		_ = s.tokens.RevokeFamily(ctx, current.FamilyID)
		return TokenPair{}, apperr.New(401, "TOKEN_REUSE_DETECTED", "refresh token reuse detected; session revoked")
	}
	if !current.ExpiresAt.After(now) {
		_ = s.tokens.RevokeFamily(ctx, current.FamilyID)
		return TokenPair{}, apperr.New(401, "TOKEN_EXPIRED", "refresh token has expired")
	}

	user, err := s.users.FindByID(ctx, current.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			_ = s.tokens.RevokeFamily(ctx, current.FamilyID)
			return TokenPair{}, apperr.New(401, "INVALID_REFRESH_TOKEN", "refresh token is invalid")
		}
		return TokenPair{}, apperr.Internal(err)
	}

	access, err := s.newAccessToken(user.ID, now)
	if err != nil {
		return TokenPair{}, err
	}
	nextRaw, next, err := s.newRefreshToken(user.ID, current.FamilyID, meta, now)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.tokens.Rotate(ctx, current.ID, next); err != nil {
		if errors.Is(err, repository.ErrTokenAlreadyRotated) {
			_ = s.tokens.RevokeFamily(ctx, current.FamilyID)
			return TokenPair{}, apperr.New(401, "TOKEN_REUSE_DETECTED", "refresh token reuse detected; session revoked")
		}
		return TokenPair{}, apperr.Internal(err)
	}
	return s.tokenPair(access, nextRaw), nil
}

func (s *AuthService) Logout(ctx context.Context, rawToken string) error {
	if strings.TrimSpace(rawToken) == "" {
		return nil
	}
	current, err := s.tokens.FindByHash(ctx, s.hashToken(rawToken))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // idempotent
		}
		return apperr.Internal(err)
	}
	if err := s.tokens.RevokeFamily(ctx, current.FamilyID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *AuthService) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func (s *AuthService) User(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	user, err := s.users.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, apperr.NotFound("user")
		}
		return nil, apperr.Internal(err)
	}
	return user, nil
}

func (s *AuthService) Authenticate(rawToken string) (uuid.UUID, error) {
	if strings.TrimSpace(rawToken) == "" {
		return uuid.Nil, apperr.Unauthorized("authentication required")
	}

	claims := &accessClaims{}
	token, err := jwt.ParseWithClaims(rawToken, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.cfg.AccessSecret), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(TokenIssuer),
		jwt.WithAudience(TokenAudience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil || !token.Valid {
		return uuid.Nil, apperr.New(401, "INVALID_ACCESS_TOKEN", "access token is invalid or expired")
	}
	if claims.Type != accessTokenType {
		return uuid.Nil, apperr.New(401, "INVALID_ACCESS_TOKEN", "access token is invalid or expired")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, apperr.New(401, "INVALID_ACCESS_TOKEN", "access token is invalid or expired")
	}
	return userID, nil
}

func (s *AuthService) CleanupTokens(ctx context.Context) (int64, error) {
	// Keep revoked/expired rows for a week for incident analysis, then purge.
	before := s.now().Add(-7 * 24 * time.Hour)
	return s.tokens.DeleteStale(ctx, before)
}

type accessClaims struct {
	Type string `json:"typ"`
	jwt.RegisteredClaims
}

func (s *AuthService) issuePair(ctx context.Context, userID, familyID uuid.UUID, meta SessionMeta) (TokenPair, error) {
	now := s.now()
	access, err := s.newAccessToken(userID, now)
	if err != nil {
		return TokenPair{}, err
	}
	raw, token, err := s.newRefreshToken(userID, familyID, meta, now)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.tokens.Create(ctx, token); err != nil {
		return TokenPair{}, apperr.Internal(err)
	}
	return s.tokenPair(access, raw), nil
}

func (s *AuthService) newAccessToken(userID uuid.UUID, now time.Time) (string, error) {
	claims := &accessClaims{
		Type: accessTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    TokenIssuer,
			Audience:  jwt.ClaimStrings{TokenAudience},
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.AccessTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.cfg.AccessSecret))
	if err != nil {
		return "", apperr.Internal(err)
	}
	return signed, nil
}

func (s *AuthService) newRefreshToken(userID, familyID uuid.UUID, meta SessionMeta, now time.Time) (string, *domain.RefreshToken, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, apperr.Internal(err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	return raw, &domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: s.hashToken(raw),
		FamilyID:  familyID,
		UserAgent: truncate(meta.UserAgent, 512),
		IPAddress: truncate(meta.IPAddress, 45),
		ExpiresAt: now.Add(s.cfg.RefreshTTL),
		CreatedAt: now,
	}, nil
}

func (s *AuthService) tokenPair(access, refresh string) TokenPair {
	return TokenPair{
		AccessToken:      access,
		RefreshToken:     refresh,
		TokenType:        "Bearer",
		ExpiresIn:        int64(s.cfg.AccessTTL.Seconds()),
		RefreshExpiresIn: int64(s.cfg.RefreshTTL.Seconds()),
	}
}

// hashToken peppers the SHA-256 digest with the refresh secret. A database
// dump alone is therefore not enough to validate or forge a stored token.
func (s *AuthService) hashToken(raw string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.RefreshSecret))
	_, _ = mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

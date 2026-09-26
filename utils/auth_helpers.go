package utils

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	// AuthCookieName is the session cookie set on login.
	AuthCookieName = "accessToken"
	// AccessTokenTTLSeconds matches GenerateToken's 15-minute expiry.
	AccessTokenTTLSeconds = 60 * 15
)

// SetAuthCookie writes the session cookie. Secure stays false until HTTPS.
func SetAuthCookie(c *gin.Context, token string, maxAge int) {
	secure := false // TODO(prod): set to true once you serve HTTPS.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(AuthCookieName, token, maxAge, "/", "", secure, true)
}

// ClearAuthCookie expires the session cookie.
func ClearAuthCookie(c *gin.Context) {
	SetAuthCookie(c, "", -1)
}

// TokenFromRequest returns the bearer token from cookie or Authorization header.
func TokenFromRequest(c *gin.Context) string {
	if cookie, err := c.Cookie(AuthCookieName); err == nil && cookie != "" {
		return cookie
	}
	if after, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// IsUniqueConstraintError reports duplicate-key failures across drivers.
// Covers gorm.ErrDuplicatedKey plus sqlite/postgres message sniffing so
// check-then-insert races map to 409 instead of 500.
func IsUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "duplicate key")
}

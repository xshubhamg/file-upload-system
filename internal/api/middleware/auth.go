package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/api/response"
	"github.com/xshubhamg/file-upload/internal/service"
)

const (
	UserIDKey = "user_id"
)

func Auth(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := TokenFromRequest(c)
		userID, err := auth.Authenticate(token)
		if err != nil {
			response.Error(c, err)
			return
		}
		c.Set(UserIDKey, userID)
		c.Next()
	}
}

func TokenFromRequest(c *gin.Context) string {
	if header := c.GetHeader("Authorization"); header != "" {
		scheme, value, ok := strings.Cut(header, " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(value)
		}
	}
	if cookie, err := c.Cookie("accessToken"); err == nil {
		return cookie
	}
	return ""
}

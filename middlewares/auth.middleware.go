package middlewares

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/xshubhamg/file-upload/utils"
)

func AuthRequired(c *gin.Context) {
	tokenStr := utils.TokenFromRequest(c)

	if tokenStr == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "No access token provided"})
		return
	}

	claims, err := utils.ValidateToken(tokenStr)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token expired"})
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid Token!"})
		return
	}

	c.Set("userID", claims.UserID)
	c.Next()
}

package utils

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xshubhamg/file-upload/models"
	"gorm.io/gorm"
)

// CurrentUser resolves the authenticated user for a request.
// It reads "userID" set by AuthRequired, fetches the row, and writes
// the JSON error response on failure. The bool is false when a response
// was already written and the caller must return.
func CurrentUser(c *gin.Context) (models.User, bool) {
	var user models.User

	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Could not get user id!"})
		return user, false
	}

	if err := DB.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not fetch user"})
		}
		return user, false
	}

	return user, true
}

package controllers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xshubhamg/file-upload/models"
	"github.com/xshubhamg/file-upload/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// loginInput is the ONLY shape HandleLogin accepts.
type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// registerInput is the ONLY shape HandleRegister accepts.
type registerInput struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userResponse is the ONLY user shape we ever send back.
type userResponse struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

func toUserResponse(u models.User) userResponse {
	return userResponse{ID: u.ID, Username: u.Username, Email: u.Email}
}

func HandleLogin(c *gin.Context) {
	var input loginInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid body!"})
		return
	}

	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || input.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Some field are missing!"})
		return
	}

	var dbUser models.User
	if err := utils.DB.Where("username = ?", input.Username).First(&dbUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(dbUser.Password), []byte(input.Password)); err != nil {
		// Same generic message as above — deliberately identical.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	accessToken, err := utils.GenerateToken(dbUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create session"})
		return
	}

	utils.SetAuthCookie(c, accessToken, utils.AccessTokenTTLSeconds)

	c.JSON(http.StatusOK, gin.H{"message": "Logged in successfully!"})
}

func HandleRegister(c *gin.Context) {
	var input registerInput

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid body!"})
		return
	}

	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)
	if input.Username == "" || input.Email == "" || input.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Some field are missing!"})
		return
	}

	if !utils.IsValidUsername(input.Username) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username must be 3-32 chars: letters, numbers, _ or -"})
		return
	}

	if !utils.IsValidPassword(input.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be 8-72 characters"})
		return
	}

	var existingUser models.User
	err := utils.DB.Where("username = ? OR email = ?", input.Username, input.Email).First(&existingUser).Error
	if err == nil {
		// A row was found -> conflict, not a generic 400.
		c.JSON(http.StatusConflict, gin.H{"error": "Username or Email already exists!"})
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		// Some REAL db error (connection, syntax) — don't confuse with "free to use".
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error hashing Password"})
		return
	}

	user := models.User{
		Username: input.Username,
		Email:    input.Email,
		Password: string(hashedPassword),
	}
	if err := utils.DB.Create(&user).Error; err != nil {
		// Handle check-then-insert race: a concurrent register may have
		// won, surfacing as a unique-constraint error instead of 500.
		if utils.IsUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "Username or Email already exists!"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create User"})
		return
	}

	c.JSON(http.StatusCreated, toUserResponse(user))
}

func HandleLogout(c *gin.Context) {
	utils.ClearAuthCookie(c)

	c.JSON(http.StatusOK, gin.H{"message": "Logout successfully!"})
}

func GetUser(c *gin.Context) {
	user, ok := utils.CurrentUser(c)
	if !ok {
		return
	}

	// Same no-hash rule as register: map to the safe response shape.
	c.JSON(http.StatusOK, toUserResponse(user))
}

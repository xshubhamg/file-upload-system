package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/xshubhamg/file-upload/internal/api/middleware"
	"github.com/xshubhamg/file-upload/internal/api/response"
	"github.com/xshubhamg/file-upload/internal/apperr"
	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/service"
)

const (
	AccessCookie  = "accessToken"
	RefreshCookie = "refreshToken"
)

type AuthHandler struct {
	svc *service.AuthService
	cfg config.Config
}

func NewAuthHandler(svc *service.AuthService, cfg config.Config) *AuthHandler {
	return &AuthHandler{svc: svc, cfg: cfg}
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if !response.BindJSON(c, &req) {
		return
	}
	user, err := h.svc.Register(c.Request.Context(), service.RegisterInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.JSON(c, http.StatusCreated, gin.H{"user": NewUserDTO(user)})
}

type loginRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	User UserDTO `json:"user"`
	service.TokenPair
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if !response.BindJSON(c, &req) {
		return
	}
	identifier := strings.TrimSpace(req.Username)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Email)
	}

	user, pair, err := h.svc.Login(c.Request.Context(), identifier, req.Password, sessionMeta(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	h.setAuthCookies(c, pair)
	response.JSON(c, http.StatusOK, authResponse{User: NewUserDTO(user), TokenPair: pair})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	token, err := refreshTokenFromRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	pair, err := h.svc.Refresh(c.Request.Context(), token, sessionMeta(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	h.setAuthCookies(c, pair)
	response.JSON(c, http.StatusOK, pair)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	token, err := refreshTokenFromRequest(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	if err := h.svc.Logout(c.Request.Context(), token); err != nil {
		response.Error(c, err)
		return
	}
	h.clearAuthCookies(c)
	response.JSON(c, http.StatusOK, gin.H{"message": "logged out"})
}

func (h *AuthHandler) LogoutAll(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	if err := h.svc.LogoutAll(c.Request.Context(), userID); err != nil {
		response.Error(c, err)
		return
	}
	h.clearAuthCookies(c)
	response.JSON(c, http.StatusOK, gin.H{"message": "all sessions revoked"})
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	user, err := h.svc.User(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.JSON(c, http.StatusOK, gin.H{"user": NewUserDTO(user)})
}

func (h *AuthHandler) setAuthCookies(c *gin.Context, pair service.TokenPair) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(AccessCookie, pair.AccessToken, int(pair.ExpiresIn), "/", h.cfg.CookieDomain, h.cfg.CookieSecure, true)
	c.SetCookie(RefreshCookie, pair.RefreshToken, int(pair.RefreshExpiresIn), "/", h.cfg.CookieDomain, h.cfg.CookieSecure, true)
}

func (h *AuthHandler) clearAuthCookies(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(AccessCookie, "", -1, "/", h.cfg.CookieDomain, h.cfg.CookieSecure, true)
	c.SetCookie(RefreshCookie, "", -1, "/", h.cfg.CookieDomain, h.cfg.CookieSecure, true)
}

func sessionMeta(c *gin.Context) service.SessionMeta {
	return service.SessionMeta{UserAgent: c.Request.UserAgent(), IPAddress: c.ClientIP()}
}

func refreshTokenFromRequest(c *gin.Context) (string, error) {
	var token string
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				return "", apperr.Validation("invalid JSON body").
					WithDetail("body", "must be valid JSON or empty")
			}
		}
		token = strings.TrimSpace(req.RefreshToken)
	}
	if token == "" {
		if cookie, err := c.Cookie(RefreshCookie); err == nil {
			token = cookie
		}
	}
	return token, nil
}

func CurrentUserID(c *gin.Context) (uuid.UUID, bool) {
	value, ok := c.Get(middleware.UserIDKey)
	if !ok {
		response.Error(c, apperr.Unauthorized("authentication required"))
		return uuid.Nil, false
	}
	userID, ok := value.(uuid.UUID)
	if !ok {
		response.Error(c, apperr.Unauthorized("authentication required"))
		return uuid.Nil, false
	}
	return userID, true
}

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		response.Error(c, apperr.Validation("invalid "+name).
			WithDetail(name, "must be a UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func parseLimit(c *gin.Context, fallback, max int) (int, bool) {
	raw := c.Query("limit")
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		response.Error(c, apperr.Validation("invalid limit").
			WithDetail("limit", "must be a positive integer"))
		return 0, false
	}
	if value > max {
		value = max
	}
	return value, true
}

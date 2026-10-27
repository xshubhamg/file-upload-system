package service

import (
	"net/mail"
	"regexp"
	"strings"

	"github.com/xshubhamg/file-upload/internal/apperr"
)

var validUsername = regexp.MustCompile(`^[a-z0-9_-]+$`)

func normalizeUsername(raw string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(raw))
	if len(username) < 3 || len(username) > 32 {
		return "", apperr.Validation("username must be 3-32 characters").
			WithDetail("username", "must be 3-32 characters")
	}
	if !validUsername.MatchString(username) {
		return "", apperr.Validation("username may only contain letters, numbers, underscores and hyphens").
			WithDetail("username", "allowed characters: a-z, 0-9, _ and -")
	}
	return username, nil
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 {
		return "", apperr.Validation("a valid email address is required").
			WithDetail("email", "must be a valid email address")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", apperr.Validation("a valid email address is required").
			WithDetail("email", "must be a valid email address")
	}
	return email, nil
}

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return apperr.Validation("password must be 8-72 characters").
			WithDetail("password", "must be 8-72 characters")
	}
	return nil
}

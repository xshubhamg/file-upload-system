package utils

import "regexp"

var validUsername = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// IsValidUsername reports whether a username is 3-32 chars of
// letters, numbers, "_" or "-".
func IsValidUsername(username string) bool {
	if len(username) < 3 || len(username) > 32 {
		return false
	}
	return validUsername.MatchString(username)
}

// IsValidPassword reports whether a password fits bcrypt limits (8-72 chars).
func IsValidPassword(password string) bool {
	return len(password) >= 8 && len(password) <= 72
}

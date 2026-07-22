package auth

import (
	"regexp"
	"strings"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]{1,30}[A-Za-z0-9])?$`)

var reservedUsernames = map[string]struct{}{
	"admin":   {},
	"api":     {},
	"root":    {},
	"support": {},
	"system":  {},
}

func NormalizeUsername(value string) (string, string, error) {
	username := strings.TrimSpace(value)
	if !usernamePattern.MatchString(username) {
		return "", "", ErrInvalidUsername
	}
	normalized := strings.ToLower(username)
	if _, reserved := reservedUsernames[normalized]; reserved {
		return "", "", ErrInvalidUsername
	}
	return username, normalized, nil
}

func NormalizeLoginIdentifier(value string) (string, bool) {
	identifier := strings.TrimSpace(value)
	if strings.Contains(identifier, "@") {
		return normalizeEmail(identifier), true
	}
	return strings.ToLower(identifier), false
}

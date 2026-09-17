package auth

import (
	"errors"
	"strings"
)

// ResolveUserID picks the explicit user first and falls back to configured default.
func ResolveUserID(explicit string, defaultUser string) (string, error) {
	userID := strings.TrimSpace(explicit)
	if userID != "" {
		return userID, nil
	}
	userID = strings.TrimSpace(defaultUser)
	if userID != "" {
		return userID, nil
	}
	return "", errors.New("missing user id")
}

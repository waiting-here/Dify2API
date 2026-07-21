package auth

import (
	"crypto/subtle"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// VerifyPassword checks a candidate against the stored password, which may be
// plaintext or a bcrypt hash ("$2a$"/"$2b$"/"$2y$" prefix).
func VerifyPassword(stored, candidate string) bool {
	if strings.HasPrefix(stored, "$2a$") || strings.HasPrefix(stored, "$2b$") || strings.HasPrefix(stored, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(candidate)) == nil
	}
	if len(stored) != len(candidate) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(candidate)) == 1
}

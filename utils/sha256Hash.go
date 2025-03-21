package utils

import (
	"crypto/sha256"
	"encoding/hex"
)

// Sha256Hash returns the SHA256 hash (in hex) for a given string.
func Sha256Hash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

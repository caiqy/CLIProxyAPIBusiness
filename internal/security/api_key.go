package security

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// apiKeyPrefix is the prefix used for generated API keys.
const apiKeyPrefix = "cpa_"

// GenerateAPIKey creates a new random API key string.
func GenerateAPIKey() (token string, err error) {
	secret := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, secret); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	secretHex := hex.EncodeToString(secret)
	token = apiKeyPrefix + secretHex
	return token, nil
}

// GenerateRandomString returns a hex-encoded random string of the given length.
func GenerateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", fmt.Errorf("generate random string: %w", err)
	}
	return hex.EncodeToString(bytes)[:length], nil
}

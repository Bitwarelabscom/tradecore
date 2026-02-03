package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// DecryptToken decrypts a token encrypted by Luna's encryption.ts
// The stored format is: iv:tag:encrypted (all hex-encoded)
// Uses AES-256-GCM with 16 byte IV
func DecryptToken(storedValue string, encryptionKey string) (string, error) {
	parts := strings.Split(storedValue, ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid encrypted token format: expected iv:tag:encrypted")
	}

	ivHex := parts[0]
	tagHex := parts[1]
	encryptedHex := parts[2]

	// Decode all hex values
	iv, err := hex.DecodeString(ivHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode IV: %w", err)
	}

	tag, err := hex.DecodeString(tagHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode tag: %w", err)
	}

	encrypted, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode encrypted data: %w", err)
	}

	// Decode the encryption key (should be 64 hex chars = 32 bytes)
	key, err := hex.DecodeString(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode encryption key: %w", err)
	}

	if len(key) != 32 {
		return "", fmt.Errorf("encryption key must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// In GCM, the tag is appended to the ciphertext for decryption
	ciphertextWithTag := append(encrypted, tag...)

	// Decrypt
	plaintext, err := gcm.Open(nil, iv, ciphertextWithTag, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// LoadEncryptionKey reads the encryption key from a Docker secret file or environment
func LoadEncryptionKey() (string, error) {
	// Try Docker secret first
	secretPath := "/run/secrets/encryption_key"
	if data, err := os.ReadFile(secretPath); err == nil {
		return strings.TrimSpace(string(data)), nil
	}

	// Fall back to environment variable
	if key := os.Getenv("ENCRYPTION_KEY"); key != "" {
		return key, nil
	}

	return "", fmt.Errorf("encryption key not found: check /run/secrets/encryption_key or ENCRYPTION_KEY env var")
}

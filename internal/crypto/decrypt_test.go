package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"io"
	"testing"
)

func TestDecryptToken(t *testing.T) {
	// 1. Generate Key (32 bytes)
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatal(err)
	}
	keyHex := hex.EncodeToString(key)

	// 2. Encrypt something
	plaintext := []byte("secret_token_123")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}

	// Nonce (IV) - Standard is 12 bytes
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}

	// Encrypt and get tag
	// Seal appends the tag to the ciphertext
	ciphertextWithTag := gcm.Seal(nil, nonce, plaintext, nil)
	
	// Split ciphertext and tag
	// Tag size is usually 16 bytes for AES-GCM
	tagSize := gcm.Overhead()
	if len(ciphertextWithTag) < tagSize {
		t.Fatal("ciphertext too short")
	}
	
	encrypted := ciphertextWithTag[:len(ciphertextWithTag)-tagSize]
	tag := ciphertextWithTag[len(ciphertextWithTag)-tagSize:]

	// 3. Format as iv:tag:encrypted
	ivHex := hex.EncodeToString(nonce)
	tagHex := hex.EncodeToString(tag)
	encryptedHex := hex.EncodeToString(encrypted)
	
	storedValue := ivHex + ":" + tagHex + ":" + encryptedHex

	// 4. Decrypt
	decrypted, err := DecryptToken(storedValue, keyHex)
	if err != nil {
		t.Fatalf("DecryptToken failed: %v", err)
	}

	if decrypted != string(plaintext) {
		t.Errorf("Expected %s, got %s", plaintext, decrypted)
	}
}

func TestDecryptToken_InvalidKey(t *testing.T) {
	// 31 bytes key (invalid)
	key := make([]byte, 31)
	keyHex := hex.EncodeToString(key)
	
	_, err := DecryptToken("iv:tag:enc", keyHex)
	if err == nil {
		t.Error("Expected error for invalid key length")
	}
}

func TestDecryptToken_InvalidFormat(t *testing.T) {
	key := make([]byte, 32)
	keyHex := hex.EncodeToString(key)

	_, err := DecryptToken("invalid", keyHex)
	if err == nil {
		t.Error("Expected error for invalid format")
	}
}

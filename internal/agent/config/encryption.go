package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
)

// getEncryptionKey derives a 32-byte key from machine-specific data
// This provides basic protection for the token in config files
func getEncryptionKey() ([]byte, error) {
	// Use hostname as a seed for machine-specific encryption
	// In production, consider using OS keyring/keychain instead
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "default-agent"
	}

	// Derive a 32-byte key using SHA-256
	hash := sha256.Sum256([]byte(hostname + "-xuannexus-agent-key"))
	return hash[:], nil
}

// EncryptToken encrypts a token using AES-256-GCM
func EncryptToken(plaintext string) (string, error) {
	key, err := getEncryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptToken decrypts a token using AES-256-GCM
func DecryptToken(ciphertext string) (string, error) {
	key, err := getEncryptionKey()
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		// If it's not base64, assume it's plaintext (backward compatibility)
		return ciphertext, nil
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(data) < gcm.NonceSize() {
		// Too short to be encrypted, assume plaintext
		return ciphertext, nil
	}

	nonce, ciphertextBytes := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		// Decryption failed, might be plaintext
		return ciphertext, nil
	}

	return string(plaintext), nil
}

// IsEncrypted checks if a token appears to be encrypted
func IsEncrypted(token string) bool {
	// Encrypted tokens are base64 encoded and longer
	if len(token) < 32 {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(token)
	return err == nil && len(token) > 64
}

// MigrateToken encrypts a plaintext token if needed
func MigrateToken(token string) (string, bool, error) {
	if token == "" {
		return "", false, nil
	}

	if IsEncrypted(token) {
		return token, false, nil // Already encrypted
	}

	encrypted, err := EncryptToken(token)
	if err != nil {
		return token, false, err // Return original on error
	}

	return encrypted, true, nil // Return encrypted and flag that migration happened
}

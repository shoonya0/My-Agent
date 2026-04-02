package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Encryptor provides AES-256-GCM encryption and decryption for sensitive
// data at rest (e.g. platform credential tokens).
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor creates an Encryptor from a 32-byte hex-encoded key.
func NewEncryptor(hexKey string) (*Encryptor, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode hex key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes (got %d)", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new GCM: %w", err)
	}

	return &Encryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM. The nonce is prepended to
// the ciphertext so Decrypt can extract it.
func (e *Encryptor) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: generate nonce: %w", err)
	}
	return e.gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt decrypts ciphertext that was produced by Encrypt.
func (e *Encryptor) Decrypt(ciphertext []byte) (string, error) {
	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("crypto: ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}
	return string(plaintext), nil
}

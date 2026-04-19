package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"morrow/internal/config"
	"os"
)

func getMasterKey() ([]byte, error) {
	key, err := os.ReadFile(config.GetKeyPath())
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid master key length, expected 32 bytes")
	}
	return key, nil
}

func InitMasterKey() error {
	_, err := ensureMasterKey()
	return err
}

func ensureMasterKey() ([]byte, error) {
	key, err := getMasterKey()
	if err == nil {
		return key, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		// Generate a new 32-byte (256-bit) AES key
		key = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("failed to generate random key: %w", err)
		}

		if err := os.WriteFile(config.GetKeyPath(), key, 0600); err != nil {
			return nil, fmt.Errorf("failed to save master key to %s: %w", config.GetKeyPath(), err)
		}
		return key, nil
	}

	return nil, fmt.Errorf("failed to load master key: %w", err)
}

// Encrypt strings to base64 using AES-GCM
func Encrypt(text string) (string, error) {
	key, err := ensureMasterKey()
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

	ciphertext := gcm.Seal(nonce, nonce, []byte(text), nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt base64 to string using AES-GCM
func Decrypt(cryptoText string) (string, error) {
	key, err := getMasterKey()
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	data, err := base64.URLEncoding.DecodeString(cryptoText)
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

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

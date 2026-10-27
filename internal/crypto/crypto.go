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
	"morrow/internal/privilege"
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

// InitMasterKey creates the master key if needed and (re)applies its access
// restrictions, so re-running init also repairs keys from older installs.
func InitMasterKey() error {
	if _, err := ensureMasterKey(); err != nil {
		return err
	}
	return privilege.RestrictKeyFile(config.GetKeyPath(), config.IsPerUser())
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
		if err := privilege.RestrictKeyFile(config.GetKeyPath(), config.IsPerUser()); err != nil {
			_ = os.Remove(config.GetKeyPath())
			return nil, err
		}
		return key, nil
	}

	return nil, fmt.Errorf("failed to load master key: %w", err)
}

// ErrNoMasterKey means the key file does not exist. Only `morrow init` creates
// it: generating one anywhere else would silently orphan every value already
// encrypted with the old key (e.g. after a deleted file or wrong MORROW_KEY_PATH).
var ErrNoMasterKey = errors.New("master key not found")

// Encrypt strings to base64 using AES-GCM. It never creates the master key.
func Encrypt(text string) (string, error) {
	key, err := getMasterKey()
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w at %s: run 'morrow init' to create it. If secured values already exist, "+
			"restore the original key file instead; a new key cannot decrypt them", ErrNoMasterKey, config.GetKeyPath())
	}
	if err != nil {
		return "", fmt.Errorf("failed to load master key: %w", err)
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

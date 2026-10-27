package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"
	"morrow/internal/config"
	"morrow/internal/privilege"
	"os"

	"golang.org/x/crypto/nacl/box"
)

// Inbox keys are an X25519 pair for anonymous sealed boxes: anyone holding the
// public key can seal a submission, only the private key can open it, and the
// sender cannot open what it sealed. The private key file holds private||public.

// InitInboxKeys creates the inbox key pair if it does not exist yet and
// (re)applies access restrictions to the private key. It never replaces an
// existing pair, so sealed submissions waiting in the inbox stay readable.
func InitInboxKeys() error {
	privPath, pubPath := config.GetInboxPrivateKeyPath(), config.GetInboxPublicKeyPath()
	if _, err := os.Stat(privPath); errors.Is(err, os.ErrNotExist) {
		pub, priv, err := box.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("failed to generate inbox key pair: %w", err)
		}
		if err := os.WriteFile(privPath, append(priv[:], pub[:]...), 0600); err != nil {
			return fmt.Errorf("failed to save inbox private key: %w", err)
		}
		if err := privilege.RestrictKeyFile(privPath, config.IsPerUser()); err != nil {
			_ = os.Remove(privPath)
			return err
		}
		if err := os.WriteFile(pubPath, pub[:], 0644); err != nil {
			return fmt.Errorf("failed to save inbox public key: %w", err)
		}
		return nil
	} else if err != nil {
		return err
	}
	if err := privilege.RestrictKeyFile(privPath, config.IsPerUser()); err != nil {
		return err
	}
	// Recreate a deleted public key from the private key file.
	if _, err := os.Stat(pubPath); errors.Is(err, os.ErrNotExist) {
		_, pub, err := readInboxPrivateKey()
		if err != nil {
			return err
		}
		return os.WriteFile(pubPath, pub[:], 0644)
	}
	return nil
}

// ErrInboxDisabled means the inbox key pair has not been created.
var ErrInboxDisabled = errors.New("the inbox is not enabled on this install: an administrator must run 'morrow init --enable-inbox'")

// Seal encrypts msg for the inbox using only the public key.
func Seal(msg []byte) ([]byte, error) {
	data, err := os.ReadFile(config.GetInboxPublicKeyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrInboxDisabled
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read inbox public key: %w", err)
	}
	if len(data) != 32 {
		return nil, errors.New("invalid inbox public key length, expected 32 bytes")
	}
	var pub [32]byte
	copy(pub[:], data)
	return box.SealAnonymous(nil, msg, &pub, rand.Reader)
}

// OpenSealed decrypts a sealed submission with the private key.
func OpenSealed(sealed []byte) ([]byte, error) {
	priv, pub, err := readInboxPrivateKey()
	if err != nil {
		return nil, err
	}
	msg, ok := box.OpenAnonymous(nil, sealed, pub, priv)
	if !ok {
		return nil, errors.New("submission could not be decrypted (not sealed for this install, or tampered with)")
	}
	return msg, nil
}

func readInboxPrivateKey() (priv, pub *[32]byte, err error) {
	data, err := os.ReadFile(config.GetInboxPrivateKeyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrInboxDisabled
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read inbox private key: %w", err)
	}
	if len(data) != 64 {
		return nil, nil, errors.New("invalid inbox private key length, expected 64 bytes")
	}
	priv, pub = new([32]byte), new([32]byte)
	copy(priv[:], data[:32])
	copy(pub[:], data[32:])
	return priv, pub, nil
}

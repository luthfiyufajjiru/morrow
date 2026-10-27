package crypto

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"morrow/internal/config"
)

func TestInboxKeysAndSeal(t *testing.T) {
	setupNoKey(t)
	if _, err := Seal([]byte("x")); !errors.Is(err, ErrInboxDisabled) {
		t.Fatalf("Seal before init: %v, want ErrInboxDisabled", err)
	}
	if _, err := OpenSealed([]byte("x")); !errors.Is(err, ErrInboxDisabled) {
		t.Fatalf("OpenSealed before init: %v, want ErrInboxDisabled", err)
	}

	if err := InitInboxKeys(); err != nil {
		t.Fatal(err)
	}
	priv1, _ := os.ReadFile(config.GetInboxPrivateKeyPath())
	pub1, _ := os.ReadFile(config.GetInboxPublicKeyPath())
	if len(priv1) != 64 || len(pub1) != 32 || !bytes.Equal(priv1[32:], pub1) {
		t.Fatalf("unexpected key files: priv %d bytes, pub %d bytes", len(priv1), len(pub1))
	}

	sealed, err := Seal([]byte("secret payload"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("secret payload")) {
		t.Fatal("sealed output contains the plaintext")
	}
	msg, err := OpenSealed(sealed)
	if err != nil || string(msg) != "secret payload" {
		t.Fatalf("OpenSealed = %q, %v", msg, err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := OpenSealed(tampered); err == nil {
		t.Error("tampered submission opened")
	}

	// Re-running init keeps the pair (pending submissions stay readable) and
	// recreates a deleted public key.
	if err := os.Remove(config.GetInboxPublicKeyPath()); err != nil {
		t.Fatal(err)
	}
	if err := InitInboxKeys(); err != nil {
		t.Fatal(err)
	}
	priv2, _ := os.ReadFile(config.GetInboxPrivateKeyPath())
	pub2, _ := os.ReadFile(config.GetInboxPublicKeyPath())
	if !bytes.Equal(priv1, priv2) || !bytes.Equal(pub1, pub2) {
		t.Fatal("InitInboxKeys replaced an existing key pair")
	}
	if msg, err := OpenSealed(sealed); err != nil || string(msg) != "secret payload" {
		t.Fatalf("old submission no longer opens: %v", err)
	}
}

func TestOpenSealedForeignKey(t *testing.T) {
	setupNoKey(t)
	if err := InitInboxKeys(); err != nil {
		t.Fatal(err)
	}
	sealed, err := Seal([]byte("for install A"))
	if err != nil {
		t.Fatal(err)
	}
	setupNoKey(t) // a different install
	if err := InitInboxKeys(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSealed(sealed); err == nil {
		t.Fatal("a submission sealed for another install was opened")
	}
}

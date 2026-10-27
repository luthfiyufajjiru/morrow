package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupNoKey points Morrow at a temp dir with no master key yet.
func setupNoKey(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "test.key")
	t.Setenv("MORROW_HOME", dir)
	t.Setenv("MORROW_KEY_PATH", keyPath)
	return keyPath
}

// setupKey is setupNoKey plus the key that `morrow init` would create.
func setupKey(t *testing.T) string {
	t.Helper()
	keyPath := setupNoKey(t)
	if err := InitMasterKey(); err != nil {
		t.Fatal(err)
	}
	return keyPath
}

func TestRoundTrip(t *testing.T) {
	setupKey(t)
	cases := map[string]string{
		"empty":   "",
		"ascii":   "hunter2",
		"unicode": "pässwörd-日本語-\U0001F511",
		"long":    string(bytes.Repeat([]byte("x"), 10000)),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			enc, err := Encrypt(in)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			out, err := Decrypt(enc)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if out != in {
				t.Fatalf("round trip mismatch: got %q want %q", out, in)
			}
		})
	}
}

func TestEncryptRandomNonce(t *testing.T) {
	setupKey(t)
	a, err := Encrypt("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two encryptions of the same plaintext are identical")
	}
}

func TestDecryptMissingKey(t *testing.T) {
	keyPath := setupKey(t)
	enc, err := Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(enc); err == nil {
		t.Fatal("expected error decrypting without key")
	}
	if _, err := os.Stat(keyPath); err == nil {
		t.Fatal("Decrypt must not create a key")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	keyPath := setupKey(t)
	enc, err := Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	other := make([]byte, 32)
	if _, err := rand.Read(other); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, other, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(enc); err == nil {
		t.Fatal("expected error decrypting with different key")
	}
}

func TestInvalidKeyLength(t *testing.T) {
	keyPath := setupKey(t)
	if err := os.WriteFile(keyPath, make([]byte, 16), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Encrypt("x"); err == nil {
		t.Error("Encrypt: expected error for 16-byte key")
	}
	if _, err := Decrypt("AAAA"); err == nil {
		t.Error("Decrypt: expected error for 16-byte key")
	}
	if err := InitMasterKey(); err == nil {
		t.Error("InitMasterKey: expected error for 16-byte key")
	}
	got, _ := os.ReadFile(keyPath)
	if len(got) != 16 {
		t.Errorf("bad key file was modified, len=%d", len(got))
	}
}

func TestDecryptMalformed(t *testing.T) {
	setupKey(t)
	if err := InitMasterKey(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"invalid base64": "!!!not base64!!!",
		"too short":      base64.URLEncoding.EncodeToString([]byte("short")),
		"empty":          "",
		"bad tag":        base64.URLEncoding.EncodeToString(make([]byte, 40)),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decrypt(in); err == nil {
				t.Fatalf("expected error for %q", in)
			}
		})
	}
}

func TestDecryptTampered(t *testing.T) {
	setupKey(t)
	enc, err := Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.URLEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 0xff
	if _, err := Decrypt(base64.URLEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("expected authentication failure on tampered ciphertext")
	}
}

func TestInitMasterKey(t *testing.T) {
	keyPath := setupKey(t)
	if err := InitMasterKey(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 {
		t.Fatalf("key length = %d, want 32", len(first))
	}
	if err := InitMasterKey(); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(keyPath)
	if !bytes.Equal(first, second) {
		t.Fatal("InitMasterKey is not idempotent: key changed")
	}
}

func TestEncryptRequiresInit(t *testing.T) {
	keyPath := setupNoKey(t)
	_, err := Encrypt("x")
	if !errors.Is(err, ErrNoMasterKey) {
		t.Fatalf("err = %v, want ErrNoMasterKey", err)
	}
	if !strings.Contains(err.Error(), "morrow init") || !strings.Contains(err.Error(), keyPath) {
		t.Errorf("error should name the key path and 'morrow init': %v", err)
	}
	if _, statErr := os.Stat(keyPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("Encrypt must not create a key")
	}
}

// A deleted key must not be silently replaced: the old ciphertext has to stay
// recoverable by restoring the original key file.
func TestEncryptAfterKeyLossKeepsOldSecretsRecoverable(t *testing.T) {
	keyPath := setupKey(t)
	enc, err := Encrypt("old-secret")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Encrypt("new-secret"); !errors.Is(err, ErrNoMasterKey) {
		t.Fatalf("err = %v, want ErrNoMasterKey", err)
	}
	if err := os.WriteFile(keyPath, backup, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := Decrypt(enc); err != nil || got != "old-secret" {
		t.Fatalf("restored key should decrypt old value: %q, %v", got, err)
	}
}

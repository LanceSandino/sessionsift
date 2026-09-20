// Package bridge tests authenticated envelope encryption for transient relay payloads.
package bridge

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvelopeCipherRoundTripRejectsTamperingAndWrongKey(t *testing.T) {
	key := make([]byte, bridgeKEKSize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	cipher, err := newEnvelopeCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"operation":"session.list","private":"transient"}`)
	sealed, err := cipher.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("session.list")) || bytes.Equal(sealed, plain) {
		t.Fatalf("relay payload was stored in plaintext: %q", sealed)
	}
	opened, err := cipher.Open(sealed)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("opened=%q err=%v", opened, err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err = cipher.Open(tampered); err == nil {
		t.Fatal("tampered envelope was accepted")
	}
	otherKey := make([]byte, bridgeKEKSize)
	_, _ = rand.Read(otherKey)
	other, _ := newEnvelopeCipher(otherKey)
	if _, err = other.Open(sealed); err == nil {
		t.Fatal("envelope opened with the wrong key")
	}
}

func TestBridgeKeyFileIsCreatedOwnerOnlyAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "bridge.kek")
	first, err := loadOrCreateBridgeKey(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode=%o", info.Mode().Perm())
	}
	second, err := loadOrCreateBridgeKey(path)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("reloaded key mismatch err=%v", err)
	}
}

func TestEnvelopeCipherValidatesInputsAndRecognizesEnvelope(t *testing.T) {
	if _, err := newEnvelopeCipher(make([]byte, bridgeKEKSize-1)); err == nil {
		t.Fatal("short key was accepted")
	}
	var unavailable *envelopeCipher
	if _, err := unavailable.Seal([]byte("private")); err == nil {
		t.Fatal("nil cipher sealed a payload")
	}
	if _, err := unavailable.Open([]byte("private")); err == nil {
		t.Fatal("nil cipher opened a payload")
	}
	key := make([]byte, bridgeKEKSize)
	_, _ = rand.Read(key)
	cipher, _ := newEnvelopeCipher(key)
	sealed, err := cipher.Seal(nil)
	if err != nil || !cipher.IsEnvelope(sealed) {
		t.Fatalf("sealed=%x err=%v", sealed, err)
	}
	if cipher.IsEnvelope([]byte("SSF")) || cipher.IsEnvelope([]byte("not-an-envelope")) {
		t.Fatal("invalid payload was identified as an envelope")
	}
	if _, err = cipher.Open([]byte("SSF1too-short")); err == nil {
		t.Fatal("short envelope was accepted")
	}
}

func TestBridgeKeyLoadsRawAndBase64AndRejectsMalformedFiles(t *testing.T) {
	key := bytes.Repeat([]byte{0x5a}, bridgeKEKSize)
	for name, body := range map[string][]byte{
		"raw.kek":    key,
		"base64.kek": []byte(base64.RawURLEncoding.EncodeToString(key) + "\n"),
	} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadOrCreateBridgeKey(path)
		if err != nil || !bytes.Equal(loaded, key) {
			t.Fatalf("%s loaded=%x err=%v", name, loaded, err)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o", name, info.Mode().Perm())
		}
	}
	badPath := filepath.Join(t.TempDir(), "bad.kek")
	if err := os.WriteFile(badPath, []byte("not-a-valid-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateBridgeKey(badPath); err == nil {
		t.Fatal("malformed key file was accepted")
	}
}

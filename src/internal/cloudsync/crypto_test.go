// Package cloudsync tests the client-held encryption and recovery contract.
package cloudsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverySecretRoundTripAndProtectedFileStore(t *testing.T) {
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "file")
	key, err := GenerateMasterKey(rand.Reader)
	if err != nil || len(key) != MasterKeySize {
		t.Fatalf("key length=%d err=%v", len(key), err)
	}
	secret := EncodeRecoverySecret(key)
	restored, err := DecodeRecoverySecret(secret)
	if err != nil || !bytes.Equal(restored, key) {
		t.Fatalf("recovery round trip err=%v", err)
	}
	if _, err = DecodeRecoverySecret(secret[:len(secret)-1] + "x"); err == nil {
		t.Fatal("modified recovery secret was accepted")
	}
	path := filepath.Join(t.TempDir(), "sync-master.key")
	if err = SaveMasterKey(path, key); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
	loaded, err := LoadMasterKey(path)
	if err != nil || !bytes.Equal(loaded, key) {
		t.Fatalf("stored key err=%v", err)
	}
}

func TestKeyStoreUsesMacOSCredentialProviderWithoutCommandLineSecret(t *testing.T) {
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "")
	previousAvailable, previousRun := keychainAvailable, runSecurity
	t.Cleanup(func() { keychainAvailable, runSecurity = previousAvailable, previousRun })
	keychainAvailable = func() bool { return true }
	var stored string
	runSecurity = func(_ context.Context, input string, args ...string) ([]byte, error) {
		for _, argument := range args {
			if strings.HasPrefix(argument, recoveryPrefix) {
				t.Fatalf("recovery secret leaked into process argument: %q", argument)
			}
		}
		switch args[0] {
		case "find-generic-password":
			if stored == "" {
				return nil, errors.New("not found")
			}
			return []byte(stored + "\n"), nil
		case "add-generic-password":
			stored = strings.TrimSpace(input)
			return nil, nil
		case "delete-generic-password":
			stored = ""
			return nil, nil
		default:
			return nil, errors.New("unexpected security command")
		}
	}
	store := KeyStore{Account: "device-test"}
	if store.Provider() != "macOS Keychain" {
		t.Fatalf("provider=%q", store.Provider())
	}
	first, _ := GenerateMasterKey(rand.Reader)
	second, _ := GenerateMasterKey(rand.Reader)
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || !bytes.Equal(loaded, first) {
		t.Fatalf("keychain load err=%v", err)
	}
	if err = store.Save(second); err == nil {
		t.Fatal("keychain save overwrote an existing key without rotation")
	}
	if err = store.Replace(second); err != nil {
		t.Fatal(err)
	}
	if loaded, _ = store.Load(); !bytes.Equal(loaded, second) {
		t.Fatal("keychain rotation did not replace key")
	}
	if err = store.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed keychain value remained: %v", err)
	}
}

func TestMacOSKeychainIntegration(t *testing.T) {
	if os.Getenv("SESSIONSIFT_RUN_KEYCHAIN_TEST") != "1" {
		t.Skip("set SESSIONSIFT_RUN_KEYCHAIN_TEST=1 for the native credential-store acceptance check")
	}
	if !keychainAvailable() {
		t.Skip("macOS Keychain is unavailable")
	}
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "")
	store := KeyStore{Account: fmt.Sprintf("sessionsift-rc-%d", os.Getpid())}
	key, _ := GenerateMasterKey(rand.Reader)
	if err := store.Save(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Remove() })
	loaded, err := store.Load()
	if err != nil || !bytes.Equal(loaded, key) {
		t.Fatalf("native keychain load err=%v", err)
	}
	if err = store.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("native keychain removal err=%v", err)
	}
}

func TestFileKeyStoreRotatesAndRemovesPrivateKey(t *testing.T) {
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "file")
	path := filepath.Join(t.TempDir(), "sync.key")
	store := KeyStore{FilePath: path, Account: "device"}
	first, _ := GenerateMasterKey(rand.Reader)
	second, _ := GenerateMasterKey(rand.Reader)
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(second); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || !bytes.Equal(loaded, second) || bytes.Equal(loaded, first) {
		t.Fatalf("rotated key err=%v", err)
	}
	if err = store.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed key remained: %v", err)
	}
}

func TestSealKeepsCanaryOpaqueAndBindsContext(t *testing.T) {
	key, _ := GenerateMasterKey(rand.Reader)
	metadata := Metadata{
		Schema: 1, WorkspaceID: "workspace_owner", ObjectID: ObjectID(key, "session", "private-session-id"),
		SourceDeviceID: "device_source", WriterDeviceID: "device_source", Kind: "session_summary", Epoch: 1, Revision: 1,
	}
	plaintext := []byte(`{"title":"CANARY-private-title","repo":"CANARY-private-path","note":"CANARY-secret-token"}`)
	first, err := Seal(key, metadata, plaintext, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := first.Ciphertext + first.Nonce + first.ObjectID
	if strings.Contains(encoded, "CANARY") || strings.Contains(encoded, "private-session-id") {
		t.Fatalf("plaintext identity leaked in envelope: %s", encoded)
	}
	opened, err := Open(key, first)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("open err=%v plaintext=%q", err, opened)
	}
	second, _ := Seal(key, metadata, plaintext, rand.Reader)
	if first.Nonce == second.Nonce || first.Ciphertext == second.Ciphertext {
		t.Fatal("AEAD nonce/ciphertext was reused")
	}
	tampered := first
	tampered.Revision++
	if _, err = Open(key, tampered); err == nil {
		t.Fatal("tampered authenticated metadata was accepted")
	}
	wrong, _ := GenerateMasterKey(rand.Reader)
	if _, err = Open(wrong, first); err == nil {
		t.Fatal("wrong recovery key decrypted content")
	}
}

func TestObjectIDsAreStableDomainSeparatedAndOpaque(t *testing.T) {
	key, _ := GenerateMasterKey(rand.Reader)
	a := ObjectID(key, "session", "session-123")
	b := ObjectID(key, "session", "session-123")
	c := ObjectID(key, "note", "session-123")
	if a != b || a == c || strings.Contains(a, "session-123") {
		t.Fatalf("ids a=%q b=%q c=%q", a, b, c)
	}
}

func TestEncryptionKnownAnswerVector(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, MasterKeySize)
	metadata := Metadata{
		Schema: SchemaVersion, WorkspaceID: "workspace-kat", ObjectID: ObjectID(key, "note", "record-kat"),
		SourceDeviceID: "device-a", WriterDeviceID: "device-a", Kind: "note", Epoch: 7, Revision: 3, ParentDigest: "parent",
	}
	envelope, err := Seal(key, metadata, []byte(`{"note":"known answer"}`), bytes.NewReader(bytes.Repeat([]byte{0x22}, 24)))
	if err != nil {
		t.Fatal(err)
	}
	if envelope.ObjectID != "obj_QN2c8a2FuU-eZ3BVduf6T8Wc4JvVCMko" || envelope.Nonce != "IiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIi" || envelope.Ciphertext != "V_E7hQGiu118zQyFo6B-E3bC9LyAmKKoeY4T3H1t8UdDIJNCcTk1" {
		t.Fatalf("known-answer vector changed: %+v", envelope)
	}
	plaintext, err := Open(key, envelope)
	if err != nil || string(plaintext) != `{"note":"known answer"}` {
		t.Fatalf("known-answer decrypt=%q err=%v", plaintext, err)
	}
}

func TestCryptoAndPrivateKeyFilesRejectMalformedInputs(t *testing.T) {
	if _, err := GenerateMasterKey(strings.NewReader("short")); err == nil {
		t.Fatal("short entropy source produced a master key")
	}
	if EncodeRecoverySecret([]byte("short")) != "" {
		t.Fatal("short key encoded as a recovery secret")
	}
	for _, value := range []string{"not-a-secret", recoveryPrefix + "%%%", recoveryPrefix + base64.RawURLEncoding.EncodeToString(make([]byte, MasterKeySize+6))} {
		if _, err := DecodeRecoverySecret(value); err == nil {
			t.Fatalf("malformed recovery secret accepted: %q", value)
		}
	}

	key, _ := GenerateMasterKey(rand.Reader)
	metadata := Metadata{Schema: SchemaVersion, WorkspaceID: "workspace", ObjectID: ObjectID(key, "note", "one"), SourceDeviceID: "source", WriterDeviceID: "writer", Kind: "note", Epoch: 1, Revision: 1}
	if _, err := Seal([]byte("short"), metadata, []byte(`{}`), rand.Reader); err == nil {
		t.Fatal("short encryption key accepted")
	}
	if _, err := Seal(key, Metadata{}, []byte(`{}`), rand.Reader); err == nil {
		t.Fatal("empty authenticated metadata accepted")
	}
	if _, err := Seal(key, metadata, []byte(`{}`), strings.NewReader("short")); err == nil {
		t.Fatal("short nonce source accepted")
	}
	if _, err := Open(key, Envelope{Metadata: metadata, Nonce: "bad", Ciphertext: "bad"}); err == nil {
		t.Fatal("malformed ciphertext encoding accepted")
	}
	envelope, _ := Seal(key, metadata, []byte(`{"note":"private"}`), rand.Reader)
	ciphertext, _ := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	ciphertext[0] ^= 0xff
	envelope.Ciphertext = base64.RawURLEncoding.EncodeToString(ciphertext)
	if _, err := Open(key, envelope); err == nil {
		t.Fatal("modified ciphertext authenticated")
	}

	path := filepath.Join(t.TempDir(), "sync.key")
	if err := SaveMasterKey(path, []byte("short")); err == nil {
		t.Fatal("short key saved")
	}
	if err := SaveMasterKey(path, key); err != nil {
		t.Fatal(err)
	}
	if err := SaveMasterKey(path, key); err == nil {
		t.Fatal("existing key overwritten")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMasterKey(path); err == nil {
		t.Fatal("broad key-file permissions accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceMasterKey(path, []byte("short")); err == nil {
		t.Fatal("short replacement key accepted")
	}
	if err := RemoveMasterKey(path); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMasterKey(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing key removal err=%v", err)
	}
}

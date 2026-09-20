// Package remote tests device identity and signed bridge responses.
package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateIdentityPersistsPrivateKeyAndSigns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-identity.json")
	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.ID == "" {
		t.Fatalf("identities differ: %q %q", first.ID, second.ID)
	}
	payload := []byte("typed response")
	signature := first.Sign(payload)
	if !Verify(first.PublicKey, payload, signature) {
		t.Fatal("signature did not verify")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
}

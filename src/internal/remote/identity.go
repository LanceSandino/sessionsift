// Package remote implements device identity, scoped remote access, and collaboration state.
// @module DeviceIdentity creates a local Ed25519 identity without provider credentials.
package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Identity struct {
	ID         string            `json:"id"`
	PublicKey  ed25519.PublicKey `json:"-"`
	privateKey ed25519.PrivateKey
}

type storedIdentity struct {
	ID         string `json:"id"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

func LoadOrCreateIdentity(path string) (Identity, error) {
	if identity, err := loadIdentity(path); err == nil {
		return identity, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Identity{}, err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	digest := sha256.Sum256(publicKey)
	identity := Identity{ID: "device_" + hex.EncodeToString(digest[:12]), PublicKey: publicKey, privateKey: privateKey}
	stored := storedIdentity{ID: identity.ID, PublicKey: base64.RawURLEncoding.EncodeToString(publicKey), PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey)}
	body, _ := json.MarshalIndent(stored, "", "  ")
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Identity{}, err
	}
	// Publish only a complete, synced 0600 file. An exclusive open at the final
	// path would expose an empty/partial key to concurrent CLI/agent startup;
	// rename would replace another process's identity. A same-directory hard
	// link atomically installs the completed inode without replacing a winner.
	temporary, err := os.CreateTemp(filepath.Dir(path), ".device-identity-*")
	if err != nil {
		return Identity{}, err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if _, err := temporary.Write(append(body, '\n')); err != nil {
		return Identity{}, err
	}
	if err := temporary.Sync(); err != nil {
		return Identity{}, err
	}
	if err := temporary.Close(); err != nil {
		return Identity{}, err
	}
	if err := os.Link(temporary.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return loadIdentity(path)
		}
		return Identity{}, err
	}
	if err := os.Remove(temporary.Name()); err != nil {
		return Identity{}, err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return Identity{}, err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func loadIdentity(path string) (Identity, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, err
	}
	var stored storedIdentity
	if json.Unmarshal(body, &stored) != nil {
		return Identity{}, errors.New("invalid device identity")
	}
	publicKey, publicErr := base64.RawURLEncoding.DecodeString(stored.PublicKey)
	privateKey, privateErr := base64.RawURLEncoding.DecodeString(stored.PrivateKey)
	if publicErr != nil || privateErr != nil || len(publicKey) != ed25519.PublicKeySize || len(privateKey) != ed25519.PrivateKeySize {
		return Identity{}, errors.New("invalid device identity key")
	}
	return Identity{ID: stored.ID, PublicKey: ed25519.PublicKey(publicKey), privateKey: ed25519.PrivateKey(privateKey)}, nil
}

func (i Identity) Sign(payload []byte) []byte { return ed25519.Sign(i.privateKey, payload) }

func Verify(publicKey ed25519.PublicKey, payload, signature []byte) bool {
	return ed25519.Verify(publicKey, payload, signature)
}

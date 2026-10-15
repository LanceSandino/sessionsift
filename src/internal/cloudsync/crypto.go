// Package cloudsync owns the client-side sync schema, encryption, and recovery-key lifecycle.
// @module SyncCrypto encrypts portable records before they leave an enrolled device.
package cloudsync

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	MasterKeySize  = 32
	SchemaVersion  = 1
	recoveryPrefix = "sssync1_"
)

// Metadata is authenticated but not encrypted. Values must be opaque or bounded
// routing data; titles, paths, labels, and content never belong here.
type Metadata struct {
	Schema         int    `json:"schema"`
	WorkspaceID    string `json:"workspace_id"`
	ObjectID       string `json:"object_id"`
	SourceDeviceID string `json:"source_device_id"`
	WriterDeviceID string `json:"writer_device_id"`
	Kind           string `json:"kind"`
	Epoch          uint64 `json:"epoch"`
	Revision       uint64 `json:"revision"`
	ParentDigest   string `json:"parent_digest,omitempty"`
	Deleted        bool   `json:"deleted,omitempty"`
}

// Envelope is the portable sync/v1 ciphertext object stored by the bridge.
type Envelope struct {
	Metadata
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// GenerateMasterKey returns a cryptographically random 256-bit key.
func GenerateMasterKey(source io.Reader) ([]byte, error) {
	if source == nil {
		source = rand.Reader
	}
	key := make([]byte, MasterKeySize)
	if _, err := io.ReadFull(source, key); err != nil {
		return nil, fmt.Errorf("generate sync master key: %w", err)
	}
	return key, nil
}

// EncodeRecoverySecret encodes the master key with a version and typo checksum.
func EncodeRecoverySecret(key []byte) string {
	if len(key) != MasterKeySize {
		return ""
	}
	checksum := sha256.Sum256(append([]byte("sessionsift.sync/recovery/v1\x00"), key...))
	payload := append(append([]byte{}, key...), checksum[:6]...)
	return recoveryPrefix + base64.RawURLEncoding.EncodeToString(payload)
}

// DecodeRecoverySecret validates and returns a recovery key supplied by the owner.
func DecodeRecoverySecret(value string) ([]byte, error) {
	if !strings.HasPrefix(value, recoveryPrefix) {
		return nil, errors.New("invalid SessionSift recovery secret")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, recoveryPrefix))
	if err != nil || len(payload) != MasterKeySize+6 {
		return nil, errors.New("invalid SessionSift recovery secret")
	}
	if recoveryPrefix+base64.RawURLEncoding.EncodeToString(payload) != value {
		return nil, errors.New("invalid SessionSift recovery secret encoding")
	}
	key := append([]byte{}, payload[:MasterKeySize]...)
	checksum := sha256.Sum256(append([]byte("sessionsift.sync/recovery/v1\x00"), key...))
	if !hmac.Equal(payload[MasterKeySize:], checksum[:6]) {
		return nil, errors.New("invalid SessionSift recovery secret checksum")
	}
	return key, nil
}

// ObjectID derives a stable opaque ID without revealing the local identifier.
func ObjectID(masterKey []byte, namespace, localID string) string {
	mac := hmac.New(sha256.New, masterKey)
	_, _ = mac.Write([]byte("sessionsift.sync/object/v1\x00" + namespace + "\x00" + localID))
	return "obj_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:24])
}

// Seal encrypts a record with XChaCha20-Poly1305 and domain-separated HKDF keying.
func Seal(masterKey []byte, metadata Metadata, plaintext []byte, source io.Reader) (Envelope, error) {
	if err := validateMetadata(masterKey, metadata); err != nil {
		return Envelope{}, err
	}
	key, err := deriveRecordKey(masterKey, metadata)
	if err != nil {
		return Envelope{}, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return Envelope{}, err
	}
	if source == nil {
		source = rand.Reader
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err = io.ReadFull(source, nonce); err != nil {
		return Envelope{}, fmt.Errorf("generate sync nonce: %w", err)
	}
	aad, _ := json.Marshal(metadata)
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)
	return Envelope{Metadata: metadata, Nonce: base64.RawURLEncoding.EncodeToString(nonce), Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext)}, nil
}

// Open authenticates metadata and decrypts one portable record.
func Open(masterKey []byte, envelope Envelope) ([]byte, error) {
	if err := validateMetadata(masterKey, envelope.Metadata); err != nil {
		return nil, err
	}
	nonce, nonceErr := base64.RawURLEncoding.DecodeString(envelope.Nonce)
	ciphertext, cipherErr := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	if nonceErr != nil || cipherErr != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, errors.New("invalid sync ciphertext encoding")
	}
	key, err := deriveRecordKey(masterKey, envelope.Metadata)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	aad, _ := json.Marshal(envelope.Metadata)
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, errors.New("sync ciphertext authentication failed")
	}
	return plaintext, nil
}

func deriveRecordKey(masterKey []byte, metadata Metadata) ([]byte, error) {
	info, _ := json.Marshal([]any{"sessionsift.sync/record/v1", metadata.WorkspaceID, metadata.ObjectID, metadata.SourceDeviceID, metadata.Kind, metadata.Epoch})
	reader := hkdf.New(sha256.New, masterKey, nil, info)
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := io.ReadFull(reader, key)
	return key, err
}

func validateMetadata(masterKey []byte, value Metadata) error {
	if len(masterKey) != MasterKeySize {
		return errors.New("sync master key must be 256 bits")
	}
	if value.Schema != SchemaVersion || value.WorkspaceID == "" || !strings.HasPrefix(value.ObjectID, "obj_") || value.SourceDeviceID == "" || value.WriterDeviceID == "" || value.Kind == "" || value.Epoch == 0 || value.Revision == 0 {
		return errors.New("invalid sync authenticated metadata")
	}
	return nil
}

// SaveMasterKey stores the key only in an explicitly private local fallback.
// OS credential-store adapters can implement the same contract without changing
// the sync engine; this fallback refuses symlinks, replacement, and broad modes.
func SaveMasterKey(path string, key []byte) error {
	secret := EncodeRecoverySecret(key)
	if secret == "" {
		return errors.New("sync master key must be 256 bits")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(secret + "\n")
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	return writeErr
}

// LoadMasterKey reads a private fallback key and refuses symlinks or broad permissions.
func LoadMasterKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("sync key file must be a private regular file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeRecoverySecret(strings.TrimSpace(string(body)))
}

// ReplaceMasterKey atomically rotates an existing private fallback key without
// ever following a symlink or leaving the replacement broadly readable.
func ReplaceMasterKey(path string, key []byte) error {
	if _, err := LoadMasterKey(path); err != nil {
		return err
	}
	secret := EncodeRecoverySecret(key)
	if secret == "" {
		return errors.New("sync master key must be 256 bits")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sync-key-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0o600); err == nil {
		_, err = file.WriteString(secret + "\n")
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// RemoveMasterKey removes only a validated private key file.
func RemoveMasterKey(path string) error {
	if _, err := LoadMasterKey(path); err != nil {
		return err
	}
	return os.Remove(path)
}

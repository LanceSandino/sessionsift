// Package bridge encrypts transient relay payloads before they enter durable storage.
package bridge

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	bridgeKEKSize = 32
	envelopeMagic = "SSF1"
)

var (
	wrappedKeyAAD = []byte("sessionsift.bridge.wrapped-dek.v1")
	payloadAAD    = []byte("sessionsift.bridge.relay-payload.v1")
)

type envelopeCipher struct {
	kek       cipher.AEAD
	backupKey []byte
}

func newEnvelopeCipher(key []byte) (*envelopeCipher, error) {
	if len(key) != bridgeKEKSize {
		return nil, errors.New("bridge encryption key must contain exactly 32 bytes")
	}
	block, err := aes.NewCipher(append([]byte(nil), key...))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &envelopeCipher{kek: aead, backupKey: append([]byte(nil), key...)}, nil
}

func (c *envelopeCipher) Seal(plain []byte) ([]byte, error) {
	if c == nil || c.kek == nil {
		return nil, errors.New("bridge encryption is unavailable")
	}
	dek := make([]byte, bridgeKEKSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, err
	}
	defer zeroBytes(dek)
	bodyBlock, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	bodyCipher, err := cipher.NewGCM(bodyBlock)
	if err != nil {
		return nil, err
	}
	wrapNonce := make([]byte, c.kek.NonceSize())
	bodyNonce := make([]byte, bodyCipher.NonceSize())
	if _, err = io.ReadFull(rand.Reader, wrapNonce); err != nil {
		return nil, err
	}
	if _, err = io.ReadFull(rand.Reader, bodyNonce); err != nil {
		return nil, err
	}
	wrapped := c.kek.Seal(nil, wrapNonce, dek, wrappedKeyAAD)
	sealed := bodyCipher.Seal(nil, bodyNonce, plain, payloadAAD)
	result := make([]byte, 0, len(envelopeMagic)+len(wrapNonce)+len(wrapped)+len(bodyNonce)+len(sealed))
	result = append(result, envelopeMagic...)
	result = append(result, wrapNonce...)
	result = append(result, wrapped...)
	result = append(result, bodyNonce...)
	return append(result, sealed...), nil
}

func (c *envelopeCipher) Open(envelope []byte) ([]byte, error) {
	if c == nil || c.kek == nil {
		return nil, errors.New("bridge encryption is unavailable")
	}
	wrapNonceSize := c.kek.NonceSize()
	wrappedSize := bridgeKEKSize + c.kek.Overhead()
	minimum := len(envelopeMagic) + wrapNonceSize + wrappedSize + 12 + 16
	if len(envelope) < minimum || !bytes.Equal(envelope[:len(envelopeMagic)], []byte(envelopeMagic)) {
		return nil, errors.New("invalid bridge envelope")
	}
	offset := len(envelopeMagic)
	wrapNonce := envelope[offset : offset+wrapNonceSize]
	offset += wrapNonceSize
	wrapped := envelope[offset : offset+wrappedSize]
	offset += wrappedSize
	dek, err := c.kek.Open(nil, wrapNonce, wrapped, wrappedKeyAAD)
	if err != nil {
		return nil, errors.New("bridge envelope key authentication failed")
	}
	defer zeroBytes(dek)
	bodyBlock, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	bodyCipher, err := cipher.NewGCM(bodyBlock)
	if err != nil {
		return nil, err
	}
	if len(envelope[offset:]) < bodyCipher.NonceSize()+bodyCipher.Overhead() {
		return nil, errors.New("invalid bridge envelope body")
	}
	bodyNonce := envelope[offset : offset+bodyCipher.NonceSize()]
	offset += bodyCipher.NonceSize()
	plain, err := bodyCipher.Open(nil, bodyNonce, envelope[offset:], payloadAAD)
	if err != nil {
		return nil, errors.New("bridge envelope body authentication failed")
	}
	return plain, nil
}

func (c *envelopeCipher) IsEnvelope(value []byte) bool {
	return len(value) >= len(envelopeMagic) && bytes.Equal(value[:len(envelopeMagic)], []byte(envelopeMagic))
}

func loadOrCreateBridgeCipher(databasePath string) (*envelopeCipher, error) {
	keyPath := os.Getenv("SESSIONSIFT_BRIDGE_KEK_FILE")
	if keyPath == "" {
		keyPath = databasePath + ".key"
	}
	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		// Never silently generate a replacement for an encrypted existing database.
		if _, dbErr := os.Stat(databasePath); dbErr == nil {
			db, openErr := sql.Open("sqlite", databasePath)
			if openErr != nil {
				return nil, openErr
			}
			defer db.Close()
			var count int
			if queryErr := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='bridge_key_check'`).Scan(&count); queryErr != nil {
				return nil, queryErr
			}
			if count > 0 {
				return nil, errors.New("bridge key missing: restore the matching database and key backup")
			}
			if queryErr := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='tasks'`).Scan(&count); queryErr != nil {
				return nil, queryErr
			}
			if count > 0 {
				if queryErr := db.QueryRow(`SELECT count(*) FROM tasks WHERE substr(request_json,1,4)=? OR substr(response_json,1,4)=?`, []byte(envelopeMagic), []byte(envelopeMagic)).Scan(&count); queryErr != nil {
					return nil, queryErr
				}
				if count > 0 {
					return nil, errors.New("encrypted bridge payloads require their original key")
				}
			}
		}
	}
	key, err := loadOrCreateBridgeKey(keyPath)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(key)
	return newEnvelopeCipher(key)
}

func loadOrCreateBridgeKey(path string) ([]byte, error) {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("bridge key must be a regular file, not a symlink")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create bridge key directory: %w", err)
	}
	body, err := os.ReadFile(path)
	if err == nil {
		if decoded, decodeErr := base64.RawURLEncoding.DecodeString(string(bytes.TrimSpace(body))); decodeErr == nil && len(decoded) == bridgeKEKSize {
			if err = os.Chmod(path, 0o600); err != nil {
				return nil, err
			}
			return decoded, nil
		}
		if len(body) == bridgeKEKSize {
			if err = os.Chmod(path, 0o600); err != nil {
				return nil, err
			}
			return body, nil
		}
		return nil, errors.New("bridge key file must contain 32 raw bytes or base64url-encoded 32 bytes")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read bridge key: %w", err)
	}
	key := make([]byte, bridgeKEKSize)
	if _, err = io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	encoded := []byte(base64.RawURLEncoding.EncodeToString(key) + "\n")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		zeroBytes(key)
		return nil, fmt.Errorf("create bridge key: %w", err)
	}
	if _, err = file.Write(encoded); err != nil {
		_ = file.Close()
		zeroBytes(key)
		return nil, fmt.Errorf("write bridge key: %w", err)
	}
	if err = file.Close(); err != nil {
		zeroBytes(key)
		return nil, err
	}
	return key, nil
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

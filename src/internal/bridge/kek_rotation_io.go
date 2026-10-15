// @module BridgeRotationIO durably publishes protected rotation files and validates paired snapshots.
package bridge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type rotationIO struct{ fault func(string) error }

func (fs rotationIO) point(label string) error {
	if fs.fault != nil {
		return fs.fault(label)
	}
	return nil
}

func readBounded(file *os.File, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("invalid protected bridge file size")
	}
	return body, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

// The random, exclusively created temporary stays on the target filesystem. Keys never reach argv/logs.
func (fs rotationIO) replace(label, destination string, write func(*os.File) error) error {
	if err := privateDirectory(filepath.Dir(destination)); err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 {
			return errors.New("unsafe bridge replacement target")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".kek-pending-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	if err = fs.point(label + "-create"); err != nil {
		return err
	}
	if err = write(file); err != nil {
		return err
	}
	if err = fs.point(label + "-write"); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = fs.point(label + "-fsync"); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = fs.point(label + "-close"); err != nil {
		return err
	}
	if err = os.Rename(name, destination); err != nil {
		return err
	}
	if err = fs.point(label + "-rename"); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(destination)); err != nil {
		return err
	}
	return fs.point(label + "-dirsync")
}

func (fs rotationIO) copy(label, source, destination string) error {
	// Database snapshots may exceed the key/journal bound; stream without plaintext or unbounded memory.
	file, err := openPrivateFile(source)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("invalid bridge snapshot file")
	}
	return fs.replace(label, destination, func(out *os.File) error { _, err := io.Copy(out, file); return err })
}

func fileDigest(path string) (string, error) {
	file, err := openPrivateFile(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func rotationKeyPath(database string) string {
	if path := os.Getenv("SESSIONSIFT_BRIDGE_KEK_FILE"); path != "" {
		return path
	}
	return database + ".key"
}

func existingCipher(path string) (*envelopeCipher, error) {
	body, err := readPrivate(path, 128)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(body)
	if len(body) == bridgeKEKSize {
		return newEnvelopeCipher(body)
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, errors.New("malformed bridge key")
	}
	defer zeroBytes(key)
	return newEnvelopeCipher(key)
}

func readonlyBridgeDB(path string) (*sql.DB, error) {
	return readonlyBridgeDBMode(path, false)
}

func readonlyBridgeDBMode(path string, immutable bool) (*sql.DB, error) {
	query := "mode=ro"
	if immutable {
		query += "&immutable=1"
	}
	value := &url.URL{Scheme: "file", Path: path, RawQuery: query}
	db, err := sql.Open("sqlite", value.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func openWritableSnapshot(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// No migrations, key creation or ciphertext repair during validation.
func validateRotationPair(ctx context.Context, path, keyPath string) (int64, string, int, error) {
	return validateRotationPairMode(ctx, path, keyPath, false)
}

func validateRotationPairMode(ctx context.Context, path, keyPath string, immutable bool) (int64, string, int, error) {
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return 0, "", 0, err
	}
	file, err := openPrivateFile(path)
	if err != nil {
		return 0, "", 0, err
	}
	file.Close()
	cipher, err := existingCipher(keyPath)
	if err != nil {
		return 0, "", 0, err
	}
	defer zeroBytes(cipher.backupKey)
	db, err := readonlyBridgeDBMode(path, immutable)
	if err != nil {
		return 0, "", 0, err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return 0, "", 0, errors.New("bridge snapshot integrity failed")
	}
	var verifier []byte
	if err = db.QueryRowContext(ctx, `SELECT envelope FROM bridge_key_check WHERE id=1`).Scan(&verifier); err != nil {
		return 0, "", 0, errors.New("bridge snapshot verifier missing")
	}
	plain, err := cipher.Open(verifier)
	valid := err == nil && string(plain) == "sessionsift.key-check.v1"
	zeroBytes(plain)
	if !valid {
		return 0, "", 0, errors.New("bridge snapshot key mismatch")
	}
	rows, err := db.QueryContext(ctx, `SELECT request_json, response_json FROM tasks`)
	if err != nil {
		return 0, "", 0, err
	}
	count := 1
	for rows.Next() {
		var request, response []byte
		if err = rows.Scan(&request, &response); err != nil {
			rows.Close()
			return 0, "", 0, err
		}
		for _, blob := range [][]byte{request, response} {
			if blob == nil {
				continue
			}
			plain, err = cipher.Open(blob)
			zeroBytes(plain)
			if err != nil {
				rows.Close()
				return 0, "", 0, errors.New("bridge snapshot contains invalid or mixed-key ciphertext")
			}
			count++
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, "", 0, err
	}
	generation, operation, err := rotationGeneration(ctx, db)
	return generation, operation, count, err
}

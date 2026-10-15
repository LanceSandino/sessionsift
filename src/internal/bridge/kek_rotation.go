// @module BridgeKEKRotation coordinates offline paired-snapshot cutover and explicit recovery.
package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var operationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type RotationResult struct {
	Generation      int64  `json:"generation"`
	Operation       string `json:"operation"`
	Records         int    `json:"records"`
	AlreadyComplete bool   `json:"already_complete"`
}

type rotationJournal struct {
	Version       int    `json:"version"`
	Operation     string `json:"operation"`
	OldGeneration int64  `json:"old_generation"`
	NewGeneration int64  `json:"new_generation"`
	Records       int    `json:"records"`
	OriginalDB    string `json:"original_db_sha256"`
	OriginalKey   string `json:"original_key_sha256"`
	OldDB         string `json:"old_db_sha256"`
	OldKey        string `json:"old_key_sha256"`
	NewDB         string `json:"new_db_sha256"`
	NewKey        string `json:"new_key_sha256"`
	KeyLocation   string `json:"key_location_sha256"`
}

func keyLocation(path string) string {
	digest := sha256.Sum256([]byte(path))
	return hex.EncodeToString(digest[:])
}

func rotationPaths(path string) (string, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", "", errors.New("bridge maintenance requires a clean absolute database path")
	}
	key := rotationKeyPath(path)
	if !filepath.IsAbs(key) || filepath.Clean(key) != key || key == path {
		return "", "", errors.New("invalid bridge key file location")
	}
	if err := privateDirectory(filepath.Dir(key)); err != nil {
		return "", "", err
	}
	// Compare resolved parents too: /var and /private/var or other directory aliases
	// must not give the KEK a second spelling for the inode carrying our flock.
	dbParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", "", err
	}
	keyParent, err := filepath.EvalSymlinks(filepath.Dir(key))
	if err != nil {
		return "", "", err
	}
	databaseCanonical := filepath.Join(dbParent, filepath.Base(path))
	keyCanonical := filepath.Join(keyParent, filepath.Base(key))
	for _, suffix := range []string{"", ".maintenance.lock", ".rotation-journal", "-wal", "-shm", "-journal"} {
		if keyCanonical == databaseCanonical+suffix {
			return "", "", errors.New("bridge key path aliases a reserved database, lock, journal or SQLite sidecar")
		}
	}
	if strings.HasPrefix(keyCanonical, databaseCanonical+".rotation-") {
		return "", "", errors.New("bridge key path must not use the rotation workspace")
	}
	return key, path + ".rotation-journal", nil
}

// RotateKEK is opt-in, stopped-service maintenance. It generates a new server KEK, not a user sync key.
// Repeating a completed operation is a no-op; an interrupted cutover requires explicit RecoverKEK.
func RotateKEK(ctx context.Context, path, operation string) (RotationResult, error) {
	return rotateKEK(ctx, path, operation, rotationIO{})
}

// PreflightKEK authenticates retained envelopes without migrating or changing keys.
// Creating the protected cooperative lock file is its only filesystem side effect.
func PreflightKEK(ctx context.Context, path string) (RotationResult, error) {
	if err := ctx.Err(); err != nil {
		return RotationResult{}, err
	}
	key, journal, err := rotationPaths(path)
	if err != nil {
		return RotationResult{}, err
	}
	lock, err := acquireMaintenanceLock(path)
	if err != nil {
		return RotationResult{}, err
	}
	defer lock.Close()
	if _, err := os.Lstat(journal); !os.IsNotExist(err) {
		return RotationResult{}, errors.New("pending bridge cutover: run explicit kek-recover")
	}
	// Immutable reads must not ignore pending WAL or rollback state.
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !os.IsNotExist(err) {
			return RotationResult{}, errors.New("preflight requires a cleanly closed database without SQLite sidecars; preserve and recover pending state first")
		}
	}
	generation, operation, count, err := validateRotationPairMode(ctx, path, key, true)
	return RotationResult{Generation: generation, Operation: operation, Records: count}, err
}

func rotateKEK(ctx context.Context, path, operation string, fs rotationIO) (RotationResult, error) {
	result := RotationResult{Operation: operation}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !operationPattern.MatchString(operation) {
		return result, errors.New("operation ID must be 1-64 ASCII letters, digits, underscore or hyphen")
	}
	keyPath, journalPath, err := rotationPaths(path)
	if err != nil {
		return result, err
	}
	lock, err := acquireMaintenanceLock(path)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if _, err = os.Lstat(journalPath); !os.IsNotExist(err) {
		return result, errors.New("pending bridge cutover: run explicit kek-recover")
	}
	generation, previousOperation, _, err := validateRotationPair(ctx, path, keyPath)
	if err != nil {
		return result, err
	}
	if previousOperation == operation {
		result.Generation, result.AlreadyComplete = generation, true
		return result, nil
	}
	if generation == math.MaxInt64 {
		return result, errors.New("bridge key generation exhausted")
	}
	root := path + ".rotation-" + operation
	if err = os.Mkdir(root, 0700); err != nil {
		return result, errors.New("rotation operation directory exists or is unavailable; retain it and use a new operation ID after checking the live pair")
	}
	if err = syncDirectory(filepath.Dir(root)); err != nil {
		return result, err
	}
	if err = fs.point("stage-directory"); err != nil {
		return result, err
	}
	// Reuse the existing consistent SQLite/loaded-key snapshot implementation under the offline lock.
	store, err := openBridgeStore(path)
	if err != nil {
		return result, err
	}
	defer store.Close()
	if _, err = store.db.ExecContext(ctx, `PRAGMA locking_mode=EXCLUSIVE; PRAGMA wal_checkpoint(TRUNCATE); PRAGMA journal_mode=DELETE; BEGIN EXCLUSIVE; COMMIT;`); err != nil {
		return result, errors.New("bridge database is busy: stop all legacy writers before rotation")
	}
	if err = fs.point("before-old-backup"); err != nil {
		return result, err
	}
	oldPair, err := store.Backup(ctx, root)
	if err != nil {
		return result, errors.New("could not create protected pre-rotation pair")
	}
	if err = fs.point("old-backup-created"); err != nil {
		return result, err
	}
	old := filepath.Join(root, "old")
	if err = os.Rename(oldPair, old); err != nil {
		return result, err
	}
	for _, name := range []string{"bridge.db", "bridge.db.key"} {
		if err = syncFile(filepath.Join(old, name)); err != nil {
			return result, err
		}
		if err = fs.point("old-" + name + "-fsync"); err != nil {
			return result, err
		}
	}
	if err = syncDirectory(old); err != nil {
		return result, err
	}
	if err = fs.point("old-directory-fsync"); err != nil {
		return result, err
	}
	if err = syncDirectory(root); err != nil {
		return result, err
	}
	if err = fs.point("old-pair-durable"); err != nil {
		return result, err
	}
	next := filepath.Join(root, "new")
	if err = os.Mkdir(next, 0700); err != nil {
		return result, err
	}
	if err = syncDirectory(root); err != nil {
		return result, err
	}
	nextDB, nextKey := filepath.Join(next, "bridge.db"), filepath.Join(next, "bridge.db.key")
	if err = fs.copy("stage-db", filepath.Join(old, "bridge.db"), nextDB); err != nil {
		return result, err
	}
	key := make([]byte, bridgeKEKSize)
	if _, err = io.ReadFull(rand.Reader, key); err != nil {
		return result, err
	}
	defer zeroBytes(key)
	nextCipher, err := newEnvelopeCipher(key)
	if err != nil {
		return result, err
	}
	defer zeroBytes(nextCipher.backupKey)
	encoded := []byte(base64.RawURLEncoding.EncodeToString(key) + "\n")
	defer zeroBytes(encoded)
	if err = fs.replace("stage-key", nextKey, func(file *os.File) error { _, err := file.Write(encoded); return err }); err != nil {
		return result, err
	}
	db, err := openWritableSnapshot(nextDB)
	if err != nil {
		return result, err
	}
	result.Generation = generation + 1
	if err = fs.point("before-rewrap-transaction"); err == nil {
		result.Records, err = rewrapDatabase(ctx, db, store.cipher, nextCipher, result.Generation, operation)
	}
	closeErr := db.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	if err = fs.point("rewrap-transaction-committed"); err != nil {
		return result, err
	}
	if err = syncFile(nextDB); err != nil {
		return result, err
	}
	if err = syncDirectory(next); err != nil {
		return result, err
	}
	if err = fs.point("new-pair-fsync"); err != nil {
		return result, err
	}
	if _, _, _, err = validateRotationPair(ctx, nextDB, nextKey); err != nil {
		return result, err
	}
	if err = fs.point("new-pair-verified"); err != nil {
		return result, err
	}
	if err = store.Close(); err != nil {
		return result, err
	}
	if err = syncFile(path); err != nil {
		return result, err
	}
	journal := rotationJournal{Version: 1, Operation: operation, OldGeneration: generation, NewGeneration: result.Generation, Records: result.Records, KeyLocation: keyLocation(keyPath)}
	for _, item := range []struct {
		path   string
		digest *string
	}{{path, &journal.OriginalDB}, {keyPath, &journal.OriginalKey}, {filepath.Join(old, "bridge.db"), &journal.OldDB}, {filepath.Join(old, "bridge.db.key"), &journal.OldKey}, {nextDB, &journal.NewDB}, {nextKey, &journal.NewKey}} {
		*item.digest, err = fileDigest(item.path)
		if err != nil {
			return result, err
		}
	}
	body, err := json.Marshal(journal)
	if err != nil {
		return result, err
	}
	if err = fs.replace("journal", journalPath, func(file *os.File) error { _, err := file.Write(body); return err }); err != nil {
		return result, err
	}
	return recoverKEKLocked(ctx, path, keyPath, journalPath, "commit", fs)
}

// RecoverKEK deliberately resumes the staged new pair or restores the protected old pair.
// It never guesses which key goes with a partially replaced database and never generates a key.
func RecoverKEK(ctx context.Context, path, action string) (RotationResult, error) {
	if action != "commit" && action != "rollback" {
		return RotationResult{}, errors.New("recovery action must be commit or rollback")
	}
	key, journal, err := rotationPaths(path)
	if err != nil {
		return RotationResult{}, err
	}
	lock, err := acquireMaintenanceLock(path)
	if err != nil {
		return RotationResult{}, err
	}
	defer lock.Close()
	return recoverKEKLocked(ctx, path, key, journal, action, rotationIO{})
}

func recoverKEKLocked(ctx context.Context, path, keyPath, journalPath, action string, fs rotationIO) (RotationResult, error) {
	result := RotationResult{}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if _, err := os.Lstat(journalPath); os.IsNotExist(err) {
		if action == "rollback" {
			return result, errors.New("no pending cutover: use a verified matching pre-rotation backup for a separately authorized restore")
		}
		generation, operation, _, err := validateRotationPair(ctx, path, keyPath)
		result.Generation, result.Operation, result.AlreadyComplete = generation, operation, true
		return result, err
	}
	body, err := readPrivate(journalPath, 4096)
	if err != nil {
		return result, err
	}
	var journal rotationJournal
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&journal); err != nil {
		return result, errors.New("invalid bridge rotation journal")
	}
	canonical, _ := json.Marshal(journal)
	if !bytes.Equal(body, canonical) || journal.Version != 1 || !operationPattern.MatchString(journal.Operation) || journal.OldGeneration < 0 || journal.NewGeneration != journal.OldGeneration+1 || journal.Records < 1 || journal.KeyLocation != keyLocation(keyPath) {
		return result, errors.New("invalid bridge rotation journal metadata")
	}
	root := path + ".rotation-" + journal.Operation
	if err = privateDirectory(root); err != nil {
		return result, err
	}
	old, next := filepath.Join(root, "old"), filepath.Join(root, "new")
	for _, pair := range []struct {
		directory, dbDigest, keyDigest string
		generation                     int64
	}{{old, journal.OldDB, journal.OldKey, journal.OldGeneration}, {next, journal.NewDB, journal.NewKey, journal.NewGeneration}} {
		for _, file := range []struct{ name, digest string }{{"bridge.db", pair.dbDigest}, {"bridge.db.key", pair.keyDigest}} {
			digest, err := fileDigest(filepath.Join(pair.directory, file.name))
			if err != nil || digest != file.digest {
				return result, errors.New("bridge rotation pair changed or unavailable")
			}
		}
		generation, operation, count, err := validateRotationPair(ctx, filepath.Join(pair.directory, "bridge.db"), filepath.Join(pair.directory, "bridge.db.key"))
		if err != nil || generation != pair.generation || count != journal.Records || (pair.directory == next && operation != journal.Operation) {
			return result, errors.New("bridge rotation pair validation failed")
		}
	}
	// Reject stale/replayed journals and unexpected live data rather than discarding another generation.
	liveDB, err := fileDigest(path)
	if err != nil || (liveDB != journal.OriginalDB && liveDB != journal.OldDB && liveDB != journal.NewDB) {
		return result, errors.New("unexpected live bridge generation: preserve state for manual recovery")
	}
	liveKey, err := fileDigest(keyPath)
	if err != nil || (liveKey != journal.OriginalKey && liveKey != journal.OldKey && liveKey != journal.NewKey) {
		return result, errors.New("unexpected live bridge key: preserve state for manual recovery")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !os.IsNotExist(err) {
			return result, errors.New("unexpected SQLite sidecar: stop writers and preserve state")
		}
	}
	selected := next
	result.Generation = journal.NewGeneration
	if action == "rollback" {
		selected, result.Generation = old, journal.OldGeneration
	}
	result.Operation, result.Records = journal.Operation, journal.Records
	if err = fs.copy("live-db", filepath.Join(selected, "bridge.db"), path); err != nil {
		return result, err
	}
	if err = fs.copy("live-key", filepath.Join(selected, "bridge.db.key"), keyPath); err != nil {
		return result, err
	}
	if _, _, _, err = validateRotationPair(ctx, path, keyPath); err != nil {
		return result, err
	}
	if err = fs.point("live-pair-verified"); err != nil {
		return result, err
	}
	if err = os.Remove(journalPath); err != nil {
		return result, err
	}
	if err = fs.point("journal-removed"); err != nil {
		return result, err
	}
	if err = syncDirectory(filepath.Dir(journalPath)); err != nil {
		return result, err
	}
	return result, fs.point("cutover-durable")
}

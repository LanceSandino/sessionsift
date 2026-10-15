// @module BridgeKEKRewrap authenticates relay records and replaces only their wrapped DEKs.
package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
)

func (c *envelopeCipher) rewrap(envelope []byte, replacement *envelopeCipher) ([]byte, error) {
	plain, err := c.Open(envelope)
	if err != nil {
		return nil, errors.New("bridge rotation found unauthenticated ciphertext")
	}
	zeroBytes(plain)
	offset := len(envelopeMagic)
	nonceSize, wrappedSize := c.kek.NonceSize(), bridgeKEKSize+c.kek.Overhead()
	dek, err := c.kek.Open(nil, envelope[offset:offset+nonceSize], envelope[offset+nonceSize:offset+nonceSize+wrappedSize], wrappedKeyAAD)
	if err != nil {
		return nil, errors.New("bridge rotation cannot unwrap record")
	}
	defer zeroBytes(dek)
	nonce := make([]byte, replacement.kek.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	result := append([]byte(envelopeMagic), nonce...)
	result = append(result, replacement.kek.Seal(nil, nonce, dek, wrappedKeyAAD)...)
	result = append(result, envelope[offset+nonceSize+wrappedSize:]...)
	plain, err = replacement.Open(result)
	zeroBytes(plain)
	if err != nil {
		return nil, errors.New("bridge rotation verification failed")
	}
	return result, nil
}

func rotationGeneration(ctx context.Context, db *sql.DB) (int64, string, error) {
	var present int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='bridge_kek_generation'`).Scan(&present)
	if err != nil || present == 0 {
		return 0, "", err
	}
	var generation int64
	var operation string
	err = db.QueryRowContext(ctx, `SELECT generation, operation FROM bridge_kek_generation WHERE id=1`).Scan(&generation, &operation)
	if err == nil && (generation < 1 || !operationPattern.MatchString(operation)) {
		err = errors.New("invalid bridge key generation")
	}
	return generation, operation, err
}

// Only relay envelopes are touched. Device identities, receipts, grants, TTLs and sync ciphertext stay intact.
func rewrapDatabase(ctx context.Context, db *sql.DB, old, next *envelopeCipher, generation int64, operation string) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	count, after := 0, ""
	for {
		rows, err := tx.QueryContext(ctx, `SELECT id, request_json, response_json FROM tasks WHERE id>? ORDER BY id LIMIT 128`, after)
		if err != nil {
			return 0, err
		}
		type record struct {
			id                string
			request, response []byte
		}
		batch := []record{}
		for rows.Next() {
			var r record
			if err = rows.Scan(&r.id, &r.request, &r.response); err != nil {
				rows.Close()
				return 0, err
			}
			batch = append(batch, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			request, err := old.rewrap(r.request, next)
			if err != nil {
				return 0, err
			}
			var response any
			if r.response != nil {
				response, err = old.rewrap(r.response, next)
				if err != nil {
					return 0, err
				}
				count++
			}
			if _, err = tx.ExecContext(ctx, `UPDATE tasks SET request_json=?, response_json=? WHERE id=?`, request, response, r.id); err != nil {
				return 0, err
			}
			count++
			after = r.id
		}
	}
	var verifier []byte
	if err = tx.QueryRowContext(ctx, `SELECT envelope FROM bridge_key_check WHERE id=1`).Scan(&verifier); err != nil {
		return 0, err
	}
	plain, err := old.Open(verifier)
	if err != nil || !bytes.Equal(plain, []byte("sessionsift.key-check.v1")) {
		return 0, errors.New("invalid bridge key verifier")
	}
	zeroBytes(plain)
	verifier, err = old.rewrap(verifier, next)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bridge_key_check SET envelope=? WHERE id=1`, verifier); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS bridge_kek_generation(id INTEGER PRIMARY KEY CHECK(id=1), generation INTEGER NOT NULL, operation TEXT NOT NULL); CREATE TABLE IF NOT EXISTS bridge_kek_history(operation TEXT PRIMARY KEY, generation INTEGER NOT NULL, record_count INTEGER NOT NULL)`); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO bridge_kek_generation(id,generation,operation) VALUES(1,?,?)`, generation, operation); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bridge_kek_history(operation,generation,record_count) VALUES(?,?,?)`, operation, generation, count+1); err != nil {
		return 0, err
	}
	return count + 1, tx.Commit()
}

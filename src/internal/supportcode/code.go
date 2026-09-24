// Package supportcode generates and verifies the short codes a source machine
// shows so another person can connect (support sessions and device links).
// @module SupportCode owns the code alphabet, normalization, and domain-separated hashing.
package supportcode

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

// Crockford base32 omits I, L, O and U so codes survive being read aloud.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Length is the number of significant characters (60 bits of entropy).
const Length = 12

// Purpose domain-separates hashes so a link code can never act as a support code.
type Purpose string

const (
	Support Purpose = "support"
	Link    Purpose = "link"
)

// Generate returns a new code formatted as XXXX-XXXX-XXXX.
func Generate() (string, error) {
	buffer := make([]byte, Length)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	var builder strings.Builder
	for index, value := range buffer {
		if index > 0 && index%4 == 0 {
			builder.WriteByte('-')
		}
		builder.WriteByte(alphabet[int(value)%len(alphabet)])
	}
	return builder.String(), nil
}

// Normalize removes separators and maps look-alike characters, returning an
// error unless exactly Length valid characters remain.
func Normalize(code string) (string, error) {
	var builder strings.Builder
	for _, character := range strings.ToUpper(code) {
		switch character {
		case '-', ' ', '\t':
			continue
		case 'O':
			character = '0'
		case 'I', 'L':
			character = '1'
		}
		if !strings.ContainsRune(alphabet, character) {
			return "", errors.New("code contains invalid characters")
		}
		builder.WriteRune(character)
		if builder.Len() > Length {
			return "", errors.New("code is too long")
		}
	}
	if builder.Len() != Length {
		return "", errors.New("code must have 12 characters")
	}
	return builder.String(), nil
}

// Hash returns the stored identity of a code for one purpose.
func Hash(purpose Purpose, code string) (string, error) {
	normalized, err := Normalize(code)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte("sessionsift." + string(purpose) + ".v1\x00" + normalized))
	return hex.EncodeToString(digest[:]), nil
}

// Matches compares a presented code against a stored hash in constant time.
func Matches(purpose Purpose, code, stored string) bool {
	candidate, err := Hash(purpose, code)
	if err != nil || len(stored) != len(candidate) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(stored)) == 1
}

// ValidHash reports whether value has the shape of a stored code hash.
func ValidHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

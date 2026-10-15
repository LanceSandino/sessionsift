// @module SyncKeyStore keeps client decryption keys in macOS Keychain when available, with an explicit private-file fallback.
package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const keychainService = "com.sessionsift.sync.master-key"

var keychainAvailable = func() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	if _, err := os.Stat("/usr/bin/security"); err != nil {
		return false
	}
	_, err := os.Stat("/usr/bin/expect")
	return err == nil
}

var runSecurity = func(ctx context.Context, input string, args ...string) ([]byte, error) {
	if input != "" {
		for _, value := range append(append([]string{}, args...), input) {
			if strings.IndexFunc(value, func(character rune) bool {
				return !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune(" ._-/\n", character)
			}) >= 0 {
				return nil, errors.New("invalid credential-store input")
			}
		}
		quoted := make([]string, 0, len(args))
		for _, argument := range args {
			quoted = append(quoted, strconv.Quote(argument))
		}
		script := "set timeout 15\nlog_user 0\nspawn /usr/bin/security " + strings.Join(quoted, " ") + "\nexpect -re {(?i)password.*:}\nsend -- " + strconv.Quote(input) + "\nexpect -re {(?i)retype password.*:}\nsend -- " + strconv.Quote(input) + "\nexpect eof\nset result [wait]\nexit [lindex $result 3]\n"
		command := exec.CommandContext(ctx, "/usr/bin/expect", "-f", "-")
		command.Stdin = strings.NewReader(script)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		return command.Output()
	}
	command := exec.CommandContext(ctx, "/usr/bin/security", args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	return command.Output()
}

type KeyStore struct {
	FilePath string
	Account  string
}

func (s KeyStore) Provider() string {
	if os.Getenv("SESSIONSIFT_SYNC_KEY_STORE") != "file" && keychainAvailable() {
		return "macOS Keychain"
	}
	return "private local file"
}

func (s KeyStore) Load() ([]byte, error) {
	if s.Provider() == "private local file" {
		return LoadMasterKey(s.FilePath)
	}
	output, err := s.security("find-generic-password", "-a", s.Account, "-s", keychainService, "-w")
	if err != nil {
		return nil, os.ErrNotExist
	}
	return DecodeRecoverySecret(strings.TrimSpace(string(output)))
}

func (s KeyStore) Save(key []byte) error {
	if s.Provider() == "private local file" {
		return SaveMasterKey(s.FilePath, key)
	}
	if _, err := s.Load(); err == nil {
		return errors.New("a sync key is already configured")
	}
	return s.writeKeychain(key, false)
}

func (s KeyStore) Replace(key []byte) error {
	if s.Provider() == "private local file" {
		return ReplaceMasterKey(s.FilePath, key)
	}
	if _, err := s.Load(); err != nil {
		return err
	}
	return s.writeKeychain(key, true)
}

func (s KeyStore) Remove() error {
	if s.Provider() == "private local file" {
		return RemoveMasterKey(s.FilePath)
	}
	_, err := s.security("delete-generic-password", "-a", s.Account, "-s", keychainService)
	return err
}

func (s KeyStore) writeKeychain(key []byte, replace bool) error {
	secret := EncodeRecoverySecret(key)
	if secret == "" || s.Account == "" {
		return errors.New("invalid sync key store account or key")
	}
	args := []string{"add-generic-password", "-a", s.Account, "-s", keychainService, "-l", "SessionSift encrypted sync"}
	if replace {
		args = append(args, "-U")
	}
	// A trailing -w makes security request the password on a terminal. The
	// adapter answers that prompt through an in-memory Expect script so the
	// recovery secret never appears in process arguments, the environment, or a file.
	args = append(args, "-w")
	_, err := s.securityInput(secret+"\n", args...)
	return err
}

func (s KeyStore) security(args ...string) ([]byte, error) {
	return s.securityInput("", args...)
}

func (s KeyStore) securityInput(input string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := runSecurity(ctx, input, args...)
	if ctx.Err() != nil {
		return nil, errors.New("credential store timed out")
	}
	if err != nil {
		return nil, fmt.Errorf("credential store unavailable: %w", err)
	}
	return output, nil
}

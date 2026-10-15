// Package remote tests device identity and signed bridge responses.
package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadOrCreateIdentityPersistsPrivateKeyAndSigns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-identity.json")
	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
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
	if !bytes.Equal(first.PublicKey, second.PublicKey) || !Verify(second.PublicKey, payload, signature) || !bytes.Equal(signature, second.Sign(payload)) {
		t.Fatal("existing identity key or signature changed")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("loading existing identity changed stored bytes")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", info.Mode().Perm())
	}
}

// This barrier runs in each child process, after its initial missing-file read
// but before key generation finishes. All four processes must therefore race
// creation of the same previously absent path, independent of scheduling.
type identityCreationBarrier struct {
	random io.Reader
	ready  bool
}

func (b *identityCreationBarrier) Read(p []byte) (int, error) {
	if !b.ready {
		b.ready = true
		if _, err := io.WriteString(os.Stdout, "ready\n"); err != nil {
			return 0, err
		}
		if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
			return 0, err
		}
	}
	return b.random.Read(p)
}

type identityCreationResult struct {
	ID        string
	Signature []byte
}

func TestLoadOrCreateIdentityConcurrentProcessHelper(t *testing.T) {
	if os.Getenv("SESSIONSIFT_IDENTITY_TEST_CHILD") != "1" {
		return
	}
	rand.Reader = &identityCreationBarrier{random: rand.Reader}
	identity, err := LoadOrCreateIdentity(os.Getenv("SESSIONSIFT_IDENTITY_TEST_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(identityCreationResult{ID: identity.ID, Signature: identity.Sign([]byte("concurrent identity proof"))}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOrCreateIdentityConcurrentProcessesUseOneCompleteKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	directory := t.TempDir()
	path := filepath.Join(directory, "state", "device-identity.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	type child struct {
		command *exec.Cmd
		input   io.WriteCloser
		output  *bufio.Reader
		stderr  bytes.Buffer
	}
	children := make([]*child, 4)
	defer func() {
		cancel()
		for _, c := range children {
			if c != nil {
				_ = c.input.Close()
				if c.command.ProcessState == nil {
					_ = c.command.Wait()
				}
			}
		}
	}()
	for index := range children {
		c := &child{command: exec.CommandContext(ctx, executable, "-test.run=^TestLoadOrCreateIdentityConcurrentProcessHelper$", "-test.timeout=10s")}
		c.command.Env = []string{"HOME=" + directory, "TMPDIR=" + directory, "SESSIONSIFT_IDENTITY_TEST_CHILD=1", "SESSIONSIFT_IDENTITY_TEST_PATH=" + path, "GOMAXPROCS=2"}
		stdout, err := c.command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		c.output = bufio.NewReader(stdout)
		c.input, err = c.command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		c.command.Stderr = &c.stderr
		if err := c.command.Start(); err != nil {
			t.Fatal(err)
		}
		children[index] = c
	}
	for index, c := range children {
		line, err := c.output.ReadString('\n')
		if err != nil || line != "ready\n" {
			t.Fatalf("child %d did not reach missing-file barrier: %v", index, err)
		}
	}
	for _, c := range children {
		if _, err := c.input.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
		_ = c.input.Close()
	}
	results := make([]identityCreationResult, len(children))
	for index, c := range children {
		if err := json.NewDecoder(c.output).Decode(&results[index]); err != nil {
			t.Fatalf("child %d failed to return an identity: %v", index, err)
		}
		if err := c.command.Wait(); err != nil {
			t.Fatalf("child %d failed: %v", index, err)
		}
	}
	stored, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("persisted identity is incomplete: %v", err)
	}
	for index, result := range results {
		if result.ID != stored.ID || !Verify(stored.PublicKey, []byte("concurrent identity proof"), result.Signature) {
			t.Fatalf("concurrent creator %d returned a different key from the persisted identity", index)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity permissions = %o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatal("successful concurrent creation left temporary identity files")
	}
}

func TestLoadOrCreateIdentityConcurrentInvalidWinnerFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	directory := t.TempDir()
	path := filepath.Join(directory, "identity.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, executable, "-test.run=^TestLoadOrCreateIdentityConcurrentProcessHelper$", "-test.timeout=10s")
	command.Env = []string{"HOME=" + directory, "TMPDIR=" + directory, "SESSIONSIFT_IDENTITY_TEST_CHILD=1", "SESSIONSIFT_IDENTITY_TEST_PATH=" + path, "GOMAXPROCS=2"}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = input.Close()
		if command.ProcessState == nil {
			_ = command.Wait()
		}
	}()
	reader := bufio.NewReader(stdout)
	if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child did not reach missing-file barrier: %v", err)
	}
	content := []byte("invalid concurrent winner")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil || !bytes.Contains(output, []byte("invalid device identity")) {
		t.Fatal("invalid concurrent winner was accepted or replaced")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, content) {
		t.Fatal("invalid concurrent winner bytes changed")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatal("failed concurrent creation left temporary identity files")
	}
}

func TestLoadOrCreateIdentityInvalidExistingFileFailsClosed(t *testing.T) {
	for index, content := range []string{"", "{", `{ "id": "device_invalid", "public_key": "!", "private_key": "!" }`, `{ "id": "device_invalid", "public_key": "AA", "private_key": "AA" }`} {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "identity.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadOrCreateIdentity(path); err == nil {
				t.Fatal("invalid existing identity accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != content {
				t.Fatal("invalid existing identity was replaced")
			}
		})
	}
}

func TestLoadOrCreateIdentityFilesystemErrorsFailClosed(t *testing.T) {
	directory := t.TempDir()
	if _, err := LoadOrCreateIdentity(directory); err == nil {
		t.Fatal("identity directory accepted")
	}
	parent := filepath.Join(directory, "file-parent")
	if err := os.WriteFile(parent, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateIdentity(filepath.Join(parent, "identity.json")); err == nil {
		t.Fatal("unreadable identity parent accepted")
	}
	after, err := os.ReadFile(parent)
	if err != nil || string(after) != "preserve" {
		t.Fatal("filesystem error changed existing bytes")
	}
}

func TestLoadOrCreateIdentityDanglingWinnerFailsClosed(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "identity.json")
	target := filepath.Join(directory, "missing.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateIdentity(path); err == nil {
		t.Fatal("dangling existing identity was accepted")
	}
	if link, err := os.Readlink(path); err != nil || link != target {
		t.Fatal("existing identity symlink was replaced")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("failed creation wrote through existing symlink")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatal("failed creation left temporary identity files")
	}
}

// Freeze four creators after their missing-file reads, before any generated
// key can be published. This makes the overwrite oracle independent of luck
// while a separate reader checks every observed file is complete and stable.
type identityConcurrentReadBarrier struct {
	random   io.Reader
	arrivals atomic.Int32
	ready    chan struct{}
	release  chan struct{}
}

func (b *identityConcurrentReadBarrier) Read(p []byte) (int, error) {
	if b.arrivals.Add(1) == 4 {
		close(b.ready)
	}
	<-b.release
	return b.random.Read(p)
}

func TestLoadOrCreateIdentityConcurrentReadersObserveOneCompleteKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "identity.json")
	barrier := &identityConcurrentReadBarrier{random: rand.Reader, ready: make(chan struct{}), release: make(chan struct{})}
	rand.Reader = barrier
	defer func() { rand.Reader = barrier.random }()
	results := make(chan Identity, 4)
	creationErrors := make(chan error, 4)
	var creators sync.WaitGroup
	for index := 0; index < 4; index++ {
		creators.Add(1)
		go func() {
			defer creators.Done()
			identity, err := LoadOrCreateIdentity(path)
			if err != nil {
				creationErrors <- err
			} else {
				results <- identity
			}
		}()
	}
	stopReader := make(chan struct{})
	readerDone := make(chan struct{})
	readerObserved := make(chan struct{})
	readerErrors := make(chan string, 1)
	go func() {
		defer close(readerDone)
		var previous Identity
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopReader:
				return
			default:
			}
			identity, err := loadIdentity(path)
			if os.IsNotExist(err) {
				runtime.Gosched()
				continue
			}
			if err != nil || identity.ID == "" || !Verify(identity.PublicKey, []byte("reader proof"), identity.Sign([]byte("reader proof"))) {
				readerErrors <- "reader observed an incomplete or incoherent published key"
				return
			}
			if previous.ID == "" {
				previous = identity
				close(readerObserved)
			} else if previous.ID != identity.ID || !bytes.Equal(previous.PublicKey, identity.PublicKey) {
				readerErrors <- "reader observed a published identity being replaced"
				return
			}
			runtime.Gosched()
		}
	}()
	barrierReached := false
	select {
	case <-barrier.ready:
		barrierReached = true
	case <-ctx.Done():
	}
	close(barrier.release)
	creators.Wait()
	select {
	case <-readerObserved:
	case <-readerDone:
	case <-ctx.Done():
	}
	close(stopReader)
	<-readerDone
	if !barrierReached {
		t.Fatal("four creators did not reach the missing-file barrier")
	}
	select {
	case message := <-readerErrors:
		t.Fatal(message)
	default:
	}
	select {
	case <-readerObserved:
	default:
		t.Fatal("dedicated reader never observed a published key")
	}
	close(creationErrors)
	for err := range creationErrors {
		t.Fatal(err)
	}
	stored, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	close(results)
	count := 0
	for identity := range results {
		count++
		if identity.ID != stored.ID || !Verify(stored.PublicKey, []byte("creator proof"), identity.Sign([]byte("creator proof"))) {
			t.Fatal("creator returned a key different from the persisted identity")
		}
	}
	if count != 4 {
		t.Fatalf("complete creator identities = %d, want 4", count)
	}
}

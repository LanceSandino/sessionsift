//go:build assurance_binary

// @module PackagedKEKRotationTests exercises the actual candidate CLI on synthetic queued state.
package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPackagedKEKRotationCLI(t *testing.T) {
	binary := os.Getenv("SESSIONSIFT_ASSURANCE_BINARY")
	if binary == "" {
		t.Fatal("an exact candidate binary is required")
	}
	path, _, _ := rotationFixture(t, true)
	invoke := func(args ...string) (RotationResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, append([]string{"bridge"}, args...)...)
		command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Dir(path), "TMPDIR=" + filepath.Dir(path), "SESSIONSIFT_HOME=" + filepath.Join(filepath.Dir(path), "local-only-cli")}
		output, err := command.Output()
		if err != nil {
			return RotationResult{}, err
		}
		var result RotationResult
		err = json.Unmarshal(output, &result)
		return result, err
	}
	old, _ := existingCipher(path + ".key")
	preflight, err := invoke("kek-preflight", "--database", path)
	if err != nil || preflight.Records != 4 || preflight.Generation != 0 {
		t.Fatal("candidate preflight failed")
	}
	result, err := invoke("kek-rotate", "--database", path, "--operation", "packaged")
	if err != nil || result.Generation != 1 || result.Records != 4 {
		t.Fatal("candidate rotation failed")
	}
	for _, blob := range rotationRecords(t, path) {
		if _, err := old.Open(blob); err == nil {
			t.Fatal("old key accepted candidate output")
		}
	}
	retry, err := invoke("kek-rotate", "--database", path, "--operation", "packaged")
	if err != nil || !retry.AlreadyComplete {
		t.Fatal("candidate retry not idempotent")
	}
	if _, err := invoke("kek-recover", "--database", path, "--action", "rollback"); err == nil {
		t.Fatal("candidate falsely reported unjournaled rollback")
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = invoke("kek-rotate", "--database", path, "--operation", "running"); err == nil {
		store.Close()
		t.Fatal("candidate bypassed active writer lock")
	}
	store.Close()
}

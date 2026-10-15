// @module BridgeMaintenanceCLITests proves explicit commands, failures and generated-key-only arguments.
package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	bridgeapp "github.com/LanceSandino/sessionsift/src/internal/bridge"
)

func TestBridgeKEKMaintenanceCLI(t *testing.T) {
	t.Setenv("SESSIONSIFT_BRIDGE_KEK_FILE", "")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bridge.db")
	store, err := bridgeapp.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	for _, args := range [][]string{{"kek-rotate"}, {"kek-rotate", "--database", path}, {"kek-rotate", "--database", path, "--operation", "valid", "--key", "no-argv-key"}, {"kek-recover", "--database", path, "--action", "guess"}, {"unknown"}} {
		var out, errOut bytes.Buffer
		if code := bridgeCommand(context.Background(), args, &out, &errOut); code == 0 {
			t.Fatal("unsafe invocation accepted")
		}
	}
	var out, errOut bytes.Buffer
	if code := bridgeCommand(context.Background(), []string{"kek-rotate", "--database", path, "--operation", "cli"}, &out, &errOut); code != 0 {
		t.Fatalf("rotate exit %d: %s", code, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"generation":1`)) {
		t.Fatal("missing content-free result")
	}
	if code := bridgeCommand(context.Background(), []string{"kek-recover", "--database", path, "--action", "commit"}, &out, &errOut); code != 0 {
		t.Fatal("completed recovery not idempotent")
	}
}

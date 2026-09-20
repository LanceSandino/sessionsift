// @module AuditHealthTests verifies persistence failure cannot remain invisible.
package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditPersistenceHealthAndRecovery(t *testing.T) {
	root := t.TempDir()
	manager, err := OpenManager(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager.RecordAudit(AuditRecord{Status: "ok"})
	if !manager.AuditHealthy() {
		t.Fatal("healthy persistence rejected")
	}
	blocker := filepath.Join(root, "file")
	if err = os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	manager.path = filepath.Join(blocker, "state.json")
	manager.RecordAudit(AuditRecord{Status: "failed"})
	if manager.AuditHealthy() {
		t.Fatal("audit failure was hidden")
	}
	manager.path = filepath.Join(root, "recovered.json")
	manager.RecordAudit(AuditRecord{Status: "recovered"})
	if !manager.AuditHealthy() {
		t.Fatal("audit health did not recover")
	}
}

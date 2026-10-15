// @module ImportAssuranceTests refuses archive-bomb metadata without decompressing or changing originals (A09).
package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestA09ArchiveBombRefusedBeforeImport(t *testing.T) {
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	entry, err := writer.CreateRaw(&zip.FileHeader{Name: "conversations.json", Method: zip.Store, UncompressedSize64: maxChatGPTExportBytes + 1, CompressedSize64: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "synthetic-bomb.zip")
	if err = os.WriteFile(path, body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	// Nil store is intentional: rejection must occur before persistence is reached.
	if _, err = ImportChatGPT(context.Background(), nil, path); err == nil || !strings.Contains(err.Error(), "exceeds 1 GiB") {
		t.Fatalf("bomb metadata accepted: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, body.Bytes()) {
		t.Fatal("source archive changed")
	}
}

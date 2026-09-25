package jsonlines

import (
	"errors"
	"strings"
	"testing"
)

type errorReader struct {
	sent bool
}

func (r *errorReader) Read(target []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(target, "partial"), nil
	}
	return 0, errors.New("read failed")
}

func TestScannerDiscardsOnlyOversizedLineAndContinues(t *testing.T) {
	scanner := NewScanner(strings.NewReader("ok\r\n0123456789\nafter"), 5)
	if !scanner.Scan() || scanner.Oversized() || string(scanner.Bytes()) != "ok" {
		t.Fatalf("first line=%q oversized=%t", scanner.Bytes(), scanner.Oversized())
	}
	if !scanner.Scan() || !scanner.Oversized() || len(scanner.Bytes()) != 0 {
		t.Fatalf("large line=%q oversized=%t", scanner.Bytes(), scanner.Oversized())
	}
	if !scanner.Scan() || scanner.Oversized() || string(scanner.Bytes()) != "after" {
		t.Fatalf("last line=%q oversized=%t", scanner.Bytes(), scanner.Oversized())
	}
	if scanner.Scan() || scanner.Err() != nil {
		t.Fatalf("scan=%t err=%v", scanner.Scan(), scanner.Err())
	}
}

func TestScannerKeepsEmptyLines(t *testing.T) {
	scanner := NewScanner(strings.NewReader("\n"), 0)
	if !scanner.Scan() || scanner.Oversized() || len(scanner.Bytes()) != 0 {
		t.Fatalf("line=%q oversized=%t", scanner.Bytes(), scanner.Oversized())
	}
	if scanner.Scan() || scanner.Err() != nil {
		t.Fatalf("unexpected trailing scan err=%v", scanner.Err())
	}
}

func TestScannerReportsUnderlyingReadError(t *testing.T) {
	scanner := NewScanner(&errorReader{}, 5)
	if scanner.Scan() || scanner.Err() == nil || scanner.Err().Error() != "read failed" {
		t.Fatalf("scan unexpectedly succeeded or lost error: %v", scanner.Err())
	}
}

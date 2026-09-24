package redact

import (
	"strings"
	"testing"
)

func BenchmarkTextToolOutput(b *testing.B) {
	input := strings.Repeat("run go test ./... in /Users/alice/project with token=abc and C:\\Users\\bob\\x ", 50)
	b.SetBytes(int64(len(input)))
	for i := 0; i < b.N; i++ {
		_ = Text(input, 4000)
	}
}

func BenchmarkTextHugeBoundedOutput(b *testing.B) {
	input := strings.Repeat("plain build log line without secrets\n", 200_000)
	for i := 0; i < b.N; i++ {
		_ = Text(input, 2000)
	}
}

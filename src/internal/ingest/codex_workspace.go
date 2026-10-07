// @module CodexWorkspace reads only the bounded transcript header for metadata redaction.
package ingest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// CWD is intentionally absent from serialized sessions. A metadata refresh
// needs only this small header to retain the existing workspace redaction;
// none of the conversation or tool records need to be parsed again.
func codexWorkspace(path string) (string, error) {
	const headerLimit = 128 << 10
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	line, err := bufio.NewReader(io.LimitReader(file, headerLimit+1)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if len(line) > headerLimit {
		return "", fmt.Errorf("Codex metadata header exceeds 128 KiB")
	}
	var header struct {
		Type    string `json:"type"`
		Payload struct {
			CWD string `json:"cwd"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(line), &header); err != nil {
		return "", fmt.Errorf("Codex metadata header unavailable")
	}
	if header.Type != "session_meta" {
		return "", fmt.Errorf("Codex metadata header unavailable")
	}
	return header.Payload.CWD, nil
}

// Package ingest coordinates discovery, parsing, analysis, and transactional persistence.
// @module IngestImporter imports Codex transcripts, user-supplied exports, and spooled hook events incrementally.
package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/analyze"
	chatgpt "github.com/LanceSandino/sessionsift/src/internal/chatgpt/export"
	"github.com/LanceSandino/sessionsift/src/internal/codex/discover"
	"github.com/LanceSandino/sessionsift/src/internal/codex/state"
	"github.com/LanceSandino/sessionsift/src/internal/codex/transcript"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/hooks"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

const maxChatGPTExportBytes = 1 << 30

// Result summarizes an incremental import run.
type Result struct {
	Discovered int      `json:"discovered"`
	Imported   int      `json:"imported"`
	Skipped    int      `json:"skipped"`
	Failed     int      `json:"failed"`
	Warnings   []string `json:"warnings,omitempty"`
}

// exportFormat describes one provider's user-supplied conversation export.
type exportFormat struct {
	label   string
	version string
	parse   func(io.Reader) ([]domain.Session, error)
}

// ImportChatGPT imports conversations from a ChatGPT JSON export or export ZIP.
func ImportChatGPT(ctx context.Context, store *storage.Store, path string) (Result, error) {
	return importExport(ctx, store, path, exportFormat{label: "ChatGPT", version: domain.ChatGPTExportVersion, parse: chatgpt.Parse})
}

// importExport streams the conversations documents of an export, imports
// changed conversations, and removes conversations absent from a complete read.
func importExport(ctx context.Context, store *storage.Store, path string, format exportFormat) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if info.Size() > maxChatGPTExportBytes {
		return Result{}, fmt.Errorf("%s export exceeds 1 GiB", format.label)
	}
	documents, err := chatGPTDocuments(path)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		for _, document := range documents {
			document.close()
		}
	}()
	result := Result{}
	fingerprint := fmt.Sprintf("%s:%d:%d", format.version, info.Size(), info.ModTime().UnixNano())
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return result, err
	}
	sourceHash := redact.Hash(filepath.Clean(absolutePath))
	var sessionIDs []string
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		sessions, parseErr := format.parse(document.reader)
		document.close()
		if parseErr != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", document.name, parseErr))
			continue
		}
		result.Discovered += len(sessions)
		for _, session := range sessions {
			sessionIDs = append(sessionIDs, session.ID)
			pathHash := redact.Hash(sourceHash + "#" + session.ExternalSessionID)
			needed, checkErr := store.ImportNeeded(ctx, pathHash, fingerprint)
			if checkErr != nil {
				return result, checkErr
			}
			if !needed {
				result.Skipped++
				continue
			}
			findings, analyzeErr := analyze.Run(ctx, session, analyze.Default())
			if analyzeErr != nil {
				result.Failed++
				result.Warnings = append(result.Warnings, analyzeErr.Error())
				continue
			}
			session.Findings = findings
			if saveErr := store.SaveImport(ctx, pathHash, fingerprint, session); saveErr != nil {
				return result, saveErr
			}
			result.Imported++
		}
	}
	if result.Discovered == 0 && result.Failed == 0 {
		return result, fmt.Errorf("no %s conversations found", format.label)
	}
	if result.Failed == 0 {
		if err := store.ReconcileExportSource(ctx, sourceHash, sessionIDs); err != nil {
			return result, err
		}
	}
	return result, nil
}

type chatGPTDocument struct {
	name   string
	reader io.Reader
	close  func()
}

func chatGPTDocuments(path string) ([]chatGPTDocument, error) {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		archive, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		var documents []chatGPTDocument
		var total uint64
		for _, entry := range archive.File {
			name := filepath.Base(entry.Name)
			if entry.FileInfo().IsDir() || !strings.HasPrefix(strings.ToLower(name), "conversations") || !strings.EqualFold(filepath.Ext(name), ".json") {
				continue
			}
			if entry.UncompressedSize64 > maxChatGPTExportBytes {
				archive.Close()
				return nil, fmt.Errorf("%s exceeds 1 GiB", name)
			}
			total += entry.UncompressedSize64
			if total > maxChatGPTExportBytes {
				archive.Close()
				return nil, fmt.Errorf("combined conversations JSON exceeds 1 GiB")
			}
			reader, openErr := entry.Open()
			if openErr != nil {
				archive.Close()
				return nil, openErr
			}
			documents = append(documents, chatGPTDocument{name: name, reader: io.LimitReader(reader, maxChatGPTExportBytes), close: func() { _ = reader.Close() }})
		}
		if len(documents) == 0 {
			archive.Close()
			return nil, fmt.Errorf("no conversations JSON found in export ZIP")
		}
		// Entry readers retain the archive file; the final document closes it.
		lastClose := documents[len(documents)-1].close
		documents[len(documents)-1].close = func() { lastClose(); _ = archive.Close() }
		return documents, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return []chatGPTDocument{{name: filepath.Base(path), reader: io.LimitReader(file, maxChatGPTExportBytes), close: func() { _ = file.Close() }}}, nil
}

// ImportSpool replays product-owned hook files after collector downtime.
func ImportSpool(ctx context.Context, store *storage.Store, directory string) (int, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	imported := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return imported, readErr
		}
		var payload map[string]any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if decoder.Decode(&payload) != nil {
			continue
		}
		provider := hooks.Codex
		if strings.HasPrefix(entry.Name(), "claude-") {
			provider = hooks.ClaudeCode
		}
		event, normalizeErr := hooks.Normalize(provider, payload, time.Now().UTC(), redact.Hash(entry.Name()+string(data)), true)
		if normalizeErr != nil {
			continue
		}
		if err := store.AppendHookEvent(ctx, event); err != nil {
			return imported, err
		}
		if err := os.Remove(path); err != nil {
			return imported, err
		}
		imported++
	}
	return imported, nil
}

// ImportAll imports changed current and archived sessions under codexHome.
func ImportAll(ctx context.Context, store *storage.Store, codexHome string) (Result, error) {
	sources, err := discover.Sessions(codexHome)
	if err != nil {
		return Result{}, err
	}
	result := Result{Discovered: len(sources)}
	threadState, stateErr := state.Load(ctx, filepath.Join(codexHome, "state_5.sqlite"))
	if stateErr != nil {
		result.Warnings = append(result.Warnings, "Codex thread metadata unavailable: "+stateErr.Error())
	}
	parser := transcript.New()
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		info, err := os.Stat(source.Path)
		if err != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, err.Error())
			continue
		}
		metadata, hasMetadata := threadState.Thread(state.IdentityFromPath(source.Path))
		quickFingerprint := sourceFingerprint(info.Size(), info.ModTime())
		if hasMetadata {
			quickFingerprint += ":" + metadata.Revision
		}
		pathHash := redact.Hash(source.Path)
		needed, err := store.ImportNeeded(ctx, pathHash, quickFingerprint)
		if err != nil {
			return result, err
		}
		if !needed {
			result.Skipped++
			continue
		}
		session, _, err := parser.ParseFile(source.Path, source.Archived)
		if err != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", redact.DisplayPath(source.Path), err))
			continue
		}
		if hasMetadata {
			enrichCodexSession(&session, metadata)
		}
		findings, err := analyze.Run(ctx, session, analyze.Default())
		if err != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, err.Error())
			continue
		}
		session.Findings = findings
		scrubWorkspace(&session)
		if err := store.SaveImport(ctx, pathHash, quickFingerprint, session); err != nil {
			return result, err
		}
		result.Imported++
	}
	return result, nil
}

func enrichCodexSession(session *domain.Session, metadata state.Metadata) {
	if metadata.Name != "" {
		session.Title = metadata.Name
		session.TitleSource = "renamed"
	}
	session.Originator = firstNonEmpty(metadata.Originator, session.Originator)
	session.Surface = firstNonEmpty(metadata.Surface, session.Surface)
	session.ThreadSource = firstNonEmpty(metadata.ThreadSource, session.ThreadSource)
	session.RuntimeLocation = metadata.RuntimeLocation
	session.ApprovalMode = metadata.ApprovalMode
	session.SandboxPolicy = metadata.SandboxPolicy
	session.NetworkAccess = metadata.NetworkAccess
	session.GitBranch = firstNonEmpty(metadata.GitBranch, session.GitBranch)
	session.SourceVersion = firstNonEmpty(metadata.CLIVersion, session.SourceVersion)
	session.Model = firstNonEmpty(metadata.Model, session.Model)
	session.ReasoningEffort = firstNonEmpty(metadata.ReasoningEffort, session.ReasoningEffort)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sourceFingerprint(size int64, modified time.Time) string {
	return fmt.Sprintf("%s:%d:%d", domain.ParserVersion, size, modified.UnixNano())
}

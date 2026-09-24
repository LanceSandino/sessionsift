// Package transcript defensively adapts unstable Claude Code JSONL into domain records.
// @module ClaudeTranscript maps prompts, tool use, usage, and compaction without retaining thinking text.
package transcript

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

// Parser converts Claude Code transcripts into provider-neutral sessions.
type Parser struct{}

// New returns the current versioned Claude Code transcript adapter.
func New() *Parser { return &Parser{} }

// ParseFile parses one main-session or subagent transcript and returns the
// SHA-256 of its bytes so unchanged files can be skipped on re-import. The
// path supplies fallback ids for files whose records never state them.
func (p *Parser) ParseFile(path string, subagent bool) (domain.Session, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return domain.Session{}, "", err
	}
	defer file.Close()
	return parse(file, subagent, hintsFromPath(path))
}

// Parse consumes a JSONL transcript and tolerates malformed individual lines.
func (p *Parser) Parse(reader io.Reader, subagent bool) (domain.Session, error) {
	session, _, err := parse(reader, subagent, identityHints{})
	return session, err
}

func parse(reader io.Reader, subagent bool, hints identityHints) (domain.Session, string, error) {
	st := newState(subagent)
	hash := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(reader, hash))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		st.line++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		rec, ok := decodeRecord(line)
		if !ok {
			st.s.Warnings = append(st.s.Warnings, fmt.Sprintf("line %d: malformed JSON", st.line))
			continue
		}
		st.apply(rec)
	}
	if err := scanner.Err(); err != nil {
		return domain.Session{}, "", err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	return st.finish(hints, digest), digest, nil
}

// transcriptMode records whether the file is a main session or a subagent
// sidechain. It is fixed by the discovery flag or by the first conversation
// record, because legacy main transcripts interleave sidechain records.
type transcriptMode int

const (
	modeUndecided transcriptMode = iota
	modeMain
	modeSubagent
)

type usageEntry struct {
	turn  int
	usage usage
}

// state accumulates one session while the scanner streams its lines.
type state struct {
	s           domain.Session
	mode        transcriptMode
	line        int
	sessionID   string
	agentID     string
	current     int
	turns       map[string]int
	tools       map[string]int
	usage       map[string]*usageEntry
	unsupported map[string]int
	sidechains  int
	customTitle string
	generated   string
	firstPrompt string
	latest      time.Time
}

func newState(subagent bool) *state {
	st := &state{
		s: domain.Session{
			ParserVersion: domain.ClaudeCodeParserVersion, Source: domain.SourceClaudeCode, Kind: domain.DeveloperSession,
			Status: "completed", Originator: "claude-code", RuntimeLocation: "local",
		},
		current: -1, turns: map[string]int{}, tools: map[string]int{}, usage: map[string]*usageEntry{}, unsupported: map[string]int{},
	}
	if subagent {
		st.mode = modeSubagent
	}
	return st
}

func (st *state) apply(rec record) {
	if !st.accept(rec) {
		st.sidechains++
		return
	}
	at := parseTime(rec.Timestamp)
	st.observe(rec, at)
	eventID := fmt.Sprintf("evt-%06d", st.line)
	switch rec.Type {
	case "user":
		st.user(rec, at, eventID)
	case "assistant":
		st.assistant(rec, at, eventID)
	case "system":
		st.system(rec, at, eventID)
	case "summary":
		if title := redact.Title(rec.Summary, 80); title != "" {
			st.generated = title
		}
	case "ai-title":
		if title := redact.Title(rec.AITitle, 80); title != "" {
			st.generated = title
		}
	case "custom-title":
		if title := redact.Title(rec.CustomTitle, 80); title != "" {
			st.customTitle = title
		}
	default:
		// Attachments, file-history snapshots, queue operations and similar
		// records carry file contents, hook output and account details, so
		// only their type is counted; nothing from them is copied.
		st.unsupported[label(rec.Type)]++
	}
}

// accept decides the transcript mode at the first conversation record and
// drops sidechain records that a legacy main transcript interleaves.
func (st *state) accept(rec record) bool {
	conversation := rec.Type == "user" || rec.Type == "assistant" || rec.Type == "system"
	if !conversation {
		return true
	}
	if st.mode == modeUndecided && rec.Type != "system" {
		st.mode = modeMain
		if rec.IsSidechain && rec.AgentID != "" {
			st.mode = modeSubagent
		}
	}
	return !(st.mode == modeMain && rec.IsSidechain)
}

// observe folds session-level context that every conversation record repeats.
func (st *state) observe(rec record, at time.Time) {
	if !at.IsZero() {
		if st.s.StartedAt.IsZero() || at.Before(st.s.StartedAt) {
			st.s.StartedAt = at
		}
		if at.After(st.latest) {
			st.latest = at
		}
	}
	// Forked and resumed transcripts begin with history copied from the
	// source session, so the latest sessionId is the best record-level id.
	if rec.SessionID != "" {
		st.sessionID = rec.SessionID
	}
	if rec.AgentID != "" && st.mode == modeSubagent {
		st.agentID = rec.AgentID
	}
	if rec.CWD != "" && st.s.CWD == "" {
		st.setCWD(rec.CWD)
	}
	if rec.GitBranch != "" {
		st.s.GitBranch = redact.Text(rec.GitBranch, 120)
	}
	if rec.Version != "" {
		st.s.SourceVersion = redact.Text(rec.Version, 80)
	}
	if rec.Entrypoint != "" {
		st.s.Surface = redact.Text(rec.Entrypoint, 80)
	}
	if rec.PermissionMode != "" {
		st.s.ApprovalMode = redact.Text(rec.PermissionMode, 80)
	}
}

// setCWD keeps the raw directory local (CWD is never serialized) and shares
// only a keyed hash plus the final element of the redacted path, so a session
// started in a home directory shows ~ or "redacted-user" rather than an account name.
func (st *state) setCWD(cwd string) {
	st.s.CWD = cwd
	clean := filepath.Clean(cwd)
	parts := strings.FieldsFunc(redact.Text(clean, 0), func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 || clean == "." {
		return
	}
	st.s.RepoDisplayName = parts[len(parts)-1]
	st.s.RepoRootHash = redact.PathHash(clean)
}

func (st *state) turn() *domain.Turn {
	if st.current < 0 || st.current >= len(st.s.Turns) {
		return nil
	}
	return &st.s.Turns[st.current]
}

func (st *state) turnKey() string {
	if turn := st.turn(); turn != nil {
		return turn.ExternalTurnID
	}
	return ""
}

func (st *state) extendTurn(at time.Time) {
	if turn := st.turn(); turn != nil && at.After(turn.EndedAt) {
		turn.EndedAt = at
	}
}

func (st *state) event(id string, at time.Time, kind, subtype string, payload map[string]any) *domain.Event {
	st.s.Events = append(st.s.Events, domain.Event{ID: id, TurnID: st.turnKey(), Timestamp: at, EventType: kind, EventSubtype: subtype, Source: "transcript", PayloadRedacted: payload})
	return &st.s.Events[len(st.s.Events)-1]
}

// finish assigns identities (path hints, else the latest record sessionId
// and agentId, else the content digest), then titles, bounds and warnings.
func (st *state) finish(hints identityHints, digest string) domain.Session {
	s := &st.s
	st.applyUsage()
	external := first(hints.sessionID, st.sessionID, fallbackID("session", digest))
	if st.mode == modeSubagent {
		external += ":agent:" + first(hints.agentID, st.agentID, fallbackID("agent", digest))
		s.ThreadSource = "subagent"
	}
	s.ExternalSessionID = external
	s.ID = "claude:" + external
	prefix := s.ID + ":"
	for index := range s.Turns {
		s.Turns[index].ID = prefix + s.Turns[index].ExternalTurnID
		s.Turns[index].SessionID = s.ID
	}
	for index := range s.Events {
		s.Events[index].SessionID = s.ID
		if s.Events[index].TurnID != "" {
			s.Events[index].TurnID = prefix + s.Events[index].TurnID
		}
	}
	for index := range s.ToolCalls {
		s.ToolCalls[index].ID = prefix + s.ToolCalls[index].ID
		s.ToolCalls[index].SessionID = s.ID
		if s.ToolCalls[index].TurnID != "" {
			s.ToolCalls[index].TurnID = prefix + s.ToolCalls[index].TurnID
		}
	}
	st.assignTitle()
	for _, turn := range s.Turns {
		candidate := turn.EndedAt
		if candidate.IsZero() {
			candidate = turn.StartedAt
		}
		if candidate.After(s.EndedAt) {
			s.EndedAt = candidate
		}
	}
	if s.EndedAt.IsZero() {
		s.EndedAt = st.latest
	}
	if s.EndedAt.IsZero() {
		s.EndedAt = s.StartedAt
	}
	if st.sidechains > 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("skipped %d sidechain records in a main transcript", st.sidechains))
	}
	kinds := make([]string, 0, len(st.unsupported))
	for kind := range st.unsupported {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		s.Warnings = append(s.Warnings, fmt.Sprintf("skipped %d unsupported %s records", st.unsupported[kind], kind))
	}
	return *s
}

// assignTitle applies the precedence user rename > generated title > first
// human prompt > fallback. Every candidate was already passed through
// redact.Title when it was observed.
func (st *state) assignTitle() {
	s := &st.s
	switch {
	case st.customTitle != "":
		s.Title, s.TitleSource = st.customTitle, "renamed"
	case st.generated != "":
		s.Title, s.TitleSource = st.generated, "generated"
	case st.firstPrompt != "":
		s.Title, s.TitleSource = st.firstPrompt, "first_prompt"
	case st.mode == modeSubagent:
		s.Title, s.TitleSource = "Claude Code subagent", "fallback"
	default:
		s.Title, s.TitleSource = "Untitled Claude Code session", "fallback"
	}
}

func label(kind string) string {
	if kind = redact.Text(strings.TrimSpace(kind), 60); kind == "" {
		return "untyped"
	}
	return kind
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

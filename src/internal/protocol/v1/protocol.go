// Package v1 defines SessionSift's restricted, versioned Session Protocol.
// @module SessionProtocolV1 exposes domain operations and intentionally no machine primitives.
package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	Version               = 1
	MaximumArgumentsBytes = 64 << 10
)

type Operation string
type Capability string

const (
	SessionList            Operation = "session.list"
	SessionGet             Operation = "session.get"
	SessionMetrics         Operation = "session.metrics"
	SessionTimeline        Operation = "session.timeline"
	SessionFindings        Operation = "session.findings"
	SessionCompare         Operation = "session.compare"
	TurnGet                Operation = "turn.get"
	ConnectionStatus       Operation = "connection.status"
	ConnectionDisconnect   Operation = "connection.disconnect"
	AnnotationList         Operation = "annotation.list"
	AnnotationCreate       Operation = "annotation.create"
	ReviewList             Operation = "review.list"
	ReviewCreate           Operation = "review.create"
	AnalysisPatterns       Operation = "analysis.patterns"
	AnalysisCorrections    Operation = "analysis.recurring_corrections"
	ContextRecommendations Operation = "context.recommendations"
	// SupportRedeem is relay-internal: the bridge asks the device to verify a
	// support code it generated. It is never grantable or exposed to clients.
	SupportRedeem Operation = "support.redeem"
)

type Definition struct {
	Operation            Operation
	Capability           Capability
	AuditCategory        string
	MaximumResponseBytes int
	RedactionPolicy      string
	Timeout              time.Duration
	Paginated            bool
	Internal             bool
}

var definitions = map[Operation]Definition{
	SessionList:            definition(SessionList, "session.read", 2<<20, true),
	SessionGet:             definition(SessionGet, "session.read", 16<<20, false),
	SessionMetrics:         definition(SessionMetrics, "session.read", 256<<10, false),
	SessionTimeline:        definition(SessionTimeline, "session.read", 8<<20, true),
	SessionFindings:        definition(SessionFindings, "session.read", 2<<20, true),
	SessionCompare:         definition(SessionCompare, "session.compare", 32<<20, false),
	TurnGet:                definition(TurnGet, "turn.read", 2<<20, false),
	ConnectionStatus:       definition(ConnectionStatus, "connection.read", 64<<10, false),
	ConnectionDisconnect:   definition(ConnectionDisconnect, "connection.manage", 64<<10, false),
	AnnotationList:         definition(AnnotationList, "collaboration.read", 2<<20, true),
	AnnotationCreate:       definition(AnnotationCreate, "collaboration.write", 64<<10, false),
	ReviewList:             definition(ReviewList, "collaboration.read", 2<<20, true),
	ReviewCreate:           definition(ReviewCreate, "collaboration.write", 64<<10, false),
	AnalysisPatterns:       definition(AnalysisPatterns, "analysis.read", 4<<20, false),
	AnalysisCorrections:    definition(AnalysisCorrections, "analysis.read", 1<<20, false),
	ContextRecommendations: definition(ContextRecommendations, "analysis.read", 1<<20, false),
	SupportRedeem:          internalDefinition(SupportRedeem, "support.connect", 16<<10),
}

func internalDefinition(operation Operation, capability Capability, maximum int) Definition {
	value := definition(operation, capability, maximum, false)
	value.Internal = true
	return value
}

// Grantable reports whether an operation may appear in any grant, pairing,
// invitation, support role, or MCP tool list.
func Grantable(operation Operation) bool {
	value, ok := definitions[operation]
	return ok && !value.Internal
}

func definition(operation Operation, capability Capability, maximum int, paginated bool) Definition {
	return Definition{Operation: operation, Capability: capability, AuditCategory: string(operation), MaximumResponseBytes: maximum, RedactionPolicy: "domain-redacted", Timeout: 5 * time.Second, Paginated: paginated}
}

func DefinitionFor(operation Operation) (Definition, bool) {
	value, ok := definitions[operation]
	return value, ok
}

func Definitions() []Definition {
	values := make([]Definition, 0, len(definitions))
	for _, value := range definitions {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Operation < values[j].Operation })
	return values
}

// ReadOperations is the complete non-mutating capability set exposed through
// the hosted MCP and browser connection surfaces.
func ReadOperations() []Operation {
	return []Operation{
		SessionList, SessionGet, SessionMetrics, SessionTimeline, SessionFindings,
		SessionCompare, TurnGet, AnalysisPatterns, AnalysisCorrections,
		ContextRecommendations,
	}
}

// HostedDefaultOperations is the least-privilege remote MCP profile. It omits
// session bodies, timelines, findings, turn excerpts, and comparisons until an
// owner explicitly grants the detailed profile on the source device.
func HostedDefaultOperations() []Operation {
	return []Operation{SessionList, AnalysisPatterns, AnalysisCorrections, ContextRecommendations}
}

// InvitationOperations returns the exact, deliberately narrow capability set
// assigned to a hosted single-session invitation role.
func InvitationOperations(role string) ([]Operation, bool) {
	view := []Operation{SessionGet, SessionMetrics, SessionTimeline, SessionFindings, TurnGet}
	switch role {
	case "view":
		return view, true
	case "comment":
		return append(view, AnnotationList, AnnotationCreate), true
	case "review":
		return append(view, AnnotationList, AnnotationCreate, ReviewList, ReviewCreate), true
	default:
		return nil, false
	}
}

type Request struct {
	Version   int             `json:"version"`
	RequestID string          `json:"request_id"`
	Operation Operation       `json:"operation"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (r Request) Validate() error {
	if r.Version != Version {
		return errors.New("unsupported protocol version")
	}
	if !requestIDPattern.MatchString(r.RequestID) {
		return errors.New("invalid request id")
	}
	if _, ok := DefinitionFor(r.Operation); !ok {
		return errors.New("unknown operation")
	}
	if len(r.Arguments) > MaximumArgumentsBytes {
		return errors.New("arguments too large")
	}
	if len(r.Arguments) > 0 && !json.Valid(r.Arguments) {
		return errors.New("invalid arguments")
	}
	if err := rejectAmbiguousKeys(r.Arguments); err != nil {
		return err
	}
	return nil
}

// rejectAmbiguousKeys refuses objects that repeat a key under any letter case.
// Go decodes struct fields case-insensitively and keeps the last duplicate, so
// an ambiguous object could satisfy an exact-key scope check while selecting a
// different value for execution.
// foldKey maps a JSON object key to the spelling encoding/json treats as
// equivalent, including Unicode look-alikes such as U+017F (long s) that
// strings.ToLower leaves unchanged.
func foldKey(key string) string {
	return strings.Map(func(r rune) rune { return unicode.ToLower(unicode.ToUpper(r)) }, key)
}

func rejectAmbiguousKeys(raw json.RawMessage) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkValue(decoder, 0); err != nil {
		return err
	}
	return nil
}

func walkValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("arguments are nested too deeply")
	}
	token, err := decoder.Token()
	if err != nil {
		return errors.New("invalid arguments")
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return errors.New("invalid arguments")
			}
			key, _ := keyToken.(string)
			folded := foldKey(key)
			if seen[folded] {
				return errors.New("arguments repeat a field name")
			}
			seen[folded] = true
			if err = walkValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err = walkValue(decoder, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err = decoder.Token(); err != nil {
		return errors.New("invalid arguments")
	}
	return nil
}

type Status string

const (
	StatusOK             Status = "ok"
	StatusInvalidRequest Status = "invalid_request"
	StatusDenied         Status = "denied"
	StatusNotFound       Status = "not_found"
	StatusError          Status = "error"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Status    Status `json:"status"`
	Data      any    `json:"data,omitempty"`
	Error     *Error `json:"error,omitempty"`
}

// sessionKeys are the argument names through which an operation can select a session.
var sessionKeys = map[string]bool{"session_id": true, "a": true, "b": true}

// SessionScoped reports whether arguments select exactly sessionID under every
// spelling the Local Agent's decoder would accept. Non-canonical spellings and
// ambiguous objects are refused rather than interpreted.
func SessionScoped(arguments json.RawMessage, sessionID string) bool {
	if sessionID == "" || len(arguments) == 0 || len(arguments) > MaximumArgumentsBytes || rejectAmbiguousKeys(arguments) != nil {
		return false
	}
	var values map[string]any
	if json.Unmarshal(arguments, &values) != nil {
		return false
	}
	target, ok := values["session_id"].(string)
	if !ok || target != sessionID {
		return false
	}
	for key, value := range values {
		folded := foldKey(key)
		if !sessionKeys[folded] {
			continue
		}
		if key != folded {
			return false
		}
		if text, isText := value.(string); !isText || text != sessionID {
			return false
		}
	}
	return true
}

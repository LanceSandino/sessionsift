// Package v1 defines SessionSift's restricted, versioned Session Protocol.
// @module SessionProtocolV1 exposes domain operations and intentionally no machine primitives.
package v1

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"time"
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
)

type Definition struct {
	Operation            Operation
	Capability           Capability
	AuditCategory        string
	MaximumResponseBytes int
	RedactionPolicy      string
	Timeout              time.Duration
	Paginated            bool
}

var definitions = map[Operation]Definition{
	SessionList:            definition(SessionList, "session.read", 2<<20, true),
	SessionGet:             definition(SessionGet, "session.read", 8<<20, false),
	SessionMetrics:         definition(SessionMetrics, "session.read", 256<<10, false),
	SessionTimeline:        definition(SessionTimeline, "session.read", 8<<20, true),
	SessionFindings:        definition(SessionFindings, "session.read", 2<<20, true),
	SessionCompare:         definition(SessionCompare, "session.compare", 8<<20, false),
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

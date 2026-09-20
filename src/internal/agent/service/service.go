// Package service is the canonical Local Agent boundary over SessionSift domain data.
// @module LocalAgentService validates protocol operations, authorization, and bounded responses.
package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/analytics"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

type Repository interface {
	ListSessions(context.Context) ([]domain.Session, error)
	GetSession(context.Context, string) (domain.Session, error)
}

type Authorizer interface {
	Authorize(context.Context, protocol.Request) error
}

type LocalAuthorizer struct{}

func (LocalAuthorizer) Authorize(_ context.Context, request protocol.Request) error {
	if _, ok := protocol.DefinitionFor(request.Operation); !ok {
		return errors.New("operation is not allowlisted")
	}
	return nil
}

type Service struct {
	repository Repository
	authorizer Authorizer
}

func New(repository Repository, authorizer Authorizer) *Service {
	return &Service{repository: repository, authorizer: authorizer}
}

func NewLocal(repository Repository) *Service { return New(repository, LocalAuthorizer{}) }

type ListArguments struct {
	IncludeInternal bool `json:"include_internal"`
	Cursor          int  `json:"cursor,omitempty"`
	Limit           int  `json:"limit,omitempty"`
}

type SessionArguments struct {
	SessionID string `json:"session_id"`
}

type TimelineArguments struct {
	SessionID string `json:"session_id"`
	Limit     int    `json:"limit,omitempty"`
}

type CompareArguments struct {
	A string `json:"a"`
	B string `json:"b"`
}

type TurnArguments struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
}

type SessionSummary struct {
	domain.Session
	Metrics domain.Metrics `json:"metrics"`
}

type SessionListResponse struct {
	Sessions   []SessionSummary `json:"sessions"`
	Total      int              `json:"total"`
	Returned   int              `json:"returned"`
	Offset     int              `json:"offset"`
	HasMore    bool             `json:"has_more"`
	NextCursor *int             `json:"next_cursor,omitempty"`
}

type SessionTimelineResponse struct {
	Turns     []domain.Turn     `json:"turns"`
	Events    []domain.Event    `json:"events"`
	ToolCalls []domain.ToolCall `json:"tool_calls"`
	Truncated bool              `json:"truncated"`
}

type SessionComparison struct {
	A           domain.Session `json:"a"`
	B           domain.Session `json:"b"`
	MetricsA    domain.Metrics `json:"metrics_a"`
	MetricsB    domain.Metrics `json:"metrics_b"`
	Differences []string       `json:"differences"`
}

type ConnectionStatusResponse struct {
	RemoteEnabled bool `json:"remote_enabled"`
	Connections   int  `json:"connections"`
}

func (s *Service) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	response := protocol.Response{Version: protocol.Version, RequestID: request.RequestID}
	if err := request.Validate(); err != nil {
		return fail(response, protocol.StatusInvalidRequest, "invalid_request", err)
	}
	if s.repository == nil || s.authorizer == nil {
		return fail(response, protocol.StatusError, "unavailable", errors.New("local agent is not configured"))
	}
	if err := s.authorizer.Authorize(ctx, request); err != nil {
		return fail(response, protocol.StatusDenied, "denied", err)
	}
	definition, _ := protocol.DefinitionFor(request.Operation)
	operationCtx, cancel := context.WithTimeout(ctx, definition.Timeout)
	defer cancel()
	data, status, err := s.execute(operationCtx, request)
	if err != nil {
		code := "operation_failed"
		if status == protocol.StatusNotFound {
			code = "not_found"
		} else if status == protocol.StatusInvalidRequest {
			code = "invalid_arguments"
		}
		return fail(response, status, code, err)
	}
	encoded, err := json.Marshal(data)
	if err != nil || len(encoded) > definition.MaximumResponseBytes {
		if err == nil {
			err = errors.New("response exceeds operation limit")
		}
		return fail(response, protocol.StatusError, "response_limit", err)
	}
	response.Status, response.Data = protocol.StatusOK, data
	return response
}

func (s *Service) execute(ctx context.Context, request protocol.Request) (any, protocol.Status, error) {
	switch request.Operation {
	case protocol.SessionList:
		var arguments ListArguments
		if err := decode(request.Arguments, &arguments); err != nil {
			return nil, protocol.StatusInvalidRequest, err
		}
		if arguments.Cursor < 0 || arguments.Limit < 0 {
			return nil, protocol.StatusInvalidRequest, errors.New("cursor and limit must be non-negative")
		}
		return s.list(ctx, arguments)
	case protocol.SessionGet:
		var arguments SessionArguments
		if err := decodeRequired(request.Arguments, &arguments, func() bool { return arguments.SessionID != "" }); err != nil {
			return nil, protocol.StatusInvalidRequest, err
		}
		return s.session(ctx, arguments.SessionID)
	case protocol.SessionMetrics:
		var arguments SessionArguments
		if err := decodeRequired(request.Arguments, &arguments, func() bool { return arguments.SessionID != "" }); err != nil {
			return nil, protocol.StatusInvalidRequest, err
		}
		session, status, err := s.session(ctx, arguments.SessionID)
		if err != nil {
			return nil, status, err
		}
		return domain.CalculateMetrics(session), protocol.StatusOK, nil
	case protocol.SessionTimeline:
		var arguments TimelineArguments
		if err := decodeRequired(request.Arguments, &arguments, func() bool { return arguments.SessionID != "" && arguments.Limit >= 0 }); err != nil {
			return nil, protocol.StatusInvalidRequest, err
		}
		return s.timeline(ctx, arguments)
	case protocol.SessionFindings:
		var arguments SessionArguments
		if err := decodeRequired(request.Arguments, &arguments, func() bool { return arguments.SessionID != "" }); err != nil {
			return nil, protocol.StatusInvalidRequest, err
		}
		session, status, err := s.session(ctx, arguments.SessionID)
		if err != nil {
			return nil, status, err
		}
		return session.Findings, protocol.StatusOK, nil
	case protocol.SessionCompare:
		var arguments CompareArguments
		if err := decodeRequired(request.Arguments, &arguments, func() bool { return arguments.A != "" && arguments.B != "" }); err != nil {
			return nil, protocol.StatusInvalidRequest, err
		}
		return s.compare(ctx, arguments)
	case protocol.TurnGet:
		var arguments TurnArguments
		if err := decodeRequired(request.Arguments, &arguments, func() bool { return arguments.SessionID != "" && arguments.TurnID != "" }); err != nil {
			return nil, protocol.StatusInvalidRequest, err
		}
		return s.turn(ctx, arguments)
	case protocol.ConnectionStatus:
		return ConnectionStatusResponse{}, protocol.StatusOK, nil
	case protocol.AnalysisPatterns:
		report, err := s.analytics(ctx)
		return report, protocol.StatusOK, err
	case protocol.AnalysisCorrections:
		report, err := s.analytics(ctx)
		if err != nil {
			return nil, protocol.StatusError, err
		}
		values := make([]analytics.FindingAggregate, 0)
		for _, finding := range report.Findings {
			if finding.Type == "possible_scope_correction" {
				values = append(values, finding)
			}
		}
		return values, protocol.StatusOK, nil
	case protocol.ContextRecommendations:
		report, err := s.analytics(ctx)
		if err != nil {
			return nil, protocol.StatusError, err
		}
		return report.Recommendations, protocol.StatusOK, nil
	case protocol.ConnectionDisconnect:
		return nil, protocol.StatusInvalidRequest, errors.New("no remote connection exists in local-only v1")
	default:
		return nil, protocol.StatusInvalidRequest, errors.New("unknown operation")
	}
}

func (s *Service) analytics(ctx context.Context) (analytics.Report, error) {
	sessions, err := s.repository.ListSessions(ctx)
	if err != nil {
		return analytics.Report{}, err
	}
	return analytics.Build(sessions), nil
}

func (s *Service) list(ctx context.Context, arguments ListArguments) (any, protocol.Status, error) {
	sessions, err := s.repository.ListSessions(ctx)
	if err != nil {
		return nil, protocol.StatusError, err
	}
	items := make([]SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		if session.Kind == domain.InternalReviewSession && !arguments.IncludeInternal {
			continue
		}
		metrics := domain.CalculateMetrics(session)
		session.CWD, session.Turns, session.Events, session.ToolCalls, session.Findings = "", nil, nil, nil, nil
		items = append(items, SessionSummary{Session: session, Metrics: metrics})
	}
	limit := arguments.Limit
	if limit == 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	offset := arguments.Cursor
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	response := SessionListResponse{Sessions: items[offset:end], Total: len(items), Returned: end - offset, Offset: offset, HasMore: end < len(items)}
	if response.HasMore {
		next := end
		response.NextCursor = &next
	}
	return response, protocol.StatusOK, nil
}

func (s *Service) session(ctx context.Context, id string) (domain.Session, protocol.Status, error) {
	session, err := s.repository.GetSession(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Session{}, protocol.StatusNotFound, errors.New("session not found")
		}
		return domain.Session{}, protocol.StatusError, errors.New("session storage unavailable or busy; retry the request")
	}
	return session, protocol.StatusOK, nil
}

func (s *Service) timeline(ctx context.Context, arguments TimelineArguments) (any, protocol.Status, error) {
	session, status, err := s.session(ctx, arguments.SessionID)
	if err != nil {
		return nil, status, err
	}
	limit := arguments.Limit
	if limit == 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	response := SessionTimelineResponse{Turns: session.Turns, Events: session.Events, ToolCalls: session.ToolCalls}
	total := len(response.Events) + len(response.ToolCalls)
	if total > limit {
		response.Truncated = true
		if len(response.Events) > limit {
			response.Events = response.Events[:limit]
			response.ToolCalls = nil
		} else {
			response.ToolCalls = response.ToolCalls[:limit-len(response.Events)]
		}
	}
	return response, protocol.StatusOK, nil
}

func (s *Service) turn(ctx context.Context, arguments TurnArguments) (any, protocol.Status, error) {
	session, status, err := s.session(ctx, arguments.SessionID)
	if err != nil {
		return nil, status, err
	}
	for _, turn := range session.Turns {
		if turn.ID == arguments.TurnID {
			return turn, protocol.StatusOK, nil
		}
	}
	return nil, protocol.StatusNotFound, errors.New("turn not found")
}

func (s *Service) compare(ctx context.Context, arguments CompareArguments) (any, protocol.Status, error) {
	a, status, err := s.session(ctx, arguments.A)
	if err != nil {
		return nil, status, err
	}
	b, status, err := s.session(ctx, arguments.B)
	if err != nil {
		return nil, status, err
	}
	ma, mb := domain.CalculateMetrics(a), domain.CalculateMetrics(b)
	return SessionComparison{A: a, B: b, MetricsA: ma, MetricsB: mb, Differences: differences(a, b, ma, mb)}, protocol.StatusOK, nil
}

func differences(a, b domain.Session, ma, mb domain.Metrics) []string {
	return []string{
		fmt.Sprintf("Session A used %d tool calls; Session B used %d.", ma.ToolCalls, mb.ToolCalls),
		fmt.Sprintf("Session A cache reuse was %.0f%%; Session B was %.0f%%.", ma.CacheRatio*100, mb.CacheRatio*100),
		fmt.Sprintf("Session A recorded %d failed calls; Session B recorded %d.", ma.FailedToolCalls, mb.FailedToolCalls),
		fmt.Sprintf("Session A performed %d read/search calls; Session B performed %d.", ma.Reads+ma.Searches, mb.Reads+mb.Searches),
		fmt.Sprintf("Session A reached its first edit after %s; Session B after %s.", observedDuration(ma.TimeToFirstEditMS), observedDuration(mb.TimeToFirstEditMS)),
		fmt.Sprintf("Session A had %d possible scope corrections; Session B had %d.", findingCount(a, "possible_scope_correction"), findingCount(b, "possible_scope_correction")),
	}
}

func observedDuration(milliseconds int64) string {
	if milliseconds <= 0 {
		return "no observed edit"
	}
	return time.Duration(milliseconds * int64(time.Millisecond)).Round(time.Second).String()
}

func findingCount(session domain.Session, kind string) int {
	count := 0
	for _, finding := range session.Findings {
		if finding.FindingType == kind {
			count++
		}
	}
	return count
}

func decode(body json.RawMessage, target any) error {
	if len(body) == 0 || bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		body = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func decodeRequired(body json.RawMessage, target any, valid func() bool) error {
	if err := decode(body, target); err != nil {
		return err
	}
	if !valid() {
		return errors.New("required arguments are missing or invalid")
	}
	return nil
}

func fail(response protocol.Response, status protocol.Status, code string, err error) protocol.Response {
	response.Status = status
	response.Error = &protocol.Error{Code: code, Message: err.Error()}
	return response
}

// Package otel decodes Codex OTLP log batches into redacted normalized events.
package otel

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Decode accepts standard OTLP/HTTP protobuf or JSON and returns attributable events.
func Decode(contentType string, body []byte) ([]domain.Event, error) {
	var request collectorlogsv1.ExportLogsServiceRequest
	var err error
	if strings.Contains(strings.ToLower(contentType), "json") {
		err = protojson.Unmarshal(body, &request)
	} else {
		err = proto.Unmarshal(body, &request)
	}
	if err != nil {
		return nil, fmt.Errorf("decode OTLP logs: %w", err)
	}
	var events []domain.Event
	for _, resourceLogs := range request.ResourceLogs {
		resource := attributes(resourceLogs.GetResource().GetAttributes())
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			for index, record := range scopeLogs.LogRecords {
				values := merge(resource, attributes(record.Attributes))
				sessionID := firstString(values, "conversation.id", "conversation_id", "session.id", "session_id", "thread.id", "thread_id")
				if sessionID == "" {
					continue
				}
				name := firstString(values, "event.name", "event_name", "name")
				if name == "" {
					name = safeEventName(anyValue(record.Body))
				}
				if name == "" {
					name = "codex.otel_event"
				}
				at := time.Unix(0, int64(record.TimeUnixNano)).UTC()
				if record.TimeUnixNano == 0 {
					at = time.Now().UTC()
				}
				payload := redactValues(values)
				events = append(events, domain.Event{
					ID: redact.Hash(fmt.Sprintf("%s:%d:%d", sessionID, record.TimeUnixNano, index))[:24], SessionID: sessionID,
					TurnID: firstString(values, "turn.id", "turn_id"), Timestamp: at, EventType: "otel", EventSubtype: name,
					Source: "otel", PayloadRedacted: payload, DurationMS: firstInt(values, "duration_ms", "duration.ms"), Success: firstBool(values, "success"),
				})
			}
		}
	}
	return events, nil
}

func attributes(values []*commonv1.KeyValue) map[string]any {
	result := map[string]any{}
	for _, value := range values {
		result[value.Key] = valueFrom(value.Value)
	}
	return result
}

func valueFrom(value *commonv1.AnyValue) any {
	if value == nil {
		return nil
	}
	switch item := value.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return item.StringValue
	case *commonv1.AnyValue_BoolValue:
		return item.BoolValue
	case *commonv1.AnyValue_IntValue:
		return item.IntValue
	case *commonv1.AnyValue_DoubleValue:
		return item.DoubleValue
	case *commonv1.AnyValue_BytesValue:
		return redact.Text(string(item.BytesValue), 2_000)
	case *commonv1.AnyValue_ArrayValue:
		items := make([]any, 0, len(item.ArrayValue.Values))
		for _, child := range item.ArrayValue.Values {
			items = append(items, valueFrom(child))
		}
		return items
	case *commonv1.AnyValue_KvlistValue:
		return attributes(item.KvlistValue.Values)
	default:
		return nil
	}
}

func anyValue(value *commonv1.AnyValue) string {
	if text, ok := valueFrom(value).(string); ok {
		return text
	}
	return ""
}

func merge(a, b map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range a {
		result[key] = value
	}
	for key, value := range b {
		result[key] = value
	}
	return result
}

func redactValues(values map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range values {
		result[key] = redactAttribute(value)
	}
	return result
}

func redactAttribute(value any) any {
	switch item := value.(type) {
	case string:
		return redact.Text(item, 2_000)
	case []any:
		result := make([]any, len(item))
		for index, child := range item {
			result[index] = redactAttribute(child)
		}
		return result
	case map[string]any:
		return redactValues(item)
	default:
		return value
	}
}

var eventNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,100}$`)

func safeEventName(value string) string {
	if eventNamePattern.MatchString(value) {
		return value
	}
	return ""
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func firstInt(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := values[key].(type) {
		case int64:
			return value
		case float64:
			return int64(value)
		}
	}
	return 0
}

func firstBool(values map[string]any, keys ...string) *bool {
	for _, key := range keys {
		if value, ok := values[key].(bool); ok {
			result := value
			return &result
		}
	}
	return nil
}

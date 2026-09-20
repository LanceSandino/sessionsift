package otel

import (
	"fmt"
	"strings"
	"testing"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestDecodeBinaryAndJSON(t *testing.T) {
	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{stringAttr("conversation.id", "session-1")}},
			ScopeLogs: []*logsv1.ScopeLogs{{
				LogRecords: []*logsv1.LogRecord{{
					TimeUnixNano: 100,
					Body:         stringValue("codex.sse_event"),
					Attributes:   []*commonv1.KeyValue{stringAttr("turn.id", "turn-1"), intAttr("input_tokens", 100), boolAttr("success", true)},
				}},
			}},
		}},
	}
	binary, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	jsonBody, err := protojson.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for contentType, body := range map[string][]byte{"application/x-protobuf": binary, "application/json": jsonBody} {
		events, err := Decode(contentType, body)
		if err != nil || len(events) != 1 || events[0].SessionID != "session-1" || events[0].TurnID != "turn-1" || events[0].EventSubtype != "codex.sse_event" {
			t.Fatalf("type=%s events=%+v err=%v", contentType, events, err)
		}
	}
}

func stringAttr(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: stringValue(value)}
}
func stringValue(value string) *commonv1.AnyValue {
	return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}
}
func intAttr(key string, value int64) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: value}}}
}
func boolAttr(key string, value bool) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: value}}}
}

func TestDecodeRejectsMalformedPayload(t *testing.T) {
	if _, err := Decode("application/x-protobuf", []byte("broken")); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDecoderValueAndAttributeHelpers(t *testing.T) {
	values := []*commonv1.AnyValue{
		stringValue("secret sk-proj-" + "abcdefghijklmnopqrstuvwxyz"),
		{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}},
		{Value: &commonv1.AnyValue_IntValue{IntValue: 4}},
		{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: 2.5}},
		{Value: &commonv1.AnyValue_BytesValue{BytesValue: []byte("bytes")}},
		{Value: &commonv1.AnyValue_ArrayValue{ArrayValue: &commonv1.ArrayValue{Values: []*commonv1.AnyValue{stringValue("one")}}}},
		{Value: &commonv1.AnyValue_KvlistValue{KvlistValue: &commonv1.KeyValueList{Values: []*commonv1.KeyValue{stringAttr("nested", "value")}}}},
		{},
	}
	for _, value := range values {
		_ = valueFrom(value)
	}
	if valueFrom(nil) != nil || anyValue(values[1]) != "" {
		t.Fatal("unexpected nil or non-string conversion")
	}
	merged := merge(map[string]any{"a": "old"}, map[string]any{"a": "new", "duration.ms": float64(12), "success": false, "prompt.body": "Bearer abcdefghijklmnopqrstuvwxyz", "metadata": "sk-proj-" + "abcdefghijklmnopqrstuvwxyz", "nested": map[string]any{"value": "ghp_abcdefghijklmnopqrstuvwxyz"}})
	if merged["a"] != "new" || firstInt(merged, "duration.ms") != 12 || firstBool(merged, "success") == nil || firstString(merged, "missing") != "" {
		t.Fatalf("helpers=%+v", merged)
	}
	redacted := redactValues(merged)
	if redacted["prompt.body"] == merged["prompt.body"] {
		t.Fatal("prompt attribute was not redacted")
	}
	encoded := fmt.Sprint(redacted)
	if strings.Contains(encoded, "sk-proj-") || strings.Contains(encoded, "ghp_") {
		t.Fatalf("unlabelled or nested secret was not redacted: %s", encoded)
	}
	if safeEventName("user prompt with spaces") != "" || safeEventName("codex.tool_result") == "" {
		t.Fatal("unsafe event body classification")
	}
}

func TestDecodeSkipsUnattributedRecords(t *testing.T) {
	request := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: []*logsv1.LogRecord{{Body: stringValue("event")}}}}}}}
	body, _ := proto.Marshal(request)
	events, err := Decode("application/x-protobuf", body)
	if err != nil || len(events) != 0 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestDecodeMetricsBinaryAndJSON(t *testing.T) {
	request := &collectormetricsv1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{{
			ScopeMetrics: []*metricsv1.ScopeMetrics{{Metrics: []*metricsv1.Metric{
				{Name: "codex.thread.started", Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{DataPoints: []*metricsv1.NumberDataPoint{{TimeUnixNano: 100}}}}},
				{Name: "codex.tool.duration", Data: &metricsv1.Metric_Histogram{Histogram: &metricsv1.Histogram{DataPoints: []*metricsv1.HistogramDataPoint{{TimeUnixNano: 200}, {TimeUnixNano: 300}}}}},
			}}}},
		},
	}
	binary, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	jsonBody, err := protojson.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for contentType, body := range map[string][]byte{"application/x-protobuf": binary, "application/json": jsonBody} {
		count, err := DecodeMetrics(contentType, body)
		if err != nil || count != 3 {
			t.Fatalf("type=%s count=%d err=%v", contentType, count, err)
		}
	}
	if _, err := DecodeMetrics("application/x-protobuf", []byte("broken")); err == nil {
		t.Fatal("expected metrics decode error")
	}
}

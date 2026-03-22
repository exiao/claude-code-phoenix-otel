package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// OTLPExporter sends spans to Phoenix via OTLP HTTP/protobuf.
type OTLPExporter struct {
	endpoint string
	apiKey   string
	project  string
	service  string
	client   *http.Client
}

func NewOTLPExporter(cfg *Config) *OTLPExporter {
	return &OTLPExporter{
		endpoint: cfg.URL,
		apiKey:   cfg.APIKey,
		project:  cfg.Project,
		service:  "claude-code",
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// OTLPSpan represents a span to export.
type OTLPSpan struct {
	TraceID      [16]byte
	SpanID       [8]byte
	ParentSpanID [8]byte
	Name         string
	StartTime    time.Time
	EndTime      time.Time
	Kind         SpanKind
	Attributes   map[string]interface{}
	Status       SpanStatus
}

type SpanKind int

const (
	SpanKindInternal SpanKind = 1
	SpanKindServer   SpanKind = 2
)

type SpanStatus struct {
	Code    int    // 0=unset, 1=ok, 2=error
	Message string
}

// ExportSpans sends a batch of spans to Phoenix via OTLP HTTP/protobuf.
func (e *OTLPExporter) ExportSpans(spans []OTLPSpan) error {
	if len(spans) == 0 {
		return nil
	}

	request := e.buildProtoRequest(spans)
	data, err := proto.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal protobuf: %w", err)
	}

	url := e.endpoint + "/v1/traces"
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-protobuf")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %d %s", url, resp.StatusCode, body)
	}

	return nil
}

func (e *OTLPExporter) buildProtoRequest(spans []OTLPSpan) *collectorpb.ExportTraceServiceRequest {
	var protoSpans []*tracepb.Span
	for _, span := range spans {
		protoSpans = append(protoSpans, e.spanToProto(span))
	}

	return &collectorpb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						stringAttr("service.name", e.service),
						stringAttr("openinference.project.name", e.project),
					},
				},
				ScopeSpans: []*tracepb.ScopeSpans{
					{
						Scope: &commonpb.InstrumentationScope{
							Name:    "claude-code-phoenix-otel",
							Version: "0.1.0",
						},
						Spans: protoSpans,
					},
				},
			},
		},
	}
}

func (e *OTLPExporter) spanToProto(span OTLPSpan) *tracepb.Span {
	s := &tracepb.Span{
		TraceId:                span.TraceID[:],
		SpanId:                 span.SpanID[:],
		Name:                   span.Name,
		Kind:                   tracepb.Span_SpanKind(span.Kind),
		StartTimeUnixNano:      uint64(span.StartTime.UnixNano()),
		EndTimeUnixNano:        uint64(span.EndTime.UnixNano()),
		Attributes:             mapToAttributes(span.Attributes),
		Status: &tracepb.Status{
			Code:    tracepb.Status_StatusCode(span.Status.Code),
			Message: span.Status.Message,
		},
	}

	emptyParent := [8]byte{}
	if span.ParentSpanID != emptyParent {
		s.ParentSpanId = span.ParentSpanID[:]
	}

	return s
}

func mapToAttributes(attrs map[string]interface{}) []*commonpb.KeyValue {
	var result []*commonpb.KeyValue
	for key, val := range attrs {
		result = append(result, toKeyValue(key, val))
	}
	return result
}

func toKeyValue(key string, val interface{}) *commonpb.KeyValue {
	switch v := val.(type) {
	case string:
		return stringAttr(key, v)
	case int:
		return intAttr(key, int64(v))
	case int64:
		return intAttr(key, v)
	case float64:
		return &commonpb.KeyValue{
			Key:   key,
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v}},
		}
	case bool:
		return &commonpb.KeyValue{
			Key:   key,
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: v}},
		}
	default:
		data, _ := json.Marshal(v)
		return stringAttr(key, string(data))
	}
}

func stringAttr(key, val string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: val}},
	}
}

func intAttr(key string, val int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: val}},
	}
}

// Helper to build OpenInference span attributes.
func openInferenceAttrs(kind string, extra map[string]interface{}) map[string]interface{} {
	attrs := map[string]interface{}{
		"openinference.span.kind": kind,
	}
	for k, v := range extra {
		attrs[k] = v
	}
	return attrs
}

// parseTimestamp parses an ISO 8601 timestamp string.
func parseTimestamp(ts string) time.Time {
	for _, layout := range []string{
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999999999Z",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t
		}
	}
	return time.Now()
}

// safeStringify converts a value to a JSON string, truncating if needed.
func safeStringify(value interface{}, maxLen int) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		if len(s) > maxLen {
			return s[:maxLen] + "...[truncated]"
		}
		return s
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	s := string(data)
	if len(s) > maxLen {
		return s[:maxLen] + "...[truncated]"
	}
	return s
}

// categorizeError creates an error type string from an error message.
func categorizeError(errMsg string) string {
	lower := strings.ToLower(errMsg)
	switch {
	case containsAny(lower, "timeout", "timed out", "deadline exceeded"):
		return "timeout"
	case containsAny(lower, "permission denied", "access denied", "forbidden"):
		return "permission_denied"
	case containsAny(lower, "not found", "no such file", "does not exist", "enoent"):
		return "not_found"
	case containsAny(lower, "connection refused", "network error", "unreachable"):
		return "network_error"
	default:
		return "tool_error"
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// hexTraceID returns the hex string of a trace ID for debug logging.
func hexTraceID(id [16]byte) string {
	return hex.EncodeToString(id[:])
}

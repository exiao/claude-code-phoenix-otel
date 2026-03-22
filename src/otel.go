package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// OTLPExporter sends spans to Phoenix via OTLP HTTP/protobuf.
// For simplicity and zero protobuf dependency, we use OTLP JSON format
// which Phoenix also supports natively.
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

// ExportSpans sends a batch of spans to Phoenix via OTLP HTTP JSON.
func (e *OTLPExporter) ExportSpans(spans []OTLPSpan) error {
	if len(spans) == 0 {
		return nil
	}

	payload := e.buildExportPayload(spans)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal OTLP payload: %w", err)
	}

	url := e.endpoint + "/v1/traces"
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
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

func (e *OTLPExporter) buildExportPayload(spans []OTLPSpan) map[string]interface{} {
	// Group spans by trace ID
	traceSpans := make(map[string][]map[string]interface{})
	for _, span := range spans {
		traceKey := hex.EncodeToString(span.TraceID[:])
		traceSpans[traceKey] = append(traceSpans[traceKey], e.spanToJSON(span))
	}

	var scopeSpans []map[string]interface{}
	for _, spans := range traceSpans {
		scopeSpans = append(scopeSpans, map[string]interface{}{
			"scope": map[string]interface{}{
				"name":    "claude-code-phoenix-otel",
				"version": "0.1.0",
			},
			"spans": spans,
		})
	}

	return map[string]interface{}{
		"resourceSpans": []map[string]interface{}{
			{
				"resource": map[string]interface{}{
					"attributes": []map[string]interface{}{
						{"key": "service.name", "value": map[string]interface{}{"stringValue": e.service}},
						{"key": "openinference.project.name", "value": map[string]interface{}{"stringValue": e.project}},
					},
				},
				"scopeSpans": scopeSpans,
			},
		},
	}
}

func (e *OTLPExporter) spanToJSON(span OTLPSpan) map[string]interface{} {
	result := map[string]interface{}{
		"traceId":            hex.EncodeToString(span.TraceID[:]),
		"spanId":             hex.EncodeToString(span.SpanID[:]),
		"name":               span.Name,
		"kind":               int(span.Kind),
		"startTimeUnixNano":  strconv.FormatInt(span.StartTime.UnixNano(), 10),
		"endTimeUnixNano":    strconv.FormatInt(span.EndTime.UnixNano(), 10),
		"attributes":         attributesToJSON(span.Attributes),
		"status":             map[string]interface{}{"code": span.Status.Code, "message": span.Status.Message},
	}

	emptyParent := [8]byte{}
	if span.ParentSpanID != emptyParent {
		result["parentSpanId"] = hex.EncodeToString(span.ParentSpanID[:])
	}

	return result
}

func attributesToJSON(attrs map[string]interface{}) []map[string]interface{} {
	var result []map[string]interface{}
	for key, val := range attrs {
		attr := map[string]interface{}{"key": key}
		switch v := val.(type) {
		case string:
			attr["value"] = map[string]interface{}{"stringValue": v}
		case int:
			attr["value"] = map[string]interface{}{"intValue": strconv.Itoa(v)}
		case int64:
			attr["value"] = map[string]interface{}{"intValue": strconv.FormatInt(v, 10)}
		case float64:
			attr["value"] = map[string]interface{}{"doubleValue": v}
		case bool:
			attr["value"] = map[string]interface{}{"boolValue": v}
		default:
			// JSON-stringify anything else
			data, _ := json.Marshal(v)
			attr["value"] = map[string]interface{}{"stringValue": string(data)}
		}
		result = append(result, attr)
	}
	return result
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
	// Try common formats
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

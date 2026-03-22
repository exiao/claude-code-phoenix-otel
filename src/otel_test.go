package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestNewOTLPExporter(t *testing.T) {
	cfg := &Config{
		URL:     "https://phoenix.example.com",
		APIKey:  "sk-test",
		Project: "test-project",
	}
	exp := NewOTLPExporter(cfg)
	if exp.endpoint != "https://phoenix.example.com" {
		t.Errorf("endpoint = %q", exp.endpoint)
	}
	if exp.apiKey != "sk-test" {
		t.Errorf("apiKey = %q", exp.apiKey)
	}
	if exp.project != "test-project" {
		t.Errorf("project = %q", exp.project)
	}
}

func TestExportSpansEmpty(t *testing.T) {
	exp := NewOTLPExporter(&Config{URL: "http://localhost"})
	err := exp.ExportSpans(nil)
	if err != nil {
		t.Errorf("ExportSpans(nil) = %v, want nil", err)
	}
	err = exp.ExportSpans([]OTLPSpan{})
	if err != nil {
		t.Errorf("ExportSpans([]) = %v, want nil", err)
	}
}

func TestExportSpansProtobufFormat(t *testing.T) {
	var receivedBody []byte
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer server.Close()

	exp := NewOTLPExporter(&Config{
		URL:     server.URL,
		APIKey:  "sk-test-key",
		Project: "my-project",
	})

	now := time.Now()
	spans := []OTLPSpan{
		{
			TraceID:   [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SpanID:    [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
			Name:      "test-span",
			StartTime: now,
			EndTime:   now.Add(time.Second),
			Kind:      SpanKindServer,
			Attributes: map[string]interface{}{
				"openinference.span.kind": "AGENT",
				"session.id":              "sess-123",
				"llm.token_count.total":   150,
			},
			Status: SpanStatus{Code: 1},
		},
	}

	err := exp.ExportSpans(spans)
	if err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}

	// Check content type is protobuf
	if ct := receivedHeaders.Get("Content-Type"); ct != "application/x-protobuf" {
		t.Errorf("Content-Type = %q, want application/x-protobuf", ct)
	}

	// Check auth header
	if auth := receivedHeaders.Get("Authorization"); auth != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q", auth)
	}

	// Deserialize protobuf
	var req collectorpb.ExportTraceServiceRequest
	if err := proto.Unmarshal(receivedBody, &req); err != nil {
		t.Fatalf("unmarshal protobuf: %v", err)
	}

	if len(req.ResourceSpans) != 1 {
		t.Fatalf("got %d resourceSpans, want 1", len(req.ResourceSpans))
	}

	rs := req.ResourceSpans[0]

	// Check resource attributes
	foundServiceName := false
	foundProject := false
	for _, attr := range rs.Resource.Attributes {
		if attr.Key == "service.name" && attr.Value.GetStringValue() == "claude-code" {
			foundServiceName = true
		}
		if attr.Key == "openinference.project.name" && attr.Value.GetStringValue() == "my-project" {
			foundProject = true
		}
	}
	if !foundServiceName {
		t.Error("missing service.name resource attribute")
	}
	if !foundProject {
		t.Error("missing openinference.project.name resource attribute")
	}

	// Check scope
	if len(rs.ScopeSpans) != 1 {
		t.Fatalf("got %d scopeSpans, want 1", len(rs.ScopeSpans))
	}
	scope := rs.ScopeSpans[0].Scope
	if scope.Name != "claude-code-phoenix-otel" {
		t.Errorf("scope name = %q", scope.Name)
	}

	// Check span
	protoSpans := rs.ScopeSpans[0].Spans
	if len(protoSpans) != 1 {
		t.Fatalf("got %d spans, want 1", len(protoSpans))
	}

	span := protoSpans[0]
	if span.Name != "test-span" {
		t.Errorf("span name = %q", span.Name)
	}
	if len(span.ParentSpanId) != 0 {
		t.Error("root span should not have parentSpanId")
	}

	// Check span attributes
	foundKind := false
	foundSession := false
	foundTokens := false
	for _, attr := range span.Attributes {
		switch attr.Key {
		case "openinference.span.kind":
			if attr.Value.GetStringValue() == "AGENT" {
				foundKind = true
			}
		case "session.id":
			if attr.Value.GetStringValue() == "sess-123" {
				foundSession = true
			}
		case "llm.token_count.total":
			if attr.Value.GetIntValue() == 150 {
				foundTokens = true
			}
		}
	}
	if !foundKind {
		t.Error("missing openinference.span.kind attribute")
	}
	if !foundSession {
		t.Error("missing session.id attribute")
	}
	if !foundTokens {
		t.Error("missing llm.token_count.total attribute")
	}
}

func TestExportSpansWithParent(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer server.Close()

	exp := NewOTLPExporter(&Config{URL: server.URL})
	now := time.Now()

	spans := []OTLPSpan{
		{
			TraceID:      [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SpanID:       [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
			ParentSpanID: [8]byte{9, 10, 11, 12, 13, 14, 15, 16},
			Name:         "child-span",
			StartTime:    now,
			EndTime:      now,
			Kind:         SpanKindInternal,
			Attributes:   map[string]interface{}{},
			Status:       SpanStatus{Code: 1},
		},
	}

	exp.ExportSpans(spans)

	var req collectorpb.ExportTraceServiceRequest
	proto.Unmarshal(receivedBody, &req)

	span := req.ResourceSpans[0].ScopeSpans[0].Spans[0]
	if len(span.ParentSpanId) == 0 {
		t.Error("child span should have parentSpanId")
	}
	expected := []byte{9, 10, 11, 12, 13, 14, 15, 16}
	for i, b := range span.ParentSpanId {
		if b != expected[i] {
			t.Errorf("parentSpanId[%d] = %d, want %d", i, b, expected[i])
			break
		}
	}
}

func TestExportSpansNoAuth(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(200)
	}))
	defer server.Close()

	exp := NewOTLPExporter(&Config{URL: server.URL})
	exp.ExportSpans([]OTLPSpan{{
		TraceID:    [16]byte{1},
		SpanID:     [8]byte{1},
		Name:       "test",
		StartTime:  time.Now(),
		EndTime:    time.Now(),
		Attributes: map[string]interface{}{},
		Status:     SpanStatus{Code: 1},
	}})

	if auth := receivedHeaders.Get("Authorization"); auth != "" {
		t.Errorf("Authorization should be empty without API key, got %q", auth)
	}
}

func TestExportSpansServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	exp := NewOTLPExporter(&Config{URL: server.URL})
	err := exp.ExportSpans([]OTLPSpan{{
		TraceID:    [16]byte{1},
		SpanID:     [8]byte{1},
		Name:       "test",
		StartTime:  time.Now(),
		EndTime:    time.Now(),
		Attributes: map[string]interface{}{},
		Status:     SpanStatus{Code: 1},
	}})

	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestOpenInferenceAttrs(t *testing.T) {
	attrs := openInferenceAttrs("TOOL", map[string]interface{}{
		"tool.name":   "Read",
		"input.value": "test",
	})

	if attrs["openinference.span.kind"] != "TOOL" {
		t.Errorf("span.kind = %q", attrs["openinference.span.kind"])
	}
	if attrs["tool.name"] != "Read" {
		t.Errorf("tool.name = %q", attrs["tool.name"])
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"2026-03-21T12:00:00.000Z", true},
		{"2026-03-21T12:00:00Z", true},
		{"invalid", false},
	}

	for _, tt := range tests {
		result := parseTimestamp(tt.input)
		if tt.valid && result.Year() != 2026 {
			t.Errorf("parseTimestamp(%q) year = %d", tt.input, result.Year())
		}
	}
}

func TestSafeStringify(t *testing.T) {
	if s := safeStringify("hello", 100); s != "hello" {
		t.Errorf("got %q", s)
	}
	if s := safeStringify("hello", 3); s != "hel...[truncated]" {
		t.Errorf("got %q", s)
	}
	if s := safeStringify(nil, 100); s != "" {
		t.Errorf("got %q for nil", s)
	}
	if s := safeStringify(map[string]int{"a": 1}, 100); s != `{"a":1}` {
		t.Errorf("got %q", s)
	}
}

func TestCategorizeError(t *testing.T) {
	tests := map[string]string{
		"connection timed out": "timeout",
		"permission denied":   "permission_denied",
		"file not found":      "not_found",
		"connection refused":  "network_error",
		"some unknown error":  "tool_error",
	}
	for input, want := range tests {
		got := categorizeError(input)
		if got != want {
			t.Errorf("categorizeError(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTruncateString(t *testing.T) {
	if s := truncateString("hello", 10); s != "hello" {
		t.Errorf("got %q", s)
	}
	if s := truncateString("hello world", 5); s != "hello..." {
		t.Errorf("got %q", s)
	}
}

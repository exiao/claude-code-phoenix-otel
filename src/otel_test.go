package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestExportSpansPayloadFormat(t *testing.T) {
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

	// Check auth header
	if auth := receivedHeaders.Get("Authorization"); auth != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer sk-test-key")
	}

	// Check content type
	if ct := receivedHeaders.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	// Parse payload
	var payload map[string]interface{}
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}

	// Check structure
	resourceSpans, ok := payload["resourceSpans"].([]interface{})
	if !ok || len(resourceSpans) == 0 {
		t.Fatal("missing resourceSpans")
	}

	rs := resourceSpans[0].(map[string]interface{})
	resource := rs["resource"].(map[string]interface{})
	attrs := resource["attributes"].([]interface{})

	// Check resource attributes
	foundServiceName := false
	foundProject := false
	for _, a := range attrs {
		attr := a.(map[string]interface{})
		key := attr["key"].(string)
		val := attr["value"].(map[string]interface{})
		if key == "service.name" && val["stringValue"] == "claude-code" {
			foundServiceName = true
		}
		if key == "openinference.project.name" && val["stringValue"] == "my-project" {
			foundProject = true
		}
	}
	if !foundServiceName {
		t.Error("missing service.name resource attribute")
	}
	if !foundProject {
		t.Error("missing openinference.project.name resource attribute")
	}

	// Check span
	scopeSpans := rs["scopeSpans"].([]interface{})
	ss := scopeSpans[0].(map[string]interface{})
	spanList := ss["spans"].([]interface{})
	span := spanList[0].(map[string]interface{})

	if span["name"] != "test-span" {
		t.Errorf("span name = %q", span["name"])
	}
	if span["traceId"] != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("traceId = %q", span["traceId"])
	}
	if span["spanId"] != "0102030405060708" {
		t.Errorf("spanId = %q", span["spanId"])
	}

	// parentSpanId should be absent for root spans
	if _, hasParent := span["parentSpanId"]; hasParent {
		t.Error("root span should not have parentSpanId")
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

	var payload map[string]interface{}
	json.Unmarshal(receivedBody, &payload)

	rs := payload["resourceSpans"].([]interface{})[0].(map[string]interface{})
	ss := rs["scopeSpans"].([]interface{})[0].(map[string]interface{})
	span := ss["spans"].([]interface{})[0].(map[string]interface{})

	if _, hasParent := span["parentSpanId"]; !hasParent {
		t.Error("child span should have parentSpanId")
	}
	if span["parentSpanId"] != "090a0b0c0d0e0f10" {
		t.Errorf("parentSpanId = %q", span["parentSpanId"])
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

func TestAttributesToJSON(t *testing.T) {
	attrs := map[string]interface{}{
		"string_key": "hello",
		"int_key":    42,
		"bool_key":   true,
		"float_key":  3.14,
	}

	result := attributesToJSON(attrs)

	if len(result) != 4 {
		t.Fatalf("got %d attributes, want 4", len(result))
	}

	found := make(map[string]bool)
	for _, attr := range result {
		key := attr["key"].(string)
		val := attr["value"].(map[string]interface{})
		found[key] = true

		switch key {
		case "string_key":
			if val["stringValue"] != "hello" {
				t.Errorf("string_key value = %v", val)
			}
		case "int_key":
			if val["intValue"] != "42" {
				t.Errorf("int_key value = %v", val)
			}
		case "bool_key":
			if val["boolValue"] != true {
				t.Errorf("bool_key value = %v", val)
			}
		case "float_key":
			if val["doubleValue"] != 3.14 {
				t.Errorf("float_key value = %v", val)
			}
		}
	}

	for _, key := range []string{"string_key", "int_key", "bool_key", "float_key"} {
		if !found[key] {
			t.Errorf("missing attribute %q", key)
		}
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
		"connection timed out":    "timeout",
		"permission denied":      "permission_denied",
		"file not found":         "not_found",
		"connection refused":     "network_error",
		"some unknown error":     "tool_error",
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

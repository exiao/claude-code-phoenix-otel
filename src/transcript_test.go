package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSampleTranscript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := []string{
		`{"type":"assistant","uuid":"uuid-1","timestamp":"2026-03-21T12:00:00.000Z","slug":"fix login bug","message":{"id":"msg-1","model":"claude-sonnet-4-20260514","content":[{"type":"thinking","thinking":"Let me analyze the issue..."}],"usage":{"input_tokens":1000,"output_tokens":50,"cache_read_input_tokens":500,"cache_creation_input_tokens":200}}}`,
		`{"type":"assistant","uuid":"uuid-2","timestamp":"2026-03-21T12:00:01.000Z","message":{"id":"msg-1","model":"claude-sonnet-4-20260514","content":[{"type":"text","text":"I'll fix the login bug by updating auth.py"}]}}`,
		`{"type":"assistant","uuid":"uuid-3","timestamp":"2026-03-21T12:00:02.000Z","message":{"id":"msg-2","content":[{"type":"tool_use","id":"tool-1","name":"Read","input":{"file_path":"auth.py"}}]}}`,
		`{"type":"user","uuid":"uuid-4","timestamp":"2026-03-21T12:00:03.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"def login():\n    pass"}]}}`,
		`{"type":"assistant","uuid":"uuid-5","timestamp":"2026-03-21T12:00:04.000Z","message":{"id":"msg-3","content":[{"type":"tool_use","id":"tool-2","name":"Edit","input":{"file_path":"auth.py","old_string":"pass","new_string":"return True"}}]}}`,
		`{"type":"user","uuid":"uuid-6","timestamp":"2026-03-21T12:00:05.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"tool-2","content":"OK","is_error":false}]}}`,
		`{"type":"assistant","uuid":"uuid-7","timestamp":"2026-03-21T12:00:06.000Z","message":{"id":"msg-4","content":[{"type":"tool_use","id":"tool-3","name":"Bash","input":{"command":"python -m pytest"}}]}}`,
		`{"type":"user","uuid":"uuid-8","timestamp":"2026-03-21T12:00:07.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"tool-3","content":"FAILED: test_login","is_error":true}]}}`,
	}

	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	os.WriteFile(path, []byte(content), 0644)
	return path
}

func TestReadTranscript(t *testing.T) {
	path := writeSampleTranscript(t)

	entries, err := ReadTranscript(path, 0)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(entries) != 8 {
		t.Fatalf("got %d entries, want 8", len(entries))
	}
	if entries[0].Type != "assistant" {
		t.Errorf("entries[0].Type = %q", entries[0].Type)
	}
	if entries[3].Type != "user" {
		t.Errorf("entries[3].Type = %q", entries[3].Type)
	}
}

func TestReadTranscriptWithStartLine(t *testing.T) {
	path := writeSampleTranscript(t)

	entries, err := ReadTranscript(path, 4)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(entries) != 4 {
		t.Errorf("got %d entries, want 4 (skipping first 4)", len(entries))
	}
}

func TestReadTranscriptMissingFile(t *testing.T) {
	_, err := ReadTranscript("/nonexistent/file.jsonl", 0)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFindSlug(t *testing.T) {
	path := writeSampleTranscript(t)
	entries, _ := ReadTranscript(path, 0)

	slug := findSlug(entries)
	if slug != "fix login bug" {
		t.Errorf("findSlug = %q, want %q", slug, "fix login bug")
	}
}

func TestFindSlugMissing(t *testing.T) {
	slug := findSlug([]TranscriptEntry{{Type: "assistant"}})
	if slug != "" {
		t.Errorf("findSlug = %q, want empty", slug)
	}
}

func TestFindModel(t *testing.T) {
	path := writeSampleTranscript(t)
	entries, _ := ReadTranscript(path, 0)

	model := FindModel(entries)
	if model != "claude-sonnet-4-20260514" {
		t.Errorf("FindModel = %q, want %q", model, "claude-sonnet-4-20260514")
	}
}

func TestBuildToolResults(t *testing.T) {
	path := writeSampleTranscript(t)
	entries, _ := ReadTranscript(path, 0)

	results := BuildToolResults(entries)

	// tool-1: Read result (success)
	if r, ok := results["tool-1"]; !ok {
		t.Error("missing result for tool-1")
	} else {
		if r.Result != "def login():\n    pass" {
			t.Errorf("tool-1 result = %q", r.Result)
		}
		if r.IsError {
			t.Error("tool-1 should not be error")
		}
	}

	// tool-3: Bash result (error)
	if r, ok := results["tool-3"]; !ok {
		t.Error("missing result for tool-3")
	} else {
		if !r.IsError {
			t.Error("tool-3 should be error")
		}
		if r.Result != "FAILED: test_login" {
			t.Errorf("tool-3 result = %q", r.Result)
		}
	}
}

func TestParseAssistantMessages(t *testing.T) {
	path := writeSampleTranscript(t)
	entries, _ := ReadTranscript(path, 0)

	parsed := ParseAssistantMessages(entries)

	// Should have: thinking, text, Read tool_use, Edit tool_use, Bash tool_use
	if len(parsed) != 5 {
		t.Fatalf("got %d parsed entries, want 5", len(parsed))
	}

	if parsed[0].ContentType != "thinking" {
		t.Errorf("parsed[0].ContentType = %q", parsed[0].ContentType)
	}
	if parsed[1].ContentType != "text" {
		t.Errorf("parsed[1].ContentType = %q", parsed[1].ContentType)
	}
	if parsed[2].ContentType != "tool_use" {
		t.Errorf("parsed[2].ContentType = %q", parsed[2].ContentType)
	}
	if parsed[2].Content.Name != "Read" {
		t.Errorf("parsed[2].Content.Name = %q", parsed[2].Content.Name)
	}
}

func TestDeduplicateUsage(t *testing.T) {
	path := writeSampleTranscript(t)
	entries, _ := ReadTranscript(path, 0)

	parsed := ParseAssistantMessages(entries)
	DeduplicateUsage(parsed)

	// msg-1 has two entries (thinking + text, same message ID)
	// Dedup assigns last entry's usage to first, clears the rest.
	// In our sample, only the thinking entry (first) has usage;
	// the text entry (last) has nil usage. So after dedup, first
	// gets the last's usage (nil), second also nil.
	// This matches Opik's behavior: final usage report wins.
	if parsed[1].Usage != nil {
		t.Error("second entry of msg-1 should have nil usage (deduped)")
	}

	// Entries with unique message IDs keep their usage
	// parsed[2] is tool_use with msg-2 (no usage in sample)
	// This just verifies dedup doesn't crash on single-entry groups
}

func TestCountLines(t *testing.T) {
	path := writeSampleTranscript(t)

	count := countLines(path)
	if count != 8 {
		t.Errorf("countLines = %d, want 8", count)
	}
}

func TestCountLinesMissingFile(t *testing.T) {
	count := countLines("/nonexistent/file.jsonl")
	if count != 0 {
		t.Errorf("countLines = %d for missing file, want 0", count)
	}
}

package main

import (
	"os"
	"testing"
)

func TestStateSaveLoadRoundtrip(t *testing.T) {
	sessionID := "test-session-" + uuid7()

	state := &State{
		TraceID:    "trace-123",
		RootSpanID: "span-456",
		StartTime:  "2026-03-21T12:00:00.000Z",
		SessionID:  sessionID,
		Transcript: "/tmp/transcript.jsonl",
		StartLine:  42,
		LastFlush:  1234567890,
		SlugSent:   true,
	}

	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	defer os.Remove(statePath(sessionID))

	loaded, err := LoadState(sessionID)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.TraceID != state.TraceID {
		t.Errorf("TraceID = %q, want %q", loaded.TraceID, state.TraceID)
	}
	if loaded.RootSpanID != state.RootSpanID {
		t.Errorf("RootSpanID = %q, want %q", loaded.RootSpanID, state.RootSpanID)
	}
	if loaded.StartLine != state.StartLine {
		t.Errorf("StartLine = %d, want %d", loaded.StartLine, state.StartLine)
	}
	if loaded.LastFlush != state.LastFlush {
		t.Errorf("LastFlush = %d, want %d", loaded.LastFlush, state.LastFlush)
	}
	if loaded.SlugSent != state.SlugSent {
		t.Errorf("SlugSent = %v, want %v", loaded.SlugSent, state.SlugSent)
	}
}

func TestLoadStateMissing(t *testing.T) {
	_, err := LoadState("nonexistent-session-id")
	if err == nil {
		t.Error("expected error for missing state")
	}
}

func TestDeleteState(t *testing.T) {
	sessionID := "test-delete-" + uuid7()

	state := &State{
		TraceID:   "trace-del",
		SessionID: sessionID,
	}
	SaveState(state)
	SaveAgentMap(sessionID, AgentMap{"agent1": "uuid1"})

	DeleteState(sessionID)

	_, err := LoadState(sessionID)
	if err == nil {
		t.Error("state should be deleted")
	}

	agents := LoadAgentMap(sessionID)
	if len(agents) != 0 {
		t.Error("agent map should be deleted")
	}
}

func TestAgentMapSaveLoadRoundtrip(t *testing.T) {
	sessionID := "test-agents-" + uuid7()

	agents := AgentMap{
		"agent-1": "uuid-aaa",
		"agent-2": "",
		"agent-3": "uuid-bbb",
	}

	if err := SaveAgentMap(sessionID, agents); err != nil {
		t.Fatalf("SaveAgentMap: %v", err)
	}
	defer os.Remove(agentsPath(sessionID))

	loaded := LoadAgentMap(sessionID)
	if len(loaded) != 3 {
		t.Errorf("got %d agents, want 3", len(loaded))
	}
	if loaded["agent-1"] != "uuid-aaa" {
		t.Errorf("agent-1 = %q", loaded["agent-1"])
	}
	if loaded["agent-2"] != "" {
		t.Errorf("agent-2 = %q, want empty", loaded["agent-2"])
	}
}

func TestLoadAgentMapMissing(t *testing.T) {
	agents := LoadAgentMap("nonexistent-session")
	if len(agents) != 0 {
		t.Errorf("got %d agents for missing file, want 0", len(agents))
	}
}

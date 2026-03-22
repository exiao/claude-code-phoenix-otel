package main

import (
	"encoding/hex"
	"regexp"
	"testing"
)

func TestUUID7Format(t *testing.T) {
	id := uuid7()
	// UUIDv7 format: xxxxxxxx-xxxx-7xxx-yxxx-xxxxxxxxxxxx
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRegex.MatchString(id) {
		t.Errorf("uuid7() = %q, does not match UUIDv7 format", id)
	}
}

func TestUUID7Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := uuid7()
		if seen[id] {
			t.Fatalf("uuid7() produced duplicate: %s", id)
		}
		seen[id] = true
	}
}

func TestToV7Deterministic(t *testing.T) {
	input := "test-uuid-input"
	a := toV7(input)
	b := toV7(input)
	if a != b {
		t.Errorf("toV7(%q) not deterministic: %q != %q", input, a, b)
	}
}

func TestToV7Format(t *testing.T) {
	id := toV7("some-uuid")
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRegex.MatchString(id) {
		t.Errorf("toV7() = %q, does not match UUIDv7 format", id)
	}
}

func TestToV7DifferentInputs(t *testing.T) {
	a := toV7("input-a")
	b := toV7("input-b")
	if a == b {
		t.Error("toV7() produced same output for different inputs")
	}
}

func TestTraceIDFromUUID(t *testing.T) {
	id := traceIDFromUUID("test-trace")
	hex := hex.EncodeToString(id[:])
	if len(hex) != 32 {
		t.Errorf("traceIDFromUUID produced %d hex chars, want 32", len(hex))
	}
}

func TestTraceIDFromUUIDDeterministic(t *testing.T) {
	a := traceIDFromUUID("same-input")
	b := traceIDFromUUID("same-input")
	if a != b {
		t.Error("traceIDFromUUID not deterministic")
	}
}

func TestSpanIDFromUUID(t *testing.T) {
	id := spanIDFromUUID("test-span")
	hex := hex.EncodeToString(id[:])
	if len(hex) != 16 {
		t.Errorf("spanIDFromUUID produced %d hex chars, want 16", len(hex))
	}
}

func TestSpanIDFromUUIDDeterministic(t *testing.T) {
	a := spanIDFromUUID("same-input")
	b := spanIDFromUUID("same-input")
	if a != b {
		t.Error("spanIDFromUUID not deterministic")
	}
}

func TestNewSpanID(t *testing.T) {
	a := newSpanID()
	b := newSpanID()
	if a == b {
		t.Error("newSpanID produced identical IDs")
	}
	empty := [8]byte{}
	if a == empty {
		t.Error("newSpanID produced zero ID")
	}
}

func TestNewTraceID(t *testing.T) {
	a := newTraceID()
	b := newTraceID()
	if a == b {
		t.Error("newTraceID produced identical IDs")
	}
	empty := [16]byte{}
	if a == empty {
		t.Error("newTraceID produced zero ID")
	}
}

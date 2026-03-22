package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".phoenix.config")
	os.WriteFile(cfgPath, []byte(`
# comment
host=https://phoenix.example.com
api_key=sk-test-123
project_name=my-project
`), 0644)

	result := parseConfigFile(cfgPath)

	if result["host"] != "https://phoenix.example.com" {
		t.Errorf("host = %q, want %q", result["host"], "https://phoenix.example.com")
	}
	if result["api_key"] != "sk-test-123" {
		t.Errorf("api_key = %q, want %q", result["api_key"], "sk-test-123")
	}
	if result["project_name"] != "my-project" {
		t.Errorf("project_name = %q, want %q", result["project_name"], "my-project")
	}
}

func TestParseConfigFileSkipsComments(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".phoenix.config")
	os.WriteFile(cfgPath, []byte(`
# this is a comment
host=https://example.com

# another comment
`), 0644)

	result := parseConfigFile(cfgPath)
	if len(result) != 1 {
		t.Errorf("got %d entries, want 1", len(result))
	}
}

func TestParseConfigFileMissing(t *testing.T) {
	result := parseConfigFile("/nonexistent/path/.phoenix.config")
	if len(result) != 0 {
		t.Errorf("got %d entries for missing file, want 0", len(result))
	}
}

func TestGetEnvOrConfig(t *testing.T) {
	fileConfig := map[string]string{"host": "from-file"}

	// Env var takes precedence
	t.Setenv("TEST_PHOENIX_HOST", "from-env")
	result := getEnvOrConfig("TEST_PHOENIX_HOST", fileConfig, "host")
	if result != "from-env" {
		t.Errorf("got %q, want %q", result, "from-env")
	}

	// Falls back to file config
	result = getEnvOrConfig("TEST_PHOENIX_MISSING", fileConfig, "host")
	if result != "from-file" {
		t.Errorf("got %q, want %q", result, "from-file")
	}

	// Returns empty when neither exists
	result = getEnvOrConfig("TEST_PHOENIX_MISSING", fileConfig, "missing_key")
	if result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestLoadConfigReturnsNilWithoutHost(t *testing.T) {
	// Clear all relevant env vars
	t.Setenv("PHOENIX_HOST", "")
	t.Setenv("PHOENIX_COLLECTOR_ENDPOINT", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config when no host is set")
	}
}

func TestLoadConfigWithEnvVars(t *testing.T) {
	t.Setenv("PHOENIX_HOST", "https://phoenix.example.com")
	t.Setenv("PHOENIX_API_KEY", "sk-test")
	t.Setenv("PHOENIX_PROJECT_NAME", "test-project")
	t.Setenv("PHOENIX_CC_DEBUG", "true")
	t.Setenv("PHOENIX_CC_TRUNCATE_FIELDS", "false")
	t.Setenv("PHOENIX_CC_PARENT_TRACE_ID", "parent-123")
	t.Setenv("PHOENIX_CC_ROOT_SPAN_ID", "span-456")

	// Need tracing enabled
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".claude", ".phoenix-tracing-enabled"), []byte(""), 0644)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	if cfg.URL != "https://phoenix.example.com" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.APIKey != "sk-test" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.Project != "test-project" {
		t.Errorf("Project = %q", cfg.Project)
	}
	if !cfg.Debug {
		t.Error("Debug should be true")
	}
	if cfg.Truncate {
		t.Error("Truncate should be false")
	}
	if cfg.ParentTraceID != "parent-123" {
		t.Errorf("ParentTraceID = %q", cfg.ParentTraceID)
	}
	if cfg.RootSpanID != "span-456" {
		t.Errorf("RootSpanID = %q", cfg.RootSpanID)
	}
}

func TestLoadConfigProjectOverride(t *testing.T) {
	t.Setenv("PHOENIX_HOST", "https://phoenix.example.com")
	t.Setenv("PHOENIX_PROJECT_NAME", "base-project")
	t.Setenv("PHOENIX_CC_PROJECT", "override-project")

	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".claude", ".phoenix-tracing-enabled"), []byte(""), 0644)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project != "override-project" {
		t.Errorf("Project = %q, want override-project", cfg.Project)
	}
}

func TestTracingStateFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)
	claudeDir := filepath.Join(tmpDir, ".claude")
	os.MkdirAll(claudeDir, 0755)

	// No file = not found (config decides)
	state := getTracingState()
	if state.found {
		t.Error("should not be found without file")
	}

	// Empty file = enabled
	os.WriteFile(filepath.Join(claudeDir, ".phoenix-tracing-enabled"), []byte(""), 0644)
	state = getTracingState()
	if !state.enabled {
		t.Error("should be enabled with file")
	}
	if !state.found {
		t.Error("should be found with file")
	}
	if state.debug {
		t.Error("should not be debug with empty file")
	}

	// "debug" content = enabled + debug
	os.WriteFile(filepath.Join(claudeDir, ".phoenix-tracing-enabled"), []byte("debug"), 0644)
	state = getTracingState()
	if !state.enabled {
		t.Error("should be enabled")
	}
	if !state.debug {
		t.Error("should be debug")
	}

	// "disabled" content = explicitly disabled
	os.WriteFile(filepath.Join(claudeDir, ".phoenix-tracing-enabled"), []byte("disabled"), 0644)
	state = getTracingState()
	if state.enabled {
		t.Error("should be disabled with 'disabled' content")
	}
	if !state.found {
		t.Error("should still be found")
	}
}

func TestAutoEnableWithConfig(t *testing.T) {
	t.Setenv("PHOENIX_HOST", "https://phoenix.example.com")
	// No tracing file, no CLAUDE_PROJECT_DIR
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if !cfg.Enabled {
		t.Error("should auto-enable when PHOENIX_HOST is set and no tracing file")
	}
}

func TestURLTrailingSlashStripped(t *testing.T) {
	t.Setenv("PHOENIX_HOST", "https://phoenix.example.com/")

	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".claude", ".phoenix-tracing-enabled"), []byte(""), 0644)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "https://phoenix.example.com" {
		t.Errorf("URL = %q, trailing slash not stripped", cfg.URL)
	}
}

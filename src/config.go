package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	URL           string
	Project       string
	APIKey        string
	Debug         bool
	Truncate      bool
	Enabled       bool
	ParentTraceID string
	RootSpanID    string
}

const truncateMsg = "[ TRUNCATED -- set PHOENIX_CC_TRUNCATE_FIELDS=false ]"

func LoadConfig() (*Config, error) {
	homeDir, _ := os.UserHomeDir()
	var fileConfig map[string]string
	if homeDir != "" {
		fileConfig = parseConfigFile(filepath.Join(homeDir, ".phoenix.config"))
	}

	url := getEnvOrConfig("PHOENIX_HOST", fileConfig, "host")
	if url == "" {
		url = getEnvOrConfig("PHOENIX_COLLECTOR_ENDPOINT", fileConfig, "collector_endpoint")
	}
	if url == "" {
		return nil, nil
	}

	tracing := getTracingState()

	// Auto-enable when PHOENIX_HOST is configured.
	// The .phoenix-tracing-enabled file can override: if it exists, use it.
	// If it doesn't exist, default to enabled (config presence = intent to trace).
	enabled := true
	if tracing.found {
		enabled = tracing.enabled
	}

	cfg := &Config{
		URL:           strings.TrimSuffix(url, "/"),
		Project:       "claude-code",
		APIKey:        getEnvOrConfig("PHOENIX_API_KEY", fileConfig, "api_key"),
		Debug:         os.Getenv("PHOENIX_CC_DEBUG") == "true" || tracing.debug,
		Truncate:      os.Getenv("PHOENIX_CC_TRUNCATE_FIELDS") != "false",
		Enabled:       enabled,
		ParentTraceID: os.Getenv("PHOENIX_CC_PARENT_TRACE_ID"),
		RootSpanID:    os.Getenv("PHOENIX_CC_ROOT_SPAN_ID"),
	}

	if proj := getEnvOrConfig("PHOENIX_PROJECT_NAME", fileConfig, "project_name"); proj != "" {
		cfg.Project = proj
	}
	if proj := os.Getenv("PHOENIX_CC_PROJECT"); proj != "" {
		cfg.Project = proj
	}

	return cfg, nil
}

func parseConfigFile(path string) map[string]string {
	result := make(map[string]string)
	file, err := os.Open(path)
	if err != nil {
		return result
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func getEnvOrConfig(envVar string, fileConfig map[string]string, configKey string) string {
	if val := os.Getenv(envVar); val != "" {
		return val
	}
	return fileConfig[configKey]
}

type tracingState struct {
	enabled bool
	debug   bool
	found   bool // whether the tracing file was found at all
}

func checkTracingFile(path string) (tracingState, bool) {
	if _, err := os.Stat(path); err == nil {
		state := tracingState{enabled: true, found: true}
		if data, err := os.ReadFile(path); err == nil {
			content := strings.TrimSpace(string(data))
			state.debug = content == "debug"
			// A file containing "false" or "disabled" explicitly disables tracing
			if content == "false" || content == "disabled" {
				state.enabled = false
			}
		}
		return state, true
	}
	return tracingState{}, false
}

func getTracingState() tracingState {
	if projectDir := os.Getenv("CLAUDE_PROJECT_DIR"); projectDir != "" {
		if state, found := checkTracingFile(filepath.Join(projectDir, ".claude", ".phoenix-tracing-enabled")); found {
			return state
		}
	}
	return tracingState{} // found=false: let config decide
}

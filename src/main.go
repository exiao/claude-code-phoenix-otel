package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	flushInterval  = 5 * time.Second
	maxLogFileSize = 1 << 20 // 1 MB
	maxFieldLen    = 64000
)

type HookInput struct {
	HookEventName        string `json:"hook_event_name"`
	SessionID            string `json:"session_id"`
	TranscriptPath       string `json:"transcript_path"`
	Prompt               string `json:"prompt"`
	AgentID              string `json:"agent_id"`
	AgentType            string `json:"agent_type"`
	AgentTranscriptPath  string `json:"agent_transcript_path"`
	CustomInstructions   string `json:"custom_instructions"`
}

var (
	config   *Config
	exporter *OTLPExporter
	input    HookInput
)

func main() {
	var err error
	config, err = LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "phoenix: %v\n", err)
		os.Exit(1)
	}
	if config == nil || !config.Enabled {
		os.Exit(0)
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "phoenix: failed to read stdin: %v\n", err)
		os.Exit(1)
	}

	if err := json.Unmarshal(data, &input); err != nil {
		fmt.Fprintf(os.Stderr, "phoenix: failed to parse input: %v\n", err)
		os.Exit(1)
	}

	debugLog("=== %s ===", input.HookEventName)

	exporter = NewOTLPExporter(config)

	switch input.HookEventName {
	case "UserPromptSubmit":
		onPrompt()
	case "PostToolUse", "PostToolUseFailure":
		onTool()
	case "SubagentStart":
		onSubagentStart()
	case "SubagentStop":
		onSubagentStop()
	case "Stop":
		onStop()
	case "SessionEnd":
		onSessionEnd()
	case "PreCompact":
		onCompact()
	default:
		debugLog("unknown event: %s", input.HookEventName)
	}
}

func onPrompt() {
	startLine := 0
	if input.TranscriptPath != "" {
		startLine = countLines(input.TranscriptPath)
	}

	traceID := uuid7()
	if config.ParentTraceID != "" {
		traceID = config.ParentTraceID
	}
	rootSpanID := uuid7()
	ts := isoNow()

	state := &State{
		TraceID:    traceID,
		RootSpanID: rootSpanID,
		StartTime:  ts,
		SessionID:  input.SessionID,
		Transcript: input.TranscriptPath,
		StartLine:  startLine,
		LastFlush:  time.Now().Unix(),
	}
	if err := SaveState(state); err != nil {
		debugLog("save state: %v", err)
	}

	debugLog("trace=%s rootSpan=%s start=%d", traceID, rootSpanID, startLine)

	// Create root AGENT span
	now := time.Now()
	rootSpan := OTLPSpan{
		TraceID:   traceIDFromUUID(traceID),
		SpanID:    spanIDFromUUID(rootSpanID),
		Name:      "claude-code",
		StartTime: now,
		EndTime:   now,
		Kind:      SpanKindServer,
		Attributes: openInferenceAttrs("AGENT", map[string]interface{}{
			"input.value":     safeStringify(map[string]string{"prompt": input.Prompt}, maxFieldLen),
			"input.mime_type": "application/json",
			"session.id":      input.SessionID,
			"agent.name":      "claude-code",
		}),
		Status: SpanStatus{Code: 0},
	}

	if config.RootSpanID != "" {
		rootSpan.ParentSpanID = spanIDFromUUID(config.RootSpanID)
	}

	if err := exporter.ExportSpans([]OTLPSpan{rootSpan}); err != nil {
		debugLog("export root span: %v", err)
	}
}

func onTool() {
	state, err := LoadState(input.SessionID)
	if err != nil {
		debugLog("load state: %v", err)
		return
	}

	now := time.Now().Unix()
	if time.Since(time.Unix(state.LastFlush, 0)) >= flushInterval {
		debugLog("flushing (%ds)", now-state.LastFlush)
		flush(state)
		state.LastFlush = now
		if err := SaveState(state); err != nil {
			debugLog("save state: %v", err)
		}
	}
}

func onStop() {
	time.Sleep(100 * time.Millisecond)

	state, err := LoadState(input.SessionID)
	if err != nil {
		debugLog("load state: %v", err)
		return
	}

	flush(state)

	// Update root span with output and end time
	output := getLastOutput(state)
	now := time.Now()
	startTime := parseTimestamp(state.StartTime)

	rootSpan := OTLPSpan{
		TraceID:   traceIDFromUUID(state.TraceID),
		SpanID:    spanIDFromUUID(state.RootSpanID),
		Name:      "claude-code",
		StartTime: startTime,
		EndTime:   now,
		Kind:      SpanKindServer,
		Attributes: openInferenceAttrs("AGENT", map[string]interface{}{
			"output.value":     output,
			"output.mime_type": "text/plain",
			"session.id":       input.SessionID,
			"agent.name":       "claude-code",
		}),
		Status: SpanStatus{Code: 1},
	}

	if config.RootSpanID != "" {
		rootSpan.ParentSpanID = spanIDFromUUID(config.RootSpanID)
	}

	// Add slug as trace name
	allEntries, err := ReadTranscript(state.Transcript, 0)
	if err == nil {
		if slug := findSlug(allEntries); slug != "" {
			rootSpan.Name = slug
		}
		if model := FindModel(allEntries); model != "" {
			rootSpan.Attributes["llm.model_name"] = model
		}
	}

	if err := exporter.ExportSpans([]OTLPSpan{rootSpan}); err != nil {
		debugLog("update root span: %v", err)
	}

	debugLog("done")
}

func onSessionEnd() {
	state, err := LoadState(input.SessionID)
	if err == nil {
		flush(state)

		now := time.Now()
		startTime := parseTimestamp(state.StartTime)
		rootSpan := OTLPSpan{
			TraceID:   traceIDFromUUID(state.TraceID),
			SpanID:    spanIDFromUUID(state.RootSpanID),
			Name:      "claude-code",
			StartTime: startTime,
			EndTime:   now,
			Kind:      SpanKindServer,
			Attributes: openInferenceAttrs("AGENT", map[string]interface{}{
				"session.id": input.SessionID,
				"agent.name": "claude-code",
			}),
			Status: SpanStatus{Code: 1},
		}
		if err := exporter.ExportSpans([]OTLPSpan{rootSpan}); err != nil {
			debugLog("session end update: %v", err)
		}
	}
	DeleteState(input.SessionID)
	debugLog("session ended")
}

func onCompact() {
	state, err := LoadState(input.SessionID)
	if err != nil {
		debugLog("compact: no state, bootstrapping: %v", err)
		traceID := uuid7()
		if config.ParentTraceID != "" {
			traceID = config.ParentTraceID
		}
		rootSpanID := uuid7()
		ts := isoNow()

		state = &State{
			TraceID:    traceID,
			RootSpanID: rootSpanID,
			StartTime:  ts,
			SessionID:  input.SessionID,
			Transcript: input.TranscriptPath,
			StartLine:  countLines(input.TranscriptPath),
			LastFlush:  time.Now().Unix(),
		}

		now := time.Now()
		rootSpan := OTLPSpan{
			TraceID:   traceIDFromUUID(traceID),
			SpanID:    spanIDFromUUID(rootSpanID),
			Name:      "claude-code",
			StartTime: now,
			EndTime:   now,
			Kind:      SpanKindServer,
			Attributes: openInferenceAttrs("AGENT", map[string]interface{}{
				"session.id": input.SessionID,
				"agent.name": "claude-code",
			}),
			Status: SpanStatus{Code: 0},
		}
		if err := exporter.ExportSpans([]OTLPSpan{rootSpan}); err != nil {
			debugLog("compact: create root: %v", err)
		}

		if err := SaveState(state); err != nil {
			debugLog("save state: %v", err)
		}
	} else {
		flush(state)
	}

	// Create compaction marker trace
	compactTraceID := uuid7()
	compactRootSpanID := uuid7()
	compactChildSpanID := uuid7()
	now := time.Now()

	traceName := "claude-code"
	allEntries, err := ReadTranscript(state.Transcript, 0)
	if err == nil {
		if slug := findSlug(allEntries); slug != "" {
			traceName = slug
		}
	}

	compactInput := "/compact"
	if input.CustomInstructions != "" {
		compactInput = "/compact " + input.CustomInstructions
	}

	compactSpans := []OTLPSpan{
		{
			TraceID:   traceIDFromUUID(compactTraceID),
			SpanID:    spanIDFromUUID(compactRootSpanID),
			Name:      traceName,
			StartTime: now,
			EndTime:   now,
			Kind:      SpanKindServer,
			Attributes: openInferenceAttrs("AGENT", map[string]interface{}{
				"session.id": input.SessionID,
				"agent.name": "claude-code",
				"metadata":   `{"tags":["compaction"]}`,
			}),
			Status: SpanStatus{Code: 1},
		},
		{
			TraceID:      traceIDFromUUID(compactTraceID),
			SpanID:       spanIDFromUUID(compactChildSpanID),
			ParentSpanID: spanIDFromUUID(compactRootSpanID),
			Name:         "Compaction",
			StartTime:    now,
			EndTime:      now,
			Kind:         SpanKindInternal,
			Attributes: openInferenceAttrs("CHAIN", map[string]interface{}{
				"input.value":      compactInput,
				"input.mime_type":  "text/plain",
				"output.value":     "compacted",
				"output.mime_type": "text/plain",
			}),
			Status: SpanStatus{Code: 1},
		},
	}

	if err := exporter.ExportSpans(compactSpans); err != nil {
		debugLog("send compaction spans: %v", err)
	}

	state.TraceID = compactTraceID
	state.RootSpanID = compactRootSpanID
	state.StartLine = countLines(input.TranscriptPath)
	state.LastFlush = time.Now().Unix()
	if err := SaveState(state); err != nil {
		debugLog("save state: %v", err)
	}
}

func onSubagentStart() {
	if input.AgentID == "" {
		return
	}
	debugLog("subagent_start: %s (%s)", input.AgentID, input.AgentType)

	agents := LoadAgentMap(input.SessionID)
	agents[input.AgentID] = ""
	if err := SaveAgentMap(input.SessionID, agents); err != nil {
		debugLog("save agent map: %v", err)
	}
}

func onSubagentStop() {
	debugLog("subagent_stop: %s", input.AgentID)

	if input.AgentID == "" || input.AgentTranscriptPath == "" {
		return
	}

	state, err := LoadState(input.SessionID)
	if err != nil {
		debugLog("load state: %v", err)
		return
	}

	agents := LoadAgentMap(input.SessionID)
	if _, ok := agents[input.AgentID]; !ok {
		return
	}

	parentUUID := agents[input.AgentID]
	if parentUUID == "" {
		parentUUID = findTaskUUID(agents)
		if parentUUID == "" {
			debugLog("subagent_stop: no matching Task for %s", input.AgentID)
			return
		}
		agents[input.AgentID] = parentUUID
		if err := SaveAgentMap(input.SessionID, agents); err != nil {
			debugLog("save agent map: %v", err)
		}
	}

	parentSpanID := spanIDFromUUID(toV7(parentUUID))
	debugLog("processing subagent with parent=%s", fmt.Sprintf("%x", parentSpanID))

	spans := processTranscript(state.TraceID, input.AgentTranscriptPath, 0, parentSpanID)
	if len(spans) == 0 {
		return
	}

	debugLog("subagent flush: %d spans", len(spans))
	if err := exporter.ExportSpans(spans); err != nil {
		debugLog("send subagent spans: %v", err)
	}
}

func findTaskUUID(agents AgentMap) string {
	subPrompt := extractSubagentPrompt(input.AgentTranscriptPath)

	entries, err := ReadTranscript(input.TranscriptPath, 0)
	if err != nil {
		return ""
	}

	claimed := make(map[string]bool, len(agents))
	for _, uuid := range agents {
		if uuid != "" {
			claimed[uuid] = true
		}
	}

	var promptMatch, typeMatch, fallbackUUID string
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Type != "assistant" || entry.Message == nil {
			continue
		}
		for _, content := range entry.Message.Content {
			if content.Type != "tool_use" || (content.Name != "Agent" && content.Name != "Task") {
				continue
			}
			if claimed[entry.UUID] {
				continue
			}
			if promptMatch == "" && subPrompt != "" {
				if p, ok := content.Input["prompt"].(string); ok && p == subPrompt {
					promptMatch = entry.UUID
				}
			}
			if typeMatch == "" {
				if st, ok := content.Input["subagent_type"].(string); ok && st == input.AgentType {
					typeMatch = entry.UUID
				}
			}
			if fallbackUUID == "" {
				fallbackUUID = entry.UUID
			}
		}
		if promptMatch != "" {
			break
		}
	}

	if promptMatch != "" {
		return promptMatch
	}
	if typeMatch != "" {
		return typeMatch
	}
	return fallbackUUID
}

func extractSubagentPrompt(path string) string {
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, initialBufferSize)
	scanner.Buffer(buf, maxBufferSize)

	for scanner.Scan() {
		var raw struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil || raw.Type != "user" || raw.Message == nil {
			continue
		}
		var msg struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw.Message, &msg); err != nil || msg.Content == nil {
			continue
		}
		var str string
		if err := json.Unmarshal(msg.Content, &str); err == nil && str != "" {
			return str
		}
		var contents []Content
		if err := json.Unmarshal(msg.Content, &contents); err == nil {
			for _, c := range contents {
				if c.Type == "text" && c.Text != "" {
					return c.Text
				}
			}
		}
	}
	return ""
}

func flush(state *State) {
	// Update root span with slug and model
	if !state.SlugSent {
		allEntries, err := ReadTranscript(state.Transcript, 0)
		if err == nil && len(allEntries) > 0 {
			slug := findSlug(allEntries)
			model := FindModel(allEntries)
			if slug != "" || model != "" {
				startTime := parseTimestamp(state.StartTime)
				now := time.Now()
				attrs := openInferenceAttrs("AGENT", map[string]interface{}{
					"session.id": state.SessionID,
					"agent.name": "claude-code",
				})
				name := "claude-code"
				if slug != "" {
					name = slug
				}
				if model != "" {
					attrs["llm.model_name"] = model
				}

				rootSpan := OTLPSpan{
					TraceID:    traceIDFromUUID(state.TraceID),
					SpanID:     spanIDFromUUID(state.RootSpanID),
					Name:       name,
					StartTime:  startTime,
					EndTime:    now,
					Kind:       SpanKindServer,
					Attributes: attrs,
					Status:     SpanStatus{Code: 0},
				}
				if err := exporter.ExportSpans([]OTLPSpan{rootSpan}); err != nil {
					debugLog("update root metadata: %v", err)
				} else if slug != "" {
					state.SlugSent = true
				}
			}
		}
	}

	entries, err := ReadTranscript(state.Transcript, state.StartLine)
	if err != nil || len(entries) == 0 {
		return
	}

	emptyParent := [8]byte{}
	spans := processTranscriptEntries(state.TraceID, entries, spanIDFromUUID(state.RootSpanID), emptyParent)
	if len(spans) == 0 {
		return
	}

	debugLog("flush: %d spans", len(spans))
	if err := exporter.ExportSpans(spans); err != nil {
		debugLog("send spans: %v", err)
	}
}

func processTranscript(traceID, path string, startLine int, parentSpanID [8]byte) []OTLPSpan {
	entries, err := ReadTranscript(path, startLine)
	if err != nil || len(entries) == 0 {
		return nil
	}
	rootSpanID := [8]byte{} // no root span for subagent transcripts
	return processTranscriptEntries(traceID, entries, rootSpanID, parentSpanID)
}

func processTranscriptEntries(traceID string, entries []TranscriptEntry, rootSpanID [8]byte, parentSpanID [8]byte) []OTLPSpan {
	toolResults := BuildToolResults(entries)
	taskResults := BuildTaskResults(entries)
	parsed := ParseAssistantMessages(entries)
	DeduplicateUsage(parsed)

	effectiveParentSpanID := parentSpanID
	emptyID := [8]byte{}
	if effectiveParentSpanID == emptyID && rootSpanID != emptyID {
		effectiveParentSpanID = rootSpanID
	}
	if effectiveParentSpanID == emptyID && config.RootSpanID != "" {
		effectiveParentSpanID = spanIDFromUUID(config.RootSpanID)
	}

	traceBytes := traceIDFromUUID(traceID)
	var spans []OTLPSpan

	for i, p := range parsed {
		startTime := parseTimestamp(p.Timestamp)
		endTime := startTime
		if i+1 < len(parsed) {
			endTime = parseTimestamp(parsed[i+1].Timestamp)
		}

		if p.ContentType == "tool_use" {
			if result, ok := toolResults[p.Content.ID]; ok && result != nil && result.Timestamp != "" {
				endTime = parseTimestamp(result.Timestamp)
			}
		}

		span := OTLPSpan{
			TraceID:      traceBytes,
			SpanID:       spanIDFromUUID(toV7(p.UUID)),
			ParentSpanID: effectiveParentSpanID,
			StartTime:    startTime,
			EndTime:      endTime,
			Kind:         SpanKindInternal,
			Status:       SpanStatus{Code: 1},
		}

		switch p.ContentType {
		case "thinking":
			span.Name = "Thinking"
			span.Attributes = openInferenceAttrs("CHAIN", map[string]interface{}{
				"output.value":     safeStringify(p.Content.Thinking, maxFieldLen),
				"output.mime_type": "text/plain",
			})

		case "text":
			span.Name = "Response"
			span.Attributes = openInferenceAttrs("CHAIN", map[string]interface{}{
				"output.value":     safeStringify(p.Content.Text, maxFieldLen),
				"output.mime_type": "text/plain",
			})

		case "tool_use":
			buildToolSpan(&span, p, toolResults, taskResults)

		default:
			continue
		}

		// Add token usage to LLM-like spans
		if p.Usage != nil {
			span.Attributes["llm.token_count.prompt"] = p.Usage.InputTokens
			span.Attributes["llm.token_count.completion"] = p.Usage.OutputTokens
			span.Attributes["llm.token_count.total"] = p.Usage.InputTokens + p.Usage.OutputTokens
			if p.Usage.CacheReadInputTokens > 0 {
				span.Attributes["llm.token_count.prompt_details.cache_read"] = p.Usage.CacheReadInputTokens
			}
			if p.Usage.CacheCreationInputTokens > 0 {
				span.Attributes["llm.token_count.prompt_details.cache_write"] = p.Usage.CacheCreationInputTokens
			}
			if p.Model != "" {
				span.Attributes["llm.model_name"] = p.Model
				span.Attributes["llm.provider"] = "anthropic"
			}
		}

		spans = append(spans, span)
	}

	return spans
}

func buildToolSpan(span *OTLPSpan, p ParsedEntry, toolResults map[string]*ToolResultInfo, taskResults map[string]*ToolUseResult) {
	toolName := p.Content.Name
	if toolName == "" {
		toolName = "Tool"
	}
	span.Name = toolName
	toolID := p.Content.ID

	inputVal := safeStringify(p.Content.Input, maxFieldLen)
	outputVal := ""

	switch toolName {
	case "Edit", "Write":
		if config.Truncate {
			inputVal = truncateMsg
			outputVal = truncateMsg
		}
	case "Read":
		if config.Truncate {
			outputVal = truncateMsg
		}
	case "Agent", "Task":
		subType := "Task"
		if st, ok := p.Content.Input["subagent_type"].(string); ok && st != "" {
			subType = st
		}
		span.Name = subType + " Subagent"

		prompt := ""
		if pr, ok := p.Content.Input["prompt"].(string); ok {
			prompt = pr
		}
		inputVal = safeStringify(map[string]string{"prompt": prompt}, maxFieldLen)

		if result, ok := taskResults[toolID]; ok && result != nil {
			resp := ""
			if len(result.Content) > 0 {
				resp = result.Content[0].Text
			}
			outputVal = safeStringify(map[string]string{"response": resp}, maxFieldLen)
			if result.TotalTokens > 0 {
				span.Attributes = openInferenceAttrs("AGENT", map[string]interface{}{
					"tool.name":      toolName,
					"input.value":    inputVal,
					"input.mime_type": "application/json",
					"output.value":    outputVal,
					"output.mime_type": "application/json",
					"llm.token_count.total": result.TotalTokens,
				})
				return
			}
		}

		span.Attributes = openInferenceAttrs("AGENT", map[string]interface{}{
			"tool.name":       toolName,
			"input.value":     inputVal,
			"input.mime_type": "application/json",
			"output.value":     outputVal,
			"output.mime_type": "application/json",
		})
		return

	default:
		if result, ok := toolResults[toolID]; ok && result != nil {
			outputVal = safeStringify(result.Result, maxFieldLen)
			if result.IsError {
				errType := categorizeError(result.Result)
				span.Status = SpanStatus{Code: 2, Message: truncateString(result.Result, 500)}
				span.Attributes = openInferenceAttrs("TOOL", map[string]interface{}{
					"tool.name":        toolName,
					"input.value":      inputVal,
					"input.mime_type":  "application/json",
					"output.value":     outputVal,
					"output.mime_type": "text/plain",
					"error.type":       errType,
				})
				return
			}
		}
	}

	span.Attributes = openInferenceAttrs("TOOL", map[string]interface{}{
		"tool.name":        toolName,
		"input.value":      inputVal,
		"input.mime_type":  "application/json",
		"output.value":     outputVal,
		"output.mime_type": "text/plain",
	})
}

func findSlug(entries []TranscriptEntry) string {
	for _, entry := range entries {
		if entry.Slug != "" {
			return entry.Slug
		}
	}
	return ""
}

func getLastOutput(state *State) string {
	entries, err := ReadTranscript(state.Transcript, state.StartLine)
	if err != nil {
		return ""
	}
	var lastText string
	for _, entry := range entries {
		if entry.Type != "assistant" || entry.Message == nil || len(entry.Message.Content) == 0 {
			continue
		}
		if entry.Message.Content[0].Type == "text" {
			lastText = entry.Message.Content[0].Text
		}
	}
	return lastText
}

func isoNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func debugLog(format string, args ...interface{}) {
	if config == nil || !config.Debug {
		return
	}
	logPath := filepath.Join(os.TempDir(), "phoenix-debug.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err == nil && info.Size() > maxLogFileSize {
		if err := f.Truncate(0); err != nil {
			return
		}
		if _, err := f.Seek(0, 0); err != nil {
			return
		}
	}

	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(f, "[%s] ", ts)
	fmt.Fprintf(f, format+"\n", args...)
}

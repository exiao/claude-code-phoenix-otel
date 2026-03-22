---
description: Control Phoenix tracing for your Claude Code sessions
argument-hint: [start|stop|status] [--debug]
allowed-tools:
  - Bash
  - Read
  - Write
model: haiku
---

# Phoenix Claude Code Session Tracing

This command enables/disables automatic tracing of your Claude Code sessions to Phoenix (Arize).

Based on the user's request: **$ARGUMENTS**

## Scope

Tracing is configured per-project via `.claude/.phoenix-tracing-enabled` in the project directory.

## File Semantics

- File exists → tracing enabled
- File contains `debug` → tracing + debug logging
- File doesn't exist → tracing disabled

## Actions

**If the request contains "start":**
- Create `.claude/` directory if needed with `mkdir -p`
- If `--debug` is present, write `debug` to `.claude/.phoenix-tracing-enabled`
- Otherwise, touch/create the file (content doesn't matter)
- Confirm: "Phoenix session tracing enabled for this project. Takes effect immediately for new conversation turns."

**If the request contains "stop":**
- Delete `.claude/.phoenix-tracing-enabled`
- Confirm: "Phoenix session tracing disabled for this project."

**If the request is "status":**
1. Check if `.claude/.phoenix-tracing-enabled` exists in current directory
2. Report state: "Tracing: [enabled/enabled+debug/disabled]"

## Examples

```
/phoenix:trace-claude-code start          # Enable session tracing
/phoenix:trace-claude-code start --debug  # Enable tracing + debug logging
/phoenix:trace-claude-code stop           # Disable tracing
/phoenix:trace-claude-code status         # Check current state
```

## What This Does

When enabled, all your Claude Code interactions are automatically logged to Phoenix:
- Each conversation turn becomes a trace
- Tool calls, thinking, and responses become spans
- Subagent invocations are nested under their parent
- Uses OpenInference semantic conventions for native Phoenix rendering

View your traces at your configured Phoenix endpoint.

## Notes

- Changes take effect immediately for new conversation turns (no restart needed)
- Debug logs are written to `$TMPDIR/phoenix-debug.log`
- Requires Phoenix configuration (env vars or `~/.phoenix.config`)

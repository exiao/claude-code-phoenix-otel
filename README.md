# claude-code-phoenix-otel

Export [Claude Code](https://docs.anthropic.com/en/docs/claude-code) session traces to [Phoenix (Arize)](https://phoenix.arize.com) via OpenTelemetry.

Each conversation turn becomes a trace. Tool calls, thinking, and responses become spans. Subagent invocations nest under their parent. Uses [OpenInference](https://github.com/Arize-ai/openinference) semantic conventions for native Phoenix rendering.

## What Gets Traced

- **LLM interactions** with full input/output content
- **Tool calls** (Read, Write, Edit, Bash, etc.) with inputs, outputs, and errors
- **Thinking blocks** captured as chain spans
- **Subagent lifecycle** with parent-child span relationships
- **Token usage** (prompt, completion, cache read/write)
- **Compaction events** as marker traces

## Install

From within Claude Code:

```
/plugin marketplace add exiao/claude-code-phoenix-otel
/plugin install phoenix-otel
```

Then restart Claude Code for hooks to take effect.

## Configure

**Option 1: Slash command** (recommended, from within Claude Code):

```
/phoenix:configure https://app.phoenix.arize.com/s/your-space your-api-key
```

This creates `~/.phoenix.config` which the plugin reads automatically. No environment variables needed.

**Option 2: Config file** (manually create `~/.phoenix.config`):

```
host=https://app.phoenix.arize.com/s/your-space
api_key=your-phoenix-api-key
project_name=claude-code
```

**Option 3: Environment variables:**

```bash
export PHOENIX_HOST="https://app.phoenix.arize.com/s/your-space"
export PHOENIX_API_KEY="your-phoenix-api-key"
```

Note: env vars must be in the shell that launches Claude Code, or set in `~/.claude/settings.json` under `"env"`. The config file approach avoids this issue entirely.

## Enable Tracing

Configuration alone doesn't start tracing. You must explicitly enable it per-project.

**Option 1: Slash command** (from within Claude Code):

```
/phoenix:trace-claude-code start
```

**Option 2: Manual** (from your terminal):

```bash
mkdir -p .claude
touch .claude/.phoenix-tracing-enabled
```

Run this in the project directory where you use Claude Code. Then restart Claude Code.

**Other commands:**

```
/phoenix:trace-claude-code start --debug  # Enable + debug logging
/phoenix:trace-claude-code stop           # Disable
/phoenix:trace-claude-code status         # Check state
```

For debug mode manually: `echo "debug" > .claude/.phoenix-tracing-enabled`

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PHOENIX_HOST` | — | Phoenix collector endpoint (required) |
| `PHOENIX_API_KEY` | — | Phoenix API key |
| `PHOENIX_PROJECT_NAME` | `claude-code` | Phoenix project name |
| `PHOENIX_CC_PROJECT` | — | Override project name (takes precedence) |
| `PHOENIX_CC_DEBUG` | `false` | Enable debug logging to `$TMPDIR/phoenix-debug.log` |
| `PHOENIX_CC_TRUNCATE_FIELDS` | `true` | Truncate large Edit/Write/Read content |
| `PHOENIX_CC_PARENT_TRACE_ID` | — | Attach to existing trace (for embedding in larger workflows) |
| `PHOENIX_CC_ROOT_SPAN_ID` | — | Set parent span for all Claude Code spans |

## How It Works

The plugin registers hooks for Claude Code lifecycle events:

| Hook | Action |
|------|--------|
| `UserPromptSubmit` | Create root AGENT span with prompt input |
| `PostToolUse` | Periodic flush of accumulated spans |
| `SubagentStart` | Register subagent in session state |
| `SubagentStop` | Parse subagent transcript, create nested spans |
| `PreCompact` | Flush spans, create compaction marker trace |
| `Stop` | Final flush, set trace output and end time |
| `SessionEnd` | Final flush, cleanup session state |

Spans use OpenInference semantic conventions:
- `openinference.span.kind`: AGENT, LLM, TOOL, CHAIN
- `llm.model_name`, `llm.provider`, `llm.token_count.*`
- `tool.name`, `input.value`, `output.value`
- `session.id`, `agent.name`

## Build from Source

```bash
# Build for current platform
make build

# Cross-compile all platforms
make build-all

# Run tests
make test
```

Requires Go 1.23+.

## License

Apache-2.0

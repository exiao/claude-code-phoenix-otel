---
description: Set up Phoenix credentials for tracing
argument-hint: [endpoint] [api-key]
allowed-tools:
  - Bash
  - Read
  - Write
model: haiku
---

# Configure Phoenix Credentials

Set up the `~/.phoenix.config` file so the phoenix-otel plugin can authenticate with your Phoenix instance.

Based on the user's request: **$ARGUMENTS**

## Steps

1. **If arguments were provided**, parse them as `endpoint` and `api-key` (in any order, the URL is the one starting with `http`).

2. **If no arguments**, ask the user for:
   - Phoenix endpoint (e.g., `https://app.phoenix.arize.com/s/your-space`)
   - Phoenix API key (from Settings → API Keys in Phoenix)

3. **Write `~/.phoenix.config`:**

```
host=<endpoint>
api_key=<api-key>
project_name=claude-code
```

4. **Confirm:** "Phoenix configured. Traces will be sent to `<endpoint>` under project `claude-code`. Restart Claude Code for changes to take effect."

## Examples

```
/phoenix-otel:configure https://app.phoenix.arize.com/s/myspace sk-abc123
/phoenix-otel:configure
```

## Notes

- This file is read by the plugin's hook binary on every event
- No environment variables needed when this file exists
- Project name can be overridden with `PHOENIX_CC_PROJECT` env var

# Local MCP onboarding

Ten read-only stdin/stdout tools use the same restricted Local Agent:
`list_sessions`, `get_session`, `get_session_metrics`, `get_session_timeline`,
`get_turn`, `get_findings`, `compare_sessions`, `get_analytics`,
`get_recurring_corrections`, `get_context_recommendations`. No shell, generic
filesystem, SQL or HTTP tool is exposed. Start the agent first; no SessionSift
OAuth, Google login or API key is needed for local MCP.

## Review without changes

```bash
sessionsift setup mcp
sessionsift setup mcp --client codex --preview
sessionsift setup mcp --client claude-code --preview
```

The first command preserves the original configuration printer. Client-specific
preview shows only the proposed absolute launcher and profile, not existing
config or secrets. Defaults: `CODEX_HOME/config.toml` (otherwise
`~/.codex/config.toml`), `CLAUDE_CONFIG_DIR/.claude.json` (otherwise
`~/.claude.json`). An absolute `--config` selects another profile. Existing
conflicting SessionSift entries are refused, never silently retargeted.

## Optional safe setup

Close the selected client and config editors before apply/undo. macOS atomic
replacement cannot prevent an uncooperative editor writing after the final byte
check; do not edit concurrently. Select only the client you want:

```bash
sessionsift setup mcp --client codex --apply --yes
sessionsift setup mcp --client codex --verify
sessionsift setup mcp --client claude-code --apply --yes
sessionsift setup mcp --client claude-code --verify
```

Apply makes private backups and durable ownership receipts under SessionSift
state, preserves unrelated configuration and refuses symlinks/conflicts. Repeated
apply is idempotent. Verify starts the selected launcher and discovers exactly
ten read-only tools; **no tools are called or transcripts read**. This is not an
authenticated AI inference test. Reconnect/restart the selected client to refresh
its cached inventory.

The absolute resolved executable is recorded, including Homebrew Cellar paths.
Before removing/upgrading it, undo; then preview/apply the new version. Source
builds use the same commands through `./dist/sessionsift`. `--command` selects
an existing absolute executable; repeatable `--arg` selects arguments (default
`mcp`). Shell interpreters and remote URLs are refused. The [Docker guide](docker-local.md)
uses native client configuration commands for its explicit Bash wrapper.

Undo removes only the unchanged entry owned by this setup, preserving other
servers/settings/data. Manual modification of that entry causes refusal:

```bash
sessionsift setup mcp --client codex --undo --yes
sessionsift setup mcp --client claude-code --undo --yes
```

## Manual client configuration

Codex uses this reviewable block (PATH must resolve the binary):

```toml
[mcp_servers.sessionsift]
command = "sessionsift"
args = ["mcp"]
```

Claude Code's native commands are:

```bash
claude mcp add --scope user sessionsift -- sessionsift mcp
claude mcp list
```

Claude Desktop uses `"sessionsift":{"command":"sessionsift","args":["mcp"]}`
under `mcpServers`; prefer an absolute executable for desktop clients. Managed
undo does not own manually added entries; remove those with the client's own
commands/editor. Codex and Claude have separate configuration/authentication.
Cloud ChatGPT cannot connect directly to local stdio.

## Tool use and troubleshooting

Ask to list at most five sessions, inspect one returned ID, request metrics or
compare two selected IDs. Session lists include paging metadata; one page is
not necessarily the whole library. Timeline responses are bounded. Invalid or
path-like IDs must not grant filesystem access.

Use Doctor, Status and the selected client's MCP status if discovery fails.
Confirm the executable exists and agent/profile select the intended state and
address. Stdout must contain MCP only; diagnostics belong on stderr. Docker
stdio uses no TTY. Never copy provider OAuth tokens into SessionSift config.

AI clients may send local tool output to their model provider. Read-only is not
guaranteed offline processing or secret-free output; read the [privacy model](privacy-and-threat-model.md).
Remote cloud connectors, hosted MCP and sync are deferred from this local-only
release; preserved [remote architecture](remote-architecture.md) has a separate
trust boundary. Never tunnel the local dashboard for cloud access.

Syntax references: [official Codex docs](https://developers.openai.com/codex/mcp)
and [official Claude Code docs](https://code.claude.com/docs/en/mcp).

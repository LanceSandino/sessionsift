# SessionSift MCP connection guide

SessionSift exposes seven read-only tools: list sessions, get a session, get metrics, get a bounded timeline, get a turn, get findings, and compare sessions. Every tool calls the canonical Local Agent service. There is no shell, process, filesystem, raw SQL, or arbitrary HTTP tool.

## Codex local stdio

Run `sessionsift setup mcp`, then merge the printed block into `~/.codex/config.toml`:

```toml
[mcp_servers.sessionsift]
command = "sessionsift"
args = ["mcp"]
```

Reconnect the MCP server after SessionSift tools change; clients commonly cache the tool list for the life of a connection.

## Remote HTTP clients

The endpoint is `https://YOUR-BRIDGE/remote/v1/mcp`. Send the short-lived grant supplied after owner approval:

```text
Authorization: Bearer YOUR_SCOPED_GRANT
MCP-Protocol-Version: 2025-11-25
```

Codex HTTP configuration:

```toml
[mcp_servers.sessionsift_remote]
url = "https://YOUR-BRIDGE/remote/v1/mcp"
http_headers = { Authorization = "Bearer YOUR_SCOPED_GRANT", MCP-Protocol-Version = "2025-11-25" }
```

Claude Code, Cursor, VS Code, MCP Inspector, and custom clients can use the same endpoint and headers. SessionSift serves legacy MCP `2025-11-25`, one JSON-RPC message per POST. It does not serve an SSE GET stream.

## ChatGPT limitation

ChatGPT does not accept this scoped bearer-grant setup; its MCP connector requires OAuth. SessionSift does not publish OAuth discovery metadata and does not claim ChatGPT connector compatibility. ChatGPT conversation data is supported separately through user-provided data exports.

## Paging and bounds

`list_sessions` returns `total`, `returned`, `offset`, `has_more`, and `next_cursor`. Continue until `has_more` is false before acting on “all” sessions. Timeline results are capped at 1,000 entries and report truncation.

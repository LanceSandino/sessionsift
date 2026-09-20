# SessionSift MCP connection guide

SessionSift exposes ten read-only tools: seven session inspection/comparison
tools plus aggregate analytics, recurring corrections, and deterministic
context recommendations. Every tool calls the canonical Local Agent service.
There is no shell, process, filesystem, raw SQL, or arbitrary HTTP tool.

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

## ChatGPT OAuth MCP

ChatGPT connects to `https://bridge.example.com/mcp`. This endpoint
uses current Streamable HTTP MCP through the official Go SDK and requires both:

1. a valid Cloudflare Access `Cf-Access-Jwt-Assertion`, produced through
   Cloudflare Managed OAuth; and
2. a non-expired local grant for the exact verified email, approved in
   **Remote access → ChatGPT MCP**.

Protected-resource discovery is available at
`/.well-known/oauth-protected-resource`. Keep Google as the upstream Access
identity provider and enable Managed OAuth on the Access application. See the
[deployment guide](deployment.md) for callbacks, lifetimes, environment values,
and verification.

ChatGPT conversation data remains supported separately through user-provided
data exports; an MCP connection is not an export source.

## Paging and bounds

`list_sessions` returns `total`, `returned`, `offset`, `has_more`, and `next_cursor`. Continue until `has_more` is false before acting on “all” sessions. Timeline results are capped at 1,000 entries and report truncation.

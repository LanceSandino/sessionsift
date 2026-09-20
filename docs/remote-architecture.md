# Secure remote architecture

The workstation remains the authority. A hosted bridge queues typed Session Protocol requests; the Local Agent opens the outbound connection, validates the grant locally, reads/redacts locally, signs the typed response with its Ed25519 device identity, and sends only the response back.

```text
reviewer web/MCP -> HTTPS bridge -> queued typed request
                                      ^
                                      | outbound authenticated poll
                                Local Agent -> SQLite/transcripts
                                      |
                                      +-- signed redacted response -> bridge -> reviewer
```

The bridge never receives Codex/ChatGPT provider credentials, repository access, the local SQLite database, or machine primitives. The production transport lives in `src/internal/bridge` and persists enrollment hashes, public device keys, scoped grants, and short-lived task state in its own mode-`0600` SQLite database.

Pairing codes live ten minutes and are single-use. A reviewer claims a code with requested capabilities; the owner explicitly allows or denies it. Approved grants are stored as SHA-256 hashes, last at most 24 hours, and are checked for expiry, revocation, and exact operation on every request. Disconnect revokes the underlying grant. Audit records contain operation metadata and response size, never grant tokens or session content.

The bundled bridge is a durable single-origin implementation with transactional task claims and a retry lease. A horizontally scaled deployment must move the same schema and rate-limit state to shared storage while preserving atomic claim semantics.

## OAuth resource-server path

The public deployment routes Cloudflare only to the hosted bridge on port 7780,
never to the Local Agent on port 7777. `/mcp` verifies the Access-signed JWT against the configured
issuer keys and application audience, then maps its verified email to a local,
revocable, operation-scoped device grant. Client-provided identity headers are
never trusted. The official MCP SDK serves only the canonical lowercase `/mcp`
route.

This does not weaken local authority: Cloudflare proves who authenticated, while
the device-created grant determines what that person can read, and the Local
Agent independently checks the same principal and operation. All three checks
must pass.
Automatic private database snapshots, a readiness integrity check, supervised
restart templates, and bounded HTTP timeouts cover the single-origin resilience
model. A future horizontally scaled relay still requires shared durable queues
and rate-limit state.

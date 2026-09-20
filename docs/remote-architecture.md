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

The bridge never receives Codex/ChatGPT provider credentials, repository access, SQLite access, or machine primitives. The reference transport lives in `src/internal/remote/httpbridge.go`; deployment providers supply TLS, durable multi-instance queue storage, and operational monitoring.

Pairing codes live ten minutes and are single-use. A reviewer claims a code with requested capabilities; the owner explicitly allows or denies it. Approved grants are stored as SHA-256 hashes, last at most 24 hours, and are checked for expiry, revocation, and exact operation on every request. Disconnect revokes the underlying grant. Audit records contain operation metadata and response size, never grant tokens or session content.

The bundled persistence is intentionally a single-Local-Agent implementation protected by a process mutex and atomic file replacement. A multi-worker hosted deployment must replace in-memory relay queues and rate buckets with a shared transactional store while preserving the same interfaces and atomic claim semantics.

# Secure remote architecture

The workstation remains the authority. A hosted bridge queues typed Session Protocol requests; the Local Agent opens the outbound connection, validates the grant locally, reads/redacts locally, signs the typed response with its Ed25519 device identity, and sends only the response back.

```text
supporter / reviewer / MCP -> HTTPS bridge -> queued typed request
                                              ^
                                              | outbound authenticated poll
                                        Local Agent -> SQLite/transcripts
                                              |
                                              +-- signed redacted response -> bridge -> requester
```

The bridge never receives Codex, Claude, or ChatGPT provider credentials, repository access, the local SQLite database, or machine primitives. The production transport lives in `src/internal/bridge` and persists device public keys, code hashes, scoped grants, and short-lived task state in its own mode-`0600` SQLite database.

## Connection flow (source machine generates every code)

SessionSift connects people the way remote-support tools do: the machine that owns the sessions shows a code, and the other person types it in.

1. **Register.** `sessionsift agent connect <bridge>` (or *Remote access → Connect this device*) sends the device public key with a signature proving possession of the private key. No account or code is needed. An unlinked device can only serve support codes it issues itself.
2. **Support code.** The source owner chooses *Generate support code* on one conversation (or `sessionsift support start <session-id> [role] [minutes]`). The device creates a 12-character Crockford code (60 bits, look-alike characters folded), stores only a purpose-separated SHA-256 hash, and registers that hash, the session ID, the role (view/comment/review), a ten-minute code expiry and an access length of 5 minutes to 8 hours.
3. **Connect.** The supporter signs in at `/support` (Cloudflare Access) and enters the code. The bridge finds the issuing device by hash and relays the typed code as an internal `support.redeem` task. **The device verifies the code itself**, binds the supporter's verified email, and creates a local grant for that one session. The bridge activates the connection only after the device's signed confirmation matches the session, role, supporter, and a bounded expiry, then sets a hashed, identity-bound `__Host-` cookie.
4. **Use.** Every supporter request is checked twice: by the bridge (cookie, identity, role, canonical single-session arguments) and by the Local Agent (the local support grant). Nothing else on the device is reachable.
5. **End.** The source owner sees the connected supporter live and can disconnect; the supporter can leave; access also expires. Any end revokes the local grant, so the Local Agent refuses further requests even if the bridge did not.

`support.redeem` is an internal operation: it is not grantable, not an MCP tool, and not submittable by clients. Codes are single-use; wrong codes are rate-limited per identity and IP at the bridge, and the device ends a code after five failed verifications.

**Owner linking.** To use MCP, email invitations, and the hosted session list for your own device, `sessionsift agent link` shows a link code (a separate hash domain from support codes) which you enter under *Add a device*. A device linked to another owner can never be re-linked.

## Grants

Email invitations and OAuth grants remain available for linked devices. Approved grants are stored as SHA-256 hashes or identity bindings, expire, and are checked for exact operation and, for single-session grants, canonical session scope on every request: argument objects that repeat a key under different letter case are rejected so a scope check and the executor can never read different values. Audit records contain operation metadata and response size, never grant tokens or session content; the local audit is an append-only, bounded log.

The CLI and the running Local Agent share `remote-state.json`. Each process reloads it when another process replaced it, so a revocation made from the CLI binds the running agent immediately and can never be overwritten by stale memory.

The bundled bridge is a durable single-origin implementation with transactional task claims and a retry lease. A horizontally scaled deployment must move the same schema and rate-limit state to shared storage while preserving atomic claim semantics.

## OAuth resource-server path

The public deployment routes Cloudflare only to the hosted bridge on port 7780,
never to the Local Agent on port 7777. `/mcp` verifies the Access-signed JWT against the configured
issuer keys and application audience, then maps its verified email to a local,
revocable, operation-scoped device grant. Client-provided identity headers are
never trusted. The official MCP SDK serves only the canonical lowercase `/mcp`
route. ChatGPT and Claude connect the same way.

This does not weaken local authority: Cloudflare proves who authenticated, while
the device-created grant determines what that person can read, and the Local
Agent independently checks the same principal and operation. All three checks
must pass.

## Localhost boundary

The Local Agent binds to loopback, but a web page can still reach loopback. Every request must carry a loopback `Host` (defeating DNS rebinding); every state-changing browser request must be same-origin (no `Sec-Fetch-Site: cross-site`, matching `Origin`) and owner API writes must be `application/json` (forcing a CORS preflight). The UI cannot be framed. The public tunnel hostname is accepted only in the explicit Docker tunnel mode.

Automatic private database snapshots, a readiness integrity check, supervised
restart templates, and bounded HTTP timeouts cover the single-origin resilience
model. A future horizontally scaled relay still requires shared durable queues
and rate-limit state.

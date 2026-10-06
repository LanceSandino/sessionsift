# Deployment and recovery

SessionSift remains a local-authority service even when Cloudflare publishes
the hosted connection bridge. The Local Agent stores the session database,
local grants, audit metadata, and backups on the private device; Cloudflare
Access authenticates public users and terminates TLS in front of the bridge.

## Required public routes

Publish one hostname to the SessionSift origin. Do not create a separate API
hostname.

| Route | Purpose | Access behavior |
| --- | --- | --- |
| `/` and application routes | Human UI and JSON API | Existing Google Access login |
| `/mcp` | Current Streamable HTTP MCP for ChatGPT and Claude | Cloudflare Managed OAuth plus a local identity grant |
| `/agent/*` | Signed outbound Local Agent relay | Access Bypass; origin verifies key-possession proof (registration) or a body-bound Ed25519 signature |
| `/invite/*` | Email-bound, single-session reviewer portal | Separate Google Access application; origin rechecks the exact invited email and invite session |
| `/support` and `/support/*` | Support-code entry and the one-session supporter view | Google Access application (the invitation application or its own); origin requires a device-verified code, then an identity-bound cookie |
| `/.well-known/oauth-protected-resource` | RFC 9728 resource discovery | Publicly discoverable/Managed OAuth |
| `/healthz` | Process liveness | Private or service-token policy |
| `/readyz` | SQLite integrity/readiness | Private or service-token policy |

Keep Google as the upstream identity provider. Enable Managed OAuth on the same
Access application so MCP clients receive standards-compatible discovery,
dynamic registration, authorization, refresh tokens, and the signed
`Cf-Access-Jwt-Assertion` at the origin. Then set the non-secret origin values
shown in [`.env.sessionsift.example`](../.env.sessionsift.example). A Cloudflare
Access service token bypasses Access for automation but cannot configure
Managed OAuth; configuration requires dashboard access or an API token with
Access Apps and Policies write permission.

Create a separate, more-specific self-hosted Access application for
`bridge.example.com/invite/*`. Its Allow policy should require the
Google login method; do not add a Bypass or Service Auth policy to this path.
Cloudflare assigns that path application a different audience from the main
application, so `SESSIONSIFT_ACCESS_AUD` must contain both exact audience tags,
comma-separated, **main application first, invitation application second**.
The main account/API/MCP routes require the verified main audience. Invitation
routes accept the configured allowlist and still require the verified Access
email to match the invitation exactly. Invitation-only Google login must never
confer main-account privileges. `/invite/assets/*` contains only compiled static
UI assets, no identity or session data.

The existing deployment already has Managed OAuth enabled; do not recreate its
clients or rotate credentials as part of routine upgrades. Google remains the
upstream login. The main app remains email-allowlisted for the private beta.

## Code-based support connections

The source machine generates every code. `sessionsift agent connect <bridge>`
(or **Connections → Connect this device**) registers the device's Ed25519 key
with a possession proof; no account or console code is needed, and an unlinked
device can do nothing except serve support codes it issues itself. Registration is
rate-limited per client IP.

To share one conversation, the source owner chooses **Support → Generate support code** (or
`sessionsift support start <session-id>`). The device creates a 12-character
Crockford code (60 bits), keeps only its domain-separated hash, and publishes that
hash with the session ID, role, a ten-minute code expiry and an access length of
5 minutes to 8 hours. The supporter signs in at `/support` and enters the code.
The bridge finds the device by hash, relays the typed code to the device, and the
**device itself** verifies it, binds the supporter's Access-verified email, and
creates a local single-session grant. Only then does the bridge set a hashed,
identity-bound `__Host-` cookie. A compromised bridge therefore cannot redeem a
code it has never seen. Codes are single-use; wrong codes are rate-limited per
identity and per IP, and the device ends a code after five failed verifications.
The source owner sees the connected supporter live and can end access; the
supporter can disconnect; either end revokes the local grant.

To use MCP, invitations and the hosted session list for your own device, link
it to your account: `sessionsift agent link` (or **Link to account**) shows a
code, and you enter it under **Add a device** in `/`. A device already linked to
another owner can never be re-linked. The console no longer issues enrollment
codes.

Add `/support` and `/support/*` to the path-scoped Google Access application you
use for `/invite/*` (or give it its own application and append its audience to
`SESSIONSIFT_ACCESS_AUD`). The main application's audience must stay first.
`/support/assets/*` contains only compiled static UI assets.

## Email invitations

An owner creates an invitation for one session, one normalized email, one role,
and an expiry of at most 30 days. The acceptance code is single-use and stored
only as a hash. Opening the link is a non-consuming preview; only the invited
identity's explicit, same-origin confirmation consumes the code. Successful
acceptance replaces it with a random per-invitation cookie token, also stored
only as a hash. Reopening the acceptance link in that browser returns to the
canonical invitation. Revocation invalidates both the bridge cookie and the
workstation grant immediately. Invitees never receive session
listing, comparison, analytics, filesystem, process, shell, repository, or
generic SQL access.

The bridge treats the edge as untrusted input. Every human, JSON API, and MCP
route passes through one fail-closed origin gate before any private lookup. The
gate requires a non-empty Access assertion and validates its signature, exact
issuer, application audience, expiry, subject, and normalized email. Missing,
forged, expired, or incomplete assertions receive `401` and cannot enqueue a
session request. `/agent/*` is a separate device boundary: registration requires
proof of possession of the device key, and every other request requires a
body-bound Ed25519 signature with a replay receipt.

## Tenant and encryption boundary

Every device, link code, support code, grant, invitation, relay task, and MCP target is
scoped to a workspace. The first authenticated visit creates a deterministic
personal workspace; owners may create additional workspaces and add members in
the connection control center. Membership does not authorize session reads.
The Local Agent must still issue an exact, expiring principal grant. If a
principal has multiple eligible devices, MCP refuses the request until the
principal selects an explicit workspace/device target in `/`.

Relay request and response payloads are envelope-encrypted before SQLite sees
them. Each payload has a random 256-bit data key; AES-256-GCM authenticates both
the payload and the KEK-wrapped data key. Set `SESSIONSIFT_BRIDGE_KEK_FILE` to a
private 32-byte or base64url-encoded key file. If absent, the single-origin
bridge creates it with mode `0600`. Back up the database and KEK as separate
protected artifacts and restore them as a pair. Never place the KEK, Access
tokens, or `.env.cloudflare` in Git or service logs.

Every request that reaches the origin's canonical `/mcp` endpoint emits one
structured `mcp_access` event. The event includes the HTTP method and status,
duration, response size, a sanitized Cloudflare Ray ID (or a generated request
ID), whether Access identity verification succeeded, and a truncated SHA-256
principal hash after successful verification. It deliberately excludes Access
assertions, authorization headers, raw email addresses, query parameters,
request bodies, tool arguments, and tool results. Use the request ID to
correlate origin outcomes with Cloudflare logs without copying credentials or
session content into service logs.

The callback and lifetime checklist is in
[`deploy/cloudflare/managed-oauth-checklist.md`](../deploy/cloudflare/managed-oauth-checklist.md).

## Supervision

Run the Local Agent and hosted bridge as separate supervised services. On macOS,
use the repository `compose.yaml`; the project no longer manages launchd agents.
Linux operators may use the systemd templates or the same Compose service. Copy
`.env.sessionsift.example` to ignored `.env.sessionsift.local` and set only the
host path, numeric owner identity, public URLs, Access issuer and audience IDs.
Do not put Access service-token secrets in Compose, an image, or a committed
environment file, and never load `.env.cloudflare` as a Docker environment file.

Before the first migration from another supervisor, create and verify a backup:

```bash
./dist/sessionsift backup
./dist/sessionsift doctor
docker compose --env-file .env.sessionsift.local config --quiet
docker compose --env-file .env.sessionsift.local build
```

Stop the old supervisor only after the image builds. Then start and verify the
Compose services:

```bash
docker compose --env-file .env.sessionsift.local up -d
docker compose --env-file .env.sessionsift.local ps
curl --fail http://127.0.0.1:7777/readyz
curl --fail http://127.0.0.1:7780/readyz
```

The agent bind-mounts existing `~/.sessionsift` read/write and only the Codex
`sessions`, `archived_sessions`, and thread-index SQLite files read-only. The
bridge bind-mounts existing `~/.sessionsift-bridge` read/write so its database
and exact KEK remain paired. Both ports publish on `127.0.0.1` only, services
use separate Docker networks, the filesystem is read-only apart from those
state mounts and private tmpfs, Linux capabilities are dropped, and restart is
`unless-stopped`. Each isolated network has an explicit gateway; the process
accepts only that exact private forwarding peer (plus loopback), so Docker port
forwarding works without trusting sibling container addresses or the LAN.

SessionSift handles `SIGTERM`, uses bounded graceful shutdown, and applies
bounded header/read/write/idle timeouts. The templates restart failed processes
with a short backoff. Cloudflare Tunnel should be supervised separately and
route only to `http://host.docker.internal:7780`. Port 7777 remains loopback-only.

## Backups and recovery

The agent creates a consistent SQLite snapshot at startup and every 24 hours,
retaining the newest seven files under `~/.sessionsift/backups`. Run an extra
snapshot before maintenance:

```bash
sessionsift backup
sessionsift doctor
```

Each snapshot is mode `0600`; the backup directory is mode `0700`. To recover,
stop the agent, preserve the failed database, copy one chosen backup to
`~/.sessionsift/sessionsift.db`, and start the agent. Recovery is deliberately a
manual stop-and-copy operation so the application never overwrites the live
database automatically.

The hosted bridge database is separate from these Local Agent snapshots. For
the bridge, stop task intake, copy `bridge.db` with SQLite's backup mechanism,
and copy the configured KEK as a separate secret. Queued task content is
transient; drain it before planned KEK replacement because online key rotation
is not yet supported.

## Deployment verification

1. `npm test` and `npm run build` pass for the release commit.
2. `sessionsift backup` succeeds before replacing the binary.
3. Recreate both Compose services. Confirm the Local Agent `/readyz` returns
   `200` on loopback and the bridge `/readyz` returns `200` on port 7780.
   Source reconciliation runs in the background after the listener opens, so a
   large active transcript does not hold the port closed during restart.
4. Confirm the connection console, `/mcp`, and protected-resource metadata through Cloudflare.
   Confirm the bridge log contains a matching `mcp_access` event with no request
   payload or credential material.
5. Confirm an unapproved OAuth identity is denied and an approved identity can
   list only the granted read-only tools.
6. On a test device run `sessionsift support start <session-id> view 15`, enter
   the code at `/support` with a controlled identity, confirm only that session
   opens, a second entry of the same code is refused, and **Disconnect
   supporter** (or `sessionsift support end <id>`) immediately denies the
   supporter's next request.
7. Create a short-lived invitation to a controlled email. Confirm the link is
   single-use, a different Access identity is denied, the portal exposes only
   the selected session and role operations, and revocation immediately denies
   the accepted cookie. Remove the test invitation afterward.
8. If verification fails, restore the prior binary; the database schema is
   forward-compatible in this release, and the pre-deploy backup remains the
   data rollback point.

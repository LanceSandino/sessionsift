# Deployment and recovery

SessionSift remains a local-authority service even when Cloudflare publishes
it. The origin stores the database, remote grants, audit metadata, and backups;
Cloudflare Access authenticates public users and terminates TLS.

## Required public routes

Publish one hostname to the SessionSift origin. Do not create a separate API
hostname.

| Route | Purpose | Access behavior |
| --- | --- | --- |
| `/` and application routes | Human UI and JSON API | Existing Google Access login |
| `/mcp` | Current Streamable HTTP MCP for ChatGPT | Cloudflare Managed OAuth plus a local identity grant |
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

The callback and lifetime checklist is in
[`deploy/cloudflare/managed-oauth-checklist.md`](../deploy/cloudflare/managed-oauth-checklist.md).

## Supervision

Use the launchd template on macOS or the systemd user-service template on
Linux. Replace paths and load environment values through the service manager;
do not put Access service-token secrets in a unit committed to Git.

SessionSift handles `SIGTERM`, allows three seconds for HTTP shutdown, and uses
bounded header/read/write/idle timeouts. The templates restart failed processes
with a short backoff. Cloudflare Tunnel should be supervised separately and
route only to port 7777.

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

## Deployment verification

1. `npm test` and `npm run build` pass for the release commit.
2. `sessionsift backup` succeeds before replacing the binary.
3. Restart the supervised agent and confirm `/readyz` returns `200` locally.
   Source reconciliation runs in the background after the listener opens, so a
   large active transcript does not hold the port closed during restart.
4. Confirm the UI, `/mcp`, and protected-resource metadata through Cloudflare.
5. Confirm an unapproved OAuth identity is denied and an approved identity can
   list only the granted read-only tools.
6. If verification fails, restore the prior binary; the database schema is
   forward-compatible in this release, and the pre-deploy backup remains the
   data rollback point.

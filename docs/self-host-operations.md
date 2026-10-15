# Self-hosted bridge and sync operations

Optional community setup only: no production certification, universal installer,
arbitrary-provider gallery or support commitment. Operators own TLS, OAuth,
backups, compliance and cost. The included verifier supports Cloudflare Access,
not arbitrary Google/OIDC tokens. Core local use needs none of this. The
owner-operated service uses existing infrastructure; these instructions do not
authorize creating or changing its resources.

The bridge uses the same binary and protocol as the Local Agent but has an independent lifecycle. It exposes only port 7780; the Local Agent remains outbound-only and host port 7777 remains loopback-bound.

## Start

Copy `.env.sessionsift.example` to an ignored local file, set the harmless public coordinates and Cloudflare Access issuer/audience, and keep `SESSIONSIFT_SYNC_ENABLED=false` until encrypted sync is intentionally offered.

Use your actual host path and numeric UID/GID (`id -u`, `id -g`), not the example
defaults. Create private bridge state with owner-only permissions. Keep local
`SESSIONSIFT_AGENT_PUBLIC_URL=http://127.0.0.1:7777`; never publish the agent or
mount provider credentials. Pin a verified image digest or reviewed source
version before starting. SHA256 is not publisher identity; Mac artifacts are
unsigned/unnotarized.

```bash
docker compose -f compose.remote.yaml --env-file .env.sessionsift.local up -d --build
curl --fail http://127.0.0.1:7780/readyz
```

To run the local agent and bridge together, add `-f compose.yaml`; the bridge-only command above does not start an agent or mount home transcript paths. Put TLS and Cloudflare Access in front of 7780. `/agent/*` may bypass browser login only because every device request has independent Ed25519 timestamp, nonce, method, path/query, and body authentication.

Keep host bindings loopback-only. Configure HTTPS, exact origin issuer/JWKS and
ordered audiences, Managed OAuth for `/mcp`, and local grants before exposure.
Browser login is not an OAuth MCP test. Wrong identity, invite audience on MCP,
missing signatures and stale/revoked grants must fail closed. See
[deployment boundaries](deployment.md); no real provider/client acceptance is
claimed by this sample.

## Enable optional sync

Set `SESSIONSIFT_SYNC_ENABLED=true`. Defaults are 2 MiB per object, 512 MiB current ciphertext per workspace, 200 changes per page, 600 signed sync requests per device per minute, and a 256 MiB free-disk reserve. Override them with the documented `SESSIONSIFT_SYNC_*` variables after capacity testing. A full quota, fair-use limit, or low-disk reserve rejects the remote write without deleting the device's durable local outbox or blocking local imports. Turning the flag off returns 404 on sync routes without changing remote MCP.

## Backup and restore

The mounted `.sessionsift-bridge` directory is the recovery unit. Preserve the SQLite database and `bridge.db.key` together with owner-only permissions; the key protects transient MCP queue records. Sync objects inside the database remain client-encrypted and are not decryptable with `bridge.db.key`. Built-in maintenance creates transaction-consistent retained snapshots. Copy snapshots off-host only through an operator-controlled encrypted channel.

On a disposable host, restore the paired database/key into the private bridge-state directory, start the exact pinned image, wait for `/readyz`, and verify a synthetic linked device, task completion, sync usage, and ciphertext restore before changing production. A missing or mismatched bridge key must stop startup. Never invent a new key for an existing database.

## Upgrade and rollback

Bridge KEK changes require the [offline rotation and recovery procedure](bridge-kek-rotation.md), not replacement of a live key file. Keep every saved database with its matching key. User sync keys are separate.

1. Stop new invitations, record the current image digest, and make a verified paired backup.
2. Start the candidate against a copy and run readiness, signed-device, MCP, and two-device sync smoke tests.
3. Upgrade the real service. If readiness or integrity fails, stop it and restore the prior image plus paired snapshot.

Database migrations are forward-only. Rollback after a migrated write uses the pre-upgrade backup, not a newer database with an older binary.

## Monitoring and deletion

Monitor readiness, disk space, database integrity, task latency, authentication denials, quota failures, and backup age without logging content. Cloud deletion immediately purges current sync objects and their change history; document how long opaque ciphertext can remain in off-host backups. Public invitations must not expand until staging capacity, alerting, recovery, and an on-call owner have been exercised.

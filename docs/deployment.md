# Deployment boundary

The free local app is independent of any bridge. Keep its port 7777 on loopback;
never route a public tunnel to it. The same binary's bridge subcommand is the
optional origin on port 7780. The Local Agent polls that origin outbound.

See [basic self-host instructions](self-host-operations.md) for the secure
reference Compose example. It is not a universal installer or certification of
arbitrary providers. The included Cloudflare Access JWT verifier is not an OAuth
authorization server and does not treat Google ID tokens as Access assertions.

## Authentication and routes

Browser Google sign-in protects human pages, but standards-compatible MCP clients
need Managed OAuth on the correct /mcp Access application and actual client
acceptance. No live setting can be inferred from this source or synthetic tests.
Do not recreate the owner's resources as a setup step.

| Route | Required boundary |
| --- | --- |
| Human console/API | Validated Access JWT plus workspace/device authorization |
| /mcp | Managed OAuth, origin JWT verification and explicit local grant |
| /agent/* | Independent key-possession/device signatures and replay rejection, even with narrow Access Bypass |
| /invite/*, /support* | Identity-bound session authorization; no broad bypass |
| /.well-known/oauth-protected-resource | Correct public resource discovery, not private data |
| /healthz, /readyz | Private operator probes, not public diagnostics |

Configure an exact HTTPS issuer and exact ordered audience allowlist: the **main
application audience is first**, invitation/support audiences follow. Origin
checks signature/JWKS, issuer, audience, expiry, subject and email; invitation
audiences cannot confer main-account/MCP privileges. Workspace membership
does not grant session reads. Multiple eligible devices require an explicit
target. The agent independently enforces each operation and single-session scope.

Consult Cloudflare's maintained [Managed OAuth documentation](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/managed-oauth/)
and [Tunnel routing documentation](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/routing-to-tunnel/).
Operators must verify supported client versions, callbacks, discovery, consent,
expiry/refresh, revocation and denial. Default browser redirects are insufficient.
No turnkey self-managed OAuth server is offered.

## Recovery and upgrades

The bridge database and server-held KEK are one protected recovery pair. Preserve
matching snapshots with owner-only permissions and encrypted off-host retention.
User sync recovery keys are separate. Never generate a replacement KEK for an
existing database. Use [stopped-writer KEK rotation](bridge-kek-rotation.md) only
with tested paired backups and explicit commit/rollback recovery.

Pin the exact tested binary/image digest. Test upgrades on disposable copies
before deliberate cutover; keep the prior image and pre-upgrade DB/key pair.
Migrations are forward-only: do not run an older binary against a newer written
database. Rollback restores its matching pre-upgrade pair.
Local-agent snapshots use sessionsift backup independently.

Remote MCP requests/responses can be plaintext in the bridge, despite authenticated
queue encryption at rest. Stored sync snapshots are encrypted on clients with
user-held keys. Monitoring must contain bounded metadata only, never tokens, raw
email, transcripts or tool payloads.

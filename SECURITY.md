# Security policy

SessionSift processes private conversation transcripts and should be treated as
sensitive local software. Never include real user content or credentials in a report.

## Supported version

Only the newest published release (currently 2.1.1) receives security fixes.
See `docs/release-candidate.md` for what that release does and does not include.

## Reporting a vulnerability

Report privately to **security@dedi-servers.com**, the project owner's existing
controlled security contact. Do not open a public issue containing transcript
data, credentials, private paths, network coordinates, or exploit details.
Include the affected version, a minimal synthetic reproduction, impact, and any
suggested mitigation. Do not send real conversations, credentials or personal
machine paths, even by email. This contact is owner-designated; no independently
verified email-delivery test or response-time guarantee is claimed.

## Deployment boundary

- Local use needs only the Local Agent on loopback. Do not expose port 7777 to
  a LAN or public tunnel.
- Remote support, invitations, and hosted MCP are optional and require the
  separate `compose.remote.yaml` bridge, TLS, identity-aware access, and local
  grants.
- Never commit `.env` files, Access tokens, bridge encryption keys, transcript
  exports, SQLite state, scanner reports, or real incident artifacts.
- Before distribution, run independent canary-validated secret detectors on
  source, compiled assets, package metadata and expanded image layers. A private
  ops directory does not prevent leaking its values into builds.
- Optional cloud sync is client-encrypted with a separately held recovery key.
  This does not make remote MCP end-to-end encrypted. See
  `docs/privacy-and-threat-model.md` and `docs/cloud-sync.md`.

The threat model and trust boundaries are described in
[`docs/remote-architecture.md`](docs/remote-architecture.md).

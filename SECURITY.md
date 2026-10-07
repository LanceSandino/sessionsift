# Security policy

SessionSift processes private conversation transcripts and should be treated as
sensitive local software. The repository is currently a private preview and is
not accepting public security reports yet.

## Supported version

Only the newest release on the default branch receives security fixes during
the private preview. No hosted service or public bridge is offered by this
repository.

## Reporting a vulnerability

Do not open a public issue containing transcript data, credentials, private
paths, network coordinates, or an exploit. Until a private reporting address is
published, contact the repository owner through the private collaboration
channel where you received access. Include the affected version, a minimal
reproduction with synthetic data, impact, and any suggested mitigation.

## Deployment boundary

- Local use needs only the Local Agent on loopback. Do not expose port 7777 to
  a LAN or public tunnel.
- Remote support, invitations, and hosted MCP are optional and require the
  separate `compose.remote.yaml` bridge, TLS, identity-aware access, and local
  grants.
- Never commit `.env` files, Access tokens, bridge encryption keys, transcript
  exports, SQLite state, scanner reports, or real incident artifacts.
- Run `scripts/scan-secrets.sh` with the project-pinned, canary-validated
  Gitleaks version before any publication candidate is reviewed.

The threat model and trust boundaries are described in
[`docs/remote-architecture.md`](docs/remote-architecture.md).

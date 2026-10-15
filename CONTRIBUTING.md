# Contributing

SessionSift is source-available under the repository license. Before opening a change, read `SECURITY.md`, the privacy/threat model, and the relevant tests.

Never attach real transcripts, exports, SQLite databases, logs, screenshots, credentials, private URLs, home paths, device identities, recovery secrets, or bridge keys to an issue or pull request. Reproduce with synthetic fixtures and canary values. Security defects belong in the private reporting route, not a public issue.

Keep changes narrow, preserve local-only operation and the restricted MCP boundary, add a regression test for changed behavior, and run `npm test`, `go test -race ./...`, `go vet ./...`, and `npm run build`. Changes to sync cryptography, authentication, redaction, release export, or deletion require explicit threat-model review.

Do not claim a provider, OS, architecture, installer, E2EE property, or deployment has been validated unless the matching acceptance evidence was observed on that environment.

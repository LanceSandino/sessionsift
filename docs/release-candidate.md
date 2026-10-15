# SessionSift 2.1.0: what this release includes

SessionSift 2.1.0 is a free, local-only release. Everything runs on your own
machine: transcript import, the dashboard on `127.0.0.1:7777`, backups and the
ten read-only MCP tools over stdio. No account, Google login, bridge or cloud
service is needed or offered.

## Supported platforms

- macOS Intel (AMD64)
- Linux AMD64

Apple Silicon and Linux ARM64 builds are not offered yet. They will be added
once they pass native installation testing; cross-compiled builds are not
treated as supported.

## Ways to install

1. Homebrew, from `LanceSandino/homebrew-tap`
2. Docker Compose, building the image from this source (`compose.local.yaml`)
3. Building from source with Go and Node

See the [README](../README.md) for the commands.

## Trust and verification

- macOS binaries are **unsigned and unnotarized**. Gatekeeper may refuse to
  launch them; do not disable Gatekeeper globally.
- Each release publishes SHA-256 checksums and an SPDX software inventory.
  Checksums verify that a download is intact. They do not prove who published
  it.
- Third-party license texts are included in `NOTICE`.

## Not included in 2.1.0

The source also contains an optional hosted bridge, remote MCP and
client-encrypted sync. These are not offered or supported as a service in this
release, and local use never depends on them.

## Reporting security issues

See [`SECURITY.md`](../SECURITY.md). Never include real transcripts, credentials
or personal file paths in a report.

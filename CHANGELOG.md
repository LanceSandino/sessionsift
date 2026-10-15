# Changelog

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Release notes describe user-visible changes, newest first. The in-app changelog
shows the same history. 2.1.0 is the first public release; earlier versions were
developed privately and are tagged in this repository's history.

## [2.1.0] — 2026-10-14

First public release. SessionSift runs entirely on your machine; no account,
login or cloud service is needed.

### Added

- Three supported ways to install: the LanceSandino Homebrew tap with a user-level `brew services` service, a local-only Docker Compose file, and building from source.
- `sessionsift web` checks that the local agent is ready and opens the dashboard.
- `sessionsift setup mcp` previews, applies, verifies and undoes the local MCP configuration for Codex and Claude Code.
- Release archives for macOS Intel and Linux amd64 with SHA-256 checksums and an SPDX software inventory.

### Improved

- The dashboard is local-first by default; sharing and remote controls appear only after you opt in.
- Docker Compose keeps the dashboard on `127.0.0.1`, mounts transcript folders read-only and needs no cloud services.

### Fixed

- MCP tool output schemas now have an object root, so strict clients such as Claude Desktop list and call all ten tools.
- Session-scope checks treat Unicode look-alike JSON keys (such as a long `s`) as the field the decoder reads, so a scoped request cannot select a different session.
- The optional hosted-bridge client no longer follows HTTP redirects, so signed requests cannot be forwarded to another origin.
- Local backups are written in a private staging folder and only appear once they are owner-only.
- Codex tool-target paths are redacted before they are shortened, so home-folder account names are not kept.
- Release binaries use the patched Go standard library version required by the security scan.
- Updated a web build dependency to address a published source-map parsing vulnerability.

### Notes

- Apple Silicon and Linux ARM64 builds are not offered yet.
- macOS binaries are unsigned and unnotarized; checksums verify downloads, not publisher identity.
- The source also contains an optional hosted bridge, remote MCP and client-encrypted sync. They are experimental and not offered or supported as a service in this release.

## [2.0.1] — 2026-10-07

### Improved

- Background collection coalesces transcript changes and waits between scans, including when no browser is open.
- Unchanged transcripts use one batched import-manifest read instead of a database query for every file.

### Fixed

- Codex heartbeat timestamps no longer trigger full transcript imports; renamed titles and execution metadata refresh without reparsing conversation history.

## [2.0.0] — 2026-10-05

### Added

- Docker Compose services for the Local Agent and hosted bridge with persistent existing state, bounded health checks, and automatic restart.
- A paginated in-app changelog backed by the same semantic release catalog as the CLI version.

### Changed

- **Breaking:** macOS service operations now use Docker Compose instead of project-managed launchd agents.
- Protocol and file reads observed in Codex tool calls are visible as provenance badges.
- Bounded full-session reads support long-running conversations without removing response ceilings.

## [1.8.0] — 2026-09-25

### Added

- A private recommendation, experiment, measurement, and weekly-report loop.
- Session ownership filters and evidence-qualified protocol, Blueprint, memory, and file-read callouts.

### Fixed

- Oversized transcript records recover without hiding later records.
- Analytics inventory and diagnostics distinguish deliberate exclusions from parser loss.

## [1.7.0] — 2026-09-24

### Changed

- Rebuilt local and shared-session interfaces around Conversation, Timeline, Tools, Review, and Evidence views.
- Mobile discovery, timeline containment, card sizing, and latest-navigation behavior now follow their actual containers.

### Fixed

- Codex turn identities are session-scoped and per-session import failures no longer stop later sessions.

## [1.6.0] — 2026-09-23

### Added

- Claude Code transcript discovery and Claude.ai export import under the shared redaction model.
- Device-generated single-session support codes with view, comment, or review roles.

### Security

- Added keyed path hashes, bounded hook metadata, fail-closed local state reloads, and explicit owner authorization controls.

## [1.5.0] — 2026-09-20

### Added

- Hosted bridge, OAuth-backed MCP, email-bound invitations, pairing, grants, annotations, and review decisions.
- Envelope-encrypted transient relay tasks, metadata-only audit, paired bridge backups, and recovery checks.

### Security

- Access identity never replaces Local Agent capability grants; public routes fail closed without origin verification.

## [1.4.0] — 2026-09-20

### Added

- Privacy-safe aggregate analytics, source/model/tool breakdowns, and deterministic recommendations.
- SQLite readiness, private retained backups, bounded HTTP timeouts, and supervised restart templates.

### Fixed

- Interactive reads remain responsive during ingestion and session lists page beyond the first 25 records.

## [1.3.0] — 2026-09-20

### Changed

- Introduced viewport-safe desktop, tablet, phone, focus, timeline, and inspector layouts.
- Timeline checkpoints summarize the work journey while exact redacted calls stay available in Tools.

### Fixed

- Safari overlap, black refresh, nullable collection, and long-timeline navigation failures.

## [1.2.0] — 2026-09-20

### Added

- A canonical Local Agent and allowlisted Session Protocol shared by CLI, UI, local MCP, and remote adapters.
- Modern stdio MCP tools with bounded redacted responses and no generic shell, filesystem, process, or SQL surface.

## [1.1.0] — 2026-09-20

### Added

- Explicit ChatGPT export import with incremental replacement reconciliation.
- Exact built-in, MCP, and app tool reporting, including nested orchestration calls.
- Renamed titles, execution context, deep links, lifecycle hooks, and OpenTelemetry capture.

## [1.0.0] — 2026-09-20

### Added

- Local-first Codex session analysis with redacted SQLite storage, live ingestion, deterministic findings, diagnostics, backups, and the initial inspection interface.

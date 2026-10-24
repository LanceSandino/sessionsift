# Changelog

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Release notes describe user-visible changes, newest first. The in-app changelog
shows the same history. 2.1.0 is the first public release; earlier versions were
developed privately and are tagged in this repository's history.

## [2.1.1] — 2026-10-24

Security release. Updates the Go toolchain, Go modules, web build and test
dependencies and the Docker build images to their patched versions, and hardens
two input checks found by code scanning.

### Security

- Release binaries are built with Go 1.27.3, which distrusts compromised intermediate certificates in `crypto/x509` (GO-2026-6809, CVE-2026-111943).
- Updated Vite to 8.3.5 (CVE-2026-112942, CVE-2026-112943, CVE-2026-112944, CVE-2026-112945, CVE-2026-112946, CVE-2026-112947). Vite builds the dashboard; its development server is not part of SessionSift.
- Updated Vitest and its coverage plugin to 5.0.5 (CVE-2026-112949, CVE-2026-112950) and jsdom to 30.1.3 with undici 8.12.3 (CVE-2026-109841, CVE-2026-109869, CVE-2026-109924). These are test-only tools.
- Updated mutation-testing dependencies, including fixes in zod (CVE-2023-54404) and fast-uri (CVE-2026-110095, CVE-2026-110180, CVE-2026-110306, CVE-2026-110725).
- The Docker build and the release builders use Node.js 26.12.1, the Node.js security release of 2026-10-21 (including CVE-2026-111497), and Go 1.27.3. Docker base images are pinned by digest, and the runtime image moves to distroless Debian 13.

### Fixed

- The optional hosted bridge caps a sync page at 1,000 changes even when `SESSIONSIFT_SYNC_MAX_PAGE_SIZE` is set higher. An oversized setting made a page request fail with a runtime panic and could size an unbounded query.
- Codex transcript exit codes and rate-limit windows outside the 32-bit range are ignored instead of wrapping on 32-bit builds.

### Improved

- Updated React to 19.3.1, the Go MCP SDK to 1.8.0, SQLite (modernc) to 1.61.0, gRPC to 1.84.0 and the other Go modules to their latest releases.
- CI scans npm (`npm audit`, moderate and above) and Go (`govulncheck`) dependencies on every change and weekly, and Dependabot proposes grouped weekly updates.
- `NOTICE` is regenerated from the locked dependencies, and the release workflow checks that it is current.

### Notes

- No data migration or configuration change is needed. As with every version change, the first start keeps a pre-migration backup.
- Apple Silicon and Linux ARM64 builds are not offered yet.
- macOS binaries are unsigned and unnotarized; checksums verify downloads, not publisher identity.

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

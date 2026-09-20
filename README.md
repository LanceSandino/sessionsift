# SessionSift

SessionSift is a local-first conversation profiler for Codex and ChatGPT. It
imports live and archived Codex transcripts plus user-provided ChatGPT data
exports, normalizes activity into SQLite, and shows where tokens, context,
tools, retries, and corrections accumulated. Its findings link back to observed
evidence; it does not assign a productivity score.

The trusted Go process is the Local Agent product boundary. React, CLI, and
future MCP clients consume the same allowlisted, versioned Session Protocol;
provider parsers and SQLite are not client-facing interfaces.

## Quick start

Requirements: Go 1.27+ and Node.js 22+.

```bash
npm install
npm run build
./dist/sessionsift import
./dist/sessionsift start
```

Open <http://127.0.0.1:7777>. SessionSift imports both
`$CODEX_HOME/sessions` and `$CODEX_HOME/archived_sessions` (normally under
`~/.codex`). Re-running `import` processes only changed transcript files.
Starting the app also watches those directories and refreshes changed sessions
every three seconds.

The main commands are:

```text
sessionsift start
sessionsift stop
sessionsift status
sessionsift import
sessionsift import-chatgpt <export.json-or-zip>
sessionsift sessions
sessionsift inspect <session-id>
sessionsift compare <session-a> <session-b>
sessionsift doctor
sessionsift setup enable
sessionsift setup hooks
sessionsift setup otel
sessionsift setup chatgpt
sessionsift setup undo
```

## ChatGPT conversations

Request a ChatGPT data export from **Settings → Data Controls**, then import the
downloaded ZIP directly:

```bash
sessionsift import-chatgpt ~/Downloads/chatgpt-export.zip
```

`conversations.json` and numbered `conversations*.json` files are also accepted.
SessionSift remembers the selected private local path and checks it for changes
every five seconds while running. Replacing that file with a newer export updates
the imported conversations. `SESSIONSIFT_CHATGPT_EXPORT` overrides the remembered
path. SessionSift does not scrape a ChatGPT account or read browser credentials.

ChatGPT exports do not always include token counts or every internal tool detail;
unavailable measurements remain zero rather than being estimated.

## Tools and MCP

The inspector groups every observed tool name and reports executions, failures,
transport, and MCP server. Built-in tools, MCP calls, and app/connector calls are
counted separately. Turn detail retains bounded redacted arguments and result
previews when the source records provide them.

## Live events

`sessionsift setup enable` safely merges SessionSift lifecycle hooks into
`~/.codex/hooks.json` and local OTLP log and metrics exporters into
`~/.codex/config.toml`. It is idempotent and refuses to replace symlinks or an
existing, different `[otel]` section. `sessionsift setup hooks` and
`sessionsift setup otel` print the same settings for manual review. The hook
command has a 40 ms localhost budget and falls back to private files under
`~/.sessionsift/raw/events`; those files are replayed on the next start and
removed after successful persistence.

Codex requires unmanaged hooks to be reviewed and trusted after installation;
open `/hooks` in Codex to complete that approval. The OTLP configuration keeps
prompt logging disabled. SessionSift validates standard binary and JSON
OTLP/HTTP log and metrics batches, retains only redacted attributable log
metadata, and uses attributable token measurements to refine turn metrics.
Transcript imports remain the authoritative session history and hooks provide
the lowest-latency activity.

## Titles, context, and navigation

For Codex sessions, the user-assigned thread name from the read-only local Codex
state index takes precedence over the first prompt. The inspector shows the
observed start time, client/surface, thread source, approval mode, sandbox,
network policy, and a clearly labeled derived local/remote classification when
the source supports it. Later user messages observed in the same turn appear as
`STEER / QUEUE`; the available local records do not reliably distinguish those
two delivery modes, so SessionSift does not guess.

Conversation, Compare, and Diagnostics screens use History API navigation and
deep links such as `/sessions/<id>`, `/compare`, and `/diagnostics` without page
reloads. Browser back and forward navigation is supported.

Codex guardian/auto-review transcripts are imported for completeness but are
classified as internal reviews and hidden from normal session lists. Enable the
**Internal reviews** filter when auditing those records.

## Privacy model

- The server binds only to `127.0.0.1:7777`.
- Product state defaults to `~/.sessionsift` and can be moved with
  `SESSIONSIFT_HOME`.
- Repository roots are hashed, home paths are normalized, and common API keys,
  bearer tokens, database URLs, private keys, and credentials are redacted.
- Tool payload previews and timelines are bounded; malformed or unknown Codex
  and ChatGPT records become safe metadata and parser warnings instead of
  breaking the import.
- SessionSift does not read `~/.codex/auth.json`, upload data, or create cloud
  accounts.

Codex transcript and ChatGPT export records are treated as changing input
formats. Each adapter is versioned, preserves warnings, and maps raw records
into a provider-neutral domain model before analysis.

## Development

```bash
npm install
npm test
npm run build
./dist/sessionsift version
```

The test command enforces coverage floors and builds the embedded frontend.

## Structure

- `src/cmd/sessionsift`: CLI entrypoint
- `src/internal`: collector, parser, storage, analyzers, and HTTP API
- `src/web`: React and TypeScript source
- `src/webui`: generated assets embedded in the Go binary

## API

The localhost API exposes session lists, session details, turns, events,
findings, metrics, and comparisons below `/api/v1`. Session lists exclude
internal reviews by default; pass `include_internal=true` to include them. Hook
ingestion is available at `/internal/hooks/codex`, OTLP logs at `/v1/logs`, OTLP
metrics at `/v1/metrics`, and `/healthz` reports collector health.

`POST /api/v1/protocol` exposes the localhost-only typed Session Protocol. Its
v1 registry contains session list/detail/metrics/timeline/findings/comparison,
turn detail, and connection-status operations. Every operation defines its
capability, audit category, response-size limit, redaction policy, timeout, and
pagination behavior. Generic shell, process, filesystem, command, and SQL
operations are intentionally absent and fail closed.

## License

SessionSift is source-available under the
[PolyForm Noncommercial License 1.0.0](LICENSE). Noncommercial use is allowed;
commercial use is not licensed.

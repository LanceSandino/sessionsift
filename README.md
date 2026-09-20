# SessionSift

SessionSift is a local-first Codex session profiler. It imports current and
archived Codex transcripts, normalizes session activity into SQLite, and shows
where tokens, context, tools, retries, and corrections accumulated. Its findings
link back to observed evidence; it does not assign a productivity score.

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
sessionsift sessions
sessionsift inspect <session-id>
sessionsift compare <session-a> <session-b>
sessionsift doctor
sessionsift setup hooks
sessionsift setup otel
sessionsift setup undo
```

## Live events

`sessionsift setup hooks` prints a reviewed starting point for
`~/.codex/hooks.json`. The hook command has a 40 ms localhost budget and falls
back to private files under `~/.sessionsift/raw/events`; those files are replayed
on the next start and removed after successful persistence.

Codex requires unmanaged hooks to be reviewed and trusted. SessionSift prints
configuration but never edits Codex configuration automatically. The optional
`sessionsift setup otel` command prints an OTLP/HTTP configuration with prompt
logging disabled. The endpoint decodes standard binary and JSON OTLP/HTTP log
batches, retains only redacted structured event metadata, and uses attributable
token measurements to refine turn metrics. Transcript imports remain the
authoritative session history and hooks provide the lowest-latency activity.

Codex guardian/auto-review transcripts are imported for completeness but are
classified as internal reviews and hidden from normal session lists. Enable the
**Internal reviews** filter when auditing those records.

## Privacy model

- The server binds only to `127.0.0.1:7777`.
- Product state defaults to `~/.sessionsift` and can be moved with
  `SESSIONSIFT_HOME`.
- Repository roots are hashed, home paths are normalized, and common API keys,
  bearer tokens, database URLs, private keys, and credentials are redacted.
- Tool payload previews and timelines are bounded; malformed or unknown
  transcript records become safe metadata and parser warnings instead of
  breaking the import.
- SessionSift does not read `~/.codex/auth.json`, upload data, or create cloud
  accounts.

Codex transcript records are treated as a changing input format. The parser is
versioned, preserves warnings, and maps raw records into a provider-neutral
domain model before analysis.

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
ingestion is available at `/internal/hooks/codex`, OTLP logs at `/v1/logs`, and
`/healthz` reports collector health.

## License

SessionSift is source-available under the
[PolyForm Noncommercial License 1.0.0](LICENSE). Noncommercial use is allowed;
commercial use is not licensed.

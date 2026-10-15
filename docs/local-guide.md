# Local SessionSift user guide

Install with [Homebrew, Docker or source](../README.md). Public Homebrew delivery
is not yet published. Core features need no account or cloud. Examples use
`sessionsift`; for source use `./dist/sessionsift`, or for Docker prepend the
documented Compose `exec -T agent /app/sessionsift` command.

## Start, open and stop

```bash
sessionsift version
sessionsift doctor
sessionsift start
```

`start` stays in the foreground. In another terminal:

```bash
sessionsift status
sessionsift web
sessionsift web --no-browser
```

The dashboard normally uses <http://127.0.0.1:7777>. `web` refuses remote hosts,
redirects and unready services. If that port is occupied, stop your own duplicate
agent or set `SESSIONSIFT_ADDR=127.0.0.1:7787` consistently for every command;
do not stop another application. For Homebrew use `brew services start sessionsift`
instead of foreground `start`, and `brew services stop sessionsift` to stop it.
`sessionsift stop` under a restarting supervisor may simply restart it.
For a foreground agent use Ctrl-C or:

```bash
sessionsift stop
```

## Import and browse

```bash
sessionsift import
sessionsift sessions
```

Import incrementally reconciles existing Codex sessions/archives and Claude Code
projects; the agent also watches them. Missing optional directories are neutral
in Doctor; an empty new library is not a failure. No accounts are scraped and
provider authentication files are not imported. Docker needs explicit read-only
source selection first.

Select an export you downloaded yourself. Replace the uppercase filename:

```bash
sessionsift import-chatgpt EXPORT.json
sessionsift import-claude EXPORT.json
```

JSON and ZIP exports are supported. Protect exports like transcripts; redaction
is not a guarantee that every secret is removed. In the dashboard choose a
session, then Conversation for messages, Timeline for prompt-centered activity,
Tools for recorded calls, and Review for local notes. Analytics shows aggregate
observations; Compare selects two sessions without inferring a winner. Diagnostics
shows source availability and parser warnings.

`sessions` prints tab-separated IDs and summaries. Replace uppercase IDs with
exact IDs from that list. Inspection/comparison output is JSON:

```bash
sessionsift inspect SESSION_ID
sessionsift compare FIRST_SESSION_ID SECOND_SESSION_ID
sessionsift backup
```

Backups are private local snapshots in the state directory, not public attachments.
State defaults to `~/.sessionsift`; `SESSIONSIFT_HOME` selects another library,
not automatic migration or deletion.

## Local AI tools

[MCP onboarding](mcp-connection-guide.md) covers reviewable configuration and
optional apply/verify/undo. Read-only MCP can still read your library: choose
imports carefully and trust the client receiving the output. Local clients are
Codex and Claude Code; cloud ChatGPT cannot reach local stdio.

## Uninstall without losing data

Undo MCP entries you explicitly installed, then stop the appropriate foreground
process, Homebrew service or Compose project. `brew uninstall sessionsift` removes
the app, not `~/.sessionsift`; Docker `down` without `--volumes` retains state.
Removing a source checkout also leaves state and provider transcripts intact.
Data deletion is never required. Review the exact state path/volume and back it
up before any separate deletion. No mandatory hosted account needs closing.

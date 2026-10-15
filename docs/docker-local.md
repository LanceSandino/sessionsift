# Local Docker installation

Docker runs the same Local Agent as the native app. No SessionSift account,
bridge, Cloudflare, Google login or cloud service is required. Docker Engine or
Docker Desktop with a maintained Engine version at least 28.5.2 and Docker
Compose v2.24.4 or later is required. Older Engine versions have documented
[localhost-port exposure vulnerabilities](https://docs.docker.com/engine/release-notes/28/#2833),
and later 28.x updates fix container-runtime vulnerabilities; use a current
[maintained security patch](https://docs.docker.com/engine/release-notes/28/#2852).
Do not rely on older loopback publish behavior. The image is
built locally from the source checkout; there is no published SessionSift
container image to pull yet. Building requires network access to dependency
registries; running and using the local library does not require cloud access.

From the downloaded source directory:

```bash
docker version --format '{{.Server.Version}}'
docker compose --env-file /dev/null -f compose.local.yaml up -d --build
docker compose --env-file /dev/null -f compose.local.yaml ps
docker compose --env-file /dev/null -f compose.local.yaml exec -T agent /app/sessionsift doctor
```

Open <http://127.0.0.1:7777> in your browser. An empty library is expected until
you explicitly select transcript sources or import a user-provided export.
The dashboard is published only on IPv4 loopback, never on the LAN. If another
application uses port 7777, stop that application yourself or use a separately
reviewed Compose override; do not restart unrelated services.

The container uses UID/GID 65532, a read-only root filesystem, no capabilities,
no-new-privileges, a bounded private temporary directory, and CPU/memory/process
limits. It stores local application data in the named volume
`sessionsift-local_local-state`; it does not mount your HOME or Docker socket.
An isolated Docker bridge with an exact trusted gateway forwards the loopback
dashboard. This is **not an outbound firewall**: the Docker network can reach
the internet, although no hosted bridge or sync credentials are configured.
An `internal: true` Docker network prevents this Docker Desktop dashboard port
from being published; offline stdio/import tests use that configuration separately.

## Optional transcript sources

By default no Codex or Claude directories are mounted. With Python 3 available,
select only existing transcript leaf directories:

```bash
python3 scripts/local-docker-sources.py \
  --codex-sessions "$HOME/.codex/sessions" \
  --codex-archived-sessions "$HOME/.codex/archived_sessions" \
  --claude-projects "$HOME/.claude/projects" \
  --output .sessionsift-local-sources.json
docker compose --env-file /dev/null -f compose.local.yaml -f .sessionsift-local-sources.json up -d --build
docker compose --env-file /dev/null -f compose.local.yaml -f .sessionsift-local-sources.json exec -T agent /app/sessionsift import
```

Review the generated owner-only JSON before using it. Its selected paths are
local configuration, not material to commit or share. Creation is exclusive:
an existing override is never replaced. Missing optional directories are omitted
without creating host directories. The helper refuses full provider/HOME paths,
symlinks (including ancestors or nested entries), credential/configuration files,
and special files. Compose binds each selected leaf read-only and disables
automatic creation of host paths. It never mounts `auth.json`, provider settings,
SSH keys, Codex state databases or a whole `.codex`/`.claude` directory.

On Linux, selected files/directories must be readable/traversable by container
UID 65532; Docker Desktop file-sharing permissions also apply. If import reports
permission errors, do not make your HOME, credentials or original transcript
directories world-readable. Prefer native installation, or explicitly selected
sanitized copies that you permit this container user to read. The helper does
not change host source ownership or permissions. Do not add the Docker socket
to solve permissions. Removing a source override removes access on the next
Compose `up`; previously imported records remain in your local state volume.

## Daily use

Use the same source override on each command if you selected sources:

```bash
docker compose --env-file /dev/null -f compose.local.yaml exec -T agent /app/sessionsift sessions
docker compose --env-file /dev/null -f compose.local.yaml down
docker compose --env-file /dev/null -f compose.local.yaml up -d
```

Browse Sessions, Conversation, Timeline, Tools, Analytics and Compare in the
dashboard. The CLI also supports `inspect ID` and `compare ID1 ID2` with IDs
reported by `sessions`. `down` stops only this Compose project and preserves
the named volume. **Do not add `--volumes`** unless you explicitly intend to
delete the application library and private keys.

## Local MCP in Codex or Claude Code

MCP is a read-only subprocess, not a public HTTP endpoint. Start the container
first. Use the absolute checkout path so launching from another directory is
reliable. The wrapper uses `exec -T` (no TTY) and keeps diagnostics out of MCP
stdout. The `/bin/bash` wrapper requires no executable-bit adjustment.
It resolves the selected Docker context from local metadata, requires an
existing local Unix socket, and pins that socket explicitly. SSH/TCP Docker
contexts are refused before any container/MCP operation; switch to your local
Docker Desktop or local Engine context yourself if needed.

```bash
codex mcp add sessionsift -- /bin/bash "$(pwd -P)/scripts/local-docker-mcp.sh"
codex mcp list
claude mcp add --scope user sessionsift -- /bin/bash "$(pwd -P)/scripts/local-docker-mcp.sh"
claude mcp list
```

These are opt-in client changes; use only the clients you want. With selected
sources, append the absolute override path to the wrapper command:

```bash
codex mcp add sessionsift -- /bin/bash "$(pwd -P)/scripts/local-docker-mcp.sh" "$(pwd -P)/.sessionsift-local-sources.json"
```

Use the equivalent extra argument for Claude Code. For reviewable TOML, the
Codex entry is `command = "/bin/bash"` with `args` containing the absolute
wrapper path and optional absolute source override; Claude Desktop uses the
same `command` and `args` in its `mcpServers` JSON. Never copy OAuth tokens to
configure local MCP. Cloud ChatGPT cannot connect directly to local stdio.

Native Codex app-server discovery/list/metrics and Claude Code's native MCP
handshake are tested without inference or credentials. Protocol acceptance also
exercises all ten read-only tool annotations, two synthetic records, metrics and
invalid/missing/path-like IDs. Claude's management handshake is not a claim that
an authenticated Claude inference turn was tested.

Undo only the MCP entries you explicitly added:

```bash
codex mcp remove sessionsift
claude mcp remove --scope user sessionsift
docker compose --env-file /dev/null -f compose.local.yaml down
```

This preserves your named-volume data, source transcripts and other client
configuration. Removing Docker or this checkout does not itself delete the
named volume. Back up that volume before any separately authorized deletion.

See the [official Codex MCP documentation](https://developers.openai.com/codex/mcp)
and [Claude Code MCP documentation](https://code.claude.com/docs/en/mcp) for client
configuration and scope details.

# Offline bridge KEK rotation

This rotates the server-held key protecting transient MCP request/response envelopes and the key-check marker. It does **not** rotate user sync keys or make hosted MCP end-to-end encrypted. The server can decrypt relay envelopes; client-encrypted sync objects remain opaque.

Only Linux/macOS single-origin bridges on an operator-owned local filesystem are supported. Authorize a maintenance window, stop ingress, every bridge writer and all automatic restarts, and keep a verified pre-maintenance database/key backup. Older binaries do not honor the new process lock. Never remove/replace the lock inode or promise zero downtime.

The database and effective key must be owned, single-link 0600 regular files in real 0700 directories. The key path is `SESSIONSIFT_BRIDGE_KEK_FILE`, otherwise `<database>.key`. Never pass key bytes in arguments, environment, logs or tickets. The tool generates a fresh 256-bit KEK internally. Allow space for two complete protected snapshots plus SQLite temporary files. Network filesystems and physical power-loss behavior are not certified.

```bash
BRIDGE_DB=/absolute/private/path/bridge.db
sessionsift bridge kek-preflight --database "$BRIDGE_DB"
sessionsift bridge kek-rotate --database "$BRIDGE_DB" --operation operator-recorded-unique-id
```

Preflight requires a cleanly closed database without WAL, SHM or rollback-journal sidecars; it refuses pending state rather than ignoring it. Never delete sidecars to bypass this refusal. It authenticates every retained envelope without migrations or key changes and may create only the protected process-lock file. Rotation authenticates every request/response/verifier, rewraps its DEK using a fresh AEAD nonce, and verifies the new pair. The encrypted body, identity/grant/support/invitation metadata, task TTL/status and replay receipts remain unchanged. The operation/generation/record count audit contains no payloads or keys. Repeating the completed operation ID is a no-op, not another rotation.

Protected pairs are retained in `<database>.rotation-<operation>/old/` and `/new/`, each containing `bridge.db` and `bridge.db.key`. These keys intentionally allow recovery of their matching encrypted bridge database; possession of both exposes retained relay contents. Keep restricted custody and an explicit retention/disposal plan. They do not recover users' sync master keys.

`<database>.rotation-journal` durably guards the two-file cutover. Startup refuses service while it exists. Keep the service stopped and choose one explicit action:

```bash
sessionsift bridge kek-recover --database "$BRIDGE_DB" --action commit
# OR restore the pre-cutover generation while the journal is pending:
sessionsift bridge kek-recover --database "$BRIDGE_DB" --action rollback
```

Recovery verifies both saved pairs, their hashes/generations and every encrypted record before replacing the live files. It refuses unexpected live state, altered backups, wrong keys, symlinks and SQLite sidecars. Never manually delete the journal or swap only the key. An interruption before journal publication leaves the original live pair usable: retain incomplete preparation state, preflight the original pair, then use a new operation ID.

With no journal, `commit` only validates the current pair and reports `already_complete`. `rollback` refuses: a post-completion rewind requires a separately authorized restore of the verified pre-rotation **database and matching old key**, and may discard subsequent writes. Do not downgrade a migrated database by swapping an older binary.

Before reopening ingress, verify integrity, new-key startup, queued synthetic request/response completion, old-key rejection in a disposable copy, signed-device/replay behavior and normal readiness. Retain the old pair until the approved recovery interval ends. No automatic key or backup deletion is performed by rotation. SHA-256 verifies bytes; it does not authenticate a trusted publisher. Release candidates remain unsigned/unnotarized unless separately qualified.

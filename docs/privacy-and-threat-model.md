# Privacy and threat model

SessionSift is local-first. Local import, analysis, backups, the browser UI on `127.0.0.1:7777`, and the ten-tool local MCP server need no SessionSift account or cloud service. Provider transcripts remain the authoritative originals and are never deleted by sync controls.

## Data paths

| Mode | Leaves the computer | Remote storage | Who can read content |
|---|---|---|---|
| Local analysis and local MCP | Nothing | None | Processes with access to the local account and SessionSift state |
| Remote MCP/support/invitations | Authorized request and redacted response | Identity, grants, bounded audit metadata, and short-lived task ciphertext | The Local Agent, bridge process while forwarding, Cloudflare, and the chosen MCP client |
| Optional encrypted sync | Only selected normalized records encrypted on the client | Opaque object IDs, ciphertext, device/workspace IDs, sizes, versions, timestamps, and quota usage | Devices holding the recovery key |

Remote MCP is not end-to-end encrypted. Its bridge encryption key protects queued records at rest, but the bridge process holds that key and handles relay plaintext. TLS, Cloudflare Access, and bridge queue encryption do not make that path zero-knowledge. Downstream ChatGPT, Claude, or another MCP client applies its own data policy.

Encrypted sync is a separate protocol. A client creates a random 256-bit master key, derives per-record/per-epoch keys with HKDF-SHA256, and encrypts selected data with XChaCha20-Poly1305 before upload. On macOS the key is stored in Keychain; headless systems use a private `0600` file fallback. The server never receives the key, recovery secret, raw transcript, local SQLite/WAL, provider token, export archive, or unselected evidence. The operator can still observe account/workspace/device identifiers, IP and timing metadata, ciphertext sizes, versions, and quota use.

## Limits and compromise

- A compromised or unlocked client can expose local source data and sync keys. Revocation stops later server access but cannot retract plaintext or a key already copied from that device.
- After removing a device, rotate the sync key. Rotation purges current server ciphertext, advances the server epoch so stale offline writes fail, and re-encrypts future records under the new client-held key.
- Losing the recovery secret and every trusted device makes synced ciphertext unrecoverable. SessionSift has no escrow or provider-side reset.
- A malicious server can withhold data. Signed lineage and a persistent high-water mark detect many substitutions and rollbacks while a trusted checkpoint survives; loss of all client state limits rollback detection.
- Redaction reduces accidental disclosure but is not perfect. Detailed prompt/tool evidence is separately disabled by default and must be explicitly selected.

## Retention, backup, and deletion

Local state is retained until the user removes it. Bridge operators choose backup retention and must protect the bridge database and its separate queue-encryption key as one recovery unit. Sync ciphertext is included in bridge backups but remains unreadable without the client recovery key.

“Delete synced cloud data” requires the current recovery secret and exact confirmation, atomically removes current objects and change history, and advances the minimum accepted epoch. It does not delete provider transcripts. Historical operator backups may retain opaque ciphertext until the documented backup-retention window expires. “Remove synced data from this device” deletes local replicas, queue state, and the local sync key only. “Disconnect” stops future sync without deleting either copy.

The bridge keeps bounded metadata-only access logs; application logs must not contain tokens, private URLs, session content, tool arguments/results, sync plaintext, or recovery secrets. See [remote architecture](remote-architecture.md), [cloud sync](cloud-sync.md), and [self-host operations](self-host-operations.md).

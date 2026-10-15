# Optional encrypted cloud sync

Cloud Sync is opt-in and independent of Remote MCP. Local use stays free, account-free, and offline-capable when sync is off.

Qualification security boundary: client application authenticates deletion records with the same user-held AEAD key as content, in addition to device signatures. A bridge-provided public key alone cannot authorize a deletion. Signature-only tombstones from the original candidate are rejected by the qualified client; they require re-authoring by an enrolled client with the current recovery key using an encrypted object PUT. A rejected legacy object does not advance the cursor or delete local records. This deliberate compatibility boundary must be checked before upgrading an existing sync workspace. Whole-workspace deletion remains a separate authenticated owner/device operation and cannot revoke already downloaded plaintext or backups.

## Enable on the first device

1. Connect and link the Local Agent to an invited hosted or self-hosted bridge.
2. Open **Connections → Cloud Sync** and review the service and categories.
3. Keep summaries/metrics and findings/notes selected, or separately opt into bounded redacted conversation evidence.
4. Generate the recovery secret, save it outside SessionSift, and re-enter it to prove possession.
5. Choose **Sync now** or leave the enabled agent running. Offline work stays in the durable local outbox and retries with bounded backoff.

No background request occurs before explicit enablement. Raw transcripts, provider credentials, OS paths, database/WAL files, original export archives, and unselected detailed evidence are never sync inputs.

## Restore on another device

Install the same SessionSift version, link that device to the same owner workspace, select **Restore on this device**, and enter the recovery secret out of band. The new device verifies signed ciphertext and decrypts selected snapshots locally. Restored data is labeled as a synced snapshot and does not overwrite or impersonate a locally imported provider session. Sync does not recreate unselected raw transcripts.

## Offline changes and conflicts

Immutable session snapshots can advance only from their source device. Mutable notes and preferences use revision compare-and-swap. Independent top-level field changes merge deterministically; edits to the same field appear as a conflict with **Keep this device** and **Use synced version** choices. Replayed revisions are idempotent and stale revisions fail instead of overwriting.

## Device loss, revocation, and rotation

Revoke a lost device from the bridge first. Revocation blocks future fetch and upload but cannot erase data already downloaded. Then use **Rotate key after device removal** and save the replacement recovery secret. Rotation deletes old server ciphertext, advances the key epoch, clears stale local sync state, and causes current selected snapshots to upload again under the new key on the next sync.

## Distinct controls

- **Pause**: keep local and cloud state, but stop scheduled/manual transfer until resumed.
- **Disconnect sync**: stop sync while retaining local replicas, key, and cloud ciphertext.
- **Remove synced data from this device**: remove local replicas/outbox/key; keep original provider data and cloud ciphertext.
- **Delete synced cloud data**: purge server objects/change history and block stale epoch resurrection; keep original provider data and the local key.

Hosted allowance is operator-configured. The UI shows actual usage and quota after a successful sync. A quota failure keeps unsent records in the local outbox and does not block imports. See [privacy and threat model](privacy-and-threat-model.md) for metadata, backup, and recovery limits.

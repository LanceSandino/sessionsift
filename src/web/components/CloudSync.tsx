/** @module CloudSync @description Owner-controlled encrypted sync onboarding, recovery, status, and trust boundaries. */
import React, { useEffect, useState } from "react";
import { api } from "../api";
import type { CloudSyncStatus, SyncConflict } from "../types";
import { Icon } from "./Icon";

type Selection = {
  summaries: boolean;
  findings_notes: boolean;
  detailed_evidence: boolean;
};
const defaults: Selection = {
  summaries: true,
  findings_notes: true,
  detailed_evidence: false,
};

export function CloudSync() {
  const [status, setStatus] = useState<CloudSyncStatus>();
  const [selection, setSelection] = useState<Selection>(defaults);
  const [offer, setOffer] = useState<string>();
  const [proof, setProof] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [conflicts, setConflicts] = useState<SyncConflict[]>([]);
  const [destructiveSecret, setDestructiveSecret] = useState("");
  const load = () =>
    Promise.all([api.syncStatus(), api.syncConflicts().catch(() => [])])
      .then(([value, items]) => {
        setStatus(value);
        setConflicts(items);
        setSelection({
          summaries: value.summaries,
          findings_notes: value.findings_notes,
          detailed_evidence: value.detailed_evidence,
        });
      })
      .catch((value) => setError(String(value)));
  useEffect(() => {
    void load();
  }, []);
  const act = async (run: () => Promise<unknown>) => {
    setBusy(true);
    setError(undefined);
    try {
      await run();
      await load();
    } catch (value) {
      setError(String(value));
    } finally {
      setBusy(false);
    }
  };
  const prepare = () =>
    act(async () => {
      const value = await api.prepareSync(selection);
      setOffer(value.recovery_secret);
      setProof("");
    });
  const enable = () =>
    act(async () => {
      await api.enableSync(proof, selection);
      setOffer(undefined);
      setProof("");
    });
  const recover = () =>
    act(async () => {
      await api.recoverSync(proof, selection);
      setProof("");
    });
  const toggle =
    (key: keyof Selection) => (event: React.ChangeEvent<HTMLInputElement>) =>
      setSelection((value) => ({ ...value, [key]: event.target.checked }));
  const connected = status?.bridge?.connected && status.bridge.owner;
  return (
    <section className="device-card" aria-labelledby="cloud-sync-title">
      <h3 id="cloud-sync-title">
        <Icon name="lock" />
        Cloud Sync{" "}
        <span className="status-badge">
          {status?.rotation_pending
            ? "Recovery needed"
            : status?.enabled
              ? status.paused
                ? "Paused"
                : "On"
              : "Off"}
        </span>
      </h3>
      <p>
        <b>
          Your sessions always stay on this computer unless you enable sync.
        </b>{" "}
        Local analysis and local MCP remain free, offline, and account-free.
      </p>
      <p>
        Sync encrypts selected records on this device with a key only you hold.{" "}
        <b>Remote MCP is separate:</b> its authorized responses pass through the
        bridge as plaintext and are not end-to-end encrypted.
      </p>
      <div className="field-grid" aria-label="Cloud sync data categories">
        <label>
          <input
            type="checkbox"
            checked={selection.summaries}
            onChange={toggle("summaries")}
            disabled={busy || status?.enabled}
          />{" "}
          Summaries &amp; metrics
        </label>
        <label>
          <input
            type="checkbox"
            checked={selection.findings_notes}
            onChange={toggle("findings_notes")}
            disabled={busy || status?.enabled}
          />{" "}
          Findings &amp; notes
        </label>
        <label>
          <input
            type="checkbox"
            checked={selection.detailed_evidence}
            onChange={toggle("detailed_evidence")}
            disabled={busy || status?.enabled}
          />{" "}
          Redacted conversation evidence
        </label>
      </div>
      {selection.detailed_evidence && (
        <p className="warning-copy">
          Conversation evidence is bounded and redacted before encryption, but
          redaction cannot guarantee removal of every secret.
        </p>
      )}
      <p>
        Storage:{" "}
        {connected
          ? status?.bridge.url?.includes("sessionsift")
            ? "SessionSift hosted beta"
            : "Your enrolled bridge"
          : "Local only — connect and link a bridge first"}
        .
      </p>
      {offer && (
        <div className="recovery-offer" role="status">
          <p>
            <b>Save this recovery secret now.</b>{" "}
            {status?.rotation_pending
              ? "The cloud deletion result is not yet confirmed. Sync remains locked until you retry the pending key rotation."
              : "SessionSift and the bridge cannot recover it. Losing it and every trusted device makes synced ciphertext unreadable."}
          </p>
          <output>{offer}</output>
          {status?.rotation_pending ? (
            <button
              className="button primary"
              disabled={busy}
              onClick={() =>
                act(async () => {
                  const value = await api.rotateSyncKey("");
                  setOffer(value.recovery_secret);
                })
              }
            >
              Retry pending key rotation
            </button>
          ) : status?.enabled ? (
            <button className="button primary" onClick={() => setOffer(undefined)}>
              I saved the replacement secret
            </button>
          ) : (
            <>
              <label>
                Re-enter recovery secret
                <input
                  aria-label="Re-enter recovery secret"
                  type="password"
                  autoComplete="off"
                  value={proof}
                  onChange={(event) => setProof(event.target.value)}
                />
              </label>
              <button
                className="button primary"
                disabled={busy || proof !== offer}
                onClick={enable}
              >
                Confirm and enable sync
              </button>
            </>
          )}
        </div>
      )}
      {!status?.enabled && !offer && (
        <div className="button-row">
          <button
            className="button primary"
            disabled={
              busy ||
              !connected ||
              (!selection.summaries &&
                !selection.findings_notes &&
                !selection.detailed_evidence)
            }
            onClick={prepare}
          >
            Prepare recovery key
          </button>
          {connected && (
            <>
              <label>
                Recovery secret
                <input
                  aria-label="Recovery secret"
                  type="password"
                  autoComplete="off"
                  value={proof}
                  onChange={(event) => setProof(event.target.value)}
                />
              </label>
              <button
                className="button"
                disabled={busy || !connected || !proof}
                onClick={recover}
              >
                Restore on this device
              </button>
            </>
          )}
        </div>
      )}
      {status?.enabled && (
        <div>
          {status.rotation_pending && !offer && (
            <div className="recovery-offer" role="alert">
              <p>
                <b>Key rotation needs recovery.</b> Sync stays locked while the
                signed bridge state is checked. Retry to finish the already
                authorized cloud deletion and receive the current recovery
                secret.
              </p>
              <button
                className="button primary"
                disabled={busy}
                onClick={() =>
                  act(async () => {
                    const value = await api.rotateSyncKey("");
                    setOffer(value.recovery_secret);
                  })
                }
              >
                Retry pending key rotation
              </button>
            </div>
          )}
          <p>
            <b>Encrypted sync is {status.paused ? "paused." : "on."}</b>{" "}
            Pending: {status.pending} · Restored snapshots: {status.replicas} ·
            Conflicts: {status.conflicts} · Key: {status.key_store}
          </p>
          <p>
            Last sync:{" "}
            {status.last_sync_at
              ? new Date(status.last_sync_at).toLocaleString()
              : "Not yet"}{" "}
            · Cloud usage:{" "}
            {status.quota_bytes
              ? `${status.usage_bytes} of ${status.quota_bytes} bytes`
              : "available after first sync"}
            {status.last_error ? ` · Offline/error: ${status.last_error}` : ""}
          </p>
          <p>
            Local originals remain authoritative and are never deleted by sync
            controls.
          </p>
          {!!status.replica_items?.length && (
            <div aria-label="Restored encrypted records">
              <h4>Restored encrypted records</h4>
              <p className="small">
                These are separate synced replicas. They never replace a local
                provider import.
              </p>
              <ul>
                {status.replica_items
                  .filter((item) => !item.deleted)
                  .map((item) => (
                    <li key={item.object_id}>
                      <b>
                        {String(
                          item.payload.label ||
                            item.payload.title ||
                            item.payload.body ||
                            item.kind,
                        )}
                      </b>{" "}
                      · {item.kind} · revision {item.revision}
                    </li>
                  ))}
              </ul>
            </div>
          )}
          <div className="button-row">
            <button
              className="button primary"
              disabled={busy || status.paused}
              onClick={() => act(() => api.runSync())}
            >
              Sync now
            </button>
            <button
              className="button"
              disabled={busy || status.rotation_pending}
              onClick={() =>
                act(() => (status.paused ? api.resumeSync() : api.pauseSync()))
              }
            >
              {status.paused ? "Resume" : "Pause"}
            </button>
            <button
              className="button"
              disabled={busy || status.rotation_pending}
              onClick={() => act(() => api.disableSync())}
            >
              Disconnect sync
            </button>
            {status.bridge.url && (
              <a
                className="button"
                href={status.bridge.url}
                target="_blank"
                rel="noopener noreferrer"
              >
                Manage devices
              </a>
            )}
          </div>
          {conflicts.length > 0 && (
            <div aria-label="Sync conflicts">
              <h4>Conflicts need your choice</h4>
              {conflicts.map((item) => (
                <div key={item.id}>
                  <code>{item.object_id}</code> revision {item.remote_revision}{" "}
                  <button
                    className="button"
                    onClick={() =>
                      act(() => api.resolveSyncConflict(item.id, "local"))
                    }
                  >
                    Keep this device
                  </button>{" "}
                  <button
                    className="button"
                    onClick={() =>
                      act(() => api.resolveSyncConflict(item.id, "remote"))
                    }
                  >
                    Use synced version
                  </button>
                </div>
              ))}
            </div>
          )}
          <details>
            <summary>Recovery and deletion</summary>
            <p>
              These controls never delete original Codex, Claude, or imported
              provider data. Enter the current recovery secret to confirm.
            </p>
            <label>
              Current recovery secret
              <input
                aria-label="Current recovery secret"
                type="password"
                autoComplete="off"
                value={destructiveSecret}
                onChange={(event) => setDestructiveSecret(event.target.value)}
              />
            </label>
            <div className="button-row">
              <button
                className="button"
                disabled={busy || !destructiveSecret}
                onClick={() =>
                  act(async () => {
                    const value = await api.rotateSyncKey(destructiveSecret);
                    setOffer(value.recovery_secret);
                    setDestructiveSecret("");
                  })
                }
              >
                Rotate key after device removal
              </button>
              <button
                className="button"
                disabled={busy || !destructiveSecret}
                onClick={() =>
                  act(() => api.deleteCloudSync(destructiveSecret))
                }
              >
                Delete synced cloud data
              </button>
              <button
                className="button"
                disabled={busy || !destructiveSecret}
                onClick={() =>
                  act(() => api.removeLocalSync(destructiveSecret))
                }
              >
                Remove synced data from this device
              </button>
            </div>
          </details>
        </div>
      )}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}

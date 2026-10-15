// @module SyncAPI exposes explicit localhost-only consent, recovery, status, and manual sync controls.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/LanceSandino/sessionsift/src/internal/cloudsync"
	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

type syncConsent struct {
	RecoverySecret   string `json:"recovery_secret,omitempty"`
	Summaries        bool   `json:"summaries"`
	FindingsNotes    bool   `json:"findings_notes"`
	DetailedEvidence bool   `json:"detailed_evidence"`
}

type syncDestructiveRequest struct {
	RecoverySecret string `json:"recovery_secret"`
	Confirmation   string `json:"confirmation"`
}

var replaceSyncKey = func(store cloudsync.KeyStore, key []byte) error {
	return store.Replace(key)
}

var persistSyncSettings = func(ctx context.Context, store *storage.Store, settings storage.SyncSettings) error {
	return store.SaveSyncSettings(ctx, settings)
}

func syncRoute(store *storage.Store, manager *remote.Manager, identity remote.Identity, hosted *remote.HostedRuntime, w http.ResponseWriter, r *http.Request) {
	paths, err := config.Resolve()
	if err != nil {
		apiError(w, err)
		return
	}
	keyStore := cloudsync.KeyStore{FilePath: paths.SyncMasterKey, Account: identity.ID}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync":
		writeSyncStatus(store, hosted, keyStore, w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/prepare":
		var consent syncConsent
		if !decodeSync(w, r, &consent) {
			return
		}
		if !validSyncSelection(consent) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "select at least one sync category"})
			return
		}
		if _, err = linkedSyncConfig(hosted); err != nil {
			remoteWrite(w, http.StatusBadRequest, nil, err)
			return
		}
		if _, err = keyStore.Load(); err == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a recovery key is already configured; confirm the saved secret or remove local sync data explicitly"})
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			apiError(w, err)
			return
		}
		key, keyErr := cloudsync.GenerateMasterKey(nil)
		if keyErr == nil {
			keyErr = keyStore.Save(key)
		}
		if keyErr != nil {
			apiError(w, keyErr)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"recovery_secret": cloudsync.EncodeRecoverySecret(key), "message": "Save this recovery secret now. SessionSift cannot recover it."})
	case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/sync/enable" || r.URL.Path == "/api/v1/sync/recover"):
		var consent syncConsent
		if !decodeSync(w, r, &consent) {
			return
		}
		if !validSyncSelection(consent) || consent.RecoverySecret == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "recovery secret and at least one category are required"})
			return
		}
		if _, err = linkedSyncConfig(hosted); err != nil {
			remoteWrite(w, http.StatusBadRequest, nil, err)
			return
		}
		current, currentErr := store.SyncSettings(r.Context())
		if currentErr != nil {
			apiError(w, currentErr)
			return
		}
		if current.RotationPending {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "sync key rotation is unresolved; retry the pending rotation before enabling sync"})
			return
		}
		provided, decodeErr := cloudsync.DecodeRecoverySecret(consent.RecoverySecret)
		if decodeErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": decodeErr.Error()})
			return
		}
		stored, loadErr := keyStore.Load()
		if errors.Is(loadErr, os.ErrNotExist) && r.URL.Path == "/api/v1/sync/recover" {
			loadErr = keyStore.Save(provided)
			stored = provided
		}
		if loadErr != nil || !bytes.Equal(stored, provided) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "recovery secret does not match this sync key"})
			return
		}
		if current.KeyEpoch == 0 {
			current.KeyEpoch = 1
		}
		settings := storage.SyncSettings{Enabled: true, Summaries: consent.Summaries, FindingsNotes: consent.FindingsNotes, DetailedEvidence: consent.DetailedEvidence, KeyEpoch: current.KeyEpoch}
		if err = store.SaveSyncSettings(r.Context(), settings); err != nil {
			apiError(w, err)
			return
		}
		writeSyncStatus(store, hosted, keyStore, w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/pause":
		setSyncPaused(store, true, w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/resume":
		setSyncPaused(store, false, w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/disable":
		settings, settingsErr := store.SyncSettings(r.Context())
		if settingsErr == nil && settings.RotationPending {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "sync key rotation is unresolved; retry it before disconnecting sync"})
			return
		}
		if settingsErr == nil {
			settings.Enabled, settings.Paused = false, false
			settingsErr = store.SaveSyncSettings(r.Context(), settings)
		}
		remoteWrite(w, http.StatusOK, settings, settingsErr)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/conflicts":
		conflicts, conflictsErr := store.SyncConflicts(r.Context())
		remoteWrite(w, http.StatusOK, conflicts, conflictsErr)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/conflicts/resolve":
		var value struct {
			ID         int64  `json:"id"`
			Resolution string `json:"resolution"`
		}
		if !decodeSync(w, r, &value) {
			return
		}
		if value.ID <= 0 || (value.Resolution != "local" && value.Resolution != "remote") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "choose the local or remote conflict version"})
			return
		}
		resolveErr := store.ResolveSyncConflict(r.Context(), value.ID, value.Resolution == "remote")
		remoteWrite(w, http.StatusOK, map[string]bool{"resolved": resolveErr == nil}, resolveErr)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/delete-cloud":
		var value syncDestructiveRequest
		if !decodeSync(w, r, &value) {
			return
		}
		if value.Confirmation != "DELETE CLOUD COPIES" || !syncSecretMatches(keyStore, value.RecoverySecret) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "exact confirmation and current recovery secret required"})
			return
		}
		bridgeConfig, configErr := linkedSyncConfig(hosted)
		if configErr != nil {
			remoteWrite(w, http.StatusBadRequest, nil, configErr)
			return
		}
		transport := &cloudsync.HTTPTransport{Config: bridgeConfig, Identity: identity}
		nextEpoch, deleteErr := transport.DeleteWorkspace(r.Context())
		if deleteErr == nil {
			deleteErr = store.ResetSyncReplicaState(r.Context())
		}
		settings, _ := store.SyncSettings(r.Context())
		settings.Enabled, settings.Paused, settings.RotationPending, settings.KeyEpoch = false, false, false, nextEpoch
		if deleteErr == nil {
			deleteErr = store.SaveSyncSettings(r.Context(), settings)
		}
		remoteWrite(w, http.StatusOK, map[string]any{"deleted": deleteErr == nil, "next_epoch": nextEpoch}, deleteErr)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/remove-local":
		var value syncDestructiveRequest
		if !decodeSync(w, r, &value) {
			return
		}
		if value.Confirmation != "REMOVE LOCAL SYNC DATA" || !syncSecretMatches(keyStore, value.RecoverySecret) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "exact confirmation and current recovery secret required"})
			return
		}
		removeErr := store.ResetSyncReplicaState(r.Context())
		settings, _ := store.SyncSettings(r.Context())
		settings.Enabled, settings.Paused, settings.RotationPending = false, false, false
		if removeErr == nil {
			removeErr = store.SaveSyncSettings(r.Context(), settings)
		}
		if removeErr == nil {
			removeErr = keyStore.Remove()
		}
		remoteWrite(w, http.StatusOK, map[string]bool{"removed": removeErr == nil}, removeErr)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/rotate-key":
		var value syncDestructiveRequest
		if !decodeSync(w, r, &value) {
			return
		}
		settings, rotateErr := store.SyncSettings(r.Context())
		if rotateErr != nil {
			remoteWrite(w, http.StatusInternalServerError, nil, rotateErr)
			return
		}
		pendingRetry := settings.RotationPending
		secretAccepted := syncSecretMatches(keyStore, value.RecoverySecret) || (pendingRetry && value.RecoverySecret == "")
		if value.Confirmation != "ROTATE SYNC KEY" || !secretAccepted {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "exact confirmation and current recovery secret required"})
			return
		}
		bridgeConfig, configErr := linkedSyncConfig(hosted)
		if configErr != nil {
			remoteWrite(w, http.StatusBadRequest, nil, configErr)
			return
		}
		transport := &cloudsync.HTTPTransport{Config: bridgeConfig, Identity: identity}
		remoteEpochBefore, rotateErr := transport.WorkspaceEpoch(r.Context())
		if rotateErr != nil {
			remoteWrite(w, http.StatusBadGateway, nil, rotateErr)
			return
		}
		oldKey, rotateErr := keyStore.Load()
		if rotateErr != nil {
			remoteWrite(w, http.StatusInternalServerError, nil, rotateErr)
			return
		}
		key, rotateErr := cloudsync.GenerateMasterKey(nil)
		if rotateErr != nil {
			remoteWrite(w, http.StatusInternalServerError, nil, rotateErr)
			return
		}
		originalPaused := settings.Paused
		if !pendingRetry {
			// Commit the fail-closed state before either changing the local key or
			// issuing the destructive request. A canceled response or storage fault
			// can therefore never leave normal sync enabled with an unknown outcome.
			settings.Paused, settings.RotationPending = true, true
			if rotateErr = persistSyncSettings(r.Context(), store, settings); rotateErr != nil {
				remoteWrite(w, http.StatusInternalServerError, nil, rotateErr)
				return
			}
		}
		if rotateErr = replaceSyncKey(keyStore, key); rotateErr != nil {
			if !pendingRetry {
				settings.Paused, settings.RotationPending = originalPaused, false
				if clearErr := persistSyncSettings(r.Context(), store, settings); clearErr != nil {
					rotateErr = errors.Join(rotateErr, errors.New("clear pending rotation after local key failure"), clearErr)
				}
			}
			remoteWrite(w, http.StatusInternalServerError, nil, rotateErr)
			return
		}
		nextEpoch, rotateErr := transport.DeleteWorkspace(r.Context())
		if rotateErr != nil {
			// A failed response does not prove DELETE failed: the bridge may have
			// committed before a proxy lost its acknowledgement. Resolve against
			// the signed workspace epoch before choosing which key remains active.
			remoteEpoch, stateErr := transport.WorkspaceEpoch(r.Context())
			if stateErr == nil && remoteEpoch > remoteEpochBefore {
				nextEpoch, rotateErr = remoteEpoch, nil
			} else if stateErr == nil {
				if restoreErr := replaceSyncKey(keyStore, oldKey); restoreErr != nil {
					rotateErr = errors.Join(rotateErr, errors.New("restore prior sync key after failed cloud rotation"), restoreErr)
				} else if !pendingRetry {
					settings.Paused, settings.RotationPending = originalPaused, false
					if clearErr := persistSyncSettings(r.Context(), store, settings); clearErr != nil {
						rotateErr = errors.Join(rotateErr, errors.New("clear pending rotation after unchanged cloud state"), clearErr)
					}
				}
			} else {
				writeJSON(w, http.StatusAccepted, map[string]any{
					"error":           "sync key rotation outcome is unknown; sync is paused until the signed workspace epoch can be checked",
					"recovery_secret": cloudsync.EncodeRecoverySecret(key),
					"message":         "Save this replacement recovery secret. Rotation must be retried after bridge status is available.",
					"retry_required":  true,
				})
				return
			}
			if rotateErr != nil {
				remoteWrite(w, http.StatusInternalServerError, nil, rotateErr)
				return
			}
		}
		if nextEpoch <= remoteEpochBefore {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"error":           "sync key rotation did not return an advanced workspace epoch; sync is paused",
				"recovery_secret": cloudsync.EncodeRecoverySecret(key),
				"message":         "Save this replacement recovery secret. Rotation must be retried after bridge status is available.",
				"retry_required":  true,
			})
			return
		}
		if rotateErr == nil {
			rotateErr = store.ResetSyncReplicaState(r.Context())
		}
		settings.Enabled, settings.Paused, settings.RotationPending, settings.KeyEpoch = true, false, false, nextEpoch
		if rotateErr == nil {
			rotateErr = persistSyncSettings(r.Context(), store, settings)
		}
		if rotateErr != nil {
			remoteWrite(w, http.StatusInternalServerError, nil, rotateErr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"recovery_secret": cloudsync.EncodeRecoverySecret(key), "message": "The old recovery secret can no longer decrypt newly synced records. Save this replacement secret now."})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/run":
		settings, settingsErr := store.SyncSettings(r.Context())
		if settingsErr != nil || !settings.Enabled || settings.Paused || settings.RotationPending {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "sync is disabled, paused, or awaiting key-rotation recovery"})
			return
		}
		bridgeConfig, configErr := linkedSyncConfig(hosted)
		key, keyErr := keyStore.Load()
		if configErr != nil || keyErr != nil {
			remoteWrite(w, http.StatusBadRequest, nil, errors.Join(configErr, keyErr))
			return
		}
		transport := &cloudsync.HTTPTransport{Config: bridgeConfig, Identity: identity}
		engine := &cloudsync.Engine{Store: store, Transport: transport, MasterKey: key, WorkspaceID: bridgeConfig.WorkspaceID, DeviceID: identity.ID, Epoch: settings.KeyEpoch, Sign: identity.Sign}
		selection := cloudsync.SnapshotSelection{Summaries: settings.Summaries, FindingsNotes: settings.FindingsNotes, DetailedEvidence: settings.DetailedEvidence}
		queued, runErr := cloudsync.QueueSnapshots(r.Context(), engine, store, selection)
		if runErr == nil {
			var personal cloudsync.QueueSummary
			personal, runErr = cloudsync.QueuePersonalState(r.Context(), engine, store, selection, personalSyncState(manager))
			queued.Examined += personal.Examined
			queued.Queued += personal.Queued
			queued.Bytes += personal.Bytes
		}
		if runErr == nil {
			var edits int
			edits, runErr = engine.QueueReplicaEdits(r.Context())
			queued.Examined += edits
			queued.Queued += edits
		}
		if runErr == nil {
			runErr = engine.RunOnce(r.Context())
		}
		usage, usageErr := transport.Usage(r.Context())
		if runErr == nil && usageErr != nil {
			runErr = usageErr
		}
		_ = store.RecordSyncRun(r.Context(), usage.Bytes, usage.Limit, runErr)
		remoteWrite(w, http.StatusOK, map[string]any{"queued": queued}, runErr)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func linkedSyncConfig(hosted *remote.HostedRuntime) (remote.HostedConfig, error) {
	if hosted == nil {
		return remote.HostedConfig{}, errors.New("connect and link this device to a bridge before enabling sync")
	}
	config, err := hosted.CurrentConfig()
	if err != nil || config.Owner == "" || config.WorkspaceID == "" {
		return remote.HostedConfig{}, errors.New("connect and link this device to a bridge before enabling sync")
	}
	return config, nil
}

func writeSyncStatus(store *storage.Store, hosted *remote.HostedRuntime, keyStore cloudsync.KeyStore, w http.ResponseWriter, r *http.Request) {
	settings, err := store.SyncSettings(r.Context())
	if err != nil {
		apiError(w, err)
		return
	}
	pending, _ := store.PendingSyncCount(r.Context())
	runtimeState, _ := store.SyncRuntimeState(r.Context())
	replicas, _ := store.ListSyncReplicas(r.Context())
	replicaItems := replicas
	if len(replicaItems) > 50 {
		replicaItems = replicaItems[:50]
	}
	conflicts, _ := store.SyncConflicts(r.Context())
	_, keyErr := keyStore.Load()
	status := remote.RuntimeStatus{}
	if hosted != nil {
		status = hosted.Status()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": settings.Enabled, "paused": settings.Paused, "rotation_pending": settings.RotationPending, "summaries": settings.Summaries,
		"findings_notes": settings.FindingsNotes, "detailed_evidence": settings.DetailedEvidence,
		"key_configured": keyErr == nil, "pending": pending, "cursor": runtimeState.HighWaterCursor,
		"replicas": len(replicas), "replica_items": replicaItems, "conflicts": len(conflicts), "bridge": status,
		"last_sync_at": runtimeState.LastSyncAt, "last_error": runtimeState.LastError,
		"usage_bytes": runtimeState.UsageBytes, "quota_bytes": runtimeState.QuotaBytes, "key_epoch": settings.KeyEpoch,
		"key_store": keyStore.Provider(),
	})
}

func personalSyncState(manager *remote.Manager) cloudsync.PersonalState {
	state := cloudsync.PersonalState{}
	if manager == nil {
		return state
	}
	for _, value := range manager.AllAnnotations() {
		state.Notes = append(state.Notes, cloudsync.PortableNote{ID: value.ID, SessionID: value.SessionID, TurnID: value.TurnID, Author: value.Author, Body: value.Body, CreatedAt: value.CreatedAt})
	}
	for _, value := range manager.AllReviews() {
		state.Reviews = append(state.Reviews, cloudsync.PortableReview{ID: value.ID, SessionID: value.SessionID, Reviewer: value.Reviewer, Status: value.Status, Summary: value.Summary, CreatedAt: value.CreatedAt})
	}
	return state
}

func syncSecretMatches(keyStore cloudsync.KeyStore, secret string) bool {
	provided, err := cloudsync.DecodeRecoverySecret(secret)
	if err != nil {
		return false
	}
	stored, err := keyStore.Load()
	return err == nil && bytes.Equal(stored, provided)
}

func setSyncPaused(store *storage.Store, paused bool, w http.ResponseWriter, r *http.Request) {
	settings, err := store.SyncSettings(r.Context())
	if err == nil {
		if !settings.Enabled {
			err = errors.New("sync is disabled")
		} else if !paused && settings.RotationPending {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "sync key rotation is unresolved; retry it before resuming sync"})
			return
		} else {
			settings.Paused = paused
			err = store.SaveSyncSettings(r.Context(), settings)
		}
	}
	remoteWrite(w, http.StatusOK, settings, err)
}

func validSyncSelection(value syncConsent) bool {
	return value.Summaries || value.FindingsNotes || value.DetailedEvidence
}

func decodeSync(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sync request"})
		return false
	}
	return true
}

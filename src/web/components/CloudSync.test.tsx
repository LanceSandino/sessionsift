/** @module CloudSyncTests @description Verifies explicit recovery proof and sync/MCP trust separation. */
import "@testing-library/jest-dom/vitest";
import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { CloudSync } from "./CloudSync";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("shows a one-time recovery secret and requires re-entry before enabling", async () => {
  let enabled = false;
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith("/prepare"))
        return Promise.resolve(
          new Response(
            JSON.stringify({
              recovery_secret: "sssync1_owner_secret",
              message: "save it",
            }),
            { status: 201, headers: { "Content-Type": "application/json" } },
          ),
        );
      if (path.endsWith("/enable")) {
        enabled = true;
        return Promise.resolve(
          new Response(
            JSON.stringify({
              enabled: true,
              paused: false,
              rotation_pending: false,
              summaries: true,
              findings_notes: true,
              detailed_evidence: false,
              key_configured: true,
              pending: 0,
              cursor: 0,
              replicas: 0,
              conflicts: 0,
              bridge: {
                connected: true,
                owner: "owner@example.com",
                device_id: "device",
              },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        );
      }
      return Promise.resolve(
        new Response(
          JSON.stringify({
            enabled,
            paused: false,
            rotation_pending: false,
            summaries: true,
            findings_notes: true,
            detailed_evidence: false,
            key_configured: false,
            pending: 0,
            cursor: 0,
            replicas: 0,
            conflicts: 0,
            bridge: {
              connected: true,
              owner: "owner@example.com",
              device_id: "device",
            },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    }),
  );
  render(<CloudSync />);
  expect(await screen.findByText(/Remote MCP is separate/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Prepare recovery key" }));
  expect(await screen.findByText("sssync1_owner_secret")).toBeInTheDocument();
  const enable = screen.getByRole("button", {
    name: "Confirm and enable sync",
  });
  expect(enable).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Re-enter recovery secret"), {
    target: { value: "sssync1_owner_secret" },
  });
  expect(enable).toBeEnabled();
  fireEvent.click(enable);
  await waitFor(() =>
    expect(screen.getByText("Encrypted sync is on.")).toBeInTheDocument(),
  );
  expect(fetch).toHaveBeenCalledWith(
    "/api/v1/sync/enable",
    expect.objectContaining({ method: "POST" }),
  );
});

it("offers recovery on a fresh enrolled device with no local key", async () => {
  let recovered = false;
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/recover")) recovered = true;
      return Promise.resolve(
        new Response(
          JSON.stringify({
            enabled: recovered,
            paused: false,
            rotation_pending: false,
            summaries: true,
            findings_notes: false,
            detailed_evidence: false,
            key_configured: recovered,
            pending: 0,
            cursor: 1,
            replicas: 1,
            replica_items: [
              {
                object_id: "obj_restored",
                source_device_id: "source-device",
                kind: "session_summary",
                epoch: 1,
                revision: 1,
                payload: {
                  label: "Synced snapshot from Source Device",
                  title: "Recovered fixture",
                },
                remote_source: true,
                locally_edited: false,
                deleted: false,
                updated_at: "2026-10-10T18:00:00Z",
              },
            ],
            conflicts: 0,
            bridge: {
              connected: true,
              owner: "owner@example.com",
              device_id: "replacement",
            },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    }),
  );
  render(<CloudSync />);
  const restore = await screen.findByRole("button", {
    name: "Restore on this device",
  });
  expect(restore).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Recovery secret"), {
    target: { value: "sssync1_saved_elsewhere" },
  });
  expect(restore).toBeEnabled();
  fireEvent.click(restore);
  await waitFor(() =>
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/sync/recover",
      expect.objectContaining({ method: "POST" }),
    ),
  );
  expect(await screen.findByText("Encrypted sync is on.")).toBeInTheDocument();
  expect(
    screen.getByText("Synced snapshot from Source Device"),
  ).toBeInTheDocument();
});

it("locks ordinary controls and retries an unresolved key rotation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.endsWith("/rotate-key"))
        return Promise.resolve(
          new Response(
            JSON.stringify({
              recovery_secret: "sssync1_pending_replacement",
              retry_required: true,
            }),
            { status: 202, headers: { "Content-Type": "application/json" } },
          ),
        );
      return Promise.resolve(
        new Response(
          JSON.stringify({
            enabled: true,
            paused: true,
            rotation_pending: true,
            summaries: true,
            findings_notes: true,
            detailed_evidence: false,
            key_configured: true,
            pending: 0,
            cursor: 1,
            replicas: 0,
            conflicts: 0,
            bridge: {
              connected: true,
              owner: "owner@example.com",
              device_id: "device",
            },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    }),
  );
  render(<CloudSync />);
  expect(await screen.findByText("Recovery needed")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Resume" })).toBeDisabled();
  expect(
    screen.getByRole("button", { name: "Disconnect sync" }),
  ).toBeDisabled();
  fireEvent.click(
    screen.getByRole("button", { name: "Retry pending key rotation" }),
  );
  expect(
    await screen.findByText("sssync1_pending_replacement"),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Confirm and enable sync" }),
  ).not.toBeInTheDocument();
  expect(fetch).toHaveBeenCalledWith(
    "/api/v1/sync/rotate-key",
    expect.objectContaining({ method: "POST" }),
  );
});

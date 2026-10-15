#!/usr/bin/env node
// @module MutationCommandTimeout bounds and force-stops one mutation test command.
import { spawn, spawnSync } from "node:child_process";

const [secondsText, executable, ...args] = process.argv.slice(2);
const seconds = Number(secondsText);
if (!Number.isFinite(seconds) || seconds <= 0 || !executable) {
  console.error("usage: run-with-timeout.mjs <seconds> <command> [args...]");
  process.exit(2);
}

const detached = process.platform !== "win32";
const child = spawn(executable, args, { detached, stdio: "inherit" });
let timedOut = false;
let forceTimer;

function stop(signal) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  if (process.platform === "win32") {
    spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], { stdio: "ignore" });
    return;
  }
  try {
    process.kill(-child.pid, signal);
  } catch (error) {
    if (error?.code !== "ESRCH") throw error;
  }
}

const timer = setTimeout(() => {
  timedOut = true;
  console.error(`command exceeded ${seconds}s; terminating its process group`);
  stop("SIGTERM");
  forceTimer = setTimeout(() => stop("SIGKILL"), 500);
}, seconds * 1000);

child.on("error", (error) => {
  clearTimeout(timer);
  console.error(error.message);
  process.exitCode = 1;
});

child.on("exit", (code) => {
  clearTimeout(timer);
  clearTimeout(forceTimer);
  process.exit(timedOut ? 124 : (code ?? 1));
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    stop(signal);
    process.exit(128 + (signal === "SIGINT" ? 2 : 15));
  });
}

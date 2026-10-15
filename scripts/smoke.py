#!/usr/bin/env python3
"""Offline smoke test of a built sessionsift binary in a disposable home.

Checks `version`, `doctor`, and the stdio MCP server: initialize, all ten
read-only tools listed, and one bounded list_sessions call. Never reads the
caller's real transcripts or state.
"""
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile

TOOLS = {"compare_sessions", "get_analytics", "get_context_recommendations", "get_findings",
         "get_recurring_corrections", "get_session", "get_session_metrics", "get_session_timeline",
         "get_turn", "list_sessions"}


def isolated_env(home):
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    return {"PATH": "/usr/bin:/bin", "HOME": str(home), "SESSIONSIFT_HOME": str(home / "state"),
            "CODEX_HOME": str(home / "codex"), "CLAUDE_HOME": str(home / "claude"),
            "CLAUDE_CONFIG_DIR": str(home / "claude"), "SESSIONSIFT_ADDR": f"127.0.0.1:{port}"}


def mcp(binary, env):
    process = subprocess.Popen([binary, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.DEVNULL, env=env, text=True)
    try:
        def call(message):
            process.stdin.write(json.dumps(message) + "\n")
            process.stdin.flush()
            if "id" in message:
                reply = json.loads(process.stdout.readline())
                if reply.get("id") != message["id"] or "error" in reply:
                    raise SystemExit(f"MCP {message['method']} failed")
                return reply["result"]
            return None
        info = call({"jsonrpc": "2.0", "id": 1, "method": "initialize",
                     "params": {"protocolVersion": "2025-11-25", "capabilities": {},
                                "clientInfo": {"name": "sessionsift-smoke", "version": "1"}}})
        call({"jsonrpc": "2.0", "method": "notifications/initialized"})
        tools = call({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})["tools"]
        names = {tool["name"] for tool in tools}
        if names != TOOLS or any(tool.get("outputSchema", {}).get("type") != "object" for tool in tools):
            raise SystemExit("MCP must list exactly the ten read-only tools with object output schemas")
        result = call({"jsonrpc": "2.0", "id": 3, "method": "tools/call",
                       "params": {"name": "list_sessions", "arguments": {"limit": 2}}})
        if result.get("isError") or result.get("structuredContent", {}).get("total") != 0:
            raise SystemExit("list_sessions must answer from the empty disposable library")
        return info["serverInfo"]["version"]
    finally:
        process.stdin.close()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: smoke.py BINARY EXPECTED_VERSION")
    binary, expected = sys.argv[1], sys.argv[2]
    with tempfile.TemporaryDirectory(prefix="sessionsift-smoke-") as folder:
        env = isolated_env(Path(folder))
        version = subprocess.run([binary, "version"], env=env, capture_output=True, text=True,
                                 timeout=20, check=True).stdout.strip()
        if version != expected:
            raise SystemExit(f"binary reports {version}, expected {expected}")
        doctor = subprocess.run([binary, "doctor"], env=env, capture_output=True, text=True, timeout=30)
        if doctor.returncode != 0 or "SessionSift" not in doctor.stdout:
            raise SystemExit("doctor failed in a disposable home")
        server = mcp(binary, env)
        if server != expected:
            raise SystemExit(f"MCP server reports {server}, expected {expected}")
    print(f"Smoke passed: version, doctor and MCP (10 tools) for {expected}")


if __name__ == "__main__":
    main()

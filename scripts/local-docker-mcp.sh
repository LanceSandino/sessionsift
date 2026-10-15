#!/bin/bash
# @module LocalDockerMCP launches the existing local stdio server without a TTY.
# Dependencies: Docker Compose; uses this checkout from any client working directory.
set -euo pipefail
# Resolve only local configuration metadata before making any daemon request.
# Explicitly pin the checked Unix endpoint: an inherited SSH/TCP context must
# never redirect transcript-bearing MCP calls to another machine.
local_docker_endpoint=""
if [[ -n "${DOCKER_CONTEXT:-}" ]]; then
  local_docker_context="$DOCKER_CONTEXT"
elif [[ -n "${DOCKER_HOST:-}" ]]; then
  local_docker_endpoint="$DOCKER_HOST"
else
  local_docker_context="$(docker context show)"
fi
if [[ -z "$local_docker_endpoint" ]]; then
  if [[ ! "$local_docker_context" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$ ]]; then
    printf '%s\n' 'Unexpected Docker context metadata; local MCP refused.' >&2
    exit 2
  fi
  local_docker_endpoint="$(docker context inspect "$local_docker_context" --format '{{.Endpoints.docker.Host}}')"
fi
if [[ ! "$local_docker_endpoint" =~ ^unix:///[a-zA-Z0-9_./-]+$ ]] || [[ ! -S "${local_docker_endpoint#unix://}" ]]; then
  printf '%s\n' 'Local MCP requires an existing local Unix Docker socket; SSH/TCP endpoints are refused.' >&2
  exit 2
fi
unset DOCKER_CONTEXT DOCKER_HOST
local_project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
if [[ $# -gt 1 ]]; then
  printf '%s\n' 'Usage: local-docker-mcp.sh [absolute-source-override.json]' >&2
  exit 2
fi
local_project_name="${SESSIONSIFT_DOCKER_PROJECT:-sessionsift-local}"
if [[ ! "$local_project_name" =~ ^[a-z0-9][a-z0-9_-]*$ ]]; then
  printf '%s\n' 'Invalid local Docker project name.' >&2
  exit 2
fi
local_compose_args=(compose --project-name "$local_project_name" --project-directory "$local_project_root" --env-file /dev/null -f "$local_project_root/compose.local.yaml")
if [[ $# -eq 1 ]]; then
  if [[ "$1" != /* || ! -f "$1" || -L "$1" ]]; then
    printf '%s\n' 'Source override must be an existing, non-symlink absolute file.' >&2
    exit 2
  fi
  local_compose_args+=(-f "$1")
fi
exec docker --host "$local_docker_endpoint" "${local_compose_args[@]}" exec -T agent /app/sessionsift mcp

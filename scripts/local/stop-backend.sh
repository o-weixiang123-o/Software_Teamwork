#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUN_DIR="$ROOT_DIR/.local/run"
CURRENT_STEP="initializing"
STOPPED_COUNT=0

COLOR_RESET=""
COLOR_BLUE=""
COLOR_GREEN=""
COLOR_YELLOW=""
COLOR_RED=""
COLOR_CYAN=""
if [[ -z "${NO_COLOR:-}" && ( -t 1 || "${FORCE_COLOR:-0}" == "1" || "${CLICOLOR_FORCE:-0}" == "1" ) ]]; then
  COLOR_RESET=$'\033[0m'
  COLOR_BLUE=$'\033[1;34m'
  COLOR_GREEN=$'\033[1;32m'
  COLOR_YELLOW=$'\033[1;33m'
  COLOR_RED=$'\033[1;31m'
  COLOR_CYAN=$'\033[1;36m'
fi

log_info() {
  printf '%b%s %s%b\n' "$COLOR_BLUE" "[stop]" "$*" "$COLOR_RESET"
}

log_success() {
  printf '%b%s %s%b\n' "$COLOR_GREEN" "[ok]" "$*" "$COLOR_RESET"
}

log_warn() {
  printf '%b%s %s%b\n' "$COLOR_YELLOW" "[warn]" "$*" "$COLOR_RESET" >&2
}

log_error() {
  printf '%b%s %s%b\n' "$COLOR_RED" "[fail]" "$*" "$COLOR_RESET" >&2
}

log_hint() {
  printf '%b%s %s%b\n' "$COLOR_CYAN" "[hint]" "$*" "$COLOR_RESET" >&2
}

on_exit() {
  status=$?
  if (( status == 0 )); then
    log_success "completed successfully; processed ${STOPPED_COUNT} pid file(s)"
  else
    log_error "failed during ${CURRENT_STEP} (exit ${status})"
    log_hint "Check .local/run/*.pid and running service processes manually."
  fi
}
trap on_exit EXIT

log_info "starting"

if [[ ! -d "$RUN_DIR" ]]; then
  log_info "no .local/run directory; nothing to stop"
  exit 0
fi

shopt -s nullglob
pid_files=("$RUN_DIR"/*.pid)

if (( ${#pid_files[@]} == 0 )); then
  log_info "no pid files found; nothing to stop"
  exit 0
fi

for pid_file in "${pid_files[@]}"; do
  pid="$(cat "$pid_file")"
  name="$(basename "$pid_file" .pid)"
  CURRENT_STEP="stopping $name"
  STOPPED_COUNT=$((STOPPED_COUNT + 1))

  if [[ ! "$pid" =~ ^[0-9]+$ ]]; then
    log_warn "removing invalid pid file for $name"
    rm -f "$pid_file"
    continue
  fi

  log_info "stopping $name"
  if kill -0 -- "-$pid" 2>/dev/null; then
    kill -TERM -- "-$pid" 2>/dev/null || true
    for _ in {1..25}; do
      kill -0 -- "-$pid" 2>/dev/null || break
      sleep 0.2
    done
    kill -0 -- "-$pid" 2>/dev/null && kill -KILL -- "-$pid" 2>/dev/null || true
  elif kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
  else
    log_info "$name was not running"
  fi
  rm -f "$pid_file"
done

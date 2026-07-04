#!/usr/bin/env bash

if [[ -z "${BASH_VERSION:-}" ]]; then
  echo "scripts/config/load-profile.sh requires bash" >&2
  exit 2
fi

_CONFIG_LOADER_SOURCED=0
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  _CONFIG_LOADER_SOURCED=1
fi

_config_loader_return() {
  local status="$1"
  if (( _CONFIG_LOADER_SOURCED )); then
    return "$status"
  fi
  exit "$status"
}

_config_loader_root() {
  local source_dir
  source_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  printf '%s\n' "${CONFIG_ROOT_DIR:-$source_dir}"
}

_config_loader_abs_path() {
  local root="$1"
  local path="$2"
  if [[ -z "$path" || "$path" == /* ]]; then
    printf '%s\n' "$path"
  else
    printf '%s\n' "$root/$path"
  fi
}

config_profile_load() {
  local root render_dir shell_env_file secret_file secret_args=()
  root="$(_config_loader_root)"
  CONFIG_ROOT_DIR="$root"
  CONFIG_PROFILE="${CONFIG_PROFILE:-dev}"
  if [[ -z "${CONFIG_SECRET_FILE+x}" ]]; then
    CONFIG_SECRET_FILE=".env.local"
  fi

  render_dir="${CONFIG_RENDER_DIR:-$root/.local/config}"
  CONFIG_RENDER_DIR="$render_dir"
  CONFIG_COMPOSE_ENV_FILE="${CONFIG_COMPOSE_ENV_FILE:-$render_dir/$CONFIG_PROFILE.env}"
  shell_env_file="${CONFIG_SHELL_ENV_FILE:-$render_dir/$CONFIG_PROFILE.env.sh}"
  CONFIG_SHELL_ENV_FILE="$shell_env_file"

  if [[ -n "$CONFIG_SECRET_FILE" ]]; then
    secret_file="$(_config_loader_abs_path "$root" "$CONFIG_SECRET_FILE")"
    CONFIG_SECRET_FILE="$secret_file"
    if [[ ! -f "$CONFIG_SECRET_FILE" ]]; then
      echo "missing config secret file: $CONFIG_SECRET_FILE" >&2
      echo "create it from the repository template: cp .env.example .env.local" >&2
      return 1
    fi
    secret_args=(--secret-file "$CONFIG_SECRET_FILE")
  fi

  if ! command -v go >/dev/null 2>&1; then
    echo "go is required to render config profiles with config/ctl" >&2
    echo "install Go 1.25.x or run from an environment with go on PATH" >&2
    return 1
  fi

  mkdir -p "$render_dir"
  (
    cd "$root/config/ctl"
    go run . render --root "$root" --profile "$CONFIG_PROFILE" "${secret_args[@]}" --format dotenv --out "$CONFIG_COMPOSE_ENV_FILE"
  ) || return 1
  (
    cd "$root/config/ctl"
    go run . render --root "$root" --profile "$CONFIG_PROFILE" "${secret_args[@]}" --format shell --out "$CONFIG_SHELL_ENV_FILE"
  ) || return 1

  export CONFIG_ROOT_DIR CONFIG_PROFILE CONFIG_SECRET_FILE CONFIG_RENDER_DIR CONFIG_COMPOSE_ENV_FILE CONFIG_SHELL_ENV_FILE
  export SOFTWARE_TEAMWORK_ROOT="${SOFTWARE_TEAMWORK_ROOT:-$root}"
  # shellcheck disable=SC1090
  . "$CONFIG_SHELL_ENV_FILE"
}

if (( _CONFIG_LOADER_SOURCED )); then
  config_profile_load "$@" || return $?
else
  case "${1:-}" in
    --print-compose-env)
      config_profile_load || exit $?
      printf '%s\n' "$CONFIG_COMPOSE_ENV_FILE"
      ;;
    --help|-h)
      cat <<'EOF'
Usage:
  CONFIG_PROFILE=dev CONFIG_SECRET_FILE=.env.local scripts/config/load-profile.sh --print-compose-env

When sourced, this script renders and sources the selected profile. Variables:
  CONFIG_PROFILE       profile name, default dev
  CONFIG_SECRET_FILE   dotenv secret file, default .env.local; set empty to use process env only
EOF
      ;;
    "")
      config_profile_load || exit $?
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
fi

#!/usr/bin/env python3
"""Diagnose local Docker network sources for this repository."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit


PROXY_ENV_KEYS = (
    "http_proxy",
    "https_proxy",
    "all_proxy",
    "no_proxy",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "NO_PROXY",
)
OUTBOUND_PROXY_ENV_KEYS = (
    "http_proxy",
    "https_proxy",
    "all_proxy",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
)
LOCALHOST_NO_PROXY_ENTRIES = ("localhost", "127.0.0.1", "::1")

CHINA_IMAGES = {
    "postgres": "docker.m.daocloud.io/library/postgres:16-alpine",
    "redis": "docker.m.daocloud.io/library/redis:7-alpine",
    "minio server": "docker.m.daocloud.io/minio/minio:RELEASE.2025-09-07T16-13-09Z",
    "minio mc": "docker.m.daocloud.io/minio/mc:RELEASE.2025-08-13T08-35-41Z",
    "elasticsearch": "docker.m.daocloud.io/docker.elastic.co/elasticsearch/elasticsearch:8.15.3",
    "ragflow deps": "docker.m.daocloud.io/infiniflow/ragflow_deps:51ce6aab",
}

DEFAULT_IMAGES = {
    "postgres": "postgres:16-alpine",
    "redis": "redis:7-alpine",
    "minio server": "minio/minio:RELEASE.2025-09-07T16-13-09Z",
    "minio mc": "minio/mc:RELEASE.2025-08-13T08-35-41Z",
    "elasticsearch": "docker.elastic.co/elasticsearch/elasticsearch:8.15.3",
    "ragflow deps": "infiniflow/ragflow_deps:51ce6aab",
}

DOCKER_HUB_DIRECT_IMAGES = {
    "postgres": "registry-1.docker.io/library/postgres:16-alpine",
    "redis": "registry-1.docker.io/library/redis:7-alpine",
    "minio server": "registry-1.docker.io/minio/minio:RELEASE.2025-09-07T16-13-09Z",
    "minio mc": "registry-1.docker.io/minio/mc:RELEASE.2025-08-13T08-35-41Z",
    "elasticsearch": "docker.elastic.co/elasticsearch/elasticsearch:8.15.3",
    "ragflow deps": "registry-1.docker.io/infiniflow/ragflow_deps:51ce6aab",
}


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Check Docker proxy/mirror state and probe repository image sources."
    )
    parser.add_argument(
        "--profile",
        choices=("china", "default", "dockerhub-direct", "all"),
        default="china",
        help="Which image source set to probe. The china profile is the recommended mainland China path.",
    )
    parser.add_argument(
        "--clean-env",
        action="store_true",
        help="Run Docker probes with shell proxy environment variables removed.",
    )
    parser.add_argument(
        "--skip-network",
        action="store_true",
        help="Only print local Docker/proxy configuration; do not inspect remote manifests.",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=45,
        help="Per-image manifest probe timeout in seconds.",
    )
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    env = clean_subprocess_env(os.environ) if args.clean_env else dict(os.environ)

    print("Docker environment diagnostic")
    print(f"workspace: {root}")
    print(f"clean shell proxy env for probes: {'yes' if args.clean_env else 'no'}")
    print_proxy_state(os.environ)
    print_docker_client_proxy_state(Path.home() / ".docker" / "config.json")
    print_docker_daemon_state(env)

    if args.skip_network:
        return 0

    image_sets = selected_image_sets(args.profile)
    failed = False
    for label, images in image_sets:
        print(f"\n[{label}] manifest probes")
        for name, image in images.items():
            ok, detail = inspect_manifest(image, env=env, timeout=args.timeout)
            status = "OK" if ok else "FAIL"
            print(f"- {status} {name}: {image}")
            if detail:
                print(f"  {detail}")
            failed = failed or not ok

    print("\nRecommendation")
    if args.profile in ("china", "all"):
        print(
            "- For mainland China, run ./scripts/local/dev-up.sh --china or keep local untracked *_IMAGE overrides."
        )
        print(
            "- Do not rely on docker.m.daocloud.io as a daemon registry-mirror unless manifest probes prove mirror-mode works."
        )
    print("- Keep root Compose infra-only; business services run on the host.")
    return 1 if failed else 0


def clean_subprocess_env(source: os._Environ[str] | dict[str, str]) -> dict[str, str]:
    env = dict(source)
    for key in PROXY_ENV_KEYS:
        env.pop(key, None)
    return env


def selected_image_sets(profile: str) -> list[tuple[str, dict[str, str]]]:
    if profile == "china":
        return [("china explicit registry", CHINA_IMAGES)]
    if profile == "default":
        return [("current daemon/default Docker Hub path", DEFAULT_IMAGES)]
    if profile == "dockerhub-direct":
        return [("Docker Hub direct registry-1", DOCKER_HUB_DIRECT_IMAGES)]
    return [
        ("china explicit registry", CHINA_IMAGES),
        ("current daemon/default Docker Hub path", DEFAULT_IMAGES),
        ("Docker Hub direct registry-1", DOCKER_HUB_DIRECT_IMAGES),
    ]


def print_proxy_state(env: os._Environ[str] | dict[str, str]) -> None:
    present = [(key, env[key]) for key in PROXY_ENV_KEYS if env.get(key)]
    print("\nShell proxy variables")
    if not present:
        print("- none")
        return
    for key, value in present:
        print(f"- {key}={redact_proxy_value(value)}")
    missing = missing_localhost_no_proxy_entries(env)
    if missing:
        print(
            "- warning: shell proxy is set but NO_PROXY/no_proxy does not cover "
            f"{', '.join(missing)}; local health checks may hit the proxy. "
            "Use curl --noproxy '*' or set NO_PROXY=localhost,127.0.0.1,::1."
        )


def print_docker_client_proxy_state(config_path: Path) -> None:
    print("\nDocker client proxy config")
    if not config_path.exists():
        print("- none (~/.docker/config.json not found)")
        return
    try:
        config = json.loads(config_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"- unreadable: {exc}")
        return
    proxies = config.get("proxies")
    if not proxies:
        print("- none")
        return
    print(f"- configured keys: {', '.join(sorted(proxies.keys()))}")


def print_docker_daemon_state(env: dict[str, str]) -> None:
    print("\nDocker daemon registry mirrors")
    ok, output = run_command(
        ["docker", "info", "--format", "{{json .RegistryConfig.Mirrors}}"],
        env=env,
        timeout=20,
    )
    if ok:
        print(f"- active: {output.strip() or '[]'}")
    else:
        print(f"- active: unavailable ({summarize_output(output)})")

    daemon_json = Path("/etc/docker/daemon.json")
    if not daemon_json.exists():
        print("- /etc/docker/daemon.json: not found")
        return
    try:
        config = json.loads(daemon_json.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"- /etc/docker/daemon.json: unreadable ({exc})")
        return
    mirrors = config.get("registry-mirrors", [])
    print(f"- /etc/docker/daemon.json registry-mirrors: {json.dumps(mirrors, ensure_ascii=False)}")


def inspect_manifest(image: str, *, env: dict[str, str], timeout: int) -> tuple[bool, str]:
    ok, output = run_command(
        ["docker", "buildx", "imagetools", "inspect", image],
        env=env,
        timeout=timeout,
    )
    if ok:
        return True, ""
    return False, summarize_output(output)


def run_command(command: list[str], *, env: dict[str, str], timeout: int) -> tuple[bool, str]:
    try:
        completed = subprocess.run(
            command,
            check=False,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=timeout,
        )
    except FileNotFoundError as exc:
        return False, str(exc)
    except subprocess.TimeoutExpired as exc:
        output = exc.stdout or ""
        if isinstance(output, bytes):
            output = output.decode(errors="replace")
        return False, f"timeout after {timeout}s\n{output}"
    return completed.returncode == 0, completed.stdout


def summarize_output(output: str, *, limit: int = 260) -> str:
    lines = [strip_ansi(line).strip() for line in output.splitlines() if line.strip()]
    if not lines:
        return "no output"
    text = lines[-1]
    if len(text) > limit:
        text = text[: limit - 3] + "..."
    return text


def strip_ansi(value: str) -> str:
    return re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", value)


def missing_localhost_no_proxy_entries(env: os._Environ[str] | dict[str, str]) -> list[str]:
    if not any(env.get(key) for key in OUTBOUND_PROXY_ENV_KEYS):
        return []
    entries = normalized_no_proxy_entries(env)
    if "*" in entries:
        return []
    return [entry for entry in LOCALHOST_NO_PROXY_ENTRIES if entry not in entries]


def normalized_no_proxy_entries(env: os._Environ[str] | dict[str, str]) -> set[str]:
    values: list[str] = []
    for key in ("no_proxy", "NO_PROXY"):
        raw = env.get(key)
        if raw:
            values.extend(part.strip().lower() for part in raw.split(",") if part.strip())
    return set(values)


def redact_proxy_value(value: str) -> str:
    parsed = urlsplit(value)
    if not parsed.scheme or not parsed.netloc:
        return value
    host = parsed.hostname or ""
    port = f":{parsed.port}" if parsed.port else ""
    username = "***@" if parsed.username or parsed.password else ""
    return urlunsplit((parsed.scheme, f"{username}{host}{port}", "", "", ""))


if __name__ == "__main__":
    sys.exit(main())

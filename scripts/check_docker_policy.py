#!/usr/bin/env python3
"""Verify repository Docker and Compose files follow local Docker policy."""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path


SCAN_ROOTS = ("deploy", "services", "apps")
EXPECTED_IMAGE_DEFAULTS = {
    "POSTGRES_IMAGE": "postgres:16-alpine",
    "REDIS_IMAGE": "redis:7-alpine",
    "QDRANT_IMAGE": "qdrant/qdrant:v1.18.2",
    "MINIO_IMAGE": "minio/minio:RELEASE.2025-09-07T16-13-09Z",
    "MINIO_MC_IMAGE": "minio/mc:RELEASE.2025-08-13T08-35-41Z",
    "KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE": "docker.elastic.co/elasticsearch/elasticsearch:8.15.3",
}
LOCAL_COMPOSE_FILE = Path("deploy/docker-compose.yml")
ALLOWED_DEFAULT_COMPOSE_SERVICES = ("postgres", "redis", "qdrant", "minio", "minio-init", "elasticsearch")
DISALLOWED_DEFAULT_COMPOSE_SERVICES = (
    "migrate-auth",
    "migrate-file",
    "migrate-knowledge",
    "migrate-qa",
    "migrate-document",
    "migrate-ai-gateway",
    "seed-local",
    "seed-local-ai",
    "auth",
    "file",
    "parser",
    "knowledge",
    "qa",
    "document",
    "ai-gateway",
    "gateway",
)
IGNORED_SCAN_DIRS = {
    ".git",
    ".local",
    ".venv",
    "__pycache__",
    "node_modules",
    "dist",
    "build",
}
DISALLOWED_BUSINESS_DOCKER_ARTIFACTS = (
    (
        re.compile(r"^(?:services|apps)/.*/Dockerfile(?:\..*)?$"),
        "business service Dockerfile",
    ),
    (
        re.compile(r"^(?:services|apps)/.*(?:docker-compose|compose)[^/]*\.ya?ml$"),
        "service-level Compose file",
    ),
    (
        re.compile(r"^deploy/.*(?:docker-compose|compose)[^/]*\.ya?ml$"),
        "non-root deploy Compose file",
    ),
)
GO_PROXY_ARG = "ARG GOPROXY=https://proxy.golang.org,direct"
GO_SUMDB_ARG = "ARG GOSUMDB=sum.golang.org"
GO_PROXY_COMPOSE = "GOPROXY: ${GO_DOCKER_GOPROXY:-https://proxy.golang.org,direct}"
GO_SUMDB_COMPOSE = "GOSUMDB: ${GO_DOCKER_GOSUMDB:-sum.golang.org}"


def verify_docker_policy(root: Path) -> list[str]:
    issues: list[str] = []
    issues.extend(validate_no_business_docker_artifacts(root))
    for dockerfile in discover_dockerfiles(root):
        issues.extend(validate_dockerfile(root, dockerfile))
    for compose_file in discover_compose_files(root):
        issues.extend(validate_compose_file(root, compose_file))
    issues.extend(validate_env_example(root))
    return issues


def validate_no_business_docker_artifacts(root: Path) -> list[str]:
    issues: list[str] = []
    for current_root, dirs, files in os.walk(root):
        dirs[:] = [directory for directory in dirs if directory not in IGNORED_SCAN_DIRS]
        current = Path(current_root)
        for filename in files:
            path = current / filename
            rel = path.relative_to(root).as_posix()
            if rel == LOCAL_COMPOSE_FILE.as_posix():
                continue
            for pattern, label in DISALLOWED_BUSINESS_DOCKER_ARTIFACTS:
                if pattern.match(rel):
                    issues.append(f"{rel}: {label} is not allowed; local Docker is infra-only")
                    break
    return issues


def should_skip_dockerfile(rel: str) -> bool:
    return False


def should_skip_compose(rel: str) -> bool:
    return False


def discover_dockerfiles(root: Path) -> list[Path]:
    paths: list[Path] = []
    for scan_root in SCAN_ROOTS:
        directory = root / scan_root
        if not directory.exists():
            continue
        for path in directory.rglob("Dockerfile*"):
            if not path.is_file() or ".git" in path.parts:
                continue
            rel = path.relative_to(root).as_posix()
            if should_skip_dockerfile(rel):
                continue
            paths.append(path)
    return sorted(paths)


def discover_compose_files(root: Path) -> list[Path]:
    paths: list[Path] = []
    for scan_root in SCAN_ROOTS:
        directory = root / scan_root
        if not directory.exists():
            continue
        for pattern in ("docker-compose*.yml", "docker-compose*.yaml", "compose*.yml", "compose*.yaml"):
            for path in directory.rglob(pattern):
                if not path.is_file() or ".git" in path.parts:
                    continue
                rel = path.relative_to(root).as_posix()
                if should_skip_compose(rel):
                    continue
                paths.append(path)
    return sorted(set(paths))


def validate_dockerfile(root: Path, dockerfile: Path) -> list[str]:
    rel = dockerfile.relative_to(root).as_posix()
    content = dockerfile.read_text(encoding="utf-8")
    arg_defaults = collect_arg_defaults(content)
    issues: list[str] = []

    if re.search(r"(?m)^\s*#\s*syntax=", content):
        issues.append(
            f"{rel}: do not require an external Dockerfile frontend; broken daemon mirrors can fail before local Dockerfile logic runs"
        )

    if "GOSUMDB=off" in content or re.search(r"GOSUMDB\s*[:=]\s*off\b", content):
        issues.append(f"{rel}: must not disable Go checksum verification with GOSUMDB=off")

    from_images = collect_base_from_images(content)
    if from_images and "ARG IMAGE_REGISTRY_PREFIX=" not in content:
        issues.append(f"{rel}: Dockerfiles with external FROM images must define ARG IMAGE_REGISTRY_PREFIX=")

    for line_no, image in from_images:
        if image == "scratch":
            continue
        full_image_arg = parse_full_image_arg(image)
        if "${IMAGE_REGISTRY_PREFIX}" not in image:
            issues.append(f"{rel}:{line_no}: base image `{image}` must use ${{IMAGE_REGISTRY_PREFIX}}")
        image_without_prefix = image.replace("${IMAGE_REGISTRY_PREFIX}", "")
        if full_image_arg is not None:
            default_image = arg_defaults.get(full_image_arg, "")
            if not default_image:
                issues.append(f"{rel}:{line_no}: base image `{image}` must have a pinned ARG default")
            image_without_prefix = default_image
        if uses_latest_tag(image_without_prefix):
            issues.append(f"{rel}:{line_no}: base image `{image}` must not use latest")
        if not has_explicit_tag_or_digest(image_without_prefix):
            issues.append(f"{rel}:{line_no}: base image `{image}` must use an explicit tag or digest")

    if is_go_dockerfile(content):
        issues.extend(validate_go_dockerfile(rel, content))
    if is_parser_dockerfile(rel, content):
        issues.append(f"{rel}: services/parser is retired; use services/knowledge-runtime instead")
    if dockerfile.name == "Dockerfile.host" and "ARG POSTGRES_VERSION=16-alpine" not in content:
        issues.append(f"{rel}: QA host Dockerfile must keep ARG POSTGRES_VERSION=16-alpine")

    if not (dockerfile.parent / ".dockerignore").exists():
        issues.append(f"{rel}: Docker build context must have a sibling .dockerignore")

    return issues


def collect_arg_defaults(content: str) -> dict[str, str]:
    defaults: dict[str, str] = {}
    for line in content.splitlines():
        match = re.match(r"^\s*ARG\s+([A-Z0-9_]+)=(\S+)\s*$", line)
        if match:
            defaults[match.group(1)] = match.group(2)
    return defaults


def parse_full_image_arg(image: str) -> str | None:
    match = re.match(r"^\$\{([A-Z0-9_]+)\}$", image)
    if not match:
        match = re.match(r"^\$\{IMAGE_REGISTRY_PREFIX\}\$\{([A-Z0-9_]+)\}$", image)
        if not match:
            return None
    return match.group(1)


def collect_base_from_images(content: str) -> list[tuple[int, str]]:
    stage_aliases: set[str] = set()
    from_images: list[tuple[int, str]] = []
    for line_no, line in enumerate(content.splitlines(), start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        tokens = stripped.split()
        if not tokens or tokens[0].upper() != "FROM":
            continue

        image_index = 1
        while image_index < len(tokens) and tokens[image_index].startswith("--"):
            image_index += 1
        if image_index >= len(tokens):
            continue

        image = tokens[image_index]
        if image.lower() not in stage_aliases:
            from_images.append((line_no, image))

        for index, token in enumerate(tokens):
            if token.upper() == "AS" and index + 1 < len(tokens):
                stage_aliases.add(tokens[index + 1].lower())
                break
    return from_images


def is_go_dockerfile(content: str) -> bool:
    return "golang:" in content or "GOPROXY" in content or "go build" in content


def is_parser_dockerfile(rel: str, content: str) -> bool:
    return rel.startswith("services/parser/") or "parser-service" in content


def validate_go_dockerfile(rel: str, content: str) -> list[str]:
    issues: list[str] = []
    required = (
        GO_PROXY_ARG,
        GO_SUMDB_ARG,
        "ARG ALPINE_MIRROR=",
        "--mount=type=cache,target=/go/pkg/mod",
        "--mount=type=cache,target=/root/.cache/go-build",
    )
    for needle in required:
        if needle not in content:
            issues.append(f"{rel}: missing Go Docker build policy `{needle}`")
    if "apk add" in content and "--mount=type=cache,target=/var/cache/apk" not in content:
        issues.append(f"{rel}: apk installs must use a BuildKit cache mount for /var/cache/apk")
    return issues


def validate_compose_file(root: Path, compose_file: Path) -> list[str]:
    rel = compose_file.relative_to(root).as_posix()
    content = compose_file.read_text(encoding="utf-8")
    issues: list[str] = []

    for line_no, image in collect_compose_images(content):
        if uses_latest_tag(image):
            issues.append(f"{rel}:{line_no}: Compose image `{image}` must not use latest")
        issues.extend(validate_compose_image_default(rel, line_no, image))

    if "GOSUMDB=off" in content or re.search(r"GOSUMDB\s*:\s*(?:\$\{[^}]*:-)?off\b", content):
        issues.append(f"{rel}: must not disable Go checksum verification with GOSUMDB=off")

    if "build:" in content:
        issues.append(f"{rel}: Compose must not use `build:`; local Docker is pull-only infrastructure")
    if "GOPROXY:" in content and GO_PROXY_COMPOSE not in content:
        issues.append(f"{rel}: Go build args must default to `{GO_PROXY_COMPOSE}`")
    if "GOSUMDB:" in content and GO_SUMDB_COMPOSE not in content:
        issues.append(f"{rel}: Go build args must default to `{GO_SUMDB_COMPOSE}`")

    if rel == LOCAL_COMPOSE_FILE.as_posix():
        issues.extend(validate_local_compose(rel, content))

    return issues


def collect_compose_images(content: str) -> list[tuple[int, str]]:
    images: list[tuple[int, str]] = []
    for line_no, line in enumerate(content.splitlines(), start=1):
        match = re.match(r"^\s*image:\s*(.+?)\s*(?:#.*)?$", line)
        if not match:
            continue
        image = match.group(1).strip().strip("'\"")
        images.append((line_no, image))
    return images


def validate_compose_image_default(rel: str, line_no: int, image: str) -> list[str]:
    issues: list[str] = []
    for variable, expected_default in EXPECTED_IMAGE_DEFAULTS.items():
        expected_expr = f"${{{variable}:-{expected_default}}}"
        if variable in image:
            if image != expected_expr:
                issues.append(
                    f"{rel}:{line_no}: `{variable}` must default to pinned `{expected_default}`"
                )
            return issues
        if image == expected_default:
            issues.append(
                f"{rel}:{line_no}: `{expected_default}` must be exposed through `{expected_expr}`"
            )
            return issues
    if re.match(r"^[^$].*:[^@]+$", image) and not image.startswith("${"):
        issues.append(
            f"{rel}:{line_no}: Compose image `{image}` must be exposed through a pinned override variable"
        )
    return issues


def validate_local_compose(rel: str, content: str) -> list[str]:
    issues: list[str] = []
    services = extract_compose_services(content)
    default_services = tuple(name for name, profiles in services.items() if not profiles)
    default_service_set = set(default_services)
    if default_service_set != set(ALLOWED_DEFAULT_COMPOSE_SERVICES):
        issues.append(
            f"{rel}: local Docker default must only define infrastructure services "
            f"{', '.join(ALLOWED_DEFAULT_COMPOSE_SERVICES)}; found {', '.join(default_services) or '(none)'}"
        )
    for service in DISALLOWED_DEFAULT_COMPOSE_SERVICES:
        if service in default_service_set:
            issues.append(f"{rel}: business service `{service}` must run on the host, not in default local Docker")

    for service, profiles in services.items():
        if service in default_service_set:
            continue
        if profiles:
            issues.append(f"{rel}: profile service `{service}` is not allowed by local Docker policy")
        else:
            issues.append(f"{rel}: unexpected local Docker service `{service}`")

    return issues


def extract_compose_services(content: str) -> dict[str, tuple[str, ...]]:
    services: dict[str, tuple[str, ...]] = {}
    for service, block in extract_compose_service_blocks(content).items():
        services[service] = extract_profiles_from_block(block)
    return services


def extract_compose_service_blocks(content: str) -> dict[str, list[tuple[int, str]]]:
    services: dict[str, list[tuple[int, str]]] = {}
    in_services = False
    current_service: str | None = None
    for line_no, line in enumerate(content.splitlines(), start=1):
        if re.match(r"^services:\s*$", line):
            in_services = True
            current_service = None
            continue
        if in_services and re.match(r"^[A-Za-z0-9_.-]+:\s*$", line):
            break
        if not in_services:
            continue
        service_match = re.match(r"^  ([A-Za-z0-9_.-]+):\s*$", line)
        if service_match:
            current_service = service_match.group(1)
            services[current_service] = []
            continue
        if current_service is not None:
            services[current_service].append((line_no, line))
    return services


def extract_profiles_from_block(block: list[tuple[int, str]]) -> tuple[str, ...]:
    for index, (_line_no, line) in enumerate(block):
        profile_match = re.match(r"^    profiles:\s*(.*?)\s*$", line)
        if not profile_match:
            continue
        inline_profiles = profile_match.group(1)
        if inline_profiles:
            return parse_profiles(inline_profiles)
        profiles: list[str] = []
        for _child_line_no, child in block[index + 1 :]:
            if re.match(r"^    [A-Za-z0-9_.-]+:", child):
                break
            list_match = re.match(r"^      -\s*(.+?)\s*$", child)
            if list_match:
                profiles.append(list_match.group(1).strip().strip("'\""))
        return tuple(profiles)
    return ()


def parse_profiles(raw: str) -> tuple[str, ...]:
    raw = raw.strip()
    inline = re.match(r"^\[(.*)\]$", raw)
    if inline:
        return tuple(
            part.strip().strip("'\"")
            for part in inline.group(1).split(",")
            if part.strip()
        )
    return (raw.strip("'\""),)


def validate_env_example(root: Path) -> list[str]:
    env_file = root / "deploy" / ".env.example"
    if not env_file.exists():
        return ["deploy/.env.example is required for local startup defaults"]

    content = env_file.read_text(encoding="utf-8")
    values = parse_env_file(content)
    issues: list[str] = []

    for key, expected_default in EXPECTED_IMAGE_DEFAULTS.items():
        value = values.get(key)
        if value is not None and value != expected_default:
            issues.append(
                f"deploy/.env.example: `{key}` must default to official pinned `{expected_default}` under the official-by-default source policy; use ./scripts/local/dev-up.sh --china or local deploy/.env overrides"
            )

    for key, value in values.items():
        if key.endswith("_IMAGE") and uses_latest_tag(value):
            issues.append(f"deploy/.env.example: `{key}` must not use latest")
    if values.get("HF_ENDPOINT") == "https://hf-mirror.com":
        issues.append(
            "deploy/.env.example: `HF_ENDPOINT=https://hf-mirror.com` must not be active by default; use run-knowledge-parse-stack.sh --china or local deploy/.env overrides"
        )
    if values.get("GO_DOCKER_GOSUMDB") == "off":
        issues.append("deploy/.env.example: must not disable Go checksum verification")

    return issues


def parse_env_file(content: str) -> dict[str, str]:
    values: dict[str, str] = {}
    for line in content.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, value = stripped.split("=", 1)
        values[key.strip()] = value.strip().strip("'\"")
    return values


def uses_latest_tag(image: str) -> bool:
    return ":latest" in image or ":-latest" in image or image.endswith(":latest")


def has_explicit_tag_or_digest(image: str) -> bool:
    if "@" in image:
        return True
    last_component = image.rsplit("/", maxsplit=1)[-1]
    return ":" in last_component


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root",
        type=Path,
        default=Path.cwd(),
        help="repository root; defaults to current working directory",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    root = args.root.resolve()
    issues = verify_docker_policy(root)
    if issues:
        for issue in issues:
            print(f"- {issue}", file=sys.stderr)
        return 1
    print("Docker policy checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

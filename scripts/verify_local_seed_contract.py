#!/usr/bin/env python3
"""Verify the root local/demo seed contract for integration fixtures."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path


SEED_001 = Path("deploy/seeds/001-local-demo-seed.sql")
SEED_002 = Path("deploy/seeds/002-ai-gateway-model-profiles.sql")
SEED_003 = Path("deploy/seeds/003-qa-document-mcp.sql")
SEED_004 = Path("deploy/seeds/004-qa-default-knowledge-base.sql")
CLEANUP_SEED = Path("deploy/seeds/099-local-demo-cleanup.sql")
DEPLOY_README = Path("deploy/README.md")
LOCAL_RUNBOOK = Path("docs/runbooks/local-integration.md")
ENV_EXAMPLE = Path("deploy/.env.example")
GITIGNORE = Path(".gitignore")
AUTH_MIGRATIONS_DIR = Path("services/auth/migrations")
DEV_UP_SCRIPT = Path("scripts/local/dev-up.sh")
RUN_BACKEND_SCRIPT = Path("scripts/local/run-backend.sh")
RUN_KNOWLEDGE_PARSE_STACK_SCRIPT = Path("scripts/local/run-knowledge-parse-stack.sh")
STOP_BACKEND_SCRIPT = Path("scripts/local/stop-backend.sh")

REQUIRED_SEED_001_TOKENS = {
    "Auth local admin user": ["usr_local_admin", "cred_local_admin_password", "urole_local_admin_admin"],
    "Auth local super admin user": [
        "usr_local_super_admin",
        "cred_local_super_admin_password",
        "urole_local_super_admin_super_admin",
    ],
    "Knowledge sample": ["kb_local_demo", "doc_local_demo_seed", "chunk_local_demo_seed_001"],
    "Document sample": [
        "22222222-2222-4222-8222-222222222201",
        "22222222-2222-4222-8222-222222222301",
        "22222222-2222-4222-8222-222222222401",
        "22222222-2222-4222-8222-222222222501",
        "22222222-2222-4222-8222-222222222502",
        "22222222-2222-4222-8222-222222222601",
        "22222222-2222-4222-8222-222222222602",
    ],
    "QA sample": [
        "33333333-3333-4333-8333-333333333301",
        "33333333-3333-4333-8333-333333333401",
        "33333333-3333-4333-8333-333333333402",
        "33333333-3333-4333-8333-333333333501",
        "33333333-3333-4333-8333-333333333502",
    ],
}

REQUIRED_DATABASE_SECTIONS = [
    r"\\connect\s+auth_system",
    r"\\connect\s+knowledge_system",
    r"\\connect\s+document_system",
    r"\\connect\s+qa_system",
]

REQUIRED_AI_TOKENS = [
    "default-chat",
    "default-embedding",
    "default-rerank",
    "http://localhost:11434/v1",
    "cred-local-chat",
    "cred-local-embedding",
    "cred-local-rerank",
    "local-demo-key-v1",
]

REQUIRED_DOCUMENT_MCP_TOKENS = [
    r"\\connect\s+qa_system",
    "33333333-3333-4333-8333-333333333601",
    "'document'",
    "'Document MCP'",
    "'streamable_http'",
    "'http://localhost:8085/mcp'",
    "'Authorization'",
    "'local-seed'",
    "ON CONFLICT (alias) DO UPDATE",
]

REQUIRED_QA_DEFAULT_KB_TOKENS = [
    r"\\connect\s+qa_system",
    "keep QA's default knowledge-base list empty",
    "defaultKnowledgeBaseIds",
    "search all indexed",
    "qa_config_knowledge_bases",
    "kb_local_demo",
    "DELETE FROM qa_config_knowledge_bases",
]

FORBIDDEN_AI_TOKENS = [
    "host.docker.internal",
]

REQUIRED_AUTH_MIGRATION_TOKENS = {
    "QA settings permission": [
        "perm_qa_settings_read",
        "qa:settings:read",
        "perm_qa_settings_write",
        "qa:settings:write",
    ],
    "admin QA settings grant": [
        "rperm_admin_qa_settings_read",
        "rperm_admin_qa_settings_write",
        "'admin', 'qa:settings:read'",
        "'admin', 'qa:settings:write'",
    ],
    "super admin QA settings grant": [
        "rperm_super_qa_settings_read",
        "rperm_super_qa_settings_write",
        "'super_admin', 'qa:settings:read'",
        "'super_admin', 'qa:settings:write'",
    ],
}

REQUIRED_DOC_TOKENS = [
    "唯一默认配置来源",
    "LOCAL_ADMIN_USERNAME=admin",
    "LOCAL_ADMIN_PASSWORD=LocalDemoAdmin#12345",
    "LOCAL_SUPER_ADMIN_USERNAME=superadmin",
    "LOCAL_SUPER_ADMIN_PASSWORD=LocalDemoAdmin#12345",
    "admin / LocalDemoAdmin#12345",
    "superadmin / LocalDemoAdmin#12345",
    "cp deploy/.env.example deploy/.env",
    "Go modules 下载默认读取",
    "源选择采用新策略",
    "旧的大陆优先默认镜像契约已废弃",
    "默认使用官方源",
    "--china",
    "大陆镜像",
    "GOPROXY=https://proxy.golang.org,direct",
    "GOSUMDB=sum.golang.org",
    "./scripts/local/dev-up.sh",
    "./scripts/local/run-backend.sh",
    "down -v",
]

REQUIRED_DEV_UP_TOKENS = [
    "[dev-up]",
    "[ok]",
    "[warn]",
    "[fail]",
    "[hint]",
    "completed successfully",
    "failed during",
    "Check Docker status:",
    "Mainland China network: rerun ./scripts/local/dev-up.sh --china.",
    "checking local tool dependencies",
    "missing required local command(s):",
    "Install Docker, Go, psql, uv, and curl",
    "Install the missing host tool(s)",
    "preparing Knowledge runtime dependencies with China mirrors",
    '--with "nltk>=3.9.4"',
    '--with "huggingface-hub>=1.3.1"',
    "ragflow_deps/download_deps.py --china",
    "--skip-knowledge-runtime-deps",
    "LOCAL_SKIP_KNOWLEDGE_RUNTIME_DEPS",
    "checking Go module settings",
    "--china",
    "using selected default for this run",
    "docker.m.daocloud.io/library/postgres:16-alpine",
    "goose@v3.27.1",
    "psql",
    "INFRA_SERVICES=(postgres redis qdrant minio)",
    "initializing MinIO buckets",
    "--exit-code-from minio-init",
    "docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs minio-init",
    "001-local-demo-seed.sql",
    "002-ai-gateway-model-profiles.sql",
    "003-qa-document-mcp.sql",
    "004-qa-default-knowledge-base.sql",
    "--wait",
    "--wait-timeout",
    "initialize_qdrant_collection",
    "QDRANT_URL",
    "QDRANT_COLLECTION",
    "EMBEDDING_DIMENSION",
    "Cosine",
    "AUTH_DATABASE_URL",
    "FILE_DATABASE_URL",
    "KNOWLEDGE_DATABASE_URL",
    "QA_DATABASE_URL",
    "DOCUMENT_DATABASE_URL",
    "AI_GATEWAY_DATABASE_URL",
    "POSTGRES_ADMIN_URL",
    "cmd/local-seed",
    "applying AI Gateway local provider seed",
    "AI_GATEWAY_LOCAL_PROVIDER_BASE_URL",
    "AI_GATEWAY_LOCAL_PROVIDER_API_KEY",
    "AI_GATEWAY_LOCAL_CHAT_MODEL",
]

REQUIRED_RUN_BACKEND_TOKENS = [
    "[backend]",
    "[ok]",
    "[warn]",
    "[fail]",
    "[hint]",
    "completed successfully",
    "failed during",
    "Check service logs under .local/logs/",
    "setsid",
    "python3",
    "os.setsid()",
    "go mod download",
    "checking Go modules",
    "--china",
    "using selected default for this run",
    "AI_GATEWAY_LOCAL_CHAT_MODEL",
    "DOCUMENT_AI_GATEWAY_MODEL",
    "LOCAL_GO_MOD_DOWNLOAD_TIMEOUT_SECONDS",
    "go mod download timed out",
    "Go module download failed before backend startup completed.",
    "Current effective Go module settings:",
    "LOCAL_BACKEND_STARTUP_CHECK_SECONDS",
    "backend startup failed",
    "The failed service log tails are shown below.",
    "Backend process startup failed after services were forked.",
    "instead of treating it as a Go module mirror issue.",
    "auth",
    "file",
    "knowledge",
    "./cmd/adapter",
    'go run "$go_target"',
    "ai-gateway",
    "qa",
    "document",
    "gateway",
]

REQUIRED_RUN_KNOWLEDGE_PARSE_STACK_TOKENS = [
    "knowledge parse stack startup: starting Knowledge parse stack",
    "setsid or python3 is required",
    "os.setsid()",
    "deploy/Dockerfile.elasticsearch-local",
    "KNOWLEDGE_RUNTIME_ES_URL",
    "KNOWLEDGE_RUNTIME_START_ELASTICSEARCH",
    "KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE",
    "HF_ENDPOINT=https://hf-mirror.com",
    "docker build",
    "docker run -d",
    "software-teamwork-knowledge-elasticsearch",
    ".local/knowledge-runtime/service_conf.yaml",
    "KNOWLEDGE_RUNTIME_MODEL_API_KEY=<your SiliconFlow key>",
    "KNOWLEDGE_VENDOR_EMBEDDING_ID=BAAI/bge-m3@default@SILICONFLOW",
    "KNOWLEDGE_AUTO_START_INGESTION=true",
]

REQUIRED_STOP_BACKEND_TOKENS = [
    "[stop]",
    "[ok]",
    "[warn]",
    "[fail]",
    "[hint]",
    "completed successfully",
    "failed during",
    "nothing to stop",
    'kill -0 -- "-$pid"',
    'kill -TERM -- "-$pid"',
    'kill -KILL -- "-$pid"',
    "*.container",
    "docker rm -f",
]

REQUIRED_ENV_TOKENS = [
    "UV_DEFAULT_INDEX=https://pypi.org/simple",
    "HF_ENDPOINT=https://hf-mirror.com",
    "GOPROXY=https://proxy.golang.org,direct",
    "GOSUMDB=sum.golang.org",
    "# POSTGRES_IMAGE=docker.m.daocloud.io/library/postgres:16-alpine",
    "# REDIS_IMAGE=docker.m.daocloud.io/library/redis:7-alpine",
    "# QDRANT_IMAGE=docker.m.daocloud.io/qdrant/qdrant:v1.18.2",
    "# MINIO_IMAGE=docker.m.daocloud.io/minio/minio:RELEASE.2025-09-07T16-13-09Z",
    "# MINIO_MC_IMAGE=docker.m.daocloud.io/minio/mc:RELEASE.2025-08-13T08-35-41Z",
    "MCP_TRANSPORT=streamable_http",
    "MCP_SERVER_ALIAS=document",
    "MCP_SERVER_URL=http://localhost:8085/mcp",
    "MCP_SERVER_TOKEN=local-dev-internal-service-token-change-me",
    "MCP_SERVER_TOKEN_HEADER=Authorization",
    "DB_TYPE=postgres",
    "VENDOR_RUNTIME_URL=http://127.0.0.1:9380",
    "VENDOR_RUNTIME_SERVICE_TOKEN=",
    "KNOWLEDGE_RUNTIME_SERVICE_TOKEN=",
    "KNOWLEDGE_AUTO_START_INGESTION=false",
    "# DOC_ENGINE=elasticsearch",
    "KNOWLEDGE_RUNTIME_ES_URL=http://127.0.0.1:9200",
    "KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=auto",
    "KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE=docker.elastic.co/elasticsearch/elasticsearch:8.15.3",
    "AI_GATEWAY_LOCAL_PROVIDER_BASE_URL=https://api.siliconflow.cn/v1",
    "AI_GATEWAY_LOCAL_PROVIDER_API_KEY=",
    "AI_GATEWAY_LOCAL_CHAT_MODEL=deepseek-ai/DeepSeek-V3",
    "AI_GATEWAY_LOCAL_EMBEDDING_MODEL=BAAI/bge-m3",
    "AI_GATEWAY_LOCAL_RERANK_MODEL=BAAI/bge-reranker-v2-m3",
    "DOCUMENT_AI_GATEWAY_MODEL=local-placeholder-chat",
    "KNOWLEDGE_VENDOR_EMBEDDING_ID=BAAI/bge-m3@default@SILICONFLOW",
]

FORBIDDEN_STARTUP_DOC_TOKENS = [
    "export AUTH_DATABASE_URL",
    "export FILE_DATABASE_URL",
    "export KNOWLEDGE_DATABASE_URL",
    "export QA_DATABASE_URL",
    "export DOCUMENT_DATABASE_URL",
    "export AI_GATEWAY_DATABASE_URL",
    "docker compose up --build",
    "docker compose --profile ai",
    "seed-local",
]

FORBIDDEN_PATTERNS = [
    (re.compile(r"sk-[A-Za-z0-9_-]{16,}"), "OpenAI-style API key"),
    (re.compile(r"AKIA[0-9A-Z]{16}"), "AWS access key"),
    (re.compile(r"AIza[0-9A-Za-z_-]{20,}"), "Google API key"),
    (re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH |)PRIVATE KEY-----"), "private key"),
    (re.compile(r"(?i)\bproduction\b.*\bpassword\b"), "production password wording"),
    (re.compile(r"(?i)\bminio(?:_|-)?secret(?:_|-)?key\b\s*[:=]\s*['\"]?[A-Za-z0-9+/]{12,}"), "MinIO secret key"),
]


def verify_local_seed_contract(root: Path) -> list[str]:
    root = root.resolve()
    issues: list[str] = []

    seed_001 = read_required(root, SEED_001, issues)
    seed_002 = read_required(root, SEED_002, issues)
    seed_003 = read_required(root, SEED_003, issues)
    seed_004 = read_required(root, SEED_004, issues)
    cleanup_seed = read_required(root, CLEANUP_SEED, issues)
    auth_migrations = read_required_glob(root, AUTH_MIGRATIONS_DIR, "*.sql", issues)
    deploy_readme = read_required(root, DEPLOY_README, issues)
    runbook = read_required(root, LOCAL_RUNBOOK, issues)
    env_example = read_required(root, ENV_EXAMPLE, issues)
    dev_up_script = read_required(root, DEV_UP_SCRIPT, issues)
    run_backend_script = read_required(root, RUN_BACKEND_SCRIPT, issues)
    run_knowledge_parse_stack_script = read_required(root, RUN_KNOWLEDGE_PARSE_STACK_SCRIPT, issues)
    stop_backend_script = read_required(root, STOP_BACKEND_SCRIPT, issues)
    gitignore = read_required(root, GITIGNORE, issues)

    issues.extend(validate_seed_001(seed_001))
    issues.extend(validate_seed_002(seed_002))
    issues.extend(validate_seed_003(seed_003))
    issues.extend(validate_seed_004(seed_004))
    issues.extend(validate_cleanup_seed(cleanup_seed))
    issues.extend(validate_auth_migrations(auth_migrations))
    issues.extend(
        validate_docs(
            deploy_readme,
            runbook,
            env_example,
            dev_up_script,
            run_backend_script,
            run_knowledge_parse_stack_script,
            stop_backend_script,
        )
    )
    issues.extend(validate_gitignore(gitignore))
    issues.extend(validate_forbidden_content(root))
    return issues


def read_required(root: Path, relative: Path, issues: list[str]) -> str:
    path = root / relative
    try:
        return path.read_text(encoding="utf-8")
    except OSError as exc:
        issues.append(f"{relative} is required but cannot be read: {exc}")
        return ""


def read_required_glob(root: Path, relative: Path, pattern: str, issues: list[str]) -> str:
    path = root / relative
    try:
        files = sorted(path.glob(pattern))
    except OSError as exc:
        issues.append(f"{relative} is required but cannot be read: {exc}")
        return ""
    if not files:
        issues.append(f"{relative} must contain `{pattern}` files")
        return ""

    contents: list[str] = []
    for file in files:
        try:
            contents.append(file.read_text(encoding="utf-8"))
        except OSError as exc:
            issues.append(f"{file.relative_to(root)} is required but cannot be read: {exc}")
    return "\n".join(contents)


def validate_seed_001(content: str) -> list[str]:
    if not content:
        return []
    issues: list[str] = []
    for pattern in REQUIRED_DATABASE_SECTIONS:
        if not re.search(pattern, content):
            issues.append(f"{SEED_001} missing database section matching `{pattern}`")
    for group, tokens in REQUIRED_SEED_001_TOKENS.items():
        for token in tokens:
            if token not in content:
                issues.append(f"{SEED_001} missing {group} token `{token}`")
    if content.count("ON CONFLICT") < 10:
        issues.append(f"{SEED_001} should use ON CONFLICT for deterministic idempotent rows")
    if "file_ref" in content.lower() and "file_ref,\n    filename" not in content:
        issues.append(f"{SEED_001} should keep demo file_ref fields explicitly null")
    return issues


def validate_seed_002(content: str) -> list[str]:
    if not content:
        return []
    issues: list[str] = []
    for token in REQUIRED_AI_TOKENS:
        if token not in content:
            issues.append(f"{SEED_002} missing AI placeholder token `{token}`")
    for token in FORBIDDEN_AI_TOKENS:
        if token in content:
            issues.append(f"{SEED_002} must not use container-only host token `{token}`")
    if content.count("ON CONFLICT") < 2:
        issues.append(f"{SEED_002} should use ON CONFLICT for model profiles and credentials")
    return issues


def validate_seed_003(content: str) -> list[str]:
    if not content:
        return []
    issues: list[str] = []
    for token in REQUIRED_DOCUMENT_MCP_TOKENS:
        if token.startswith(r"\\connect"):
            if not re.search(token, content):
                issues.append(f"{SEED_003} missing database section matching `{token}`")
        elif token not in content:
            issues.append(f"{SEED_003} missing Document MCP token `{token}`")
    if "token_encrypted" not in content or "NULL" not in content:
        issues.append(f"{SEED_003} must keep the Document MCP credential out of PostgreSQL")
    return issues


def validate_seed_004(content: str) -> list[str]:
    if not content:
        return []
    issues: list[str] = []
    for token in REQUIRED_QA_DEFAULT_KB_TOKENS:
        if token.startswith(r"\\connect"):
            if not re.search(token, content):
                issues.append(f"{SEED_004} missing database section matching `{token}`")
        elif token not in content:
            issues.append(f"{SEED_004} missing QA global-search seed token `{token}`")
    return issues


def validate_cleanup_seed(content: str) -> list[str]:
    if not content:
        return []
    issues: list[str] = []
    for token in [
        "usr_local_admin",
        "usr_local_super_admin",
        "doc_local_demo_seed",
        "22222222-2222-4222-8222-222222222301",
        "33333333-3333-4333-8333-333333333301",
        "33333333-3333-4333-8333-333333333601",
        "kb_local_demo",
    ]:
        if token not in content:
            issues.append(f"{CLEANUP_SEED} missing cleanup token `{token}`")
    for table in [
        "mcp_servers",
        "qa_config_knowledge_bases",
        "message_content_blocks",
        "report_section_versions",
        "document_chunks",
        "auth_credentials",
    ]:
        if table not in content:
            issues.append(f"{CLEANUP_SEED} missing cleanup table `{table}`")
    return issues


def validate_auth_migrations(content: str) -> list[str]:
    if not content:
        return []
    issues: list[str] = []
    for group, tokens in REQUIRED_AUTH_MIGRATION_TOKENS.items():
        for token in tokens:
            if token not in content:
                issues.append(f"{AUTH_MIGRATIONS_DIR} missing {group} token `{token}`")
    return issues


def validate_docs(
    deploy_readme: str,
    runbook: str,
    env_example: str,
    dev_up_script: str,
    run_backend_script: str,
    run_knowledge_parse_stack_script: str,
    stop_backend_script: str,
) -> list[str]:
    issues: list[str] = []
    combined = "\n".join([deploy_readme, runbook, env_example])
    for token in REQUIRED_DOC_TOKENS:
        if token not in combined:
            issues.append(f"seed documentation missing `{token}`")
    for token in REQUIRED_ENV_TOKENS:
        if token not in env_example:
            issues.append(f"{ENV_EXAMPLE} missing local default `{token}`")
    for token in FORBIDDEN_STARTUP_DOC_TOKENS:
        if token in combined:
            issues.append(f"startup documentation must not include `{token}`")
    for token in REQUIRED_DEV_UP_TOKENS:
        if token not in dev_up_script:
            issues.append(f"{DEV_UP_SCRIPT} missing local seed runner token `{token}`")
    for token in REQUIRED_RUN_BACKEND_TOKENS:
        if token not in run_backend_script:
            issues.append(f"{RUN_BACKEND_SCRIPT} missing backend startup token `{token}`")
    for token in REQUIRED_RUN_KNOWLEDGE_PARSE_STACK_TOKENS:
        if token not in run_knowledge_parse_stack_script:
            issues.append(f"{RUN_KNOWLEDGE_PARSE_STACK_SCRIPT} missing Knowledge parse stack token `{token}`")
    for token in REQUIRED_STOP_BACKEND_TOKENS:
        if token not in stop_backend_script:
            issues.append(f"{STOP_BACKEND_SCRIPT} missing backend stop token `{token}`")
    return issues


def validate_gitignore(content: str) -> list[str]:
    if not content:
        return []
    issues: list[str] = []
    for token in ["/.local/", "DL_T_673-1999.pdf"]:
        if token not in content:
            issues.append(f"{GITIGNORE} missing local runtime ignore token `{token}`")
    return issues


def validate_forbidden_content(root: Path) -> list[str]:
    issues: list[str] = []
    for relative in [
        SEED_001,
        SEED_002,
        SEED_003,
        SEED_004,
        CLEANUP_SEED,
        DEPLOY_README,
        LOCAL_RUNBOOK,
        ENV_EXAMPLE,
        DEV_UP_SCRIPT,
        RUN_BACKEND_SCRIPT,
        STOP_BACKEND_SCRIPT,
    ]:
        path = root / relative
        if not path.exists():
            continue
        content = path.read_text(encoding="utf-8", errors="replace")
        for pattern, label in FORBIDDEN_PATTERNS:
            if pattern.search(content):
                issues.append(f"{relative} contains forbidden {label} pattern")
    return issues


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "root",
        nargs="?",
        type=Path,
        default=Path("."),
        help="Repository root to verify.",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_arg_parser().parse_args(argv)
    issues = verify_local_seed_contract(args.root)
    if issues:
        print("Local seed contract verification failed:")
        for issue in issues:
            print(f"- {issue}")
        return 1
    print("Local seed contract verification passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

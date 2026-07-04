#!/usr/bin/env python3
"""Verify model/provider calls outside AI Gateway stay behind explicit boundaries."""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path


SCAN_ROOTS = ("services", "scripts", "config", ".github")
TEXT_SUFFIXES = {
    ".go",
    ".py",
    ".sh",
    ".yaml",
    ".yml",
    ".toml",
    ".env",
    ".example",
}
IGNORED_DIRS = {
    ".git",
    ".local",
    ".venv",
    "__pycache__",
    "node_modules",
    "dist",
    "build",
}

DIRECT_PROVIDER_PATTERNS: tuple[tuple[re.Pattern[str], str], ...] = (
    (re.compile(r"\bfrom\s+openai\s+import\b"), "OpenAI Python SDK import"),
    (re.compile(r"\bimport\s+openai\b"), "OpenAI Python SDK import"),
    (re.compile(r"\b(?:OpenAI|AsyncOpenAI|AzureOpenAI|AsyncAzureOpenAI)\s*\("), "OpenAI SDK client construction"),
    (re.compile(r"github\.com/(?:openai|sashabaranov/go-openai)"), "OpenAI Go SDK import"),
    (re.compile(r"https://api\.openai\.com/v1"), "direct OpenAI provider base URL"),
    (re.compile(r"https://api\.siliconflow\.cn/v1"), "direct SiliconFlow provider base URL"),
    (re.compile(r"(?<!internal)/v1/chat/completions"), "direct chat completions endpoint"),
    (re.compile(r"(?<!internal)/v1/embeddings"), "direct embeddings endpoint"),
    (re.compile(r"(?<!internal)/v1/rerank(?:ings)?"), "direct rerank endpoint"),
)


ALLOWLIST_PREFIXES: tuple[tuple[str, str], ...] = (
    ("services/ai-gateway/", "AI Gateway is the only normal provider exit"),
    (
        "services/knowledge-runtime/rag/llm/",
        "vendored Knowledge runtime local/emergency provider fallbacks",
    ),
    ("services/knowledge-runtime/conf/", "vendored provider/model catalog"),
    ("services/knowledge-runtime/test/", "runtime fallback and AI Gateway adapter tests"),
)

ALLOWLIST_FILES: dict[str, str] = {
    "scripts/check_ai_gateway_provider_policy.py": "policy checker owns the direct-provider patterns",
    "scripts/local/render_ai_gateway_local_seed.go": "renders AI Gateway model profiles for local seed",
    "scripts/verify_local_seed_contract.py": "asserts AI Gateway local seed contract",
    "scripts/tests/test_ai_gateway_local_seed_renderer.py": "tests AI Gateway local seed renderer",
    "scripts/tests/test_local_seed_contract.py": "tests AI Gateway local seed contract",
    "scripts/tests/test_local_dev_up_script.py": "tests local seed env overlay examples",
}


def verify_ai_gateway_provider_policy(root: Path) -> list[str]:
    issues: list[str] = []
    for path in discover_scan_files(root):
        rel = path.relative_to(root).as_posix()
        if is_allowlisted(rel):
            continue
        issues.extend(scan_file(root, path))
    return issues


def discover_scan_files(root: Path) -> list[Path]:
    paths: list[Path] = []
    for scan_root in SCAN_ROOTS:
        base = root / scan_root
        if not base.exists():
            continue
        for current_root, dirs, files in os.walk(base):
            dirs[:] = [directory for directory in dirs if directory not in IGNORED_DIRS]
            current = Path(current_root)
            for filename in files:
                path = current / filename
                if should_scan_file(path):
                    paths.append(path)
    return paths


def should_scan_file(path: Path) -> bool:
    if path.name.startswith(".") and path.name != ".env.example":
        return False
    if path.suffix in TEXT_SUFFIXES:
        return True
    return path.name in {".env.example"}


def is_allowlisted(rel: str) -> bool:
    if rel in ALLOWLIST_FILES:
        return True
    if rel.endswith("_test.go") or rel.startswith("scripts/tests/"):
        return True
    if "/test/" in rel or "/tests/" in rel:
        return True
    return any(rel.startswith(prefix) for prefix, _ in ALLOWLIST_PREFIXES)


def scan_file(root: Path, path: Path) -> list[str]:
    rel = path.relative_to(root).as_posix()
    try:
        text = path.read_text(encoding="utf-8")
    except UnicodeDecodeError:
        return []
    issues: list[str] = []
    for line_no, line in enumerate(text.splitlines(), start=1):
        for pattern, label in DIRECT_PROVIDER_PATTERNS:
            if pattern.search(line):
                issues.append(f"{rel}:{line_no}: direct provider exit matched {label}; route model calls through services/ai-gateway or add an explicit allowlist rationale")
    return issues


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    issues = verify_ai_gateway_provider_policy(args.root.resolve())
    if issues:
        for issue in issues:
            print(issue, file=sys.stderr)
        return 1
    print("AI Gateway provider policy checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

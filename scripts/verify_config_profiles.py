#!/usr/bin/env python3
"""Verify committed configuration profiles."""

from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path


def verify_config_profiles(root: Path) -> list[str]:
    ctl_dir = root / "config" / "ctl"
    if not ctl_dir.is_dir():
        return ["config/ctl is required for profile verification"]
    result = subprocess.run(
        ["go", "run", ".", "verify", "--root", str(root)],
        cwd=ctl_dir,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode == 0:
        return []
    issues = [line[2:] for line in result.stderr.splitlines() if line.startswith("- ")]
    if issues:
        return issues
    return [result.stderr.strip() or result.stdout.strip() or "config profile verification failed"]


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
    issues = verify_config_profiles(args.root.resolve())
    if issues:
        for issue in issues:
            print(f"- {issue}", file=sys.stderr)
        return 1
    print("Config profile checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

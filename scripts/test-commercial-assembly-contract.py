from __future__ import annotations

import copy
import json
import pathlib
import subprocess
import sys
import tempfile
from typing import Callable


ROOT = pathlib.Path(__file__).resolve().parents[1]
CHECKER = ROOT / "scripts" / "check-commercial-assembly-contract.py"
LOCK = ROOT / "packages" / "edition" / "fixtures" / "core-lock.valid.json"
OVERLAY = ROOT / "packages" / "edition" / "fixtures" / "overlay-allowlist.valid.json"


def run(lock: pathlib.Path, overlay: pathlib.Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [
            sys.executable,
            str(CHECKER),
            "--schema-only",
            "--core-lock",
            str(lock),
            "--overlay",
            str(overlay),
        ],
        cwd=ROOT,
        text=True,
        encoding="utf-8",
        capture_output=True,
        check=False,
    )


def write_json(path: pathlib.Path, value: dict) -> None:
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def main() -> int:
    valid = run(LOCK, OVERLAY)
    if valid.returncode != 0:
        print(valid.stdout, end="")
        print(valid.stderr, end="", file=sys.stderr)
        return 1

    baseline_lock = json.loads(LOCK.read_text(encoding="utf-8"))
    baseline_overlay = json.loads(OVERLAY.read_text(encoding="utf-8"))
    mutations: dict[str, Callable[[dict, dict], None]] = {
        "core-commit-drift": lambda lock, overlay: overlay.__setitem__(
            "coreCommit", "3" * 40
        ),
        "mutable-commit": lambda lock, overlay: lock.__setitem__("coreCommit", "main"),
        "path-traversal": lambda lock, overlay: overlay["files"][0].__setitem__(
            "destination", "../escape.txt"
        ),
        "windows-path": lambda lock, overlay: overlay["files"][0].__setitem__(
            "destination", "C:/escape.txt"
        ),
        "backslash-path": lambda lock, overlay: overlay["files"][0].__setitem__(
            "destination", "assembly\\escape.txt"
        ),
        "protected-core-path": lambda lock, overlay: overlay["files"][0].__setitem__(
            "destination", "go.mod"
        ),
        "undeclared-replacement": lambda lock, overlay: (
            overlay["files"][0].__setitem__("operation", "replace"),
            overlay["files"][0].__setitem__("destination", "apps/api/main.go"),
        ),
        "slot-as-add": lambda lock, overlay: overlay["files"][0].__setitem__(
            "destination", "apps/web/src/edition/selected-entry.ts"
        ),
        "case-collision": lambda lock, overlay: overlay["files"].append(
            {
                "source": "overlay/fixture-two.txt",
                "destination": "ASSEMBLY/FIXTURE.TXT",
                "operation": "add",
                "sha256": "8" * 64,
            }
        ),
        "unknown-field": lambda lock, overlay: overlay.__setitem__("copyEverything", True),
    }

    with tempfile.TemporaryDirectory(prefix="cineweave-assembly-contract-") as temp:
        directory = pathlib.Path(temp)
        for name, mutate in mutations.items():
            lock = copy.deepcopy(baseline_lock)
            overlay = copy.deepcopy(baseline_overlay)
            mutate(lock, overlay)
            lock_path = directory / f"{name}-lock.json"
            overlay_path = directory / f"{name}-overlay.json"
            write_json(lock_path, lock)
            write_json(overlay_path, overlay)
            result = run(lock_path, overlay_path)
            if result.returncode == 0:
                print(f"Assembly checker accepted invalid case {name}", file=sys.stderr)
                return 1

    print(f"Commercial Assembly contract regression checks passed: invalidCases={len(mutations)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

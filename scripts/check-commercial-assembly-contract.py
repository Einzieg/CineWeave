from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
import subprocess
import sys
from typing import Any


ROOT = pathlib.Path(__file__).resolve().parents[1]
DEFAULT_LOCK = ROOT / "packages" / "edition" / "fixtures" / "core-lock.valid.json"
DEFAULT_OVERLAY = (
    ROOT / "packages" / "edition" / "fixtures" / "overlay-allowlist.valid.json"
)
DEFAULT_SLOTS = ROOT / "packages" / "edition" / "overlay-slots.v1.json"
CORE_LOCK_SCHEMA = ROOT / "packages" / "edition" / "core-lock.schema.json"
OVERLAY_SCHEMA = ROOT / "packages" / "edition" / "overlay-allowlist.schema.json"

SHA256_PATTERN = re.compile(r"^[0-9a-f]{64}$")
COMMIT_PATTERN = re.compile(r"^(?:[0-9a-f]{40}|[0-9a-f]{64})$")
WINDOWS_RESERVED_SEGMENT = re.compile(
    r"^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\..*)?$",
    re.IGNORECASE,
)

CORE_HASH_PATHS = {
    "editionContractSha256": "packages/edition/edition.v2.json",
    "ddlOwnerManifestSha256": "packages/edition/ddl-owners.v1.json",
    "overlaySlotsSha256": "packages/edition/overlay-slots.v1.json",
    "releaseManifestSchemaSha256": "packages/edition/release-manifest.schema.json",
    "openAPIContractSha256": "packages/openapi/openapi.yaml",
    "eventCatalogSha256": "packages/events/catalog.yaml",
}


class AssemblyContractError(ValueError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssemblyContractError(message)


def load_json(path: pathlib.Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise AssemblyContractError(f"cannot read JSON {path}: {exc}") from exc
    require(isinstance(value, dict), f"{path} must contain a JSON object")
    return value


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def validate_exact_keys(
    value: dict[str, Any],
    required: set[str],
    location: str,
) -> None:
    missing = sorted(required - set(value))
    extra = sorted(set(value) - required)
    require(not missing, f"{location} is missing keys: {', '.join(missing)}")
    require(not extra, f"{location} has unsupported keys: {', '.join(extra)}")


def validate_hash(value: Any, location: str) -> None:
    require(
        isinstance(value, str) and SHA256_PATTERN.fullmatch(value) is not None,
        f"{location} must be a lowercase SHA-256",
    )


def validate_commit(value: Any, location: str) -> None:
    require(
        isinstance(value, str) and COMMIT_PATTERN.fullmatch(value) is not None,
        f"{location} must be a full Git object ID",
    )


def validate_relative_path(value: Any, location: str) -> str:
    require(isinstance(value, str) and value != "", f"{location} is required")
    require("\\" not in value, f"{location} must use forward slashes")
    require("\x00" not in value, f"{location} contains NUL")
    pure = pathlib.PurePosixPath(value)
    require(not pure.is_absolute(), f"{location} must be relative")
    require(":" not in pure.parts[0], f"{location} must not be drive-qualified")
    require(all(part not in ("", ".", "..") for part in pure.parts), f"{location} contains traversal")
    require(
        all(WINDOWS_RESERVED_SEGMENT.fullmatch(part) is None for part in pure.parts),
        f"{location} contains a reserved Windows path segment",
    )
    return pure.as_posix()


def validate_core_lock(lock: dict[str, Any]) -> None:
    required = {
        "schemaVersion",
        "coreRepository",
        "coreCommit",
        "editionContractSha256",
        "ddlOwnerManifestSha256",
        "overlaySlotsSha256",
        "releaseManifestSchemaSha256",
        "openAPIContractSha256",
        "eventCatalogSha256",
        "coreMigrationHead",
    }
    validate_exact_keys(lock, required, "core.lock")
    require(lock["schemaVersion"] == "cineweave.core-lock.v1", "core.lock schemaVersion is invalid")
    require(
        isinstance(lock["coreRepository"], str) and lock["coreRepository"].strip() != "",
        "core.lock coreRepository is required",
    )
    validate_commit(lock["coreCommit"], "core.lock.coreCommit")
    for field in CORE_HASH_PATHS:
        validate_hash(lock[field], f"core.lock.{field}")
    require(
        isinstance(lock["coreMigrationHead"], int)
        and not isinstance(lock["coreMigrationHead"], bool)
        and lock["coreMigrationHead"] >= 1,
        "core.lock.coreMigrationHead must be a positive integer",
    )


def validate_overlay(
    overlay: dict[str, Any],
    lock: dict[str, Any],
    slots: dict[str, Any],
) -> None:
    validate_exact_keys(
        overlay,
        {"schemaVersion", "coreCommit", "files"},
        "overlay allowlist",
    )
    require(
        overlay["schemaVersion"] == "cineweave.overlay-allowlist.v1",
        "overlay allowlist schemaVersion is invalid",
    )
    validate_commit(overlay["coreCommit"], "overlay.coreCommit")
    require(
        overlay["coreCommit"] == lock["coreCommit"],
        "overlay coreCommit does not match core.lock",
    )
    require(
        isinstance(overlay["files"], list) and len(overlay["files"]) > 0,
        "overlay allowlist must contain at least one explicit file",
    )

    replaceable = {path.casefold() for path in slots["replaceablePaths"]}
    protected = {path.casefold() for path in slots["protectedPaths"]}
    protected_prefixes = [prefix.casefold() for prefix in slots["protectedPrefixes"]]
    sources: set[str] = set()
    destinations: set[str] = set()
    for index, item in enumerate(overlay["files"]):
        location = f"overlay.files[{index}]"
        require(isinstance(item, dict), f"{location} must be an object")
        validate_exact_keys(item, {"source", "destination", "operation", "sha256"}, location)
        source = validate_relative_path(item["source"], f"{location}.source")
        destination = validate_relative_path(item["destination"], f"{location}.destination")
        validate_hash(item["sha256"], f"{location}.sha256")
        require(item["operation"] in {"add", "replace"}, f"{location}.operation is invalid")

        source_key = source.casefold()
        destination_key = destination.casefold()
        require(source_key not in sources, f"overlay source {source!r} is duplicated")
        require(
            destination_key not in destinations,
            f"overlay destination {destination!r} collides case-insensitively",
        )
        sources.add(source_key)
        destinations.add(destination_key)

        require(destination_key not in protected, f"overlay destination {destination!r} is protected")
        require(
            not any(destination_key.startswith(prefix) for prefix in protected_prefixes),
            f"overlay destination {destination!r} is under a protected prefix",
        )
        if item["operation"] == "replace":
            require(
                destination_key in replaceable,
                f"overlay replacement {destination!r} is not a declared assembly slot",
            )
        else:
            require(
                destination_key not in replaceable,
                f"declared replacement slot {destination!r} must use operation=replace",
            )


def git_output(repository: pathlib.Path, *arguments: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(repository), *arguments],
        text=True,
        encoding="utf-8",
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        raise AssemblyContractError(
            f"git {' '.join(arguments)} failed in {repository}: {result.stderr.strip()}"
        )
    return result.stdout.strip()


def git_bytes(repository: pathlib.Path, *arguments: str) -> bytes:
    result = subprocess.run(
        ["git", "-C", str(repository), *arguments],
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        stderr = result.stderr.decode("utf-8", errors="replace").strip()
        raise AssemblyContractError(
            f"git {' '.join(arguments)} failed in {repository}: {stderr}"
        )
    return result.stdout


def sha256_git_blob(
    repository: pathlib.Path,
    commit: str,
    relative_path: str,
) -> str:
    content = git_bytes(repository, "show", f"{commit}:{relative_path}")
    return hashlib.sha256(content).hexdigest()


def require_clean_repository(repository: pathlib.Path, expected_commit: str, label: str) -> None:
    actual_commit = git_output(repository, "rev-parse", "HEAD")
    require(actual_commit == expected_commit, f"{label} HEAD does not match pinned commit")
    status = git_output(repository, "status", "--porcelain=v1", "--untracked-files=all")
    require(status == "", f"{label} repository is dirty")


def core_migration_head(core_root: pathlib.Path) -> int:
    versions: list[int] = []
    for path in (core_root / "db" / "migrations").glob("*.sql"):
        match = re.fullmatch(r"(\d+)_[a-z0-9_]+\.sql", path.name)
        require(match is not None, f"invalid Core migration filename {path.name!r}")
        versions.append(int(match.group(1)))
    require(bool(versions), "Core migration directory is empty")
    versions.sort()
    require(
        versions == list(range(1, versions[-1] + 1)),
        "Core migration versions are not consecutive",
    )
    return versions[-1]


def verify_core(
    core_root: pathlib.Path,
    lock: dict[str, Any],
) -> None:
    require_clean_repository(core_root, lock["coreCommit"], "Core")
    origin = git_output(core_root, "remote", "get-url", "origin")
    require(origin == lock["coreRepository"], "core.lock repository does not match Core origin")
    for field, relative in CORE_HASH_PATHS.items():
        path = core_root / pathlib.PurePosixPath(relative)
        require(path.is_file() and not path.is_symlink(), f"Core contract file is missing: {relative}")
        require(
            sha256_git_blob(core_root, lock["coreCommit"], relative) == lock[field],
            f"Core contract hash drifted: {relative}",
        )
    require(
        core_migration_head(core_root) == lock["coreMigrationHead"],
        "Core migration head does not match core.lock",
    )


def verify_overlay_files(
    core_root: pathlib.Path,
    overlay_root: pathlib.Path,
    overlay: dict[str, Any],
    commercial_commit: str,
) -> None:
    require_clean_repository(
        overlay_root,
        commercial_commit,
        "Commercial Assembly",
    )
    for item in overlay["files"]:
        source = overlay_root / pathlib.PurePosixPath(item["source"])
        destination = core_root / pathlib.PurePosixPath(item["destination"])
        require(source.is_file(), f"overlay source is missing or not a regular file: {item['source']}")
        require(not source.is_symlink(), f"overlay source must not be a symlink: {item['source']}")
        require(
            sha256_git_blob(
                overlay_root,
                commercial_commit,
                item["source"],
            )
            == item["sha256"],
            f"overlay source hash drifted: {item['source']}",
        )
        if item["operation"] == "add":
            require(
                not destination.exists(),
                f"overlay add destination already exists in Core: {item['destination']}",
            )
        else:
            require(
                destination.is_file() and not destination.is_symlink(),
                f"overlay replacement slot is missing from Core: {item['destination']}",
            )


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate CineWeave core.lock and explicit Commercial Overlay allowlist.",
    )
    parser.add_argument("--core-lock", type=pathlib.Path, default=DEFAULT_LOCK)
    parser.add_argument("--overlay", type=pathlib.Path, default=DEFAULT_OVERLAY)
    parser.add_argument("--slots", type=pathlib.Path, default=DEFAULT_SLOTS)
    parser.add_argument("--core-root", type=pathlib.Path)
    parser.add_argument("--overlay-root", type=pathlib.Path)
    parser.add_argument("--commercial-commit")
    parser.add_argument(
        "--schema-only",
        action="store_true",
        help="validate contract structure without checking Git trees or file hashes",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    try:
        # Keep both public JSON Schema files parseable even though the checker
        # also applies cross-file invariants that JSON Schema cannot express.
        core_schema = load_json(CORE_LOCK_SCHEMA)
        overlay_schema = load_json(OVERLAY_SCHEMA)
        require(
            core_schema.get("$id", "").endswith("/core-lock.v1.json"),
            "Core lock JSON Schema ID is invalid",
        )
        require(
            overlay_schema.get("$id", "").endswith("/overlay-allowlist.v1.json"),
            "Overlay allowlist JSON Schema ID is invalid",
        )
        lock = load_json(args.core_lock.resolve())
        overlay = load_json(args.overlay.resolve())
        slots = load_json(args.slots.resolve())
        require(
            slots.get("schemaVersion") == "cineweave.overlay-slots.v1",
            "Overlay slot manifest schemaVersion is invalid",
        )
        validate_core_lock(lock)
        validate_overlay(overlay, lock, slots)

        if not args.schema_only:
            require(args.core_root is not None, "--core-root is required unless --schema-only is used")
            require(
                args.overlay_root is not None,
                "--overlay-root is required unless --schema-only is used",
            )
            require(
                args.commercial_commit is not None,
                "--commercial-commit is required unless --schema-only is used",
            )
            validate_commit(args.commercial_commit, "--commercial-commit")
            core_root = args.core_root.resolve()
            overlay_root = args.overlay_root.resolve()
            verify_core(core_root, lock)
            verify_overlay_files(core_root, overlay_root, overlay, args.commercial_commit)
    except AssemblyContractError as exc:
        print(f"Commercial Assembly contract check failed: {exc}", file=sys.stderr)
        return 1

    print(
        "Commercial Assembly contract passed: "
        f"core={lock['coreCommit']} commercial={args.commercial_commit or 'unverified'} "
        f"files={len(overlay['files'])} mode={'schema' if args.schema_only else 'full'}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Fail-closed Community Edition source and release artifact leak audit."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import tarfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import BinaryIO, Sequence


VALID_SCOPES = frozenset(
    {
        "git-history",
        "source-archive",
        "go-binaries",
        "web-assets",
        "image-layer",
        "sbom",
    }
)


@dataclass(frozen=True)
class Rule:
    rule_id: str
    regex: re.Pattern[bytes]
    allow_paths: tuple[re.Pattern[str], ...] = ()
    scopes: tuple[str, ...] = ()

    def allowed_for_path(self, path: str) -> bool:
        return any(pattern.search(path) for pattern in self.allow_paths)

    def applies_to_scope(self, scope: str) -> bool:
        return not self.scopes or scope in self.scopes


@dataclass(frozen=True)
class Policy:
    path_rules: tuple[Rule, ...]
    content_rules: tuple[Rule, ...]
    git_metadata_rules: tuple[Rule, ...]
    chunk_bytes: int
    overlap_bytes: int
    source_map_suffixes: tuple[str, ...]


@dataclass(frozen=True)
class Violation:
    scope: str
    rule_id: str
    path: str


def normalize_path(value: str) -> str:
    normalized = str(PurePosixPath(value.replace("\\", "/")))
    while normalized.startswith("./"):
        normalized = normalized[2:]
    return normalized.lstrip("/")


def safe_archive_path(value: str) -> str:
    raw = value.replace("\\", "/")
    path = PurePosixPath(raw)
    if raw.startswith("/") or re.match(r"^[A-Za-z]:/", raw) or ".." in path.parts:
        raise RuntimeError(f"archive contains unsafe path: {value}")
    return normalize_path(raw)


def compile_rule(raw: dict[str, object]) -> Rule:
    rule_id = str(raw["id"])
    expression = str(raw["regex"]).encode("utf-8")
    allow_paths = tuple(
        re.compile(str(value), re.IGNORECASE)
        for value in raw.get("allowPathRegexes", [])
    )
    scopes = tuple(str(value) for value in raw.get("scopes", []))
    return Rule(
        rule_id=rule_id,
        regex=re.compile(expression, re.IGNORECASE | re.MULTILINE),
        allow_paths=allow_paths,
        scopes=scopes,
    )


def load_policy(path: Path) -> Policy:
    raw = json.loads(path.read_text(encoding="utf-8"))
    if raw.get("schemaVersion") != 1 or raw.get("edition") != "community":
        raise ValueError("unsupported CE release policy")
    chunk_bytes = int(raw.get("contentChunkBytes", 1024 * 1024))
    overlap_bytes = int(raw.get("contentOverlapBytes", 4096))
    if chunk_bytes <= 0 or overlap_bytes < 0 or overlap_bytes >= chunk_bytes:
        raise ValueError("invalid content chunk/overlap sizes")
    path_rules = tuple(compile_rule(value) for value in raw["forbiddenPathRules"])
    forbidden_content_rules = tuple(
        compile_rule(value) for value in raw["forbiddenContentRules"]
    )
    secret_rules = tuple(compile_rule(value) for value in raw["secretRules"])
    content_rules = (*forbidden_content_rules, *secret_rules)
    suffixes = tuple(str(value).lower() for value in raw["sourceMapSuffixes"])
    all_rules = (*path_rules, *content_rules)
    rule_ids = [rule.rule_id for rule in all_rules]
    if len(rule_ids) != len(set(rule_ids)):
        raise ValueError("CE release policy contains duplicate rule IDs")
    unknown_scopes = sorted(
        {
            scope
            for rule in all_rules
            for scope in rule.scopes
            if scope not in VALID_SCOPES
        }
    )
    if unknown_scopes:
        raise ValueError(
            f"CE release policy contains unknown scopes: {', '.join(unknown_scopes)}"
        )
    if not path_rules or not content_rules or not suffixes:
        raise ValueError("CE release policy must define path, content, and source-map rules")
    return Policy(
        path_rules,
        content_rules,
        secret_rules,
        chunk_bytes,
        overlap_bytes,
        suffixes,
    )


def matching_path_rules(policy: Policy, path: str, scope: str) -> list[Rule]:
    encoded = normalize_path(path).encode("utf-8", errors="surrogateescape")
    return [
        rule
        for rule in policy.path_rules
        if rule.applies_to_scope(scope) and rule.regex.search(encoded)
    ]


def matching_content_rules(
    policy: Policy, path: str, data: bytes, scope: str
) -> list[Rule]:
    normalized = normalize_path(path)
    return [
        rule
        for rule in policy.content_rules
        if rule.applies_to_scope(scope)
        and not rule.allowed_for_path(normalized)
        and rule.regex.search(data)
    ]


def scan_stream(
    policy: Policy,
    scope: str,
    path: str,
    stream: BinaryIO,
) -> list[Violation]:
    violations: list[Violation] = []
    tail = b""
    matched: set[str] = set()
    while True:
        chunk = stream.read(policy.chunk_bytes)
        if not chunk:
            break
        window = tail + chunk
        for rule in matching_content_rules(policy, path, window, scope):
            if rule.rule_id not in matched:
                violations.append(Violation(scope, rule.rule_id, normalize_path(path)))
                matched.add(rule.rule_id)
        tail = window[-policy.overlap_bytes :] if policy.overlap_bytes else b""
    return violations


def scan_tree(policy: Policy, root: Path, scope: str) -> tuple[list[Violation], dict[str, int]]:
    violations: list[Violation] = []
    files = 0
    source_maps = 0
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.is_symlink():
            continue
        relative = normalize_path(str(path.relative_to(root)))
        files += 1
        if relative.lower().endswith(policy.source_map_suffixes):
            source_maps += 1
        for rule in matching_path_rules(policy, relative, scope):
            violations.append(Violation(scope, rule.rule_id, relative))
        with path.open("rb") as stream:
            violations.extend(scan_stream(policy, scope, relative, stream))
    return violations, {"files": files, "sourceMaps": source_maps}


def git_lines(repo: Path, *args: str) -> list[str]:
    result = subprocess.run(
        ["git", *args],
        cwd=repo,
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return result.stdout.decode("utf-8", errors="surrogateescape").splitlines()


def release_history_refs(repo: Path) -> list[str]:
    refs = git_lines(
        repo,
        "for-each-ref",
        "--format=%(refname)",
        "refs/heads",
        "refs/remotes",
        "refs/tags",
    )
    if not refs:
        raise RuntimeError("CE history audit found no branch, remote, or tag refs")
    return refs


def git_revision_lines(repo: Path, refs: Sequence[str]) -> list[str]:
    result = subprocess.run(
        ["git", "rev-list", "--objects", "--stdin"],
        cwd=repo,
        check=True,
        input=("\n".join(refs) + "\n").encode("utf-8"),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return result.stdout.decode("utf-8", errors="surrogateescape").splitlines()


def scan_git_history(
    policy: Policy, repo: Path
) -> tuple[list[Violation], dict[str, int]]:
    refs = release_history_refs(repo)
    object_paths: dict[str, set[str]] = {}
    violations: list[Violation] = []
    for line in git_revision_lines(repo, refs):
        object_id, separator, raw_path = line.partition(" ")
        paths = object_paths.setdefault(object_id, set())
        if separator:
            path = normalize_path(raw_path)
            paths.add(path)
            for rule in matching_path_rules(policy, path, "git-history"):
                violations.append(Violation("git-history", rule.rule_id, path))

    process = subprocess.Popen(
        ["git", "cat-file", "--batch"],
        cwd=repo,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    assert process.stdin is not None
    assert process.stdout is not None
    assert process.stderr is not None
    object_type_counts: dict[str, int] = {}
    try:
        for object_id, paths in object_paths.items():
            process.stdin.write(f"{object_id}\n".encode("ascii"))
            process.stdin.flush()
            header = process.stdout.readline().decode("ascii", errors="replace").strip()
            parts = header.split()
            if len(parts) < 3 or parts[1] == "missing":
                raise RuntimeError(f"cannot inspect Git object {object_id}: {header}")
            object_type = parts[1]
            size = int(parts[2])
            object_type_counts[object_type] = (
                object_type_counts.get(object_type, 0) + 1
            )
            applicable: dict[str, list[Rule]] = {}
            if object_type == "blob":
                blob_paths = paths or {f"@git-object/blob/{object_id}"}
                for path in blob_paths:
                    rules = [
                        rule
                        for rule in policy.content_rules
                        if rule.applies_to_scope("git-history")
                        and not rule.allowed_for_path(path)
                    ]
                    if rules:
                        applicable[path] = rules
            elif object_type in {"commit", "tag"}:
                metadata_path = f"@git-object/{object_type}/{object_id}"
                rules = [
                    rule
                    for rule in policy.git_metadata_rules
                    if rule.applies_to_scope("git-history")
                    and not rule.allowed_for_path(metadata_path)
                ]
                if rules:
                    applicable[metadata_path] = rules

            remaining = size
            tails = {path: b"" for path in applicable}
            matched: dict[str, set[str]] = {path: set() for path in applicable}
            while remaining:
                chunk = process.stdout.read(min(policy.chunk_bytes, remaining))
                if not chunk:
                    raise RuntimeError(f"unexpected EOF reading Git object {object_id}")
                remaining -= len(chunk)
                for path, rules in applicable.items():
                    window = tails[path] + chunk
                    for rule in rules:
                        if rule.rule_id not in matched[path] and rule.regex.search(window):
                            violations.append(
                                Violation("git-history", rule.rule_id, path)
                            )
                            matched[path].add(rule.rule_id)
                    tails[path] = (
                        window[-policy.overlap_bytes :]
                        if policy.overlap_bytes
                        else b""
                    )
            if process.stdout.read(1) != b"\n":
                raise RuntimeError(f"invalid Git batch delimiter for {object_id}")
    finally:
        process.stdin.close()
        process.wait(timeout=30)
    if process.returncode != 0:
        error = process.stderr.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"git cat-file failed: {error}")
    return violations, {
        "refs": len(refs),
        "objects": len(object_paths),
        "objectsWithPaths": sum(1 for paths in object_paths.values() if paths),
        **{
            f"{object_type}s": count
            for object_type, count in sorted(object_type_counts.items())
        },
    }


def scan_tar_members(
    policy: Policy,
    archive: tarfile.TarFile,
    scope: str,
    prefix: str,
) -> tuple[list[Violation], dict[str, int]]:
    violations: list[Violation] = []
    files = 0
    source_maps = 0
    for member in archive:
        if not member.isfile():
            continue
        member_path = safe_archive_path(member.name)
        relative = normalize_path(f"{prefix}/{member_path}")
        files += 1
        if relative.lower().endswith(policy.source_map_suffixes):
            source_maps += 1
        for rule in matching_path_rules(policy, relative, scope):
            violations.append(Violation(scope, rule.rule_id, relative))
        stream = archive.extractfile(member)
        if stream is None:
            continue
        violations.extend(scan_stream(policy, scope, relative, stream))
    return violations, {"files": files, "sourceMaps": source_maps}


def scan_tar(
    policy: Policy, archive_path: Path, scope: str
) -> tuple[list[Violation], dict[str, int]]:
    with tarfile.open(archive_path, mode="r:*") as archive:
        try:
            manifest_member = archive.getmember("manifest.json")
        except KeyError as error:
            raise RuntimeError("Docker image archive is missing manifest.json") from error
        manifest_stream = archive.extractfile(manifest_member)
        if manifest_stream is None:
            raise RuntimeError("Docker image archive manifest.json is unreadable")
        try:
            manifest = json.load(manifest_stream)
        except (json.JSONDecodeError, UnicodeDecodeError) as error:
            raise RuntimeError("Docker image archive manifest.json is invalid") from error
        if not isinstance(manifest, list):
            raise RuntimeError("Docker image archive manifest.json must be a list")
        layer_paths: dict[str, str] = {}
        for image in manifest:
            if not isinstance(image, dict):
                raise RuntimeError("Docker image archive manifest entry is invalid")
            for layer in image.get("Layers", []):
                raw_layer_path = str(layer)
                layer_paths[safe_archive_path(raw_layer_path)] = raw_layer_path
        if not layer_paths:
            raise RuntimeError("Docker image archive contains no declared layers")

        violations: list[Violation] = []
        files = 0
        source_maps = 0
        for member in archive.getmembers():
            if not member.isfile():
                continue
            member_path = safe_archive_path(member.name)
            relative = normalize_path(f"{archive_path.name}/{member_path}")
            files += 1
            if relative.lower().endswith(policy.source_map_suffixes):
                source_maps += 1
            for rule in matching_path_rules(policy, relative, scope):
                violations.append(Violation(scope, rule.rule_id, relative))
            if member_path in layer_paths:
                continue
            stream = archive.extractfile(member)
            if stream is not None:
                violations.extend(scan_stream(policy, scope, relative, stream))

        scanned_layers = 0
        for layer_path in sorted(layer_paths):
            raw_layer_path = layer_paths[layer_path]
            try:
                layer_member = archive.getmember(raw_layer_path)
            except KeyError as error:
                raise RuntimeError(
                    f"Docker image archive is missing declared layer {layer_path}"
                ) from error
            layer_stream = archive.extractfile(layer_member)
            if layer_stream is None:
                raise RuntimeError(
                    f"Docker image archive layer {layer_path} is unreadable"
                )
            try:
                with tarfile.open(fileobj=layer_stream, mode="r|*") as layer:
                    nested_violations, nested_counts = scan_tar_members(
                        policy,
                        layer,
                        scope,
                        normalize_path(f"{archive_path.name}/{layer_path}"),
                    )
            except tarfile.TarError as error:
                raise RuntimeError(
                    f"Docker image archive layer {layer_path} is invalid"
                ) from error
            violations.extend(nested_violations)
            files += nested_counts["files"]
            source_maps += nested_counts["sourceMaps"]
            scanned_layers += 1
        return violations, {
            "files": files,
            "sourceMaps": source_maps,
            "layers": scanned_layers,
        }


def print_result(
    violations: Sequence[Violation], counts: dict[str, int], label: str
) -> int:
    summary = ", ".join(f"{key}={value}" for key, value in sorted(counts.items()))
    if violations:
        print(f"{label}: FAILED ({summary})", file=sys.stderr)
        for violation in sorted(
            set(violations), key=lambda item: (item.scope, item.rule_id, item.path)
        ):
            print(
                f"  [{violation.scope}] {violation.rule_id}: {violation.path}",
                file=sys.stderr,
            )
        return 1
    print(f"{label}: passed ({summary})")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--policy", required=True, type=Path)
    subparsers = parser.add_subparsers(dest="command", required=True)

    history = subparsers.add_parser("history")
    history.add_argument("--repo", required=True, type=Path)

    tree = subparsers.add_parser("tree")
    tree.add_argument("--root", required=True, type=Path)
    tree.add_argument(
        "--scope",
        required=True,
        choices=("source-archive", "go-binaries", "web-assets", "sbom"),
    )

    archive = subparsers.add_parser("tar")
    archive.add_argument("--archive", required=True, type=Path)
    archive.add_argument("--scope", required=True, choices=("image-layer",))
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    policy = load_policy(args.policy.resolve())
    if args.command == "history":
        violations, counts = scan_git_history(policy, args.repo.resolve())
        return print_result(violations, counts, "CE Git history audit")
    if args.command == "tree":
        violations, counts = scan_tree(policy, args.root.resolve(), args.scope)
        return print_result(violations, counts, f"CE {args.scope} audit")
    if args.command == "tar":
        violations, counts = scan_tar(
            policy, args.archive.resolve(), args.scope
        )
        return print_result(violations, counts, "CE image layer audit")
    raise AssertionError(f"unsupported command: {args.command}")


if __name__ == "__main__":
    raise SystemExit(main())

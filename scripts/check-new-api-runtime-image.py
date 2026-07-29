from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import pathlib
import re
import sys
from typing import Any


RUNTIME_SCHEMA_VERSION = "cineweave.new-api-runtime-image.v1"
UPSTREAM_SCHEMA_VERSION = "cineweave.new-api-upstream-evidence.v1"
CONTRACT_SCHEMA_VERSION = "cineweave.new-api-contract-fixtures.v1"
RELEASE_SCHEMA_VERSION = "cineweave.release-manifest.v2"

DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
IMAGE_REFERENCE = re.compile(
    r"^(?P<repository>[a-z0-9][a-z0-9._/-]*[a-z0-9])@"
    r"(?P<digest>sha256:[0-9a-f]{64})$"
)
FULL_COMMIT = re.compile(r"^[0-9a-f]{40}$")


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def load_json(path: pathlib.Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read JSON {path}: {exc}") from exc


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    try:
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError as exc:
        raise ValueError(f"cannot hash {path}: {exc}") from exc
    return digest.hexdigest()


def exact_keys(value: Any, keys: set[str], label: str) -> dict[str, Any]:
    require(isinstance(value, dict), f"{label} must be an object")
    actual = set(value)
    require(actual == keys, f"{label} fields mismatch: expected={sorted(keys)!r} actual={sorted(actual)!r}")
    return value


def parse_timestamp(value: Any, label: str) -> dt.datetime:
    require(isinstance(value, str) and value, f"{label} must be a timestamp")
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise ValueError(f"{label} is not an RFC 3339 timestamp") from exc
    require(parsed.tzinfo is not None, f"{label} must include a timezone")
    return parsed


def normalize_repository(value: str) -> str:
    repository = value.casefold()
    for prefix in ("index.docker.io/", "docker.io/"):
        if repository.startswith(prefix):
            return repository[len(prefix) :]
    return repository


def parse_image_reference(value: Any, label: str) -> tuple[str, str]:
    require(isinstance(value, str), f"{label} must be a string")
    match = IMAGE_REFERENCE.fullmatch(value)
    require(match is not None, f"{label} must use repository@sha256:<64 hex>")
    return normalize_repository(match.group("repository")), match.group("digest")


def validate_runtime(document: Any) -> tuple[str, str]:
    runtime = exact_keys(
        document,
        {"schemaVersion", "capturedAt", "container", "image"},
        "runtime image evidence",
    )
    require(
        runtime["schemaVersion"] == RUNTIME_SCHEMA_VERSION,
        "runtime image evidence schemaVersion is invalid",
    )
    parse_timestamp(runtime["capturedAt"], "runtime image evidence capturedAt")

    container = exact_keys(
        runtime["container"],
        {"name", "configuredImageReference", "imageId"},
        "runtime image evidence container",
    )
    require(
        isinstance(container["name"], str) and container["name"].strip(),
        "runtime image evidence container.name is empty",
    )
    require(
        isinstance(container["imageId"], str)
        and DIGEST.fullmatch(container["imageId"]) is not None,
        "runtime image evidence container.imageId is invalid",
    )
    configured_repository, configured_digest = parse_image_reference(
        container["configuredImageReference"],
        "runtime configured image reference",
    )

    image = exact_keys(
        runtime["image"],
        {"repoDigests"},
        "runtime image evidence image",
    )
    repo_digests = image["repoDigests"]
    require(
        isinstance(repo_digests, list) and repo_digests,
        "runtime image evidence has no RepoDigests",
    )
    parsed_repo_digests: set[tuple[str, str]] = set()
    for index, reference in enumerate(repo_digests):
        parsed_repo_digests.add(
            parse_image_reference(reference, f"runtime RepoDigests[{index}]")
        )
    require(
        (configured_repository, configured_digest) in parsed_repo_digests,
        "running image RepoDigests do not contain the configured immutable image",
    )
    return configured_repository, configured_digest


def validate_upstream(document: Any) -> tuple[str, str, str, str]:
    require(isinstance(document, dict), "upstream evidence must be an object")
    require(
        document.get("schemaVersion") == UPSTREAM_SCHEMA_VERSION,
        "upstream evidence schemaVersion is invalid",
    )
    image = document.get("image")
    require(isinstance(image, dict), "upstream evidence image is missing")
    repository, digest = parse_image_reference(
        image.get("reference"),
        "upstream evidence image.reference",
    )
    require(
        normalize_repository(str(image.get("repository", ""))) == repository,
        "upstream evidence image repository is inconsistent",
    )
    require(image.get("digest") == digest, "upstream evidence image digest is inconsistent")

    source = document.get("source")
    require(isinstance(source, dict), "upstream evidence source is missing")
    commit = source.get("commit")
    tag = source.get("tag")
    require(
        isinstance(commit, str) and FULL_COMMIT.fullmatch(commit) is not None,
        "upstream evidence source commit is invalid",
    )
    require(isinstance(tag, str) and tag.strip(), "upstream evidence source tag is empty")
    require(
        source.get("tagResolvedCommit") == commit,
        "upstream evidence source tag does not resolve to its commit",
    )
    return repository, digest, commit, tag


def validate_contract(
    document: Any,
    *,
    expected_digest: str,
    expected_commit: str,
    expected_tag: str,
) -> None:
    require(isinstance(document, dict), "New API contract fixture manifest must be an object")
    require(
        document.get("schemaVersion") == CONTRACT_SCHEMA_VERSION,
        "New API contract fixture manifest schemaVersion is invalid",
    )
    upstream = document.get("upstream")
    require(isinstance(upstream, dict), "New API contract fixture manifest upstream is missing")
    require(
        upstream.get("imageDigest") == expected_digest,
        "New API contract fixture image digest drifted",
    )
    require(
        upstream.get("commit") == expected_commit,
        "New API contract fixture source commit drifted",
    )
    require(
        upstream.get("version") == expected_tag,
        "New API contract fixture version drifted",
    )


def validate_release(
    document: Any,
    *,
    upstream_evidence_sha256: str,
    expected_repository: str,
    expected_digest: str,
    expected_commit: str,
    expected_tag: str,
) -> None:
    require(isinstance(document, dict), "Combined Release Manifest must be an object")
    require(
        document.get("schemaVersion") == RELEASE_SCHEMA_VERSION,
        "Combined Release Manifest schemaVersion is invalid",
    )
    upstream = document.get("upstreamNewAPI")
    require(isinstance(upstream, dict), "Combined Release Manifest upstreamNewAPI is missing")
    require(
        normalize_repository(str(upstream.get("registry", ""))) == expected_repository,
        "Combined Release Manifest New API repository drifted",
    )
    require(
        upstream.get("imageDigest") == expected_digest,
        "Combined Release Manifest New API image digest drifted",
    )
    require(
        upstream.get("evidenceSha256") == upstream_evidence_sha256,
        "Combined Release Manifest New API evidence hash drifted",
    )
    require(
        upstream.get("sourceCommit") == expected_commit,
        "Combined Release Manifest New API source commit drifted",
    )
    require(
        upstream.get("sourceTag") == expected_tag,
        "Combined Release Manifest New API source tag drifted",
    )


def check(
    *,
    runtime_evidence_path: pathlib.Path,
    upstream_evidence_path: pathlib.Path,
    contract_manifest_path: pathlib.Path,
    release_manifest_path: pathlib.Path,
) -> tuple[str, str]:
    runtime_repository, runtime_digest = validate_runtime(
        load_json(runtime_evidence_path)
    )
    upstream_repository, upstream_digest, upstream_commit, upstream_tag = (
        validate_upstream(load_json(upstream_evidence_path))
    )
    require(
        (runtime_repository, runtime_digest)
        == (upstream_repository, upstream_digest),
        "runtime image does not match immutable upstream evidence",
    )
    validate_contract(
        load_json(contract_manifest_path),
        expected_digest=upstream_digest,
        expected_commit=upstream_commit,
        expected_tag=upstream_tag,
    )
    validate_release(
        load_json(release_manifest_path),
        upstream_evidence_sha256=sha256_file(upstream_evidence_path),
        expected_repository=upstream_repository,
        expected_digest=upstream_digest,
        expected_commit=upstream_commit,
        expected_tag=upstream_tag,
    )
    return upstream_repository, upstream_digest


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Verify that the configured and running New API image, upstream "
            "evidence, contract fixtures, and Combined Release Manifest agree."
        )
    )
    parser.add_argument("--runtime-evidence", type=pathlib.Path, required=True)
    parser.add_argument("--upstream-evidence", type=pathlib.Path, required=True)
    parser.add_argument("--contract-manifest", type=pathlib.Path, required=True)
    parser.add_argument("--release-manifest", type=pathlib.Path, required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        repository, digest = check(
            runtime_evidence_path=args.runtime_evidence.resolve(),
            upstream_evidence_path=args.upstream_evidence.resolve(),
            contract_manifest_path=args.contract_manifest.resolve(),
            release_manifest_path=args.release_manifest.resolve(),
        )
    except ValueError as exc:
        print(f"New API runtime image verification failed: {exc}", file=sys.stderr)
        return 1
    print(
        "New API runtime image verified: "
        f"image={repository}@{digest} sources=runtime,upstream,contract,release"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

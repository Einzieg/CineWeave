from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
import subprocess
import sys
import tempfile
import zipfile
from typing import Any


SCHEMA_VERSION = "cineweave.new-api-upstream-evidence.v1"
DEFAULT_REMOTE = "https://github.com/QuantumNous/new-api.git"
FULL_COMMIT = re.compile(r"^[0-9a-f]{40}$")
IMAGE_REFERENCE = re.compile(
    r"^(?P<repository>[a-z0-9][a-z0-9._/-]*[a-z0-9])@(?P<digest>sha256:[0-9a-f]{64})$"
)
MUTABLE_TAGS = {"main", "master", "latest", "dev", "development"}


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def run(command: list[str], *, cwd: pathlib.Path | None = None) -> str:
    completed = subprocess.run(
        command,
        cwd=cwd,
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout).strip()
        raise ValueError(f"{' '.join(command)} failed: {detail}")
    return completed.stdout


def sha256_bytes(payload: bytes) -> str:
    return hashlib.sha256(payload).hexdigest()


def read_archive_file(
    archive: zipfile.ZipFile,
    candidates: tuple[str, ...],
) -> tuple[str, bytes] | None:
    by_casefold = {name.casefold(): name for name in archive.namelist() if not name.endswith("/")}
    for candidate in candidates:
        actual = by_casefold.get(candidate.casefold())
        if actual is not None:
            return actual, archive.read(actual)
    return None


def markdown_section(payload: bytes, heading_terms: tuple[str, ...]) -> bytes | None:
    text = payload.decode("utf-8", errors="strict").replace("\r\n", "\n")
    lines = text.splitlines(keepends=True)
    start = None
    start_level = None
    heading_pattern = re.compile(r"^(#{1,6})\s+(.+?)\s*$")
    for index, line in enumerate(lines):
        match = heading_pattern.match(line.rstrip("\n"))
        if match is None:
            continue
        title = match.group(2).strip().casefold()
        if any(term.casefold() in title for term in heading_terms):
            start = index
            start_level = len(match.group(1))
            break
    if start is None or start_level is None:
        return None
    end = len(lines)
    for index in range(start + 1, len(lines)):
        match = heading_pattern.match(lines[index].rstrip("\n"))
        if match is not None and len(match.group(1)) <= start_level:
            end = index
            break
    return "".join(lines[start:end]).encode("utf-8")


def detect_license_family(payload: bytes) -> str:
    normalized = " ".join(
        payload.decode("utf-8", errors="replace").split()
    ).casefold()
    if "gnu affero general public license" in normalized and "version 3" in normalized:
        return "AGPL-3.0"
    if "gnu general public license" in normalized and "version 3" in normalized:
        return "GPL-3.0"
    return "UNKNOWN"


def resolve_tag(remote: str, tag: str) -> str:
    require(tag and tag.casefold() not in MUTABLE_TAGS, "source tag must be immutable")
    output = run(
        [
            "git",
            "ls-remote",
            "--tags",
            remote,
            f"refs/tags/{tag}",
            f"refs/tags/{tag}^{{}}",
        ]
    )
    direct = None
    peeled = None
    for line in output.splitlines():
        fields = line.split()
        if len(fields) != 2:
            continue
        commit, reference = fields
        if reference == f"refs/tags/{tag}^{{}}":
            peeled = commit
        elif reference == f"refs/tags/{tag}":
            direct = commit
    resolved = peeled or direct
    require(resolved is not None and FULL_COMMIT.fullmatch(resolved) is not None, f"source tag {tag} was not found")
    return resolved


def source_archive(
    remote: str,
    commit: str,
    directory: pathlib.Path,
) -> pathlib.Path:
    repository = directory / "source.git"
    run(["git", "init", "--bare", str(repository)])
    run(["git", "-C", str(repository), "fetch", "--depth=1", remote, commit])
    fetched = run(["git", "-C", str(repository), "rev-parse", "FETCH_HEAD"]).strip()
    require(fetched == commit, f"fetched source commit {fetched} differs from requested {commit}")
    archive_path = directory / "source.zip"
    run(
        [
            "git",
            "-C",
            str(repository),
            "archive",
            "--format=zip",
            f"--output={archive_path}",
            commit,
        ]
    )
    require(archive_path.is_file() and archive_path.stat().st_size > 0, "source archive is empty")
    return archive_path


def capture(
    *,
    source_remote: str,
    source_commit: str,
    source_tag: str,
    image_reference: str,
    image_source_label: str,
    image_revision_label: str,
    image_version_label: str,
    image_license_label: str,
    image_created_at: str,
) -> dict[str, Any]:
    require(FULL_COMMIT.fullmatch(source_commit) is not None, "source commit must be a full 40-character SHA")
    match = IMAGE_REFERENCE.fullmatch(image_reference)
    require(match is not None, "image reference must use repository@sha256:<64 hex>")
    tag_commit = resolve_tag(source_remote, source_tag)
    require(tag_commit == source_commit, "source tag does not resolve to the requested commit")
    require(image_revision_label == source_commit, "image revision label does not match source commit")
    require(image_version_label == source_tag, "image version label does not match source tag")
    require(
        image_source_label.rstrip("/").casefold()
        == "https://github.com/quantumnous/new-api".casefold(),
        "image source label is not the official New API repository",
    )

    with tempfile.TemporaryDirectory(prefix="cineweave-new-api-evidence-") as temp:
        archive_path = source_archive(source_remote, source_commit, pathlib.Path(temp))
        archive_sha256 = sha256_bytes(archive_path.read_bytes())
        with zipfile.ZipFile(archive_path) as archive:
            license_file = read_archive_file(
                archive,
                ("LICENSE", "LICENSE.md", "COPYING"),
            )
            readme_file = read_archive_file(
                archive,
                ("README.md", "README.en.md", "README"),
            )
            notice_file = read_archive_file(
                archive,
                ("NOTICE", "NOTICE.md"),
            )
            require(license_file is not None, "source commit has no root LICENSE")
            require(readme_file is not None, "source commit has no root README")
            license_name, license_payload = license_file
            readme_name, readme_payload = readme_file
            license_section = markdown_section(
                readme_payload,
                ("license", "许可证", "许可"),
            )
            require(license_section is not None, "README has no license section")
            license_family = detect_license_family(license_payload)
            require(license_family != "UNKNOWN", "source LICENSE family is unknown")
            section_text = license_section.decode("utf-8", errors="replace").casefold()
            attribution_markers = {
                "mentionsAttribution": any(
                    value in section_text for value in ("attribution", "署名", "标识")
                ),
                "mentionsOriginalProjectLink": any(
                    value in section_text
                    for value in (
                        "original project",
                        "原项目",
                        "new-api",
                        "github.com/quantumnous/new-api",
                    )
                ),
            }
            notice_evidence = (
                {
                    "present": True,
                    "path": notice_file[0],
                    "sha256": sha256_bytes(notice_file[1]),
                }
                if notice_file is not None
                else {
                    "present": False,
                    "path": None,
                    "sha256": None,
                }
            )
    return {
        "schemaVersion": SCHEMA_VERSION,
        "source": {
            "remote": source_remote,
            "commit": source_commit,
            "tag": source_tag,
            "tagResolvedCommit": tag_commit,
            "archiveSha256": archive_sha256,
            "license": {
                "path": license_name,
                "sha256": sha256_bytes(license_payload),
                "detectedFamily": license_family,
            },
            "readme": {
                "path": readme_name,
                "sha256": sha256_bytes(readme_payload),
                "licenseSectionSha256": sha256_bytes(license_section),
                **attribution_markers,
            },
            "notice": notice_evidence,
        },
        "image": {
            "reference": image_reference,
            "repository": match.group("repository"),
            "digest": match.group("digest"),
            "createdAt": image_created_at,
            "labels": {
                "source": image_source_label,
                "revision": image_revision_label,
                "version": image_version_label,
                "license": image_license_label,
            },
        },
        "assertions": {
            "tagMatchesCommit": True,
            "imageRevisionMatchesCommit": True,
            "imageVersionMatchesTag": True,
            "imageSourceIsOfficialRepository": True,
            "licenseLabelMatchesDetectedFamily": image_license_label
            in {license_family, license_family + "-only", license_family + "-or-later"},
            "imageContentMatchesSourceArchive": "unverified",
            "modificationAssessment": "unverified",
        },
    }


def write_json(path: pathlib.Path, document: dict[str, Any]) -> None:
    path.resolve().parent.mkdir(parents=True, exist_ok=True)
    path.resolve().write_text(
        json.dumps(document, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
        newline="\n",
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Capture immutable source and image-label evidence for a New API release."
    )
    parser.add_argument("--source-remote", default=DEFAULT_REMOTE)
    parser.add_argument("--source-commit", required=True)
    parser.add_argument("--source-tag", required=True)
    parser.add_argument("--image-reference", required=True)
    parser.add_argument("--image-source-label", required=True)
    parser.add_argument("--image-revision-label", required=True)
    parser.add_argument("--image-version-label", required=True)
    parser.add_argument("--image-license-label", required=True)
    parser.add_argument("--image-created-at", required=True)
    parser.add_argument("--output", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        evidence = capture(
            source_remote=args.source_remote,
            source_commit=args.source_commit,
            source_tag=args.source_tag,
            image_reference=args.image_reference,
            image_source_label=args.image_source_label,
            image_revision_label=args.image_revision_label,
            image_version_label=args.image_version_label,
            image_license_label=args.image_license_label,
            image_created_at=args.image_created_at,
        )
        write_json(pathlib.Path(args.output), evidence)
    except (OSError, ValueError, zipfile.BadZipFile) as exc:
        print(f"New API upstream evidence capture failed: {exc}", file=sys.stderr)
        return 1
    print(
        "New API upstream evidence captured: "
        f"version={evidence['source']['tag']} "
        f"commit={evidence['source']['commit']} "
        f"digest={evidence['image']['digest']} "
        f"modificationAssessment={evidence['assertions']['modificationAssessment']}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

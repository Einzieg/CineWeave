from __future__ import annotations

import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
from collections import defaultdict
from typing import Any

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT_SCHEMA_VERSION = "cineweave.source-licensing-audit.v1"
APPROVAL_SCHEMA_VERSION = "cineweave.source-license-approval.v1"
EXPECTED_SOFTWARE_LICENSE_SPDX = "AGPL-3.0-or-later"
HASH_PATTERN = re.compile(r"^[0-9a-f]{64}$")
LEGAL_ARTIFACTS = {
    "softwareLicense": ("LICENSE", "LICENSE.md", "COPYING", "COPYING.md"),
    "notice": ("NOTICE", "NOTICE.md"),
    "copyright": ("COPYRIGHT", "COPYRIGHT.md"),
    "trademarkPolicy": ("TRADEMARKS", "TRADEMARKS.md"),
    "contributionPolicy": ("CONTRIBUTING", "CONTRIBUTING.md"),
    "contributorAgreement": ("CLA", "CLA.md", "CONTRIBUTOR_LICENSE_AGREEMENT.md"),
}
REVIEW_LICENSE_MARKERS = (
    "AGPL",
    "GPL",
    "LGPL",
    "MPL",
    "EPL",
    "CDDL",
    "CC-BY",
    "OSL",
)
UNKNOWN_LICENSE_MARKERS = (
    "UNKNOWN",
    "NOASSERTION",
    "UNLICENSED",
    "SEE LICENSE",
    "CUSTOM",
)
BINARY_ASSET_SUFFIXES = {
    ".7z",
    ".avi",
    ".eot",
    ".gif",
    ".gz",
    ".ico",
    ".jpeg",
    ".jpg",
    ".mov",
    ".mp3",
    ".mp4",
    ".ogg",
    ".otf",
    ".pdf",
    ".png",
    ".tar",
    ".ttf",
    ".wav",
    ".webm",
    ".webp",
    ".woff",
    ".woff2",
    ".zip",
}


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def run(command: list[str]) -> str:
    executable = command[0]
    if os.name == "nt":
        resolved = shutil.which(executable) or shutil.which(executable + ".cmd")
        if resolved is not None:
            command = [resolved, *command[1:]]
    completed = subprocess.run(
        command,
        cwd=ROOT,
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


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def canonical_hash(value: Any) -> str:
    return sha256_bytes(
        json.dumps(
            value,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
        ).encode("utf-8")
    )


def git_history_inventory() -> dict[str, Any]:
    head = run(["git", "rev-parse", "HEAD"]).strip()
    remote = run(["git", "remote", "get-url", "origin"]).strip()
    raw = run(
        [
            "git",
            "log",
            "--format=%H%x1f%aN%x1f%aE%x1f%aI%x1f%G?%x1f%B%x1e",
            "--all",
        ]
    )
    contributors: dict[tuple[str, str], dict[str, Any]] = {}
    commit_count = 0
    unsigned_dco = 0
    cryptographically_unsigned = 0
    for record in raw.split("\x1e"):
        record = record.strip("\r\n")
        if not record:
            continue
        parts = record.split("\x1f", 5)
        require(len(parts) == 6, "cannot parse Git contribution history")
        commit_hash, name, email, authored_at, signature_state, body = parts
        commit_count += 1
        email_normalized = email.strip().lower()
        key = (name.strip(), sha256_bytes(email_normalized.encode("utf-8")))
        contributor = contributors.setdefault(
            key,
            {
                "name": name.strip(),
                "emailSha256": key[1],
                "commitCount": 0,
                "firstAuthoredAt": authored_at,
                "lastAuthoredAt": authored_at,
                "dcoSignedCommitCount": 0,
                "cryptographicallySignedCommitCount": 0,
            },
        )
        contributor["commitCount"] += 1
        contributor["firstAuthoredAt"] = min(contributor["firstAuthoredAt"], authored_at)
        contributor["lastAuthoredAt"] = max(contributor["lastAuthoredAt"], authored_at)
        signed_off = bool(
            re.search(
                rf"(?im)^Signed-off-by:\s*.+<\s*{re.escape(email_normalized)}\s*>\s*$",
                body,
            )
        )
        if signed_off:
            contributor["dcoSignedCommitCount"] += 1
        else:
            unsigned_dco += 1
        if signature_state in {"G", "U", "X", "Y", "R"}:
            contributor["cryptographicallySignedCommitCount"] += 1
        else:
            cryptographically_unsigned += 1
    ordered = sorted(
        contributors.values(),
        key=lambda item: (item["name"].casefold(), item["emailSha256"]),
    )
    return {
        "headCommit": head,
        "origin": remote,
        "commitCount": commit_count,
        "contributors": ordered,
        "commitsWithoutDCO": unsigned_dco,
        "commitsWithoutVerifiedOrKnownSignature": cryptographically_unsigned,
    }


def legal_artifact_inventory() -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, candidates in LEGAL_ARTIFACTS.items():
        found = next((ROOT / name for name in candidates if (ROOT / name).is_file()), None)
        result[key] = (
            {
                "present": True,
                "path": found.relative_to(ROOT).as_posix(),
                "sha256": sha256_file(found),
            }
            if found is not None
            else {
                "present": False,
                "acceptedPaths": list(candidates),
            }
        )
    return result


def parse_json_stream(value: str) -> list[dict[str, Any]]:
    decoder = json.JSONDecoder()
    offset = 0
    documents: list[dict[str, Any]] = []
    while offset < len(value):
        while offset < len(value) and value[offset].isspace():
            offset += 1
        if offset >= len(value):
            break
        document, offset = decoder.raw_decode(value, offset)
        require(isinstance(document, dict), "JSON stream item must be an object")
        documents.append(document)
    return documents


def detect_license_expression(text: str) -> str:
    normalized = " ".join(text.replace("\x00", " ").split()).lower()
    if "gnu affero general public license" in normalized:
        return "AGPL"
    if "gnu lesser general public license" in normalized:
        return "LGPL"
    if "gnu general public license" in normalized:
        return "GPL"
    if "mozilla public license" in normalized and "version 2.0" in normalized:
        return "MPL-2.0"
    if "attribution-sharealike 4.0 international" in normalized:
        return "CC-BY-SA-4.0"
    if "apache license" in normalized and "version 2.0" in normalized:
        return "Apache-2.0"
    if "permission is hereby granted, free of charge" in normalized:
        return "MIT"
    if (
        "redistribution and use in source and binary forms" in normalized
        and "neither the name" in normalized
    ):
        return "BSD-3-Clause"
    if "redistribution and use in source and binary forms" in normalized:
        return "BSD-2-Clause"
    if "permission to use, copy, modify, and/or distribute this software" in normalized:
        return "ISC"
    if (
        "permission to use, copy, modify, and distribute this software" in normalized
        and "with or without fee" in normalized
    ):
        return "ISC"
    if "the unlicense" in normalized or "released into the public domain" in normalized:
        return "Unlicense"
    return "UNKNOWN"


def find_module_license_files(module_directory: pathlib.Path, module_cache: pathlib.Path) -> list[pathlib.Path]:
    current = module_directory.resolve()
    cache = module_cache.resolve()
    while True:
        candidates = sorted(
            {
                path
                for pattern in ("LICENSE*", "LICENCE*", "COPYING*", "NOTICE*")
                for path in current.glob(pattern)
                if path.is_file() and path.stat().st_size <= 2 * 1024 * 1024
            },
            key=lambda path: path.name.casefold(),
        )
        if candidates:
            return candidates
        if current == cache or cache not in current.parents:
            return []
        current = current.parent


def go_dependency_inventory() -> dict[str, Any]:
    documents = parse_json_stream(run(["go", "list", "-m", "-json", "all"]))
    downloads = parse_json_stream(run(["go", "mod", "download", "-json", "all"]))
    downloaded_by_identity = {
        (str(item.get("Path", "")), str(item.get("Version", ""))): item
        for item in downloads
    }
    module_cache = pathlib.Path(run(["go", "env", "GOMODCACHE"]).strip())
    modules: list[dict[str, Any]] = []
    expressions: set[str] = set()
    missing_license_files: list[str] = []
    unknown_primary_licenses: list[str] = []
    unknown_supplemental_files: list[str] = []
    for module in documents:
        if module.get("Main"):
            continue
        replacement = module.get("Replace") if isinstance(module.get("Replace"), dict) else None
        effective = replacement or module
        download = downloaded_by_identity.get(
            (str(effective.get("Path", "")), str(effective.get("Version", ""))),
            {},
        )
        directory_value = effective.get("Dir") or download.get("Dir")
        license_files: list[dict[str, Any]] = []
        detected: set[str] = set()
        primary_detected: set[str] = set()
        if isinstance(directory_value, str) and directory_value:
            directory = pathlib.Path(directory_value)
            for license_path in find_module_license_files(directory, module_cache):
                content = license_path.read_text(
                    encoding="utf-8",
                    errors="replace",
                )
                normalized_name = license_path.name.upper()
                is_notice = normalized_name.startswith("NOTICE")
                is_primary = (
                    normalized_name in {
                        "LICENSE",
                        "LICENSE.TXT",
                        "LICENSE.MD",
                        "LICENCE",
                        "LICENCE.TXT",
                        "LICENCE.MD",
                        "COPYING",
                        "COPYING.TXT",
                        "COPYING.MD",
                    }
                )
                expression = None if is_notice else detect_license_expression(content)
                if expression is not None:
                    if is_primary:
                        primary_detected.add(expression)
                        detected.add(expression)
                    elif expression != "UNKNOWN":
                        detected.add(expression)
                    else:
                        unknown_supplemental_files.append(
                            f"{module.get('Path', '')}:{license_path.name}"
                        )
                license_files.append(
                    {
                        "name": license_path.name,
                        "sha256": sha256_file(license_path),
                        "kind": (
                            "notice"
                            if is_notice
                            else "primary_license"
                            if is_primary
                            else "supplemental_license"
                        ),
                        "detectedExpression": expression,
                        "inheritedFromParentModule": license_path.parent.resolve()
                        != directory.resolve(),
                    }
                )
        if not license_files:
            missing_license_files.append(str(module.get("Path", "")))
            detected.add("UNKNOWN")
        elif not primary_detected:
            unknown_primary_licenses.append(str(module.get("Path", "")))
            detected.add("UNKNOWN")
        expressions.update(detected)
        modules.append(
            {
                "path": module.get("Path"),
                "version": module.get("Version", ""),
                "replacement": (
                    {
                        "path": replacement.get("Path"),
                        "version": replacement.get("Version", ""),
                    }
                    if replacement
                    else None
                ),
                "downloadError": str(download.get("Error", "")).strip(),
                "licenseFiles": license_files,
                "detectedExpressions": sorted(detected),
            }
        )
    modules.sort(key=lambda item: (str(item["path"]), str(item["version"])))
    return {
        "moduleCount": len(modules),
        "licenseExpressions": sorted(expressions),
        "missingLicenseFileModules": sorted(filter(None, missing_license_files)),
        "unknownPrimaryLicenseModules": sorted(filter(None, unknown_primary_licenses)),
        "unknownSupplementalLicenseFiles": sorted(unknown_supplemental_files),
        "modules": modules,
    }


def node_dependency_inventory() -> dict[str, Any]:
    document = json.loads(run(["pnpm", "licenses", "list", "--json", "--prod"]))
    require(isinstance(document, dict), "pnpm license inventory must be an object")
    packages: list[dict[str, Any]] = []
    expressions: set[str] = set()
    seen: set[tuple[str, tuple[str, ...], str]] = set()
    for grouped_expression, raw_packages in document.items():
        require(isinstance(raw_packages, list), f"pnpm license group {grouped_expression} must be an array")
        for package in raw_packages:
            require(isinstance(package, dict), "pnpm license package must be an object")
            name = str(package.get("name", "")).strip()
            versions = tuple(sorted(str(value) for value in package.get("versions", [])))
            expression = str(package.get("license") or grouped_expression).strip()
            require(name and versions and expression, "pnpm license package identity is incomplete")
            key = (name, versions, expression)
            if key in seen:
                continue
            seen.add(key)
            expressions.add(expression)
            packages.append(
                {
                    "name": name,
                    "versions": list(versions),
                    "licenseExpression": expression,
                    "author": str(package.get("author", "")).strip(),
                    "homepage": str(package.get("homepage", "")).strip(),
                }
            )
    packages.sort(key=lambda item: (item["name"], item["versions"], item["licenseExpression"]))
    return {
        "packageCount": len(packages),
        "licenseExpressions": sorted(expressions),
        "packages": packages,
    }


def tracked_files() -> list[pathlib.Path]:
    raw = subprocess.run(
        ["git", "ls-files", "-z"],
        cwd=ROOT,
        check=True,
        capture_output=True,
    ).stdout
    return [
        ROOT / value.decode("utf-8", errors="strict")
        for value in raw.split(b"\x00")
        if value
    ]


def container_inventory(files: list[pathlib.Path]) -> dict[str, Any]:
    images: dict[str, dict[str, Any]] = {}
    for path in files:
        if not path.name.startswith("Dockerfile"):
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        known_stages: set[str] = set()
        for match in re.finditer(
            r"(?im)^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)(?:\s+AS\s+(\S+))?\s*$",
            text,
        ):
            image, stage = match.group(1), match.group(2)
            if image in known_stages:
                if stage:
                    known_stages.add(stage)
                continue
            entry = images.setdefault(
                image,
                {
                    "image": image,
                    "digestPinned": "@sha256:" in image,
                    "sources": [],
                },
            )
            entry["sources"].append(path.relative_to(ROOT).as_posix())
            if stage:
                known_stages.add(stage)
    compose_path = ROOT / "compose.yml"
    if compose_path.is_file():
        compose = yaml.safe_load(compose_path.read_text(encoding="utf-8"))
        services = compose.get("services", {}) if isinstance(compose, dict) else {}
        require(isinstance(services, dict), "compose.yml services must be an object")
        for service_name, service in services.items():
            if not isinstance(service, dict) or "build" in service:
                continue
            image = service.get("image")
            if not isinstance(image, str) or not image.strip():
                continue
            entry = images.setdefault(
                image,
                {
                    "image": image,
                    "digestPinned": "@sha256:" in image,
                    "sources": [],
                },
            )
            entry["sources"].append(f"compose.yml#services.{service_name}")
    ordered = sorted(images.values(), key=lambda item: item["image"])
    for item in ordered:
        item["sources"] = sorted(set(item["sources"]))
    return {
        "externalImageCount": len(ordered),
        "unpinnedImages": [
            item["image"] for item in ordered if not item["digestPinned"]
        ],
        "images": ordered,
    }


def binary_asset_inventory(files: list[pathlib.Path]) -> dict[str, Any]:
    assets = []
    for path in files:
        if path.suffix.lower() not in BINARY_ASSET_SUFFIXES or not path.is_file():
            continue
        assets.append(
            {
                "path": path.relative_to(ROOT).as_posix(),
                "sha256": sha256_file(path),
                "sizeBytes": path.stat().st_size,
            }
        )
    assets.sort(key=lambda item: item["path"])
    return {
        "assetCount": len(assets),
        "assets": assets,
    }


def classify_license_expression(expression: str) -> str:
    normalized = expression.upper()
    if any(marker in normalized for marker in UNKNOWN_LICENSE_MARKERS):
        return "unknown"
    if any(marker in normalized for marker in REVIEW_LICENSE_MARKERS):
        return "legal_review_required"
    return "inventory_only"


def build_findings(report: dict[str, Any]) -> list[dict[str, str]]:
    findings: list[dict[str, str]] = []
    missing_artifacts = [
        key
        for key, value in report["legalArtifacts"].items()
        if not value["present"]
    ]
    if missing_artifacts:
        findings.append(
            {
                "severity": "blocker",
                "code": "LEGAL_ARTIFACTS_MISSING",
                "message": "Missing root legal artifacts: " + ", ".join(missing_artifacts),
            }
        )
    if report["gitHistory"]["commitsWithoutDCO"] > 0:
        findings.append(
            {
                "severity": "review",
                "code": "CONTRIBUTION_GRANTS_UNPROVEN",
                "message": (
                    f"{report['gitHistory']['commitsWithoutDCO']} commits have no matching "
                    "Signed-off-by trailer; ownership or CLA evidence requires counsel review."
                ),
            }
        )
    expressions = sorted(
        set(report["thirdParty"]["go"]["licenseExpressions"])
        | set(report["thirdParty"]["node"]["licenseExpressions"])
    )
    unknown = [
        expression
        for expression in expressions
        if classify_license_expression(expression) == "unknown"
    ]
    review = [
        expression
        for expression in expressions
        if classify_license_expression(expression) == "legal_review_required"
    ]
    if unknown:
        findings.append(
            {
                "severity": "blocker",
                "code": "DEPENDENCY_LICENSE_UNKNOWN",
                "message": "Unknown or non-license dependency expressions: " + ", ".join(unknown),
            }
        )
    supplemental_unknown = report["thirdParty"]["go"].get(
        "unknownSupplementalLicenseFiles",
        [],
    )
    if supplemental_unknown:
        findings.append(
            {
                "severity": "review",
                "code": "SUPPLEMENTAL_ASSET_LICENSE_REVIEW_REQUIRED",
                "message": (
                    "Supplemental license or asset files need manual classification: "
                    + ", ".join(supplemental_unknown)
                ),
            }
        )
    if review:
        findings.append(
            {
                "severity": "review",
                "code": "RECIPROCAL_OR_ATTRIBUTION_REVIEW_REQUIRED",
                "message": "Dependency expressions requiring counsel review: " + ", ".join(review),
            }
        )
    if report["containers"]["unpinnedImages"]:
        findings.append(
            {
                "severity": "blocker",
                "code": "CONTAINER_IMAGE_NOT_PINNED",
                "message": "External images without digest: "
                + ", ".join(report["containers"]["unpinnedImages"]),
            }
        )
    if report["binaryAssets"]["assetCount"]:
        findings.append(
            {
                "severity": "review",
                "code": "BINARY_ASSET_PROVENANCE_REQUIRED",
                "message": (
                    f"{report['binaryAssets']['assetCount']} tracked binary assets require "
                    "origin and redistribution evidence."
                ),
            }
        )
    findings.append(
        {
            "severity": "blocker",
            "code": "QUALIFIED_COUNSEL_APPROVAL_REQUIRED",
            "message": "Engineering inventory is not a legal opinion; a matching signed approval record is required.",
        }
    )
    return findings


def load_approval(path: pathlib.Path, inventory_hash: str) -> dict[str, Any]:
    try:
        approval = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read legal approval {path}: {exc}") from exc
    require(isinstance(approval, dict), "legal approval must be an object")
    require(
        approval.get("schemaVersion") == APPROVAL_SCHEMA_VERSION,
        f"legal approval schemaVersion must be {APPROVAL_SCHEMA_VERSION}",
    )
    require(approval.get("inventorySha256") == inventory_hash, "legal approval does not bind the current inventory")
    require(approval.get("reviewerRole") == "qualified_counsel", "legal approval reviewerRole must be qualified_counsel")
    require(
        approval.get("softwareLicenseSpdx") == EXPECTED_SOFTWARE_LICENSE_SPDX,
        "legal approval softwareLicenseSpdx must match the selected CE license",
    )
    for field in (
        "softwareLicenseApproved",
        "dualLicenseApproved",
        "contributorGrantApproved",
        "thirdPartyNoticesApproved",
        "trademarkPolicyApproved",
    ):
        require(approval.get(field) is True, f"legal approval field {field} must be true")
    require(
        isinstance(approval.get("evidenceReference"), str)
        and approval["evidenceReference"].strip(),
        "legal approval evidenceReference is required",
    )
    return approval


def build_report(approval_path: pathlib.Path | None = None) -> dict[str, Any]:
    files = tracked_files()
    report: dict[str, Any] = {
        "schemaVersion": REPORT_SCHEMA_VERSION,
        "gitHistory": git_history_inventory(),
        "legalArtifacts": legal_artifact_inventory(),
        "thirdParty": {
            "go": go_dependency_inventory(),
            "node": node_dependency_inventory(),
        },
        "containers": container_inventory(files),
        "binaryAssets": binary_asset_inventory(files),
    }
    inventory_payload = {
        key: report[key]
        for key in (
            "gitHistory",
            "legalArtifacts",
            "thirdParty",
            "containers",
            "binaryAssets",
        )
    }
    inventory_hash = canonical_hash(inventory_payload)
    report["inventorySha256"] = inventory_hash
    report["findings"] = build_findings(report)
    approval = None
    approval_error = None
    if approval_path is not None:
        try:
            approval = load_approval(approval_path, inventory_hash)
        except ValueError as exc:
            approval_error = str(exc)
    report["legalApproval"] = {
        "approved": approval is not None,
        "path": display_path(approval_path) if approval_path is not None else None,
        "error": approval_error,
    }
    report["status"] = (
        "approved"
        if approval is not None
        and all(value["present"] for value in report["legalArtifacts"].values())
        and not any(
            finding["severity"] == "blocker"
            and finding["code"] != "QUALIFIED_COUNSEL_APPROVAL_REQUIRED"
            for finding in report["findings"]
        )
        else "blocked_legal_review"
    )
    return report


def write_report(path: pathlib.Path, report: dict[str, Any]) -> None:
    resolved = path.resolve()
    resolved.parent.mkdir(parents=True, exist_ok=True)
    resolved.write_text(
        json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
        newline="\n",
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Inventory CineWeave source ownership and third-party licensing evidence."
    )
    parser.add_argument("--output", default="tmp/source-licensing-audit.json")
    parser.add_argument("--approval")
    parser.add_argument("--require-legal-approval", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        approval_path = (
            (ROOT / args.approval).resolve()
            if args.approval and not pathlib.Path(args.approval).is_absolute()
            else pathlib.Path(args.approval).resolve()
            if args.approval
            else None
        )
        if approval_path is not None:
            require(approval_path.is_file(), f"legal approval does not exist: {approval_path}")
        report = build_report(approval_path)
        output = (
            (ROOT / args.output).resolve()
            if not pathlib.Path(args.output).is_absolute()
            else pathlib.Path(args.output).resolve()
        )
        write_report(output, report)
    except (OSError, ValueError, subprocess.SubprocessError, yaml.YAMLError) as exc:
        print(f"Source licensing audit failed: {exc}", file=sys.stderr)
        return 2
    print(
        "Source licensing inventory complete: "
        f"status={report['status']} "
        f"contributors={len(report['gitHistory']['contributors'])} "
        f"goModules={report['thirdParty']['go']['moduleCount']} "
        f"nodePackages={report['thirdParty']['node']['packageCount']} "
        f"inventorySha256={report['inventorySha256']}"
    )
    if args.require_legal_approval and report["status"] != "approved":
        for finding in report["findings"]:
            if finding["severity"] == "blocker":
                print(
                    f"{finding['code']}: {finding['message']}",
                    file=sys.stderr,
                )
        if report["legalApproval"]["error"]:
            print(report["legalApproval"]["error"], file=sys.stderr)
        return 1
    return 0


def display_path(path: pathlib.Path) -> str:
    resolved = path.resolve()
    try:
        return resolved.relative_to(ROOT.resolve()).as_posix()
    except ValueError:
        return str(resolved)


if __name__ == "__main__":
    raise SystemExit(main())

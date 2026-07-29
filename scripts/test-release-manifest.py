from __future__ import annotations

import copy
import hashlib
import io
import json
import pathlib
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from typing import Any, Callable


ROOT = pathlib.Path(__file__).resolve().parents[1]
CHECKER = ROOT / "scripts" / "check-release-manifest.py"
FIXTURE = ROOT / "packages" / "edition" / "fixtures" / "combined-release-manifest.valid.json"


def run(
    manifest: pathlib.Path,
    extra_args: list[str] | None = None,
    *,
    contract_only: bool = True,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [
            sys.executable,
            str(CHECKER),
            "--manifest",
            str(manifest),
            *(["--contract-only"] if contract_only else []),
            *(extra_args or []),
        ],
        cwd=ROOT,
        text=True,
        encoding="utf-8",
        capture_output=True,
        check=False,
    )


def write_case(directory: pathlib.Path, name: str, document: dict) -> pathlib.Path:
    path = directory / f"{name}.json"
    path.write_text(json.dumps(document, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return path


def sha256_bytes(content: bytes) -> str:
    return hashlib.sha256(content).hexdigest()


def sha256_file(path: pathlib.Path) -> str:
    return sha256_bytes(path.read_bytes())


def write_json(path: pathlib.Path, document: dict[str, Any]) -> bytes:
    content = (json.dumps(document, ensure_ascii=False, indent=2) + "\n").encode()
    path.write_bytes(content)
    return content


def write_archive(path: pathlib.Path, files: dict[str, bytes]) -> None:
    if path.suffix == ".zip":
        with zipfile.ZipFile(path, mode="w", compression=zipfile.ZIP_STORED) as archive:
            for name, content in sorted(files.items()):
                info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
                info.external_attr = 0o100644 << 16
                archive.writestr(info, content)
        return
    with tarfile.open(path, mode="w") as archive:
        for name, content in sorted(files.items()):
            info = tarfile.TarInfo(name)
            info.size = len(content)
            info.mode = 0o644
            info.mtime = 0
            archive.addfile(info, io.BytesIO(content))


def create_evidence_bundle(
    directory: pathlib.Path,
    baseline: dict[str, Any],
    archive_suffix: str = ".tar",
) -> dict[str, Any]:
    directory.mkdir(parents=True)
    document = copy.deepcopy(baseline)
    core_commit = document["assembly"]["coreCommit"]
    commercial_commit = document["assembly"]["commercialAssemblyCommit"]
    overlay_slots_hash = "5" * 64
    overlay_content = b"commercial overlay fixture\n"
    overlay_entry = {
        "source": "overlay/commercial.txt",
        "destination": "commercial/commercial.txt",
        "operation": "add",
        "sha256": sha256_bytes(overlay_content),
    }

    core_lock_path = directory / "core.lock"
    overlay_path = directory / "overlay-allowlist.v1.json"
    assembly_script_path = directory / "assemble-commercial-release.ps1"
    assembly_inputs_path = directory / "assembly-inputs.json"
    source_archive_path = directory / f"combined-source{archive_suffix}"
    manifest_path = directory / "release-manifest.json"

    write_json(
        core_lock_path,
        {
            "schemaVersion": "cineweave.core-lock.v1",
            "coreCommit": core_commit,
            "overlaySlotsSha256": overlay_slots_hash,
        },
    )
    write_json(
        overlay_path,
        {
            "schemaVersion": "cineweave.overlay-allowlist.v1",
            "coreCommit": core_commit,
            "files": [overlay_entry],
        },
    )
    assembly_script_path.write_text(
        "$ErrorActionPreference = 'Stop'\nWrite-Host 'fixture'\n",
        encoding="utf-8",
    )
    assembly_inputs = {
        "schemaVersion": "cineweave.assembly-inputs.v1",
        "coreCommit": core_commit,
        "commercialAssemblyCommit": commercial_commit,
        "coreLockSha256": sha256_file(core_lock_path),
        "overlayAllowlistSha256": sha256_file(overlay_path),
        "overlaySlotsSha256": overlay_slots_hash,
        "assemblyScriptPath": "scripts/assemble-commercial-release.ps1",
        "assemblyScriptSha256": sha256_file(assembly_script_path),
        "cleanCoreTree": True,
        "cleanCommercialTree": True,
        "assembledAt": "2026-07-29T00:00:00Z",
        "files": [overlay_entry],
    }
    assembly_inputs_bytes = write_json(assembly_inputs_path, assembly_inputs)
    write_archive(
        source_archive_path,
        {
            ".cineweave/assembly-inputs.json": assembly_inputs_bytes,
            assembly_inputs["assemblyScriptPath"]: assembly_script_path.read_bytes(),
            overlay_entry["destination"]: overlay_content,
        },
    )
    document["assembly"].update(
        {
            "coreLockSha256": assembly_inputs["coreLockSha256"],
            "overlayAllowlistSha256": assembly_inputs["overlayAllowlistSha256"],
            "assemblyScriptSha256": assembly_inputs["assemblyScriptSha256"],
            "sourceArchiveSha256": sha256_file(source_archive_path),
            "cleanCoreTree": True,
            "cleanCommercialTree": True,
        }
    )
    write_json(manifest_path, document)
    evidence_args = [
        "--require-assembly-evidence",
        "--assembly-inputs",
        str(assembly_inputs_path),
        "--core-lock",
        str(core_lock_path),
        "--overlay-allowlist",
        str(overlay_path),
        "--assembly-script",
        str(assembly_script_path),
        "--source-archive",
        str(source_archive_path),
    ]
    return {
        "document": document,
        "manifest": manifest_path,
        "assembly_inputs_document": assembly_inputs,
        "assembly_inputs_bytes": assembly_inputs_bytes,
        "assembly_inputs": assembly_inputs_path,
        "core_lock": core_lock_path,
        "overlay": overlay_path,
        "assembly_script": assembly_script_path,
        "source_archive": source_archive_path,
        "overlay_entry": overlay_entry,
        "overlay_content": overlay_content,
        "args": evidence_args,
    }


def rewrite_manifest(bundle: dict[str, Any]) -> None:
    write_json(bundle["manifest"], bundle["document"])


def main() -> int:
    baseline = json.loads(FIXTURE.read_text(encoding="utf-8"))
    valid = run(FIXTURE)
    if valid.returncode != 0:
        print(valid.stdout, end="")
        print(valid.stderr, end="", file=sys.stderr)
        return 1

    mutations: dict[str, Callable[[dict], None]] = {
        "component-release-drift": lambda doc: doc["componentReleaseIds"].__setitem__(
            "api", "release-different-001"
        ),
        "ddl-owner-hash-drift": lambda doc: doc["migrations"].__setitem__(
            "ddlOwnerManifestSha256", "0" * 64
        ),
        "shared-ledger": lambda doc: doc["migrations"].__setitem__(
            "commercial", copy.deepcopy(doc["migrations"]["core"])
        ),
        "missing-image": lambda doc: doc["artifacts"]["images"].pop(),
        "duplicate-image": lambda doc: doc["artifacts"]["images"].append(
            copy.deepcopy(doc["artifacts"]["images"][0])
        ),
        "mutable-image-tag": lambda doc: doc["artifacts"]["images"][0].__setitem__(
            "tag", "latest"
        ),
        "worker-build-drift": lambda doc: doc["artifacts"]["temporalWorkerBuilds"][0].__setitem__(
            "buildId", "release-different-001"
        ),
        "modified-without-patch": lambda doc: doc["upstreamNewAPI"].__setitem__(
            "modified", True
        ),
        "unreviewed-upstream": lambda doc: doc["upstreamNewAPI"]["legalReview"].__setitem__(
            "approved", False
        ),
        "unverified-upstream-image": lambda doc: doc["upstreamNewAPI"].__setitem__(
            "modificationAssessment", "unverified"
        ),
        "notice-presence-drift": lambda doc: doc["upstreamNewAPI"].__setitem__(
            "noticePresent", False
        ),
        "unreviewed-source-license": lambda doc: doc["sourceLicensing"].__setitem__(
            "approved", False
        ),
        "source-license-report-drift": lambda doc: doc["sourceLicensing"].__setitem__(
            "reportSha256", "0" * 64
        ),
        "wrong-community-license": lambda doc: doc["sourceLicensing"].__setitem__(
            "softwareLicenseSpdx", "AGPL-3.0-only"
        ),
        "unreviewed-retention": lambda doc: doc["retention"].__setitem__(
            "approved", False
        ),
        "retention-hash-alias": lambda doc: doc["retention"].__setitem__(
            "approvalEvidenceSha256", doc["retention"]["policySha256"]
        ),
        "future-retention-review": lambda doc: doc["retention"].__setitem__(
            "reviewedAt", "2026-07-30"
        ),
    }

    evidence_invalid_count = 0
    with tempfile.TemporaryDirectory(prefix="cineweave-release-manifest-") as temp:
        directory = pathlib.Path(temp)
        for name, mutate in mutations.items():
            document = copy.deepcopy(baseline)
            mutate(document)
            result = run(write_case(directory, name, document))
            if result.returncode == 0:
                print(f"Release Manifest checker accepted invalid case {name}", file=sys.stderr)
                return 1

        default_without_inputs = run(FIXTURE, contract_only=False)
        evidence_invalid_count += 1
        if default_without_inputs.returncode == 0:
            print(
                "Release Manifest checker accepted missing assembly evidence by default",
                file=sys.stderr,
            )
            return 1

        required_without_inputs = run(
            FIXTURE,
            ["--require-assembly-evidence"],
            contract_only=False,
        )
        evidence_invalid_count += 1
        if required_without_inputs.returncode == 0:
            print(
                "Release Manifest checker accepted missing required assembly evidence",
                file=sys.stderr,
            )
            return 1

        partial_bundle = create_evidence_bundle(directory / "partial-evidence", baseline)
        partial = run(
            partial_bundle["manifest"],
            partial_bundle["args"][:-2],
            contract_only=False,
        )
        evidence_invalid_count += 1
        if partial.returncode == 0:
            print(
                "Release Manifest checker accepted partial assembly evidence",
                file=sys.stderr,
            )
            return 1

        valid_bundle = create_evidence_bundle(directory / "valid-evidence", baseline)
        evidence_valid = run(
            valid_bundle["manifest"],
            valid_bundle["args"],
            contract_only=False,
        )
        if evidence_valid.returncode != 0:
            print(evidence_valid.stdout, end="")
            print(evidence_valid.stderr, end="", file=sys.stderr)
            return 1
        valid_zip_bundle = create_evidence_bundle(
            directory / "valid-zip-evidence",
            baseline,
            archive_suffix=".zip",
        )
        evidence_zip_valid = run(
            valid_zip_bundle["manifest"],
            valid_zip_bundle["args"],
            contract_only=False,
        )
        if evidence_zip_valid.returncode != 0:
            print(evidence_zip_valid.stdout, end="")
            print(evidence_zip_valid.stderr, end="", file=sys.stderr)
            return 1

        def manifest_core_drift(bundle: dict[str, Any]) -> None:
            bundle["document"]["assembly"]["coreCommit"] = "a" * 40
            rewrite_manifest(bundle)

        def manifest_commercial_drift(bundle: dict[str, Any]) -> None:
            bundle["document"]["assembly"]["commercialAssemblyCommit"] = "b" * 40
            rewrite_manifest(bundle)

        def core_lock_drift(bundle: dict[str, Any]) -> None:
            bundle["core_lock"].write_bytes(bundle["core_lock"].read_bytes() + b"\n")

        def overlay_allowlist_drift(bundle: dict[str, Any]) -> None:
            bundle["overlay"].write_bytes(bundle["overlay"].read_bytes() + b"\n")

        def assembly_script_drift(bundle: dict[str, Any]) -> None:
            bundle["assembly_script"].write_bytes(bundle["assembly_script"].read_bytes() + b"\n")

        def source_archive_drift(bundle: dict[str, Any]) -> None:
            bundle["source_archive"].write_bytes(bundle["source_archive"].read_bytes() + b"drift")

        def assembly_inputs_schema_drift(bundle: dict[str, Any]) -> None:
            document = copy.deepcopy(bundle["assembly_inputs_document"])
            document["schemaVersion"] = "cineweave.assembly-inputs.v2"
            write_json(bundle["assembly_inputs"], document)

        def embedded_evidence_drift(bundle: dict[str, Any]) -> None:
            embedded = copy.deepcopy(bundle["assembly_inputs_document"])
            embedded["assembledAt"] = "2026-07-29T01:00:00Z"
            write_archive(
                bundle["source_archive"],
                {
                    ".cineweave/assembly-inputs.json": (
                        json.dumps(embedded, ensure_ascii=False, indent=2) + "\n"
                    ).encode(),
                    bundle["assembly_inputs_document"]["assemblyScriptPath"]: bundle[
                        "assembly_script"
                    ].read_bytes(),
                    bundle["overlay_entry"]["destination"]: bundle["overlay_content"],
                },
            )
            bundle["document"]["assembly"]["sourceArchiveSha256"] = sha256_file(
                bundle["source_archive"]
            )
            rewrite_manifest(bundle)

        def archived_overlay_drift(bundle: dict[str, Any]) -> None:
            write_archive(
                bundle["source_archive"],
                {
                    ".cineweave/assembly-inputs.json": bundle["assembly_inputs_bytes"],
                    bundle["assembly_inputs_document"]["assemblyScriptPath"]: bundle[
                        "assembly_script"
                    ].read_bytes(),
                    bundle["overlay_entry"]["destination"]: b"tampered overlay\n",
                },
            )
            bundle["document"]["assembly"]["sourceArchiveSha256"] = sha256_file(
                bundle["source_archive"]
            )
            rewrite_manifest(bundle)

        def archived_assembly_script_drift(bundle: dict[str, Any]) -> None:
            write_archive(
                bundle["source_archive"],
                {
                    ".cineweave/assembly-inputs.json": bundle["assembly_inputs_bytes"],
                    bundle["assembly_inputs_document"]["assemblyScriptPath"]: b"tampered script\n",
                    bundle["overlay_entry"]["destination"]: bundle["overlay_content"],
                },
            )
            bundle["document"]["assembly"]["sourceArchiveSha256"] = sha256_file(
                bundle["source_archive"]
            )
            rewrite_manifest(bundle)

        evidence_mutations: dict[str, Callable[[dict[str, Any]], None]] = {
            "assembly-manifest-core-drift": manifest_core_drift,
            "assembly-manifest-commercial-drift": manifest_commercial_drift,
            "assembly-core-lock-drift": core_lock_drift,
            "assembly-overlay-allowlist-drift": overlay_allowlist_drift,
            "assembly-script-drift": assembly_script_drift,
            "assembly-source-archive-drift": source_archive_drift,
            "assembly-inputs-schema-drift": assembly_inputs_schema_drift,
            "assembly-embedded-evidence-drift": embedded_evidence_drift,
            "assembly-archived-overlay-drift": archived_overlay_drift,
            "assembly-archived-script-drift": archived_assembly_script_drift,
        }
        for name, mutate in evidence_mutations.items():
            bundle = create_evidence_bundle(directory / name, baseline)
            mutate(bundle)
            result = run(bundle["manifest"], bundle["args"], contract_only=False)
            evidence_invalid_count += 1
            if result.returncode == 0:
                print(
                    f"Release Manifest checker accepted invalid evidence case {name}",
                    file=sys.stderr,
                )
                return 1

    print(
        "Combined Release Manifest regression checks passed: "
        f"invalidCases={len(mutations) + evidence_invalid_count}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

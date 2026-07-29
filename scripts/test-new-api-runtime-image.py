from __future__ import annotations

import copy
import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile
from typing import Any, Callable


ROOT = pathlib.Path(__file__).resolve().parents[1]
CHECKER = ROOT / "scripts" / "check-new-api-runtime-image.py"
CAPTURE = ROOT / "scripts" / "capture-new-api-runtime-image.ps1"
REPOSITORY = "calciumion/new-api"
DIGEST = "sha256:" + "a" * 64
COMMIT = "b" * 40
TAG = "v1.0.0-test"


def write_json(directory: pathlib.Path, name: str, value: Any) -> pathlib.Path:
    path = directory / f"{name}.json"
    path.write_text(
        json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
        newline="\n",
    )
    return path


def run(
    runtime: pathlib.Path,
    upstream: pathlib.Path,
    contract: pathlib.Path,
    release: pathlib.Path,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [
            sys.executable,
            str(CHECKER),
            "--runtime-evidence",
            str(runtime),
            "--upstream-evidence",
            str(upstream),
            "--contract-manifest",
            str(contract),
            "--release-manifest",
            str(release),
        ],
        cwd=ROOT,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )


def main() -> int:
    rejected_output = ROOT / "tmp" / "runtime-image-evidence-must-not-be-written.json"
    capture_boundary = subprocess.run(
        [
            "pwsh",
            "-NoProfile",
            "-File",
            str(CAPTURE),
            "-ContainerName",
            "not-inspected",
            "-OutputPath",
            str(rejected_output),
            "-UpstreamEvidencePath",
            "not-read",
            "-ContractManifestPath",
            "not-read",
            "-ReleaseManifestPath",
            "not-read",
        ],
        cwd=ROOT,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    if capture_boundary.returncode == 0:
        raise AssertionError("runtime image capture accepted in-repository evidence")
    if "outside both source repositories" not in (
        capture_boundary.stdout + capture_boundary.stderr
    ):
        raise AssertionError(
            "runtime image capture failed before enforcing its evidence boundary"
        )
    if rejected_output.exists():
        raise AssertionError("runtime image capture wrote rejected evidence")

    runtime = {
        "schemaVersion": "cineweave.new-api-runtime-image.v1",
        "capturedAt": "2026-07-29T00:00:00Z",
        "container": {
            "name": "/new-api",
            "configuredImageReference": f"{REPOSITORY}@{DIGEST}",
            "imageId": "sha256:" + "c" * 64,
        },
        "image": {
            "repoDigests": [f"docker.io/{REPOSITORY}@{DIGEST}"],
        },
    }
    upstream = {
        "schemaVersion": "cineweave.new-api-upstream-evidence.v1",
        "image": {
            "reference": f"{REPOSITORY}@{DIGEST}",
            "repository": REPOSITORY,
            "digest": DIGEST,
        },
        "source": {
            "commit": COMMIT,
            "tag": TAG,
            "tagResolvedCommit": COMMIT,
        },
    }
    contract = {
        "schemaVersion": "cineweave.new-api-contract-fixtures.v1",
        "upstream": {
            "version": TAG,
            "commit": COMMIT,
            "imageDigest": DIGEST,
        },
    }

    with tempfile.TemporaryDirectory(prefix="cineweave-new-api-runtime-") as temp:
        directory = pathlib.Path(temp)
        upstream_path = write_json(directory, "upstream", upstream)
        release = {
            "schemaVersion": "cineweave.release-manifest.v2",
            "upstreamNewAPI": {
                "registry": REPOSITORY,
                "imageDigest": DIGEST,
                "evidenceSha256": hashlib.sha256(upstream_path.read_bytes()).hexdigest(),
                "sourceCommit": COMMIT,
                "sourceTag": TAG,
            },
        }
        paths = {
            "runtime": write_json(directory, "runtime", runtime),
            "upstream": upstream_path,
            "contract": write_json(directory, "contract", contract),
            "release": write_json(directory, "release", release),
        }

        valid = run(**paths)
        if valid.returncode != 0:
            print(valid.stdout, file=sys.stderr)
            print(valid.stderr, file=sys.stderr)
            raise AssertionError("valid New API runtime image evidence was rejected")

        invalid_cases: dict[
            str,
            Callable[[dict[str, Any], dict[str, Any], dict[str, Any], dict[str, Any]], None],
        ] = {
            "mutable-configured-image": lambda runtime_doc, _upstream, _contract, _release: runtime_doc[
                "container"
            ].__setitem__("configuredImageReference", f"{REPOSITORY}:latest"),
            "running-digest-drift": lambda runtime_doc, _upstream, _contract, _release: runtime_doc[
                "image"
            ].__setitem__(
                "repoDigests",
                [f"{REPOSITORY}@sha256:" + "d" * 64],
            ),
            "upstream-digest-drift": lambda _runtime, upstream_doc, _contract, _release: upstream_doc[
                "image"
            ].__setitem__("digest", "sha256:" + "d" * 64),
            "contract-commit-drift": lambda _runtime, _upstream, contract_doc, _release: contract_doc[
                "upstream"
            ].__setitem__("commit", "d" * 40),
            "release-repository-drift": lambda _runtime, _upstream, _contract, release_doc: release_doc[
                "upstreamNewAPI"
            ].__setitem__("registry", "example.invalid/new-api"),
            "release-evidence-hash-drift": lambda _runtime, _upstream, _contract, release_doc: release_doc[
                "upstreamNewAPI"
            ].__setitem__("evidenceSha256", "d" * 64),
        }

        for name, mutate in invalid_cases.items():
            runtime_doc = copy.deepcopy(runtime)
            upstream_doc = copy.deepcopy(upstream)
            contract_doc = copy.deepcopy(contract)
            release_doc = copy.deepcopy(release)
            mutate(runtime_doc, upstream_doc, contract_doc, release_doc)
            invalid_upstream = write_json(directory, f"{name}-upstream", upstream_doc)
            if name != "release-evidence-hash-drift":
                release_doc["upstreamNewAPI"]["evidenceSha256"] = hashlib.sha256(
                    invalid_upstream.read_bytes()
                ).hexdigest()
            rejected = run(
                write_json(directory, f"{name}-runtime", runtime_doc),
                invalid_upstream,
                write_json(directory, f"{name}-contract", contract_doc),
                write_json(directory, f"{name}-release", release_doc),
            )
            if rejected.returncode == 0:
                raise AssertionError(f"invalid case {name!r} passed")

    print(
        "New API runtime image contract checks passed: "
        f"invalidCases={len(invalid_cases)}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

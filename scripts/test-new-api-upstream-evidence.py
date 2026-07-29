from __future__ import annotations

import importlib.util
import pathlib
import subprocess
import sys
import tempfile


ROOT = pathlib.Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts" / "capture-new-api-upstream-evidence.py"


def load_module():
    spec = importlib.util.spec_from_file_location("new_api_upstream_evidence", MODULE_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load capture-new-api-upstream-evidence.py")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def run(command: list[str], cwd: pathlib.Path) -> str:
    completed = subprocess.run(
        command,
        cwd=cwd,
        check=True,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    return completed.stdout


def expect_value_error(callback) -> None:
    try:
        callback()
    except ValueError:
        return
    raise AssertionError("expected ValueError")


def main() -> int:
    evidence_module = load_module()
    temp_parent = ROOT / ".tmp" / "new-api-evidence-test"
    temp_parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=temp_parent) as temp_directory:
        repository = pathlib.Path(temp_directory) / "upstream"
        repository.mkdir()
        run(["git", "init"], repository)
        run(["git", "config", "user.name", "New API Test"], repository)
        run(["git", "config", "user.email", "new-api-test@example.invalid"], repository)
        (repository / "LICENSE").write_text(
            "GNU AFFERO GENERAL PUBLIC LICENSE\nVersion 3, 19 November 2007\n",
            encoding="utf-8",
        )
        (repository / "README.md").write_text(
            "# New API\n\n## License\n\nAGPLv3. Preserve attribution and the original project link.\n\n## Other\n",
            encoding="utf-8",
        )
        (repository / "NOTICE").write_text("Test notice\n", encoding="utf-8")
        run(["git", "add", "LICENSE", "README.md", "NOTICE"], repository)
        run(["git", "commit", "-m", "fixture"], repository)
        run(["git", "tag", "v1.0.0-test"], repository)
        commit = run(["git", "rev-parse", "HEAD"], repository).strip()

        evidence = evidence_module.capture(
            source_remote=str(repository),
            source_commit=commit,
            source_tag="v1.0.0-test",
            image_reference="calciumion/new-api@" + "sha256:" + "a" * 64,
            image_source_label="https://github.com/QuantumNous/new-api",
            image_revision_label=commit,
            image_version_label="v1.0.0-test",
            image_license_label="AGPL-3.0",
            image_created_at="2026-07-29T00:00:00Z",
        )
        if evidence["source"]["license"]["detectedFamily"] != "AGPL-3.0":
            raise AssertionError(f"license evidence = {evidence['source']['license']}")
        if not evidence["source"]["notice"]["present"]:
            raise AssertionError(f"notice evidence = {evidence['source']['notice']}")
        if not evidence["source"]["readme"]["mentionsOriginalProjectLink"]:
            raise AssertionError(f"README evidence = {evidence['source']['readme']}")
        if evidence["assertions"]["modificationAssessment"] != "unverified":
            raise AssertionError(f"assertions = {evidence['assertions']}")

        expect_value_error(
            lambda: evidence_module.capture(
                source_remote=str(repository),
                source_commit=commit,
                source_tag="v1.0.0-test",
                image_reference="calciumion/new-api@" + "sha256:" + "a" * 64,
                image_source_label="https://github.com/QuantumNous/new-api",
                image_revision_label="b" * 40,
                image_version_label="v1.0.0-test",
                image_license_label="AGPL-3.0",
                image_created_at="2026-07-29T00:00:00Z",
            )
        )
        expect_value_error(
            lambda: evidence_module.resolve_tag(str(repository), "latest")
        )
    try:
        temp_parent.rmdir()
    except OSError:
        pass
    print("New API upstream evidence contract checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

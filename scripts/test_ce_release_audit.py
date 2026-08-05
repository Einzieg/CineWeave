#!/usr/bin/env python3
"""Focused regression tests for the CE release leak auditor."""

from __future__ import annotations

import importlib.util
import io
import json
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts" / "ce_release_audit.py"
POLICY_PATH = ROOT / "packages" / "edition" / "ce-release-policy.v1.json"

spec = importlib.util.spec_from_file_location("ce_release_audit", MODULE_PATH)
assert spec is not None and spec.loader is not None
audit = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = audit
spec.loader.exec_module(audit)


def run(*args: str, cwd: Path) -> None:
    subprocess.run(args, cwd=cwd, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)


def write_layer(outer_path: Path, member_path: str, payload: bytes) -> None:
    layer = io.BytesIO()
    with tarfile.open(fileobj=layer, mode="w") as archive:
        info = tarfile.TarInfo(member_path)
        info.size = len(payload)
        archive.addfile(info, io.BytesIO(payload))
    layer.seek(0)
    with tarfile.open(outer_path, mode="w") as archive:
        layer_path = "blobs/sha256/synthetic-layer"
        info = tarfile.TarInfo(layer_path)
        info.size = len(layer.getvalue())
        archive.addfile(info, io.BytesIO(layer.getvalue()))
        manifest = json.dumps(
            [{"Config": "config.json", "RepoTags": ["test:latest"], "Layers": [layer_path]}]
        ).encode("utf-8")
        info = tarfile.TarInfo("manifest.json")
        info.size = len(manifest)
        archive.addfile(info, io.BytesIO(manifest))


def main() -> None:
    policy = audit.load_policy(POLICY_PATH)
    with tempfile.TemporaryDirectory(prefix="cineweave-ce-audit-test-") as raw:
        temp = Path(raw)

        invalid_policy = json.loads(POLICY_PATH.read_text(encoding="utf-8"))
        invalid_policy["forbiddenPathRules"][0]["scopes"] = ["misspelled-scope"]
        invalid_policy_path = temp / "invalid-policy.json"
        invalid_policy_path.write_text(
            json.dumps(invalid_policy), encoding="utf-8"
        )
        try:
            audit.load_policy(invalid_policy_path)
        except ValueError as error:
            assert "unknown scopes" in str(error)
        else:
            raise AssertionError("invalid policy scope was accepted")

        safe = temp / "safe"
        safe.mkdir()
        (safe / "main.go").write_text("package main\n", encoding="utf-8")
        violations, counts = audit.scan_tree(policy, safe, "source-archive")
        assert not violations
        assert counts["files"] == 1

        compiled_strings = temp / "compiled-strings"
        compiled_strings.mkdir()
        (compiled_strings / "api.exe").write_bytes(
            b"activity_name\x00tas"
            b"k-queuessystem-infodeploymentsset-manager\x00"
            b"sk"
            b"-ecdsa-sha2-nistp256-cert-v01@openssh.com"
        )
        violations, _ = audit.scan_tree(policy, compiled_strings, "go-binaries")
        assert not any(
            value.rule_id == "openai-compatible-key" for value in violations
        )
        (compiled_strings / "packed.js").write_bytes(
            b"prefixAK"
            b"IAABCDEFGHIJKLMNOPsuffix"
        )
        violations, _ = audit.scan_tree(policy, compiled_strings, "web-assets")
        assert not any(value.rule_id == "aws-access-key" for value in violations)
        (compiled_strings / "leaked.txt").write_bytes(
            b"credential="
            + b"AKIA"
            + b"ABCDEFGHIJKLMNOP\n"
            + b"api_key="
            + b"sk-"
            + b"this_is_a_synthetic_key_1234567890\n"
            + b"router_key="
            + b"sk-or-v1-"
            + b"0123456789ABCDEF0123456789ABCDEF\n"
        )
        violations, _ = audit.scan_tree(policy, compiled_strings, "web-assets")
        assert any(value.rule_id == "aws-access-key" for value in violations)
        assert any(
            value.rule_id == "openai-compatible-key" for value in violations
        )
        assert any(value.rule_id == "openrouter-key" for value in violations)

        environment_files = temp / "environment-files"
        environment_files.mkdir()
        (environment_files / ".env.example").write_text(
            "SAFE_EXAMPLE=true\n", encoding="utf-8"
        )
        violations, _ = audit.scan_tree(policy, environment_files, "source-archive")
        assert not violations
        (environment_files / ".env.production").write_text(
            "NOT_A_REAL_SECRET=test\n", encoding="utf-8"
        )
        violations, _ = audit.scan_tree(policy, environment_files, "source-archive")
        assert any(
            value.rule_id == "local-environment-file" for value in violations
        )

        generated = temp / "generated"
        generated_dependency = generated / "standalone" / "node_modules" / "safe"
        generated_dependency.mkdir(parents=True)
        (generated_dependency / "index.js").write_text(
            "module.exports = {};\n", encoding="utf-8"
        )
        violations, _ = audit.scan_tree(policy, generated, "web-assets")
        assert not any(
            value.rule_id == "generated-source-artifact" for value in violations
        )
        violations, _ = audit.scan_tree(policy, generated, "source-archive")
        assert any(
            value.rule_id == "generated-source-artifact" for value in violations
        )

        private = temp / "private-tree" / "services" / "new-api-billing-bridge"
        private.mkdir(parents=True)
        (private / "main.go").write_text("package main\n", encoding="utf-8")
        violations, _ = audit.scan_tree(
            policy, temp / "private-tree", "source-archive"
        )
        assert any(value.rule_id == "billing-bridge-service" for value in violations)

        artifact = temp / "artifact"
        artifact.mkdir()
        (artifact / "chunk.js").write_text(
            'import "@cineweave/' + 'commercial-billing";\n', encoding="utf-8"
        )
        violations, _ = audit.scan_tree(policy, artifact, "web-assets")
        assert any(value.rule_id == "private-web-module" for value in violations)

        image_path = temp / "image.tar"
        private_key = (
            b"-----BEGIN PRIVATE "
            b"KEY-----\n"
            + (b"A" * 120)
            + b"\n-----END PRIVATE KEY-----\n"
        )
        write_layer(
            image_path,
            "app/secret.txt",
            private_key,
        )
        violations, counts = audit.scan_tar(policy, image_path, "image-layer")
        assert counts["layers"] == 1
        assert any(value.rule_id == "private-key" for value in violations)

        traversal_path = temp / "traversal-image.tar"
        write_layer(
            traversal_path,
            "../commercial/secret.go",
            b"package commercial\n",
        )
        try:
            audit.scan_tar(policy, traversal_path, "image-layer")
        except RuntimeError as error:
            assert "unsafe path" in str(error)
        else:
            raise AssertionError("unsafe image layer path was accepted")

        repo = temp / "history"
        repo.mkdir()
        run("git", "init", "--initial-branch=main", cwd=repo)
        run("git", "config", "user.email", "ce-audit@example.invalid", cwd=repo)
        run("git", "config", "user.name", "CE Audit", cwd=repo)
        (repo / "README.md").write_text("safe\n", encoding="utf-8")
        run("git", "add", "README.md", cwd=repo)
        run("git", "commit", "-m", "safe", cwd=repo)
        metadata_key = "sk-" + "metadata_secret_0123456789ABCDEF"
        run(
            "git",
            "commit",
            "--allow-empty",
            "-m",
            f"accidental credential {metadata_key}",
            cwd=repo,
        )
        leaked = repo / "commercial"
        leaked.mkdir()
        (leaked / "secret.go").write_text("package commercial\n", encoding="utf-8")
        run("git", "add", "commercial/secret.go", cwd=repo)
        run("git", "commit", "-m", "leak", cwd=repo)
        run("git", "rm", "commercial/secret.go", cwd=repo)
        run("git", "commit", "-m", "remove leak", cwd=repo)
        violations, counts = audit.scan_git_history(policy, repo)
        assert counts["blobs"] >= 2
        assert any(value.rule_id == "private-root" for value in violations)
        assert any(
            value.rule_id == "openai-compatible-key"
            and value.path.startswith("@git-object/commit/")
            for value in violations
        )

        transient_repo = temp / "history-transient-refs"
        transient_repo.mkdir()
        run("git", "init", "--initial-branch=main", cwd=transient_repo)
        run(
            "git",
            "config",
            "user.email",
            "ce-audit@example.invalid",
            cwd=transient_repo,
        )
        run("git", "config", "user.name", "CE Audit", cwd=transient_repo)
        (transient_repo / "README.md").write_text("safe\n", encoding="utf-8")
        run("git", "add", "README.md", cwd=transient_repo)
        run("git", "commit", "-m", "safe", cwd=transient_repo)
        run("git", "switch", "--detach", cwd=transient_repo)
        (transient_repo / "internal-snapshot.txt").write_text(
            "api_key=sk-" + "transient_internal_ref_0123456789ABCDEF\n",
            encoding="utf-8",
        )
        run("git", "add", "internal-snapshot.txt", cwd=transient_repo)
        run("git", "commit", "-m", "tool snapshot", cwd=transient_repo)
        transient_commit = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=transient_repo,
            check=True,
            stdout=subprocess.PIPE,
            text=True,
        ).stdout.strip()
        run(
            "git",
            "update-ref",
            "refs/codex/turn-diffs/test",
            transient_commit,
            cwd=transient_repo,
        )
        run("git", "switch", "main", cwd=transient_repo)
        violations, counts = audit.scan_git_history(policy, transient_repo)
        assert counts["refs"] == 1
        assert not violations

        run(
            "git",
            "update-ref",
            "refs/heads/leaked-release-branch",
            transient_commit,
            cwd=transient_repo,
        )
        violations, counts = audit.scan_git_history(policy, transient_repo)
        assert counts["refs"] == 2
        assert any(
            value.rule_id == "openai-compatible-key"
            and value.path == "internal-snapshot.txt"
            for value in violations
        )

    print("CE release audit regression tests passed.")


if __name__ == "__main__":
    main()

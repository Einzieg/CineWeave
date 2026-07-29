from __future__ import annotations

import importlib.util
import json
import pathlib
import sys
import tempfile


ROOT = pathlib.Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts" / "audit-source-licensing.py"


def load_audit_module():
    spec = importlib.util.spec_from_file_location("source_licensing_audit", MODULE_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load audit-source-licensing.py")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def expect_value_error(callback) -> None:
    try:
        callback()
    except ValueError:
        return
    raise AssertionError("expected ValueError")


def main() -> int:
    audit = load_audit_module()
    license_cases = {
        "Permission is hereby granted, free of charge, to any person": "MIT",
        "Apache License Version 2.0, January 2004": "Apache-2.0",
        "GNU AFFERO GENERAL PUBLIC LICENSE Version 3": "AGPL",
        (
            "Permission to use, copy, modify, and distribute this software for any "
            "purpose with or without fee is hereby granted"
        ): "ISC",
        "private terms supplied elsewhere": "UNKNOWN",
    }
    for text, expected in license_cases.items():
        actual = audit.detect_license_expression(text)
        if actual != expected:
            raise AssertionError(f"license detection = {actual}, want {expected}")

    parsed = audit.parse_json_stream('{"a":1}\n{"b":2}\n')
    if parsed != [{"a": 1}, {"b": 2}]:
        raise AssertionError(f"JSON stream = {parsed}")
    if audit.classify_license_expression("MIT") != "inventory_only":
        raise AssertionError("MIT classification drifted")
    if (
        audit.classify_license_expression("Apache-2.0 AND LGPL-3.0-or-later")
        != "legal_review_required"
    ):
        raise AssertionError("LGPL classification drifted")
    if audit.classify_license_expression("UNLICENSED") != "unknown":
        raise AssertionError("UNLICENSED classification drifted")

    temp_parent = ROOT / ".tmp" / "source-license-audit-test"
    temp_parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=temp_parent) as temp_directory:
        approval_path = pathlib.Path(temp_directory) / "approval.json"
        inventory_hash = "a" * 64
        approval = {
            "schemaVersion": "cineweave.source-license-approval.v2",
            "inventorySha256": inventory_hash,
            "reviewId": "LEGAL-TEST",
            "reviewedAt": "2026-07-29T00:00:00Z",
            "reviewerRole": "qualified_counsel",
            "softwareLicenseSpdx": "AGPL-3.0-or-later",
            "softwareLicenseApproved": True,
            "internalCommercialUseApproved": True,
            "contributorGrantApproved": True,
            "thirdPartyNoticesApproved": True,
            "trademarkPolicyApproved": True,
            "evidenceReference": "controlled://legal/LEGAL-TEST",
        }
        approval_path.write_text(json.dumps(approval), encoding="utf-8")
        loaded = audit.load_approval(approval_path, inventory_hash)
        if loaded["reviewId"] != "LEGAL-TEST":
            raise AssertionError(f"approval = {loaded}")
        expect_value_error(lambda: audit.load_approval(approval_path, "b" * 64))
        approval["softwareLicenseSpdx"] = "AGPL-3.0-only"
        approval_path.write_text(json.dumps(approval), encoding="utf-8")
        expect_value_error(lambda: audit.load_approval(approval_path, inventory_hash))

    try:
        temp_parent.rmdir()
    except OSError:
        pass
    print("Source licensing audit contract checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

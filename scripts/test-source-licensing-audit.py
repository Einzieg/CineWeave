from __future__ import annotations

import importlib.util
import pathlib
import sys


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
        != "manual_review"
    ):
        raise AssertionError("LGPL classification drifted")
    if audit.classify_license_expression("UNLICENSED") != "unknown":
        raise AssertionError("UNLICENSED classification drifted")
    if not audit.is_release_ready({"status": "ready"}):
        raise AssertionError("ready report was rejected")
    if audit.is_release_ready({"status": "attention_required"}):
        raise AssertionError("attention_required report was accepted")

    print("Source licensing audit contract checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

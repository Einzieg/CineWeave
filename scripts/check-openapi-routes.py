from __future__ import annotations

import pathlib
import re
import sys

try:
    import yaml
except ImportError:
    print("PyYAML is required: python -m pip install pyyaml", file=sys.stderr)
    raise


ROOT = pathlib.Path(__file__).resolve().parents[1]
OPENAPI_PATH = ROOT / "packages" / "openapi" / "openapi.yaml"

ROUTE_SOURCES = [
    ROOT / "internal" / "api" / "server.go",
    ROOT / "apps" / "realtime" / "main.go",
    ROOT / "services" / "provider-gateway" / "main.go",
]

HTTP_METHODS = {"get", "post", "patch", "delete", "put"}

ALLOW_MISSING_OPENAPI: set[tuple[str, str]] = set()


def infer_method(path: str) -> str:
    if path in {"/healthz", "/readyz", "/api/realtime/events"}:
        return "get"
    return "post"


def actual_routes() -> set[tuple[str, str]]:
    routes: set[tuple[str, str]] = set()
    pattern = re.compile(r'mux\.Handle(?:Func)?\("([^"]+)"')
    for source in ROUTE_SOURCES:
        text = source.read_text(encoding="utf-8")
        for match in pattern.finditer(text):
            registration = match.group(1)
            if " " in registration:
                method, path = registration.split(" ", 1)
                routes.add((method.lower(), path))
            else:
                routes.add((infer_method(registration), registration))
    return routes


def openapi_routes() -> set[tuple[str, str]]:
    document = yaml.safe_load(OPENAPI_PATH.read_text(encoding="utf-8"))
    routes: set[tuple[str, str]] = set()
    for path, operations in document.get("paths", {}).items():
        for method in operations:
            if method in HTTP_METHODS:
                routes.add((method, path))
    return routes


def main() -> int:
    actual = actual_routes()
    documented = openapi_routes()

    missing = sorted((actual - documented) - ALLOW_MISSING_OPENAPI)
    stale = sorted(documented - actual)

    if missing or stale:
        if missing:
            print("Routes registered in Go but missing from OpenAPI:", file=sys.stderr)
            for method, path in missing:
                print(f"  {method.upper()} {path}", file=sys.stderr)
        if stale:
            print("Routes documented in OpenAPI but not registered in Go:", file=sys.stderr)
            for method, path in stale:
                print(f"  {method.upper()} {path}", file=sys.stderr)
        return 1

    print(f"OpenAPI routes match Go registrations ({len(documented)} documented routes).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys
from dataclasses import dataclass

try:
    import yaml
except ImportError:
    print("PyYAML is required: python -m pip install pyyaml", file=sys.stderr)
    raise


ROOT = pathlib.Path(__file__).resolve().parents[1]
DEFAULT_OPENAPI_PATH = ROOT / "packages" / "openapi" / "openapi.yaml"
DEFAULT_ROUTE_SOURCE_MANIFEST = ROOT / "packages" / "openapi" / "route-sources.ce.json"
HTTP_METHODS = {"get", "post", "patch", "delete", "put"}
MANIFEST_SCHEMA_VERSION = "cineweave.route-sources.v1"
ROUTE_LIST_SCHEMA_VERSION = "cineweave.edition-api-routes.v1"


@dataclass(frozen=True)
class RouteSource:
    path: pathlib.Path
    parser: str
    default_implicit_method: str | None


@dataclass(frozen=True)
class RouteSourceManifest:
    sources: tuple[RouteSource, ...]
    implicit_methods: dict[str, str]
    allow_missing_openapi: frozenset[tuple[str, str]]


def workspace_path(value: str, *, label: str) -> pathlib.Path:
    candidate = pathlib.Path(value)
    if not candidate.is_absolute():
        candidate = ROOT / candidate
    resolved = candidate.resolve()
    try:
        resolved.relative_to(ROOT.resolve())
    except ValueError as exc:
        raise ValueError(f"{label} must stay inside the assembled workspace: {value}") from exc
    return resolved


def load_route_source_manifest(path: pathlib.Path) -> RouteSourceManifest:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read route-source manifest {path}: {exc}") from exc
    if document.get("schemaVersion") != MANIFEST_SCHEMA_VERSION:
        raise ValueError(
            f"route-source manifest schemaVersion must be {MANIFEST_SCHEMA_VERSION}"
        )
    raw_sources = document.get("sources")
    if not isinstance(raw_sources, list) or not raw_sources:
        raise ValueError("route-source manifest must contain at least one source")
    sources: list[RouteSource] = []
    seen_paths: set[pathlib.Path] = set()
    for index, raw_source in enumerate(raw_sources):
        if not isinstance(raw_source, dict):
            raise ValueError(f"route source {index} must be an object")
        raw_path = raw_source.get("path")
        parser = raw_source.get("parser")
        default_implicit_method = raw_source.get("defaultImplicitMethod")
        if not isinstance(raw_path, str) or not raw_path.strip():
            raise ValueError(f"route source {index} path is required")
        if parser not in {"go-http-mux", "route-list-json"}:
            raise ValueError(f"route source {raw_path} has unsupported parser {parser!r}")
        if default_implicit_method is not None:
            default_implicit_method = str(default_implicit_method).lower()
            if default_implicit_method not in HTTP_METHODS:
                raise ValueError(
                    f"route source {raw_path} has invalid defaultImplicitMethod {default_implicit_method!r}"
                )
        source_path = workspace_path(raw_path, label="route source")
        if source_path in seen_paths:
            raise ValueError(f"route source is duplicated: {raw_path}")
        if not source_path.is_file():
            raise ValueError(f"route source does not exist: {raw_path}")
        seen_paths.add(source_path)
        sources.append(
            RouteSource(
                path=source_path,
                parser=parser,
                default_implicit_method=default_implicit_method,
            )
        )

    implicit_methods = document.get("implicitMethods", {})
    if not isinstance(implicit_methods, dict):
        raise ValueError("implicitMethods must be an object")
    normalized_implicit_methods: dict[str, str] = {}
    for route_path, method in implicit_methods.items():
        if not isinstance(route_path, str) or not route_path.startswith("/"):
            raise ValueError(f"implicit method route is invalid: {route_path!r}")
        normalized_method = str(method).lower()
        if normalized_method not in HTTP_METHODS:
            raise ValueError(
                f"implicit method for {route_path} is invalid: {normalized_method}"
            )
        normalized_implicit_methods[route_path] = normalized_method

    raw_allowlist = document.get("allowMissingOpenAPI", [])
    if not isinstance(raw_allowlist, list):
        raise ValueError("allowMissingOpenAPI must be an array")
    allowlist: set[tuple[str, str]] = set()
    for index, item in enumerate(raw_allowlist):
        if not isinstance(item, dict):
            raise ValueError(f"allowMissingOpenAPI item {index} must be an object")
        method = str(item.get("method", "")).lower()
        route_path = item.get("path")
        if method not in HTTP_METHODS or not isinstance(route_path, str) or not route_path.startswith("/"):
            raise ValueError(f"allowMissingOpenAPI item {index} is invalid")
        allowlist.add((method, route_path))

    return RouteSourceManifest(
        sources=tuple(sources),
        implicit_methods=normalized_implicit_methods,
        allow_missing_openapi=frozenset(allowlist),
    )


def actual_routes(manifest: RouteSourceManifest) -> set[tuple[str, str]]:
    routes: set[tuple[str, str]] = set()
    pattern = re.compile(r'mux\.Handle(?:Func)?\("([^"]+)"')
    for source in manifest.sources:
        if source.parser == "route-list-json":
            routes.update(route_list_routes(source.path))
            continue
        text = source.path.read_text(encoding="utf-8")
        for match in pattern.finditer(text):
            registration = match.group(1)
            if " " in registration:
                method, route_path = registration.split(" ", 1)
                normalized_method = method.lower()
            else:
                route_path = registration
                normalized_method = manifest.implicit_methods.get(
                    route_path, source.default_implicit_method or ""
                )
                if not normalized_method:
                    raise ValueError(
                        f"route {route_path} in {source.path} has no method and no implicitMethods entry"
                    )
            if normalized_method not in HTTP_METHODS:
                raise ValueError(
                    f"route {route_path} in {source.path} uses unsupported method {normalized_method}"
                )
            routes.add((normalized_method, route_path))
    return routes


def route_list_routes(path: pathlib.Path) -> set[tuple[str, str]]:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read Edition API route list {path}: {exc}") from exc
    if not isinstance(document, dict) or document.get("schemaVersion") != ROUTE_LIST_SCHEMA_VERSION:
        raise ValueError(
            f"Edition API route list {path} schemaVersion must be {ROUTE_LIST_SCHEMA_VERSION}"
        )
    raw_routes = document.get("routes")
    if not isinstance(raw_routes, list) or not raw_routes:
        raise ValueError(f"Edition API route list {path} must contain routes")
    routes: set[tuple[str, str]] = set()
    operation_ids: set[str] = set()
    for index, item in enumerate(raw_routes):
        if not isinstance(item, dict):
            raise ValueError(f"Edition API route list {path} route {index} must be an object")
        method = str(item.get("method", "")).lower()
        route_path = item.get("path")
        operation_id = item.get("operationId")
        if (
            method not in HTTP_METHODS
            or not isinstance(route_path, str)
            or not route_path.startswith("/api/")
            or not isinstance(operation_id, str)
            or not operation_id.strip()
        ):
            raise ValueError(f"Edition API route list {path} route {index} is invalid")
        route = (method, route_path)
        if route in routes:
            raise ValueError(
                f"Edition API route list {path} duplicates {method.upper()} {route_path}"
            )
        if operation_id in operation_ids:
            raise ValueError(
                f"Edition API route list {path} duplicates operationId {operation_id}"
            )
        routes.add(route)
        operation_ids.add(operation_id)
    return routes


def openapi_routes(path: pathlib.Path) -> set[tuple[str, str]]:
    try:
        document = yaml.safe_load(path.read_text(encoding="utf-8"))
    except OSError as exc:
        raise ValueError(f"cannot read OpenAPI contract {path}: {exc}") from exc
    if not isinstance(document, dict):
        raise ValueError(f"OpenAPI contract {path} must contain an object")
    routes: set[tuple[str, str]] = set()
    for route_path, operations in document.get("paths", {}).items():
        if not isinstance(operations, dict):
            continue
        for method in operations:
            if method in HTTP_METHODS:
                routes.add((method, route_path))
    return routes


def check_routes(
    contract_path: pathlib.Path, route_source_manifest_path: pathlib.Path
) -> tuple[list[tuple[str, str]], list[tuple[str, str]], int]:
    manifest = load_route_source_manifest(route_source_manifest_path)
    actual = actual_routes(manifest)
    documented = openapi_routes(contract_path)
    missing = sorted((actual - documented) - manifest.allow_missing_openapi)
    stale = sorted(documented - actual)
    return missing, stale, len(documented)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Verify an explicit OpenAPI contract against an explicit route-source manifest."
    )
    parser.add_argument(
        "--contract",
        default=str(DEFAULT_OPENAPI_PATH),
        help="OpenAPI YAML path, relative to the assembled workspace or absolute within it.",
    )
    parser.add_argument(
        "--route-source-manifest",
        default=str(DEFAULT_ROUTE_SOURCE_MANIFEST),
        help="Route-source manifest path, relative to the assembled workspace or absolute within it.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        contract_path = workspace_path(args.contract, label="OpenAPI contract")
        route_source_manifest_path = workspace_path(
            args.route_source_manifest, label="route-source manifest"
        )
        missing, stale, documented_count = check_routes(
            contract_path, route_source_manifest_path
        )
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 2

    if missing or stale:
        if missing:
            print("Routes registered in sources but missing from OpenAPI:", file=sys.stderr)
            for method, route_path in missing:
                print(f"  {method.upper()} {route_path}", file=sys.stderr)
        if stale:
            print("Routes documented in OpenAPI but not registered in sources:", file=sys.stderr)
            for method, route_path in stale:
                print(f"  {method.upper()} {route_path}", file=sys.stderr)
        return 1

    print(
        f"OpenAPI routes match route-source manifest ({documented_count} documented routes)."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

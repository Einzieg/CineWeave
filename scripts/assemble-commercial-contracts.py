from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
import pathlib
import shutil
import sys
import tempfile
from typing import Any

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
HTTP_METHODS = {"get", "post", "put", "patch", "delete"}
ROUTE_SOURCE_SCHEMA_VERSION = "cineweave.route-sources.v1"
ROUTE_LIST_SCHEMA_VERSION = "cineweave.edition-api-routes.v1"
EVIDENCE_SCHEMA_VERSION = "cineweave.commercial-contract-assembly.v1"
OPENAPI_EXTENSION_KEYS = {"openapi", "info", "paths", "components", "tags"}


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def workspace_path(value: str | pathlib.Path, *, label: str) -> pathlib.Path:
    candidate = pathlib.Path(value)
    if not candidate.is_absolute():
        candidate = ROOT / candidate
    resolved = candidate.resolve()
    try:
        resolved.relative_to(ROOT.resolve())
    except ValueError as exc:
        raise ValueError(f"{label} must stay inside the assembled workspace: {value}") from exc
    return resolved


def relative_workspace_path(path: pathlib.Path) -> str:
    return path.resolve().relative_to(ROOT.resolve()).as_posix()


def load_yaml_object(path: pathlib.Path, *, label: str) -> dict[str, Any]:
    try:
        document = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as exc:
        raise ValueError(f"cannot read {label} {path}: {exc}") from exc
    require(isinstance(document, dict), f"{label} {path} must contain an object")
    return document


def load_json_object(path: pathlib.Path, *, label: str) -> dict[str, Any]:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read {label} {path}: {exc}") from exc
    require(isinstance(document, dict), f"{label} {path} must contain an object")
    return document


def canonical_hash(value: Any) -> str:
    payload = json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return hashlib.sha256(payload).hexdigest()


def file_hash(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def operation_index(document: dict[str, Any], *, label: str) -> dict[str, tuple[str, str, str]]:
    paths = document.get("paths")
    require(isinstance(paths, dict), f"{label} paths must be an object")
    result: dict[str, tuple[str, str, str]] = {}
    for route_path, path_item in paths.items():
        require(
            isinstance(route_path, str) and route_path.startswith("/"),
            f"{label} path {route_path!r} is invalid",
        )
        require(isinstance(path_item, dict), f"{label} path {route_path} must be an object")
        for method, operation in path_item.items():
            if method not in HTTP_METHODS:
                continue
            require(
                isinstance(operation, dict),
                f"{label} operation {method.upper()} {route_path} must be an object",
            )
            operation_id = operation.get("operationId")
            require(
                isinstance(operation_id, str) and operation_id.strip(),
                f"{label} operation {method.upper()} {route_path} requires operationId",
            )
            if operation_id in result:
                previous = result[operation_id]
                raise ValueError(
                    f"{label} duplicates operationId {operation_id}: "
                    f"{previous[0].upper()} {previous[1]} and {method.upper()} {route_path}"
                )
            result[operation_id] = (method, route_path, canonical_hash(operation))
    return result


def merge_openapi(
    core_path: pathlib.Path,
    extension_paths: list[pathlib.Path],
) -> tuple[dict[str, Any], dict[tuple[str, str], str]]:
    core = load_yaml_object(core_path, label="Core OpenAPI")
    require(
        isinstance(core.get("openapi"), str) and core["openapi"].startswith("3."),
        "Core OpenAPI version must be 3.x",
    )
    merged = copy.deepcopy(core)
    merged.setdefault("paths", {})
    merged.setdefault("components", {})
    core_routes = {
        (method, route_path): operation_id
        for operation_id, (method, route_path, _) in operation_index(core, label="Core OpenAPI").items()
    }
    operation_ids = operation_index(merged, label="Core OpenAPI")
    added_routes: dict[tuple[str, str], str] = {}

    for extension_path in extension_paths:
        extension = load_yaml_object(extension_path, label="Commercial OpenAPI extension")
        unsupported = set(extension) - OPENAPI_EXTENSION_KEYS
        require(
            not unsupported,
            f"Commercial OpenAPI extension {extension_path} has unsupported top-level keys: {sorted(unsupported)}",
        )
        require(
            extension.get("openapi") == core.get("openapi"),
            f"Commercial OpenAPI extension {extension_path} version differs from Core",
        )
        extension_paths_object = extension.get("paths")
        require(
            isinstance(extension_paths_object, dict) and extension_paths_object,
            f"Commercial OpenAPI extension {extension_path} must contain paths",
        )
        for route_path, path_item in extension_paths_object.items():
            require(
                isinstance(route_path, str) and route_path.startswith("/api/"),
                f"Commercial OpenAPI extension path {route_path!r} must start with /api/",
            )
            require(
                isinstance(path_item, dict),
                f"Commercial OpenAPI extension path {route_path} must be an object",
            )
            target_path_item = merged["paths"].setdefault(route_path, {})
            require(
                isinstance(target_path_item, dict),
                f"Core OpenAPI path {route_path} must be an object",
            )
            for key, value in path_item.items():
                if key not in HTTP_METHODS:
                    if key in target_path_item:
                        require(
                            canonical_hash(target_path_item[key]) == canonical_hash(value),
                            f"OpenAPI path metadata conflict at {route_path}.{key}",
                        )
                    else:
                        target_path_item[key] = copy.deepcopy(value)
                    continue
                require(
                    isinstance(value, dict),
                    f"Commercial OpenAPI operation {key.upper()} {route_path} must be an object",
                )
                operation_id = value.get("operationId")
                require(
                    isinstance(operation_id, str) and operation_id.strip(),
                    f"Commercial OpenAPI operation {key.upper()} {route_path} requires operationId",
                )
                route = (key, route_path)
                if key in target_path_item:
                    require(
                        canonical_hash(target_path_item[key]) == canonical_hash(value),
                        f"OpenAPI route conflict at {key.upper()} {route_path}",
                    )
                    continue
                if operation_id in operation_ids:
                    previous = operation_ids[operation_id]
                    raise ValueError(
                        f"OpenAPI operationId conflict {operation_id}: "
                        f"{previous[0].upper()} {previous[1]} and {key.upper()} {route_path}"
                    )
                target_path_item[key] = copy.deepcopy(value)
                operation_ids[operation_id] = (key, route_path, canonical_hash(value))
                if route not in core_routes:
                    if route in added_routes:
                        raise ValueError(f"Commercial OpenAPI route {key.upper()} {route_path} is duplicated")
                    added_routes[route] = operation_id

        merge_components(merged, extension, extension_path)
        merge_tags(merged, extension, extension_path)

    operation_index(merged, label="Combined OpenAPI")
    return merged, added_routes


def merge_components(
    merged: dict[str, Any],
    extension: dict[str, Any],
    extension_path: pathlib.Path,
) -> None:
    extension_components = extension.get("components", {})
    require(
        isinstance(extension_components, dict),
        f"Commercial OpenAPI extension {extension_path} components must be an object",
    )
    target_components = merged.setdefault("components", {})
    require(isinstance(target_components, dict), "Core OpenAPI components must be an object")
    for category, entries in extension_components.items():
        require(
            isinstance(entries, dict),
            f"Commercial OpenAPI component category {category} must be an object",
        )
        target_entries = target_components.setdefault(category, {})
        require(
            isinstance(target_entries, dict),
            f"Core OpenAPI component category {category} must be an object",
        )
        for name, value in entries.items():
            if name in target_entries:
                require(
                    canonical_hash(target_entries[name]) == canonical_hash(value),
                    f"OpenAPI component conflict at {category}.{name}",
                )
            else:
                target_entries[name] = copy.deepcopy(value)


def merge_tags(
    merged: dict[str, Any],
    extension: dict[str, Any],
    extension_path: pathlib.Path,
) -> None:
    extension_tags = extension.get("tags", [])
    require(
        isinstance(extension_tags, list),
        f"Commercial OpenAPI extension {extension_path} tags must be an array",
    )
    target_tags = merged.setdefault("tags", [])
    require(isinstance(target_tags, list), "Core OpenAPI tags must be an array")
    by_name: dict[str, dict[str, Any]] = {}
    for tag in target_tags:
        require(isinstance(tag, dict), "Core OpenAPI tag must be an object")
        name = tag.get("name")
        require(isinstance(name, str) and name.strip(), "Core OpenAPI tag requires name")
        require(name not in by_name, f"Core OpenAPI duplicates tag {name}")
        by_name[name] = tag
    for tag in extension_tags:
        require(isinstance(tag, dict), f"Commercial OpenAPI extension {extension_path} tag must be an object")
        name = tag.get("name")
        require(isinstance(name, str) and name.strip(), f"Commercial OpenAPI extension {extension_path} tag requires name")
        if name in by_name:
            require(
                canonical_hash(by_name[name]) == canonical_hash(tag),
                f"OpenAPI tag conflict at {name}",
            )
        else:
            cloned = copy.deepcopy(tag)
            target_tags.append(cloned)
            by_name[name] = cloned


def merge_events(
    core_path: pathlib.Path,
    extension_paths: list[pathlib.Path],
) -> dict[str, Any]:
    core = load_yaml_object(core_path, label="Core Event Catalog")
    require(core.get("version") == 1, "Core Event Catalog version must be 1")
    core_events = core.get("events")
    require(isinstance(core_events, list), "Core Event Catalog events must be an array")
    merged = copy.deepcopy(core)
    by_name: dict[str, dict[str, Any]] = {}
    for event in merged["events"]:
        require(isinstance(event, dict), "Core Event Catalog event must be an object")
        name = event.get("name")
        require(isinstance(name, str) and name.strip(), "Core Event Catalog event requires name")
        require(name not in by_name, f"Core Event Catalog duplicates event {name}")
        by_name[name] = event
    for extension_path in extension_paths:
        extension = load_yaml_object(extension_path, label="Commercial Event Catalog extension")
        require(
            set(extension) <= {"version", "events"},
            f"Commercial Event Catalog extension {extension_path} has unsupported keys",
        )
        require(
            extension.get("version") == core["version"],
            f"Commercial Event Catalog extension {extension_path} version differs from Core",
        )
        extension_events = extension.get("events")
        require(
            isinstance(extension_events, list) and extension_events,
            f"Commercial Event Catalog extension {extension_path} must contain events",
        )
        for event in extension_events:
            require(
                isinstance(event, dict),
                f"Commercial Event Catalog extension {extension_path} event must be an object",
            )
            name = event.get("name")
            require(
                isinstance(name, str) and name.strip(),
                f"Commercial Event Catalog extension {extension_path} event requires name",
            )
            if name in by_name:
                require(
                    canonical_hash(by_name[name]) == canonical_hash(event),
                    f"Event Catalog conflict at {name}",
                )
            else:
                cloned = copy.deepcopy(event)
                merged["events"].append(cloned)
                by_name[name] = cloned
    merged["events"].sort(key=lambda item: item["name"])
    return merged


def load_route_list(path: pathlib.Path) -> dict[tuple[str, str], str]:
    document = load_json_object(path, label="Commercial Edition API route list")
    require(
        document.get("schemaVersion") == ROUTE_LIST_SCHEMA_VERSION,
        f"Commercial Edition API route list {path} schemaVersion must be {ROUTE_LIST_SCHEMA_VERSION}",
    )
    raw_routes = document.get("routes")
    require(
        isinstance(raw_routes, list) and raw_routes,
        f"Commercial Edition API route list {path} must contain routes",
    )
    routes: dict[tuple[str, str], str] = {}
    operation_ids: set[str] = set()
    for index, item in enumerate(raw_routes):
        require(isinstance(item, dict), f"Commercial Edition API route list {path} route {index} must be an object")
        method = str(item.get("method", "")).lower()
        route_path = item.get("path")
        operation_id = item.get("operationId")
        require(
            method in HTTP_METHODS
            and isinstance(route_path, str)
            and route_path.startswith("/api/")
            and isinstance(operation_id, str)
            and operation_id.strip(),
            f"Commercial Edition API route list {path} route {index} is invalid",
        )
        route = (method, route_path)
        require(route not in routes, f"Commercial Edition API route list {path} duplicates {method.upper()} {route_path}")
        require(operation_id not in operation_ids, f"Commercial Edition API route list {path} duplicates operationId {operation_id}")
        routes[route] = operation_id
        operation_ids.add(operation_id)
    return routes


def merge_route_sources(
    core_path: pathlib.Path,
    route_list_paths: list[pathlib.Path],
    added_openapi_routes: dict[tuple[str, str], str],
) -> dict[str, Any]:
    core = load_json_object(core_path, label="Core route-source manifest")
    require(
        core.get("schemaVersion") == ROUTE_SOURCE_SCHEMA_VERSION,
        f"Core route-source manifest schemaVersion must be {ROUTE_SOURCE_SCHEMA_VERSION}",
    )
    require(isinstance(core.get("sources"), list) and core["sources"], "Core route-source manifest must contain sources")
    require(isinstance(core.get("implicitMethods", {}), dict), "Core route-source implicitMethods must be an object")
    require(isinstance(core.get("allowMissingOpenAPI", []), list), "Core route-source allowMissingOpenAPI must be an array")
    merged = copy.deepcopy(core)
    declared_routes: dict[tuple[str, str], str] = {}
    seen_source_paths = {
        source.get("path")
        for source in merged["sources"]
        if isinstance(source, dict) and isinstance(source.get("path"), str)
    }
    for route_list_path in route_list_paths:
        for route, operation_id in load_route_list(route_list_path).items():
            if route in declared_routes:
                raise ValueError(
                    f"Commercial Edition API route lists duplicate {route[0].upper()} {route[1]}"
                )
            if operation_id in declared_routes.values():
                raise ValueError(
                    f"Commercial Edition API route lists duplicate operationId {operation_id}"
                )
            declared_routes[route] = operation_id
        source_path = relative_workspace_path(route_list_path)
        require(
            source_path not in seen_source_paths,
            f"route-source manifest already contains Commercial route list {source_path}",
        )
        merged["sources"].append({"path": source_path, "parser": "route-list-json"})
        seen_source_paths.add(source_path)
    require(
        declared_routes == added_openapi_routes,
        "Commercial route lists must exactly match newly added OpenAPI method/path/operationId entries",
    )
    return merged


def write_yaml(path: pathlib.Path, document: dict[str, Any]) -> None:
    path.write_text(
        yaml.safe_dump(
            document,
            allow_unicode=True,
            sort_keys=False,
            width=120,
        ),
        encoding="utf-8",
        newline="\n",
    )


def write_json(path: pathlib.Path, document: dict[str, Any]) -> None:
    path.write_text(
        json.dumps(document, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
        newline="\n",
    )


def assemble(
    *,
    core_openapi: pathlib.Path,
    openapi_extensions: list[pathlib.Path],
    core_events: pathlib.Path,
    event_extensions: list[pathlib.Path],
    core_route_sources: pathlib.Path,
    route_lists: list[pathlib.Path],
    output_directory: pathlib.Path,
) -> dict[str, Any]:
    require(openapi_extensions, "at least one Commercial OpenAPI extension is required")
    require(event_extensions, "at least one Commercial Event Catalog extension is required")
    require(route_lists, "at least one Commercial Edition API route list is required")
    for path, label in [
        (core_openapi, "Core OpenAPI"),
        (core_events, "Core Event Catalog"),
        (core_route_sources, "Core route-source manifest"),
    ]:
        require(path.is_file(), f"{label} does not exist: {path}")
    for path in openapi_extensions + event_extensions + route_lists:
        require(path.is_file(), f"Commercial contract input does not exist: {path}")

    combined_openapi, added_routes = merge_openapi(core_openapi, openapi_extensions)
    combined_events = merge_events(core_events, event_extensions)
    combined_route_sources = merge_route_sources(
        core_route_sources,
        route_lists,
        added_routes,
    )
    require(not output_directory.exists(), f"output directory already exists: {output_directory}")
    require(output_directory != ROOT.resolve(), "output directory cannot be the assembled workspace root")

    output_directory.parent.mkdir(parents=True, exist_ok=True)
    temporary = pathlib.Path(
        tempfile.mkdtemp(
            prefix=f".{output_directory.name}.tmp-",
            dir=output_directory.parent,
        )
    )
    try:
        openapi_output = temporary / "openapi.yaml"
        events_output = temporary / "events.yaml"
        route_sources_output = temporary / "route-sources.combined.json"
        write_yaml(openapi_output, combined_openapi)
        write_yaml(events_output, combined_events)
        write_json(route_sources_output, combined_route_sources)
        evidence = {
            "schemaVersion": EVIDENCE_SCHEMA_VERSION,
            "inputs": {
                "coreOpenAPI": contract_input_evidence(core_openapi),
                "openAPIExtensions": [
                    contract_input_evidence(path) for path in openapi_extensions
                ],
                "coreEvents": contract_input_evidence(core_events),
                "eventExtensions": [
                    contract_input_evidence(path) for path in event_extensions
                ],
                "coreRouteSources": contract_input_evidence(core_route_sources),
                "routeLists": [contract_input_evidence(path) for path in route_lists],
            },
            "outputs": {
                "openapi.yaml": file_hash(openapi_output),
                "events.yaml": file_hash(events_output),
                "route-sources.combined.json": file_hash(route_sources_output),
            },
            "counts": {
                "openAPIRoutes": len(
                    {
                        (method, route_path)
                        for operation_id, (method, route_path, _) in operation_index(
                            combined_openapi,
                            label="Combined OpenAPI",
                        ).items()
                    }
                ),
                "events": len(combined_events["events"]),
                "commercialRoutes": len(added_routes),
            },
        }
        write_json(temporary / "assembly-evidence.json", evidence)
        os.replace(temporary, output_directory)
        return evidence
    except BaseException:
        shutil.rmtree(temporary, ignore_errors=True)
        raise


def contract_input_evidence(path: pathlib.Path) -> dict[str, str]:
    return {
        "path": relative_workspace_path(path),
        "sha256": file_hash(path),
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Assemble fail-closed Commercial OpenAPI, Event Catalog, and route-source contracts."
    )
    parser.add_argument("--core-openapi", default="packages/openapi/openapi.yaml")
    parser.add_argument("--openapi-extension", action="append", required=True)
    parser.add_argument("--core-events", default="packages/events/catalog.yaml")
    parser.add_argument("--event-extension", action="append", required=True)
    parser.add_argument(
        "--core-route-sources",
        default="packages/openapi/route-sources.ce.json",
    )
    parser.add_argument("--route-list", action="append", required=True)
    parser.add_argument("--output-directory", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        core_openapi = workspace_path(args.core_openapi, label="Core OpenAPI")
        openapi_extensions = [
            workspace_path(path, label="Commercial OpenAPI extension")
            for path in args.openapi_extension
        ]
        core_events = workspace_path(args.core_events, label="Core Event Catalog")
        event_extensions = [
            workspace_path(path, label="Commercial Event Catalog extension")
            for path in args.event_extension
        ]
        core_route_sources = workspace_path(
            args.core_route_sources,
            label="Core route-source manifest",
        )
        route_lists = [
            workspace_path(path, label="Commercial Edition API route list")
            for path in args.route_list
        ]
        output_directory = workspace_path(
            args.output_directory,
            label="contract output directory",
        )
        evidence = assemble(
            core_openapi=core_openapi,
            openapi_extensions=openapi_extensions,
            core_events=core_events,
            event_extensions=event_extensions,
            core_route_sources=core_route_sources,
            route_lists=route_lists,
            output_directory=output_directory,
        )
    except (OSError, ValueError, yaml.YAMLError) as exc:
        print(f"Commercial contract assembly failed: {exc}", file=sys.stderr)
        return 1
    print(
        "Commercial contracts assembled: "
        f"routes={evidence['counts']['openAPIRoutes']} "
        f"commercialRoutes={evidence['counts']['commercialRoutes']} "
        f"events={evidence['counts']['events']}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

from __future__ import annotations

import argparse
import json
import pathlib
import sys
from typing import Any

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
CATALOG_PATH = ROOT / "packages" / "mcp" / "tool-catalog.v1.json"
PROTOCOL_FIXTURES_PATH = ROOT / "packages" / "mcp" / "protocol-fixtures.v1.json"
MATRIX_PATH = ROOT / "packages" / "project-control" / "action-matrix.v1.json"
OPENAPI_PATH = ROOT / "packages" / "openapi" / "openapi.yaml"
WRITE_METHODS = {"post", "put", "patch", "delete"}


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def load_json(path: pathlib.Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    require(isinstance(value, dict), f"{path} must contain a JSON object")
    return value


def openapi_operations(document: dict[str, Any]) -> dict[str, dict[str, str]]:
    result: dict[str, dict[str, str]] = {}
    for path, path_item in document.get("paths", {}).items():
        if not isinstance(path_item, dict):
            continue
        for method, operation in path_item.items():
            method = method.lower()
            if method not in {"get", *WRITE_METHODS} or not isinstance(operation, dict):
                continue
            operation_id = operation.get("operationId")
            require(isinstance(operation_id, str) and operation_id, f"{method.upper()} {path} has no operationId")
            require(operation_id not in result, f"duplicate OpenAPI operationId {operation_id}")
            result[operation_id] = {"method": method, "path": path}
    return result


def commercial_operations(path: pathlib.Path) -> dict[str, dict[str, str]]:
    document = load_json(path)
    result: dict[str, dict[str, str]] = {}
    for route in document.get("routes", []):
        require(isinstance(route, dict), "Commercial route entry must be an object")
        operation_id = route.get("operationId")
        method = str(route.get("method", "")).lower()
        route_path = route.get("path")
        require(isinstance(operation_id, str) and operation_id, "Commercial route operationId is required")
        require(method in {"get", *WRITE_METHODS}, f"Commercial operation {operation_id} has invalid method")
        require(isinstance(route_path, str) and route_path, f"Commercial operation {operation_id} has no path")
        require(operation_id not in result, f"duplicate Commercial operationId {operation_id}")
        result[operation_id] = {"method": method, "path": route_path}
    return result


def validate_catalog_and_matrix(catalog: dict[str, Any], matrix: dict[str, Any], production: bool) -> None:
    require(catalog.get("schemaVersion") == "cineweave.mcp-tool-catalog.v1", "MCP tool catalog schemaVersion is invalid")
    require(matrix.get("schemaVersion") == "cineweave.project-control-action-matrix.v1", "action matrix schemaVersion is invalid")
    tools = catalog.get("tools")
    actions = matrix.get("actions")
    exclusions = matrix.get("operationExclusions")
    require(isinstance(tools, list), "MCP tool catalog tools must be an array")
    require(isinstance(actions, list), "action matrix actions must be an array")
    require(isinstance(exclusions, list), "action matrix operationExclusions must be an array")

    tool_names = [entry.get("name") for entry in tools]
    tool_action_names = [entry.get("actionName") for entry in tools]
    action_names = [entry.get("actionName") for entry in actions]
    require(tool_names == sorted(tool_names), "MCP tool catalog is not sorted")
    require(action_names == sorted(action_names), "action matrix is not sorted")
    require(len(tool_names) == len(set(tool_names)), "MCP tool catalog contains duplicate names")
    require(len(tool_action_names) == len(set(tool_action_names)), "MCP tool catalog contains duplicate action mappings")
    require(len(action_names) == len(set(action_names)), "action matrix contains duplicate action names")

    exported_actions = {entry["actionName"]: entry for entry in actions if entry.get("exportToMcp")}
    require(set(tool_action_names) == set(exported_actions), "MCP tool catalog and action matrix exported action sets differ")
    for tool in tools:
        require(
            isinstance(tool.get("name"), str)
            and 1 <= len(tool["name"]) <= 64
            and all(character.isascii() and (character.isalnum() or character in "_-") for character in tool["name"]),
            f"MCP wire tool {tool.get('name')} is not Responses API compatible",
        )
        action = exported_actions[tool["actionName"]]
        for field in (
            "version", "label", "summary", "permissions", "projectKinds", "readOnly",
            "destructive", "idempotent", "costed", "startsWorkflow", "supportsDryRun",
            "executionMode", "activityVisibility",
        ):
            action_field = "actionVersion" if field == "version" else field
            require(tool.get(field) == action.get(action_field), f"tool {tool['name']} field {field} drifted from action matrix")
        require(isinstance(tool.get("inputSchemaHash"), str) and len(tool["inputSchemaHash"]) == 64, f"tool {tool['name']} input schema hash is invalid")
        require(isinstance(tool.get("outputSchemaHash"), str) and len(tool["outputSchemaHash"]) == 64, f"tool {tool['name']} output schema hash is invalid")

    seen_agent_tools: set[str] = set()
    seen_rest_operations: set[str] = set()
    seen_commercial_actions: set[str] = set()
    for action in actions:
        status = action.get("migrationStatus")
        require(status in {"migrated", "adapter_backed", "excluded"}, f"action {action['actionName']} has invalid migration status")
        implementation_kind = action.get("implementationKind")
        require(
            implementation_kind in {"native_project_control", "shared_domain", "agent_adapter", "edition_http_adapter"},
            f"action {action['actionName']} has invalid implementation kind",
        )
        is_adapter = implementation_kind in {"agent_adapter", "edition_http_adapter"}
        require(status != "migrated" or not is_adapter, f"migrated action {action['actionName']} still uses {implementation_kind}")
        require(status != "adapter_backed" or is_adapter, f"adapter-backed action {action['actionName']} has non-adapter implementation")
        if production:
            require(status == "migrated", f"production action {action['actionName']} remains {status}")
        for field, seen in (
            ("agentToolNames", seen_agent_tools),
            ("restOperationIds", seen_rest_operations),
            ("commercialActionNames", seen_commercial_actions),
        ):
            values = action.get(field)
            require(isinstance(values, list), f"action {action['actionName']} {field} must be an array")
            require(values == sorted(set(values)), f"action {action['actionName']} {field} is not unique and sorted")
            for value in values:
                require(value not in seen, f"{field} entry {value} is mapped more than once")
                seen.add(value)


def validate_protocol_fixtures(document: dict[str, Any]) -> None:
    require(document.get("schemaVersion") == "cineweave.mcp-protocol-fixtures.v1", "MCP protocol fixtures schemaVersion is invalid")
    require(document.get("primaryProtocol") == "2026-07-28", "MCP primary protocol fixture is invalid")
    require(document.get("legacyProtocols") == ["2025-11-25", "2025-06-18"], "MCP legacy protocol fixtures are invalid")
    mcp_fixtures = document.get("mcpFixtures")
    http_fixtures = document.get("httpFixtures")
    require(isinstance(mcp_fixtures, list), "MCP fixtures must be an array")
    require(isinstance(http_fixtures, list), "MCP HTTP fixtures must be an array")

    mcp_by_id = {fixture.get("id"): fixture for fixture in mcp_fixtures if isinstance(fixture, dict)}
    required_mcp = {
        "primary-server-discover",
        "primary-tools-list",
        "primary-tools-call-success",
        "primary-tools-call-business-error",
        "primary-tools-call-permission-error",
        "legacy-2025-11-25-initialize",
        "legacy-2025-06-18-initialize",
    }
    require(set(mcp_by_id) == required_mcp, "MCP protocol fixture set is incomplete or contains unknown fixtures")
    for fixture_id, fixture in mcp_by_id.items():
        request = fixture.get("request")
        expected = fixture.get("expected")
        require(isinstance(request, dict) and request.get("jsonrpc") == "2.0", f"fixture {fixture_id} request is invalid")
        require(isinstance(expected, dict) and expected.get("httpStatus") == 200, f"fixture {fixture_id} expectation is invalid")
    require(mcp_by_id["primary-server-discover"]["request"].get("method") == "server/discover", "discover fixture method drifted")
    require(mcp_by_id["primary-tools-list"]["request"].get("method") == "tools/list", "tools/list fixture method drifted")
    require(mcp_by_id["primary-tools-call-success"]["request"].get("params", {}).get("name") == "project_get", "success fixture must use the MCP wire name")
    require(mcp_by_id["primary-tools-call-business-error"]["request"].get("params", {}).get("name") == "project_get", "business error fixture must use the MCP wire name")
    require(mcp_by_id["primary-tools-call-permission-error"]["request"].get("params", {}).get("name") == "project_update", "permission fixture must use the MCP wire name")
    require(mcp_by_id["primary-tools-call-business-error"]["expected"].get("isError") is True, "business error must be an MCP tool error")
    require(mcp_by_id["primary-tools-call-permission-error"]["expected"].get("structuredErrorCode") == "PERMISSION_DENIED", "permission fixture must remain a tool-level error")
    for fixture_id in ("legacy-2025-11-25-initialize", "legacy-2025-06-18-initialize"):
        require(mcp_by_id[fixture_id]["request"].get("method") == "initialize", f"legacy fixture {fixture_id} method drifted")
        require(
            mcp_by_id[fixture_id]["request"].get("params", {}).get("protocolVersion") == mcp_by_id[fixture_id].get("protocolVersion"),
            f"legacy fixture {fixture_id} protocol drifted",
        )

    http_by_id = {fixture.get("id"): fixture for fixture in http_fixtures if isinstance(fixture, dict)}
    expected_http = {
        "missing-authentication": (401, "AUTHENTICATION_REQUIRED", False),
        "invalid-origin": (403, "ORIGIN_NOT_ALLOWED", False),
        "request-too-large": (413, "REQUEST_TOO_LARGE", False),
        "request-rate-limited": (429, "RATE_LIMITED", True),
        "command-concurrency-limited": (429, "COMMAND_CONCURRENCY_LIMIT", True),
    }
    require(set(http_by_id) == set(expected_http), "MCP HTTP fixture set is incomplete or contains unknown fixtures")
    for fixture_id, (status, code, retryable) in expected_http.items():
        fixture = http_by_id[fixture_id]
        require(fixture.get("httpStatus") == status, f"HTTP fixture {fixture_id} status drifted")
        require(fixture.get("errorCode") == code, f"HTTP fixture {fixture_id} error code drifted")
        require(fixture.get("retryable") is retryable, f"HTTP fixture {fixture_id} retryability drifted")


def validate_operation_coverage(
    matrix: dict[str, Any],
    core_operations: dict[str, dict[str, str]],
    commercial_route_path: pathlib.Path | None,
    allow_unmapped: bool,
) -> list[str]:
    actions = matrix["actions"]
    exclusions = matrix["operationExclusions"]
    mapped_core = {operation for action in actions for operation in action["restOperationIds"]}
    mapped_commercial = {operation for action in actions for operation in action["commercialActionNames"]}
    excluded: dict[str, set[str]] = {}
    for entry in exclusions:
        registry = entry.get("registry")
        operation_id = entry.get("operationId")
        reason = entry.get("reason")
        require(registry in {"core_openapi", "commercial_routes"}, f"unknown exclusion registry {registry}")
        require(isinstance(operation_id, str) and operation_id, "excluded operationId is required")
        require(isinstance(reason, str) and len(reason.strip()) >= 12, f"excluded operation {operation_id} needs a specific reason")
        require("TODO" not in reason.upper() and "TEMP" not in reason.upper(), f"excluded operation {operation_id} uses a placeholder reason")
        excluded.setdefault(registry, set())
        require(operation_id not in excluded[registry], f"excluded operation {registry}/{operation_id} is duplicated")
        excluded[registry].add(operation_id)

    for operation_id in mapped_core:
        require(operation_id in core_operations, f"mapped Core REST operation {operation_id} is missing from OpenAPI")
        require(operation_id not in excluded.get("core_openapi", set()), f"Core REST operation {operation_id} is both mapped and excluded")

    required_core = {
        operation_id
        for operation_id, operation in core_operations.items()
        if operation["method"] in WRITE_METHODS and "{projectId}" in operation["path"]
    }
    unknown_core = sorted(required_core - mapped_core - excluded.get("core_openapi", set()))
    stale_core_exclusions = sorted(excluded.get("core_openapi", set()) - required_core)
    require(not stale_core_exclusions, f"stale Core REST exclusions: {', '.join(stale_core_exclusions)}")

    unknown_commercial: list[str] = []
    if commercial_route_path is not None:
        operations = commercial_operations(commercial_route_path)
        for operation_id in mapped_commercial:
            require(operation_id in operations, f"mapped Commercial operation {operation_id} is missing from route registry")
            require(operation_id not in excluded.get("commercial_routes", set()), f"Commercial operation {operation_id} is both mapped and excluded")
        required_commercial = {
            operation_id
            for operation_id, operation in operations.items()
            if operation["method"] in WRITE_METHODS and "{projectId}" in operation["path"]
        }
        unknown_commercial = sorted(required_commercial - mapped_commercial - excluded.get("commercial_routes", set()))
        stale_commercial = sorted(excluded.get("commercial_routes", set()) - required_commercial)
        require(not stale_commercial, f"stale Commercial exclusions: {', '.join(stale_commercial)}")

    unknown = [f"core_openapi:{value}" for value in unknown_core]
    unknown.extend(f"commercial_routes:{value}" for value in unknown_commercial)
    if unknown and not allow_unmapped:
        raise ValueError("unmapped project write operations: " + ", ".join(unknown))
    return unknown


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--production", action="store_true", help="reject adapter-backed actions")
    parser.add_argument("--allow-unmapped", action="store_true", help="report uncovered project writes without failing")
    parser.add_argument("--commercial-routes", type=pathlib.Path)
    parser.add_argument("--catalog", type=pathlib.Path, default=CATALOG_PATH)
    parser.add_argument("--matrix", type=pathlib.Path, default=MATRIX_PATH)
    parser.add_argument("--protocol-fixtures", type=pathlib.Path, default=PROTOCOL_FIXTURES_PATH)
    parser.add_argument("--openapi", type=pathlib.Path, default=OPENAPI_PATH)
    args = parser.parse_args()
    try:
        catalog = load_json(args.catalog.resolve())
        matrix = load_json(args.matrix.resolve())
        protocol_fixtures = load_json(args.protocol_fixtures.resolve())
        openapi = yaml.safe_load(args.openapi.resolve().read_text(encoding="utf-8"))
        require(isinstance(openapi, dict), "OpenAPI document is invalid")
        validate_catalog_and_matrix(catalog, matrix, args.production)
        validate_protocol_fixtures(protocol_fixtures)
        unknown = validate_operation_coverage(
            matrix,
            openapi_operations(openapi),
            args.commercial_routes.resolve() if args.commercial_routes else None,
            args.allow_unmapped,
        )
        print(
            "Project Control contracts are consistent: "
            f"{len(catalog['tools'])} MCP tools, {len(matrix['actions'])} actions, "
            f"{len(matrix['operationExclusions'])} explicit exclusions."
        )
        if unknown:
            print(f"Unmapped operations ({len(unknown)}):")
            for value in unknown:
                print(f"  {value}")
        return 0
    except (OSError, ValueError, json.JSONDecodeError, yaml.YAMLError) as error:
        print(f"Project Control contract check failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

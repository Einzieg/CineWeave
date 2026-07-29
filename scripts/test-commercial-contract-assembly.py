from __future__ import annotations

import copy
import importlib.util
import json
import pathlib
import sys
import tempfile

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
ASSEMBLER_PATH = ROOT / "scripts" / "assemble-commercial-contracts.py"
ROUTE_CHECKER_PATH = ROOT / "scripts" / "check-openapi-routes.py"


def load_module(name: str, path: pathlib.Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def write_yaml(path: pathlib.Path, value: object) -> None:
    path.write_text(
        yaml.safe_dump(value, allow_unicode=True, sort_keys=False),
        encoding="utf-8",
    )


def write_json(path: pathlib.Path, value: object) -> None:
    path.write_text(
        json.dumps(value, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def expect_value_error(label: str, callback) -> None:
    try:
        callback()
    except ValueError:
        return
    raise AssertionError(f"{label} was accepted")


def main() -> int:
    assembler = load_module("commercial_contract_assembler", ASSEMBLER_PATH)
    route_checker = load_module("commercial_contract_route_checker", ROUTE_CHECKER_PATH)
    temp_parent = ROOT / ".tmp" / "commercial-contract-assembly-test"
    temp_parent.mkdir(parents=True, exist_ok=True)
    invalid_cases = 0

    with tempfile.TemporaryDirectory(dir=temp_parent) as temp_directory:
        temp = pathlib.Path(temp_directory)
        core_openapi = temp / "core-openapi.yaml"
        extension_openapi = temp / "commercial-openapi.yaml"
        core_events = temp / "core-events.yaml"
        extension_events = temp / "commercial-events.yaml"
        core_routes_go = temp / "core-routes.go"
        core_route_sources = temp / "core-route-sources.json"
        commercial_route_list = temp / "commercial-routes.json"

        core_openapi_document = {
            "openapi": "3.1.0",
            "info": {"title": "Core", "version": "1"},
            "paths": {
                "/healthz": {
                    "get": {
                        "operationId": "getHealth",
                        "responses": {"200": {"description": "ok"}},
                    }
                }
            },
            "components": {
                "schemas": {
                    "SharedIdentifier": {
                        "type": "string",
                        "format": "uuid",
                    }
                }
            },
            "tags": [{"name": "System"}],
        }
        extension_openapi_document = {
            "openapi": "3.1.0",
            "info": {"title": "Commercial", "version": "1"},
            "paths": {
                "/api/billing/accounts": {
                    "get": {
                        "operationId": "listBillingAccounts",
                        "responses": {"200": {"description": "ok"}},
                    }
                }
            },
            "components": {
                "schemas": {
                    "BillingAccount": {
                        "type": "object",
                        "required": ["id"],
                        "properties": {"id": {"$ref": "#/components/schemas/SharedIdentifier"}},
                    }
                }
            },
            "tags": [{"name": "Billing"}],
        }
        core_events_document = {
            "version": 1,
            "events": [
                {
                    "name": "core.created",
                    "schemaVersion": 1,
                    "scopeType": "organization",
                    "aggregateType": "core",
                    "requiredPayloadFields": [],
                    "terminal": False,
                }
            ],
        }
        extension_events_document = {
            "version": 1,
            "events": [
                {
                    "name": "billing.balance.updated",
                    "schemaVersion": 1,
                    "scopeType": "organization",
                    "aggregateType": "billing_account",
                    "requiredPayloadFields": ["billingAccountId"],
                    "terminal": False,
                }
            ],
        }
        route_list_document = {
            "schemaVersion": "cineweave.edition-api-routes.v1",
            "routes": [
                {
                    "method": "GET",
                    "path": "/api/billing/accounts",
                    "operationId": "listBillingAccounts",
                }
            ],
        }
        write_yaml(core_openapi, core_openapi_document)
        write_yaml(extension_openapi, extension_openapi_document)
        write_yaml(core_events, core_events_document)
        write_yaml(extension_events, extension_events_document)
        core_routes_go.write_text(
            'mux.HandleFunc("GET /healthz", health)\n',
            encoding="utf-8",
        )
        write_json(
            core_route_sources,
            {
                "schemaVersion": "cineweave.route-sources.v1",
                "sources": [
                    {
                        "path": core_routes_go.relative_to(ROOT).as_posix(),
                        "parser": "go-http-mux",
                    }
                ],
                "implicitMethods": {},
                "allowMissingOpenAPI": [],
            },
        )
        write_json(commercial_route_list, route_list_document)

        output = temp / "combined"
        evidence = assembler.assemble(
            core_openapi=core_openapi,
            openapi_extensions=[extension_openapi],
            core_events=core_events,
            event_extensions=[extension_events],
            core_route_sources=core_route_sources,
            route_lists=[commercial_route_list],
            output_directory=output,
        )
        if evidence["counts"] != {
            "openAPIRoutes": 2,
            "events": 2,
            "commercialRoutes": 1,
        }:
            raise AssertionError(f"assembly counts = {evidence['counts']}")
        missing, stale, count = route_checker.check_routes(
            output / "openapi.yaml",
            output / "route-sources.combined.json",
        )
        if missing or stale or count != 2:
            raise AssertionError(
                f"combined route check failed: missing={missing}, stale={stale}, count={count}"
            )
        combined_events = yaml.safe_load((output / "events.yaml").read_text(encoding="utf-8"))
        if [event["name"] for event in combined_events["events"]] != [
            "billing.balance.updated",
            "core.created",
        ]:
            raise AssertionError(f"combined events = {combined_events['events']}")

        def invalid_openapi_case(label: str, mutate) -> None:
            nonlocal invalid_cases
            document = copy.deepcopy(extension_openapi_document)
            mutate(document)
            path = temp / f"invalid-openapi-{invalid_cases}.yaml"
            write_yaml(path, document)
            expect_value_error(
                label,
                lambda: assembler.merge_openapi(core_openapi, [path]),
            )
            invalid_cases += 1

        invalid_openapi_case(
            "route conflict",
            lambda document: document["paths"].update(
                {
                    "/healthz": {
                        "get": {
                            "operationId": "commercialHealth",
                            "responses": {"200": {"description": "different"}},
                        }
                    }
                }
            ),
        )
        invalid_openapi_case(
            "operationId conflict",
            lambda document: document["paths"]["/api/billing/accounts"]["get"].update(
                {"operationId": "getHealth"}
            ),
        )
        invalid_openapi_case(
            "component conflict",
            lambda document: document["components"]["schemas"].update(
                {"SharedIdentifier": {"type": "integer"}}
            ),
        )
        invalid_openapi_case(
            "unsupported top-level key",
            lambda document: document.update({"security": [{"BearerAuth": []}]}),
        )

        conflicting_events = copy.deepcopy(extension_events_document)
        conflicting_events["events"][0] = {
            **core_events_document["events"][0],
            "terminal": True,
        }
        conflicting_events_path = temp / "invalid-events.yaml"
        write_yaml(conflicting_events_path, conflicting_events)
        expect_value_error(
            "event conflict",
            lambda: assembler.merge_events(core_events, [conflicting_events_path]),
        )
        invalid_cases += 1

        wrong_route_list = copy.deepcopy(route_list_document)
        wrong_route_list["routes"][0]["operationId"] = "wrongOperation"
        wrong_route_list_path = temp / "wrong-route-list.json"
        write_json(wrong_route_list_path, wrong_route_list)
        _, added_routes = assembler.merge_openapi(core_openapi, [extension_openapi])
        expect_value_error(
            "route-list operation mismatch",
            lambda: assembler.merge_route_sources(
                core_route_sources,
                [wrong_route_list_path],
                added_routes,
            ),
        )
        invalid_cases += 1

        duplicate_route_list = copy.deepcopy(route_list_document)
        duplicate_route_list["routes"].append(copy.deepcopy(route_list_document["routes"][0]))
        duplicate_route_list_path = temp / "duplicate-route-list.json"
        write_json(duplicate_route_list_path, duplicate_route_list)
        expect_value_error(
            "duplicate route list",
            lambda: assembler.load_route_list(duplicate_route_list_path),
        )
        invalid_cases += 1

        expect_value_error(
            "existing output directory",
            lambda: assembler.assemble(
                core_openapi=core_openapi,
                openapi_extensions=[extension_openapi],
                core_events=core_events,
                event_extensions=[extension_events],
                core_route_sources=core_route_sources,
                route_lists=[commercial_route_list],
                output_directory=output,
            ),
        )
        invalid_cases += 1

    try:
        temp_parent.rmdir()
    except OSError:
        pass
    if invalid_cases != 8:
        raise AssertionError(f"invalid case count = {invalid_cases}")
    print(
        "Commercial contract assembly checks passed: "
        f"invalidCases={invalid_cases}, combinedRoutes=2, combinedEvents=2"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

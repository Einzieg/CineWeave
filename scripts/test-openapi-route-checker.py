from __future__ import annotations

import importlib.util
import json
import pathlib
import sys
import tempfile

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts" / "check-openapi-routes.py"


def load_checker():
    spec = importlib.util.spec_from_file_location("check_openapi_routes", MODULE_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load check-openapi-routes.py")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def write_contract(path: pathlib.Path, routes: list[tuple[str, str]]) -> None:
    paths: dict[str, dict[str, object]] = {}
    for method, route_path in routes:
        paths.setdefault(route_path, {})[method] = {
            "operationId": f"{method}_{route_path}".replace("/", "_").replace("{", "").replace("}", ""),
            "responses": {"200": {"description": "ok"}},
        }
    path.write_text(
        yaml.safe_dump({"openapi": "3.1.0", "info": {"title": "test", "version": "1"}, "paths": paths}),
        encoding="utf-8",
    )


def write_manifest(path: pathlib.Path, source_path: pathlib.Path) -> None:
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "cineweave.route-sources.v1",
                "sources": [
                    {
                        "path": str(source_path.relative_to(ROOT)).replace("\\", "/"),
                        "parser": "go-http-mux",
                    }
                ],
                "implicitMethods": {"/healthz": "get"},
                "allowMissingOpenAPI": [],
            }
        ),
        encoding="utf-8",
    )


def main() -> int:
    checker = load_checker()
    temp_root = ROOT / ".tmp" / "openapi-route-checker-test"
    temp_root.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=temp_root) as temp_directory:
        temp = pathlib.Path(temp_directory)
        source = temp / "routes.go"
        contract = temp / "openapi.yaml"
        manifest = temp / "routes.json"
        source.write_text(
            'mux.HandleFunc("/healthz", health)\n'
            'mux.HandleFunc("GET /api/items/{itemId}", getItem)\n',
            encoding="utf-8",
        )
        write_manifest(manifest, source)
        write_contract(
            contract,
            [("get", "/healthz"), ("get", "/api/items/{itemId}")],
        )

        missing, stale, count = checker.check_routes(contract, manifest)
        if missing or stale or count != 2:
            raise AssertionError(
                f"matching contract failed: missing={missing}, stale={stale}, count={count}"
            )

        write_contract(contract, [("get", "/healthz"), ("post", "/api/stale")])
        missing, stale, _ = checker.check_routes(contract, manifest)
        if missing != [("get", "/api/items/{itemId}")]:
            raise AssertionError(f"missing routes = {missing}")
        if stale != [("post", "/api/stale")]:
            raise AssertionError(f"stale routes = {stale}")

        route_list = temp / "commercial-routes.json"
        route_list.write_text(
            json.dumps(
                {
                    "schemaVersion": "cineweave.edition-api-routes.v1",
                    "routes": [
                        {
                            "method": "GET",
                            "path": "/api/billing/accounts",
                            "operationId": "listBillingAccounts",
                        }
                    ],
                }
            ),
            encoding="utf-8",
        )
        manifest.write_text(
            json.dumps(
                {
                    "schemaVersion": "cineweave.route-sources.v1",
                    "sources": [
                        {
                            "path": str(source.relative_to(ROOT)).replace("\\", "/"),
                            "parser": "go-http-mux",
                        },
                        {
                            "path": str(route_list.relative_to(ROOT)).replace("\\", "/"),
                            "parser": "route-list-json",
                        },
                    ],
                    "implicitMethods": {"/healthz": "get"},
                    "allowMissingOpenAPI": [],
                }
            ),
            encoding="utf-8",
        )
        write_contract(
            contract,
            [
                ("get", "/healthz"),
                ("get", "/api/items/{itemId}"),
                ("get", "/api/billing/accounts"),
            ],
        )
        missing, stale, count = checker.check_routes(contract, manifest)
        if missing or stale or count != 3:
            raise AssertionError(
                f"route-list contract failed: missing={missing}, stale={stale}, count={count}"
            )

    try:
        temp_root.rmdir()
    except OSError:
        pass
    print("OpenAPI route checker explicit contract and source-manifest tests passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

from __future__ import annotations

import pathlib
import re
import sys

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
DOCUMENT_PATH = ROOT / "docs" / "commerce-video-development-plan.md"
OPENAPI_PATH = ROOT / "packages" / "openapi" / "openapi.yaml"
EVENT_CATALOG_PATH = ROOT / "packages" / "events" / "catalog.yaml"


def documented_api_operations(document: str) -> set[tuple[str, str]]:
    operations: set[tuple[str, str]] = set()
    for method, raw_path in re.findall(r"\b(GET|POST|PUT|PATCH|DELETE)\s+(/api/[^\s`]+)", document):
        path = raw_path.split("?", 1)[0].rstrip(",.;:")
        operations.add((method.lower(), path))
    return operations


def openapi_operations(document: dict[str, object]) -> set[tuple[str, str]]:
    result: set[tuple[str, str]] = set()
    paths = document.get("paths", {})
    if not isinstance(paths, dict):
        return result
    for path, path_item in paths.items():
        if not isinstance(path, str) or not isinstance(path_item, dict):
            continue
        for method in ("get", "post", "put", "patch", "delete"):
            if method in path_item:
                result.add((method, path))
    return result


def documented_commerce_events(document: str) -> set[str]:
    section = re.search(
        r"### 11\.2 事件目录(?P<body>.*?)(?:\n## |\Z)",
        document,
        flags=re.DOTALL,
    )
    if section is None:
        raise ValueError("docs/commerce-video-development-plan.md 缺少 11.2 事件目录")
    code_block = re.search(r"```text\s*(?P<events>.*?)```", section.group("body"), flags=re.DOTALL)
    if code_block is None:
        raise ValueError("11.2 事件目录缺少 text 代码块")
    return {
        line.strip()
        for line in code_block.group("events").splitlines()
        if line.strip().startswith("commerce.")
    }


def catalog_commerce_events(document: dict[str, object]) -> set[str]:
    events = document.get("events", [])
    if not isinstance(events, list):
        return set()
    return {
        str(event["name"])
        for event in events
        if isinstance(event, dict) and str(event.get("name", "")).startswith("commerce.")
    }


def format_operations(values: set[tuple[str, str]]) -> str:
    return "\n".join(f"  {method.upper()} {path}" for method, path in sorted(values))


def format_events(values: set[str]) -> str:
    return "\n".join(f"  {event}" for event in sorted(values))


def main() -> int:
    document = DOCUMENT_PATH.read_text(encoding="utf-8")
    openapi = yaml.safe_load(OPENAPI_PATH.read_text(encoding="utf-8"))
    catalog = yaml.safe_load(EVENT_CATALOG_PATH.read_text(encoding="utf-8"))

    documented_operations = documented_api_operations(document)
    available_operations = openapi_operations(openapi)
    missing_operations = documented_operations - available_operations

    documented_events = documented_commerce_events(document)
    registered_events = catalog_commerce_events(catalog)
    missing_events = documented_events - registered_events
    undocumented_events = registered_events - documented_events

    failures: list[str] = []
    if missing_operations:
        failures.append("开发文档引用了 OpenAPI 中不存在的接口：\n" + format_operations(missing_operations))
    if missing_events:
        failures.append("开发文档中的 Commerce 事件未注册：\n" + format_events(missing_events))
    if undocumented_events:
        failures.append("事件目录注册了开发文档未列出的 Commerce 事件：\n" + format_events(undocumented_events))
    if failures:
        print("\n\n".join(failures), file=sys.stderr)
        return 1

    print(
        "Commerce development contract matches "
        f"OpenAPI ({len(documented_operations)} operations) and event catalog ({len(documented_events)} events)."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

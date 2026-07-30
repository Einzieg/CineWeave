from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import pathlib
import re
import sys
import tarfile
import zipfile
from typing import Any


ROOT = pathlib.Path(__file__).resolve().parents[1]
DEFAULT_SCHEMA = ROOT / "packages" / "edition" / "release-manifest.schema.json"
DEFAULT_MANIFEST = (
    ROOT / "packages" / "edition" / "fixtures" / "combined-release-manifest.valid.json"
)
DEFAULT_DDL_OWNERS = ROOT / "packages" / "edition" / "ddl-owners.v1.json"

REQUIRED_COMPONENTS = {
    "api",
    "web",
    "provider-gateway",
    "realtime",
    "event-publisher",
    "script-worker",
    "agent-worker",
    "media-worker",
    "audio-worker",
    "billing-bridge",
    "migrate-core",
    "migrate-commercial",
    "seed-core",
    "seed-commercial",
}
REQUIRED_TEMPORAL_DEPLOYMENTS = {
    "script-worker",
    "agent-worker",
    "media-worker",
    "audio-worker",
}
MUTABLE_IDS = {"latest", "main", "master", "local-dev", "dev", "development"}
ASSEMBLY_EVIDENCE_FIELDS = (
    "assembly_inputs",
    "core_lock",
    "overlay_allowlist",
    "assembly_script",
    "source_archive",
    "core_fk_actions",
)
ASSEMBLY_INPUT_KEYS = {
    "schemaVersion",
    "coreCommit",
    "commercialAssemblyCommit",
    "coreLockSha256",
    "overlayAllowlistSha256",
    "overlaySlotsSha256",
    "assemblyScriptPath",
    "assemblyScriptSha256",
    "cleanCoreTree",
    "cleanCommercialTree",
    "assembledAt",
    "files",
}
ASSEMBLY_FILE_KEYS = {"source", "destination", "operation", "sha256"}


class ManifestError(ValueError):
    pass


def fail(condition: bool, message: str) -> None:
    if not condition:
        raise ManifestError(message)


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_json(path: pathlib.Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ManifestError(f"cannot read JSON {path}: {exc}") from exc


def require_file(path: pathlib.Path, label: str) -> pathlib.Path:
    resolved = path.resolve()
    fail(resolved.is_file(), f"{label} is missing or not a regular file: {resolved}")
    return resolved


def normalize_archive_name(name: str) -> str:
    normalized = pathlib.PurePosixPath(name.replace("\\", "/"))
    fail(not normalized.is_absolute(), f"source archive contains an absolute path: {name!r}")
    fail(
        ".." not in normalized.parts,
        f"source archive contains a parent traversal path: {name!r}",
    )
    result = normalized.as_posix()
    while result.startswith("./"):
        result = result[2:]
    fail(result not in {"", "."}, f"source archive contains an empty path: {name!r}")
    fail(
        re.match(r"^[A-Za-z]:", result) is None,
        f"source archive contains a drive-qualified path: {name!r}",
    )
    return result


def read_archive_files(path: pathlib.Path, required_names: set[str]) -> dict[str, bytes]:
    files: dict[str, bytes] = {}
    seen: set[str] = set()

    def add_file(name: str, read_content: Any) -> None:
        normalized = normalize_archive_name(name)
        fail(bool(normalized), "source archive contains an empty path")
        fail(normalized not in seen, f"source archive contains duplicate file {normalized!r}")
        seen.add(normalized)
        if normalized in required_names:
            files[normalized] = read_content()

    try:
        if zipfile.is_zipfile(path):
            with zipfile.ZipFile(path) as archive:
                for item in archive.infolist():
                    if item.is_dir():
                        continue
                    fail(
                        (item.external_attr >> 16) & 0o170000 != 0o120000,
                        f"source archive contains a symbolic link: {item.filename!r}",
                    )
                    add_file(item.filename, lambda item=item: archive.read(item))
            missing = sorted(required_names - set(files))
            fail(not missing, f"source archive is missing required files: {', '.join(missing)}")
            return files
        if tarfile.is_tarfile(path):
            with tarfile.open(path, mode="r:*") as archive:
                for item in archive.getmembers():
                    if item.isdir():
                        continue
                    fail(item.isfile(), f"source archive contains a non-regular entry: {item.name!r}")

                    def read_tar_item(item: tarfile.TarInfo = item) -> bytes:
                        extracted = archive.extractfile(item)
                        fail(extracted is not None, f"cannot read source archive entry {item.name!r}")
                        return extracted.read()

                    add_file(item.name, read_tar_item)
            missing = sorted(required_names - set(files))
            fail(not missing, f"source archive is missing required files: {', '.join(missing)}")
            return files
    except (OSError, tarfile.TarError, zipfile.BadZipFile, RuntimeError) as exc:
        raise ManifestError(f"cannot read source archive {path}: {exc}") from exc
    raise ManifestError(f"source archive must be a readable tar or zip archive: {path}")


def parse_json_bytes(content: bytes, label: str) -> Any:
    try:
        return json.loads(content.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ManifestError(f"cannot read {label} JSON: {exc}") from exc


def resolve_ref(root_schema: dict[str, Any], reference: str) -> dict[str, Any]:
    fail(reference.startswith("#/"), f"unsupported non-local JSON Schema ref {reference!r}")
    current: Any = root_schema
    for part in reference[2:].split("/"):
        key = part.replace("~1", "/").replace("~0", "~")
        fail(isinstance(current, dict) and key in current, f"unresolved JSON Schema ref {reference!r}")
        current = current[key]
    fail(isinstance(current, dict), f"JSON Schema ref {reference!r} is not an object")
    return current


def type_matches(value: Any, expected: str) -> bool:
    if expected == "object":
        return isinstance(value, dict)
    if expected == "array":
        return isinstance(value, list)
    if expected == "string":
        return isinstance(value, str)
    if expected == "integer":
        return isinstance(value, int) and not isinstance(value, bool)
    if expected == "number":
        return isinstance(value, (int, float)) and not isinstance(value, bool)
    if expected == "boolean":
        return isinstance(value, bool)
    if expected == "null":
        return value is None
    raise ManifestError(f"unsupported JSON Schema type {expected!r}")


def validate_schema_value(
    value: Any,
    schema: dict[str, Any],
    root_schema: dict[str, Any],
    location: str,
) -> None:
    if "$ref" in schema:
        validate_schema_value(value, resolve_ref(root_schema, schema["$ref"]), root_schema, location)

    for branch in schema.get("allOf", []):
        validate_schema_value(value, branch, root_schema, location)
    if "if" in schema:
        try:
            validate_schema_value(value, schema["if"], root_schema, location)
            selected = schema.get("then")
        except ManifestError:
            selected = schema.get("else")
        if selected is not None:
            validate_schema_value(value, selected, root_schema, location)
    if "anyOf" in schema:
        failures: list[str] = []
        for branch in schema["anyOf"]:
            try:
                validate_schema_value(value, branch, root_schema, location)
                break
            except ManifestError as exc:
                failures.append(str(exc))
        else:
            raise ManifestError(f"{location} does not match any allowed schema: {'; '.join(failures)}")

    if "const" in schema:
        fail(value == schema["const"], f"{location} must equal {schema['const']!r}")
    if "enum" in schema:
        fail(value in schema["enum"], f"{location} must be one of {schema['enum']!r}")

    expected_type = schema.get("type")
    if expected_type is not None:
        expected_types = [expected_type] if isinstance(expected_type, str) else expected_type
        fail(
            any(type_matches(value, candidate) for candidate in expected_types),
            f"{location} has invalid type; expected {expected_types!r}",
        )

    if isinstance(value, dict):
        required = schema.get("required", [])
        missing = sorted(set(required) - set(value))
        fail(not missing, f"{location} is missing required keys: {', '.join(missing)}")
        properties = schema.get("properties", {})
        additional = schema.get("additionalProperties", True)
        for key, item in value.items():
            child_location = f"{location}.{key}"
            if key in properties:
                validate_schema_value(item, properties[key], root_schema, child_location)
            elif additional is False:
                raise ManifestError(f"{child_location} is not allowed")
            elif isinstance(additional, dict):
                validate_schema_value(item, additional, root_schema, child_location)
        if "minProperties" in schema:
            fail(
                len(value) >= schema["minProperties"],
                f"{location} requires at least {schema['minProperties']} properties",
            )

    if isinstance(value, list):
        if "minItems" in schema:
            fail(
                len(value) >= schema["minItems"],
                f"{location} requires at least {schema['minItems']} items",
            )
        if "items" in schema:
            for index, item in enumerate(value):
                validate_schema_value(item, schema["items"], root_schema, f"{location}[{index}]")

    if isinstance(value, str):
        if "minLength" in schema:
            fail(len(value) >= schema["minLength"], f"{location} is too short")
        if "maxLength" in schema:
            fail(len(value) <= schema["maxLength"], f"{location} is too long")
        if "pattern" in schema:
            fail(re.search(schema["pattern"], value) is not None, f"{location} has invalid format")
        if schema.get("format") == "date":
            try:
                dt.date.fromisoformat(value)
            except ValueError as exc:
                raise ManifestError(f"{location} is not an ISO date") from exc
        if schema.get("format") == "date-time":
            try:
                dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
            except ValueError as exc:
                raise ManifestError(f"{location} is not an ISO date-time") from exc

    if isinstance(value, (int, float)) and not isinstance(value, bool) and "minimum" in schema:
        fail(value >= schema["minimum"], f"{location} must be at least {schema['minimum']}")


def validate_stream_identity(
    manifest_stream: dict[str, Any],
    owner_stream: dict[str, Any],
    label: str,
) -> None:
    for field in ("streamId", "controlSchema", "ledgerTable", "auditTable"):
        fail(
            manifest_stream.get(field) == owner_stream.get(field),
            f"migrations.{label}.{field} does not match DDL owner manifest",
        )


def unique_field(items: list[dict[str, Any]], field: str, location: str) -> set[str]:
    values = [item[field] for item in items]
    fail(len(values) == len(set(values)), f"{location} contains duplicate {field} values")
    return set(values)


def core_migration_head() -> int:
    versions: list[int] = []
    for path in (ROOT / "db" / "migrations").glob("*.sql"):
        match = re.fullmatch(r"(\d+)_[a-z0-9_]+\.sql", path.name)
        fail(match is not None, f"invalid Core migration filename {path.name!r}")
        versions.append(int(match.group(1)))
    fail(bool(versions), "Core migration directory is empty")
    versions.sort()
    fail(
        versions == list(range(1, versions[-1] + 1)),
        "Core migration versions are not consecutive",
    )
    return versions[-1]


def validate_assembly_evidence(
    manifest: dict[str, Any],
    assembly_inputs_path: pathlib.Path,
    core_lock_path: pathlib.Path,
    overlay_allowlist_path: pathlib.Path,
    assembly_script_path: pathlib.Path,
    source_archive_path: pathlib.Path,
    core_fk_actions_path: pathlib.Path,
) -> None:
    assembly_inputs_path = require_file(assembly_inputs_path, "assembly inputs")
    core_lock_path = require_file(core_lock_path, "Core lock")
    overlay_allowlist_path = require_file(overlay_allowlist_path, "Overlay allowlist")
    assembly_script_path = require_file(assembly_script_path, "assembly script")
    source_archive_path = require_file(source_archive_path, "source archive")
    core_fk_actions_path = require_file(
        core_fk_actions_path,
        "Core foreign-key action manifest",
    )

    assembly_inputs_bytes = assembly_inputs_path.read_bytes()
    assembly_inputs = parse_json_bytes(assembly_inputs_bytes, "assembly inputs")
    fail(isinstance(assembly_inputs, dict), "assembly inputs must be a JSON object")
    fail(
        set(assembly_inputs) == ASSEMBLY_INPUT_KEYS,
        "assembly inputs contain missing or unexpected top-level keys",
    )
    fail(
        assembly_inputs["schemaVersion"] == "cineweave.assembly-inputs.v1",
        "assembly inputs schemaVersion is invalid",
    )
    for field in ("coreCommit", "commercialAssemblyCommit"):
        fail(
            isinstance(assembly_inputs[field], str)
            and re.fullmatch(r"[0-9a-f]{40}", assembly_inputs[field]) is not None,
            f"assembly inputs {field} is not a lowercase full Git commit",
        )
    for field in (
        "coreLockSha256",
        "overlayAllowlistSha256",
        "overlaySlotsSha256",
        "assemblyScriptSha256",
    ):
        fail(
            isinstance(assembly_inputs[field], str)
            and re.fullmatch(r"[0-9a-f]{64}", assembly_inputs[field]) is not None,
            f"assembly inputs {field} is not a lowercase SHA-256",
        )
    fail(assembly_inputs["cleanCoreTree"] is True, "assembly inputs do not prove a clean Core tree")
    fail(
        assembly_inputs["cleanCommercialTree"] is True,
        "assembly inputs do not prove a clean Commercial tree",
    )
    fail(isinstance(assembly_inputs["assembledAt"], str), "assembly inputs assembledAt is invalid")
    try:
        assembled_at = dt.datetime.fromisoformat(
            assembly_inputs["assembledAt"].replace("Z", "+00:00")
        )
    except ValueError as exc:
        raise ManifestError("assembly inputs assembledAt is not an ISO date-time") from exc
    fail(
        assembled_at.tzinfo is not None and assembled_at.utcoffset() is not None,
        "assembly inputs assembledAt must include a timezone",
    )
    fail(
        isinstance(assembly_inputs["assemblyScriptPath"], str),
        "assembly inputs assemblyScriptPath is invalid",
    )
    assembly_script_archive_path = normalize_archive_name(
        assembly_inputs["assemblyScriptPath"]
    )
    fail(
        assembly_script_archive_path == assembly_inputs["assemblyScriptPath"],
        "assembly inputs assemblyScriptPath must be a normalized archive path",
    )

    files = assembly_inputs["files"]
    fail(isinstance(files, list), "assembly inputs files must be an array")
    destinations: set[str] = set()
    for index, item in enumerate(files):
        location = f"assembly inputs files[{index}]"
        fail(isinstance(item, dict), f"{location} must be an object")
        fail(set(item) == ASSEMBLY_FILE_KEYS, f"{location} contains missing or unexpected keys")
        fail(
            isinstance(item["source"], str) and bool(item["source"]),
            f"{location}.source is invalid",
        )
        fail(
            isinstance(item["destination"], str) and bool(item["destination"]),
            f"{location}.destination is invalid",
        )
        fail(item["operation"] in {"add", "replace"}, f"{location}.operation is invalid")
        fail(
            isinstance(item["sha256"], str)
            and re.fullmatch(r"[0-9a-f]{64}", item["sha256"]) is not None,
            f"{location}.sha256 is invalid",
        )
        destination = normalize_archive_name(item["destination"])
        fail(
            destination == item["destination"],
            f"{location}.destination must be a normalized archive path",
        )
        fail(
            destination != ".cineweave/assembly-inputs.json",
            f"{location}.destination cannot replace assembly evidence",
        )
        fail(destination not in destinations, f"assembly inputs contain duplicate destination {destination!r}")
        destinations.add(destination)

    core_lock = load_json(core_lock_path)
    overlay = load_json(overlay_allowlist_path)
    fail(isinstance(core_lock, dict), "Core lock must be a JSON object")
    fail(isinstance(overlay, dict), "Overlay allowlist must be a JSON object")
    fail(
        core_lock.get("schemaVersion") == "cineweave.core-lock.v1",
        "Core lock schemaVersion is invalid",
    )
    fail(
        overlay.get("schemaVersion") == "cineweave.overlay-allowlist.v1",
        "Overlay allowlist schemaVersion is invalid",
    )
    fail(
        core_lock.get("coreCommit") == assembly_inputs["coreCommit"],
        "Core lock commit does not match assembly inputs",
    )
    fail(
        overlay.get("coreCommit") == assembly_inputs["coreCommit"],
        "Overlay allowlist commit does not match assembly inputs",
    )
    fail(
        core_lock.get("overlaySlotsSha256") == assembly_inputs["overlaySlotsSha256"],
        "Core lock overlay slot hash does not match assembly inputs",
    )
    fail(
        overlay.get("files") == files,
        "Overlay allowlist files do not match assembly inputs",
    )

    assembly = manifest["assembly"]
    for field in (
        "coreCommit",
        "commercialAssemblyCommit",
        "coreLockSha256",
        "overlayAllowlistSha256",
        "assemblyScriptSha256",
        "cleanCoreTree",
        "cleanCommercialTree",
    ):
        fail(
            assembly[field] == assembly_inputs[field],
            f"release manifest assembly.{field} does not match assembly inputs",
        )
    fail(
        sha256_file(core_lock_path) == assembly_inputs["coreLockSha256"],
        "Core lock content hash does not match assembly evidence",
    )
    fail(
        sha256_file(overlay_allowlist_path) == assembly_inputs["overlayAllowlistSha256"],
        "Overlay allowlist content hash does not match assembly evidence",
    )
    fail(
        sha256_file(assembly_script_path) == assembly_inputs["assemblyScriptSha256"],
        "assembly script content hash does not match assembly evidence",
    )
    fail(
        sha256_file(source_archive_path) == assembly["sourceArchiveSha256"],
        "source archive content hash does not match the release manifest",
    )
    fail(
        sha256_file(core_fk_actions_path)
        == manifest["migrations"]["coreForeignKeyActionManifestSha256"],
        "Core foreign-key action manifest hash does not match the release manifest",
    )

    required_archive_files = destinations | {
        ".cineweave/assembly-inputs.json",
        assembly_script_archive_path,
    }
    archive_files = read_archive_files(source_archive_path, required_archive_files)
    fail(
        archive_files[".cineweave/assembly-inputs.json"] == assembly_inputs_bytes,
        "source archive assembly inputs differ from the checked evidence file",
    )
    fail(
        hashlib.sha256(archive_files[assembly_script_archive_path]).hexdigest()
        == assembly_inputs["assemblyScriptSha256"],
        "source archive assembly script differs from the checked assembly script",
    )
    for item in files:
        fail(
            hashlib.sha256(archive_files[item["destination"]]).hexdigest() == item["sha256"],
            f"source archive overlay file hash drifted: {item['destination']}",
        )


def validate_semantics(
    manifest: dict[str, Any],
    ddl_owners: dict[str, Any],
    ddl_owner_path: pathlib.Path,
    verify_local_core: bool,
) -> None:
    release_id = manifest["releaseId"]
    fail(release_id.lower() not in MUTABLE_IDS, "releaseId is mutable")
    fail(manifest["distributionId"].lower() not in MUTABLE_IDS, "distributionId is mutable")
    internal_operation = manifest["internalOperation"]
    fail(
        internal_operation["deploymentId"] == manifest["distributionId"],
        "internalOperation.deploymentId does not match distributionId",
    )

    owner_hash = sha256_file(ddl_owner_path)
    fail(
        manifest["migrations"]["ddlOwnerManifestSha256"] == owner_hash,
        "DDL owner manifest hash does not match the release manifest",
    )
    validate_stream_identity(
        manifest["migrations"]["core"],
        ddl_owners["streams"]["core"],
        "core",
    )
    validate_stream_identity(
        manifest["migrations"]["commercial"],
        ddl_owners["streams"]["commercial"],
        "commercial",
    )
    fail(
        ddl_owners["streams"]["core"]["controlSchema"]
        != ddl_owners["streams"]["commercial"]["controlSchema"],
        "Core and Commercial control schemas are not independent",
    )
    fail(
        ddl_owners["streams"]["core"]["advisoryLockKey"]
        != ddl_owners["streams"]["commercial"]["advisoryLockKey"],
        "Core and Commercial advisory lock keys are not independent",
    )
    if verify_local_core:
        fail(
            manifest["migrations"]["core"]["head"] == core_migration_head(),
            "release manifest Core migration head does not match the checked-out Core source",
        )

    component_ids = manifest["componentReleaseIds"]
    fail(
        set(component_ids) == REQUIRED_COMPONENTS,
        "componentReleaseIds must contain exactly the required combined-release components",
    )
    for component, component_release_id in component_ids.items():
        fail(
            component_release_id == release_id,
            f"componentReleaseIds.{component} does not match releaseId",
        )

    images = manifest["artifacts"]["images"]
    image_services = unique_field(images, "service", "artifacts.images")
    fail(
        image_services == REQUIRED_COMPONENTS,
        "artifacts.images must contain exactly the required combined-release components",
    )
    for image in images:
        fail(image["tag"].lower() not in MUTABLE_IDS, f"image {image['service']} uses a mutable tag")

    worker_builds = manifest["artifacts"]["temporalWorkerBuilds"]
    deployments = unique_field(worker_builds, "deployment", "artifacts.temporalWorkerBuilds")
    fail(
        deployments == REQUIRED_TEMPORAL_DEPLOYMENTS,
        "Temporal Worker Build IDs must cover exactly script, agent, media and audio deployments",
    )
    for worker in worker_builds:
        fail(
            worker["buildId"] == release_id,
            f"Temporal deployment {worker['deployment']} buildId does not match releaseId",
        )
    fail(
        manifest["artifacts"]["webBuildId"] == release_id,
        "Web build ID does not match releaseId",
    )
    source_licensing = manifest["sourceLicensing"]
    fail(
        source_licensing["reportSha256"]
        == manifest["artifacts"]["thirdPartyLicenseReportSha256"],
        "source licensing report hash does not match artifacts.thirdPartyLicenseReportSha256",
    )

    new_api = manifest["upstreamNewAPI"]
    if new_api["modified"]:
        fail(new_api["patchSha256"] is not None, "modified New API release requires patchSha256")
        fail(
            new_api["modificationAssessment"] == "modified_patch_bound",
            "modified New API release must bind the assessed patch",
        )
    else:
        fail(new_api["patchSha256"] is None, "unmodified New API release must use null patchSha256")
        fail(
            new_api["modificationAssessment"] == "unmodified_verified",
            "unmodified New API release must have verified source/image equivalence",
        )


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate a CineWeave internal Commercial Release Manifest.",
    )
    parser.add_argument("--manifest", type=pathlib.Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--schema", type=pathlib.Path, default=DEFAULT_SCHEMA)
    parser.add_argument("--ddl-owners", type=pathlib.Path, default=DEFAULT_DDL_OWNERS)
    parser.add_argument(
        "--verify-local-core",
        action="store_true",
        help="also require the manifest Core migration head to equal this checkout",
    )
    parser.add_argument(
        "--require-assembly-evidence",
        action="store_true",
        help="document that this invocation is a formal candidate check (the default mode)",
    )
    parser.add_argument(
        "--contract-only",
        action="store_true",
        help="validate a non-release contract fixture without immutable assembly evidence",
    )
    parser.add_argument("--assembly-inputs", type=pathlib.Path)
    parser.add_argument("--core-lock", type=pathlib.Path)
    parser.add_argument("--overlay-allowlist", type=pathlib.Path)
    parser.add_argument("--assembly-script", type=pathlib.Path)
    parser.add_argument("--source-archive", type=pathlib.Path)
    parser.add_argument("--core-fk-actions", type=pathlib.Path)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    try:
        schema = load_json(args.schema.resolve())
        manifest = load_json(args.manifest.resolve())
        ddl_owners = load_json(args.ddl_owners.resolve())
        fail(
            schema.get("$schema") == "https://json-schema.org/draft/2020-12/schema",
            "release manifest schema must use JSON Schema draft 2020-12",
        )
        fail(
            ddl_owners.get("schemaVersion") == "cineweave.ddl-owners.v1",
            "DDL owner manifest schemaVersion is invalid",
        )
        validate_schema_value(manifest, schema, schema, "$")
        validate_semantics(
            manifest,
            ddl_owners,
            args.ddl_owners.resolve(),
            args.verify_local_core,
        )
        evidence_values = [getattr(args, field) for field in ASSEMBLY_EVIDENCE_FIELDS]
        if args.contract_only:
            fail(
                not args.require_assembly_evidence,
                "--contract-only cannot be combined with --require-assembly-evidence",
            )
            fail(
                not any(value is not None for value in evidence_values),
                "--contract-only cannot accept partial or real assembly evidence",
            )
        else:
            missing = [
                f"--{field.replace('_', '-')}"
                for field, value in zip(ASSEMBLY_EVIDENCE_FIELDS, evidence_values, strict=True)
                if value is None
            ]
            fail(
                not missing,
                "complete assembly evidence is required; missing " + ", ".join(missing),
            )
            validate_assembly_evidence(
                manifest,
                args.assembly_inputs,
                args.core_lock,
                args.overlay_allowlist,
                args.assembly_script,
                args.source_archive,
                args.core_fk_actions,
            )
    except ManifestError as exc:
        print(f"Combined Release Manifest check failed: {exc}", file=sys.stderr)
        return 1

    print(
        "Combined Release Manifest passed: "
        f"release={manifest['releaseId']} edition={manifest['edition']} "
        f"coreHead={manifest['migrations']['core']['head']} "
        f"commercialHead={manifest['migrations']['commercial']['head']} "
        f"images={len(manifest['artifacts']['images'])}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

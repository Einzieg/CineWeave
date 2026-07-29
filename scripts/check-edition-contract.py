from __future__ import annotations

import json
import pathlib
import re
import sys

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
CONTRACT_PATH = ROOT / "packages" / "edition" / "edition.v1.json"
GO_CONTRACT_PATH = ROOT / "internal" / "edition" / "contracts.go"
GO_AUTHORIZATION_PATH = ROOT / "internal" / "edition" / "community.go"
WEB_TYPES_PATH = ROOT / "apps" / "web" / "src" / "lib" / "types.ts"
WEB_ENTRY_CONTRACT_PATH = ROOT / "apps" / "web" / "src" / "edition" / "contract.ts"
OPENAPI_PATH = ROOT / "packages" / "openapi" / "openapi.yaml"
API_MODULE_PATH = ROOT / "internal" / "api" / "edition_modules.go"


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def go_typed_constant_values(source: str, type_name: str) -> list[str]:
    pattern = re.compile(
        rf"^\s*[A-Za-z0-9_]+\s+{re.escape(type_name)}\s*=\s*\"([^\"]+)\"",
        re.MULTILINE,
    )
    return pattern.findall(source)


def typescript_union_values(source: str, type_name: str) -> list[str]:
    match = re.search(
        rf"export type {re.escape(type_name)}\s*=\s*(.*?);",
        source,
        re.DOTALL,
    )
    require(match is not None, f"TypeScript union {type_name} is missing")
    return re.findall(r'"([^"]+)"', match.group(1))


def openapi_enum(document: dict, schema_name: str, property_name: str | None = None) -> list[str]:
    schema = document["components"]["schemas"][schema_name]
    if property_name is not None:
        schema = schema["properties"][property_name]
    values = schema.get("enum")
    require(isinstance(values, list), f"OpenAPI enum {schema_name}.{property_name or ''} is missing")
    return values


def assert_same_ordered_values(label: str, expected: list[str], actual: list[str]) -> None:
    require(
        actual == expected,
        f"{label} drifted from edition.v1.json: expected={expected}, actual={actual}",
    )


def interface_methods(source: str, interface_name: str) -> list[str]:
    match = re.search(
        rf"type {re.escape(interface_name)} interface \{{(.*?)\n\}}",
        source,
        re.DOTALL,
    )
    require(match is not None, f"Go interface {interface_name} is missing")
    return re.findall(r"^\s*([A-Z][A-Za-z0-9_]*)\(", match.group(1), re.MULTILINE)


def go_struct_fields(source: str, struct_name: str) -> list[str]:
    match = re.search(
        rf"type {re.escape(struct_name)} struct \{{(.*?)\n\}}",
        source,
        re.DOTALL,
    )
    require(match is not None, f"Go struct {struct_name} is missing")
    return re.findall(r"^\s*([A-Z][A-Za-z0-9_]*)\s+", match.group(1), re.MULTILINE)


def web_entry_slots(source: str) -> list[str]:
    match = re.search(r"export type EditionEntry = \{(.*?)\n\};", source, re.DOTALL)
    require(match is not None, "Web EditionEntry type is missing")
    return re.findall(r"^\s*([A-Za-z][A-Za-z0-9_]*):", match.group(1), re.MULTILINE)


def main() -> int:
    try:
        contract = json.loads(CONTRACT_PATH.read_text(encoding="utf-8"))
        require(
            contract.get("schemaVersion") == "cineweave.edition-contract.v1",
            "edition contract schemaVersion is invalid",
        )
        require(contract.get("contractVersion") == "edition.v1", "edition contractVersion is invalid")

        go_contract = GO_CONTRACT_PATH.read_text(encoding="utf-8")
        go_authorization = (
            GO_AUTHORIZATION_PATH.read_text(encoding="utf-8")
            + "\n"
            + (ROOT / "internal" / "edition" / "license.go").read_text(encoding="utf-8")
        )
        web_types = WEB_TYPES_PATH.read_text(encoding="utf-8")
        web_entry = WEB_ENTRY_CONTRACT_PATH.read_text(encoding="utf-8")
        openapi = yaml.safe_load(OPENAPI_PATH.read_text(encoding="utf-8"))

        assert_same_ordered_values(
            "Go editions",
            contract["editions"],
            go_typed_constant_values(go_contract, "Edition"),
        )
        assert_same_ordered_values(
            "Go operational modes",
            contract["operationalModes"],
            go_typed_constant_values(go_contract, "OperationalMode"),
        )
        assert_same_ordered_values(
            "Go restriction reasons",
            contract["restrictionReasons"],
            go_typed_constant_values(go_contract, "RestrictionReason"),
        )
        assert_same_ordered_values(
            "Go feature keys",
            contract["featureKeys"],
            go_typed_constant_values(go_contract, "FeatureKey"),
        )
        assert_same_ordered_values(
            "Go denial codes",
            contract["denialCodes"],
            go_typed_constant_values(go_contract, "DenialCode"),
        )
        assert_same_ordered_values(
            "Go Provider management scopes",
            contract["providerManagementScopes"],
            go_typed_constant_values(go_contract, "ProviderManagementScope"),
        )
        assert_same_ordered_values(
            "Go BillingAccount scopes",
            contract["billingAccountScopes"],
            go_typed_constant_values(go_contract, "BillingAccountScope"),
        )
        assert_same_ordered_values(
            "Go commercial API resource scopes",
            contract["apiResourceScopes"],
            go_typed_constant_values(go_contract, "APIResourceScope"),
        )
        assert_same_ordered_values(
            "Go license states",
            contract["licenseStates"],
            go_typed_constant_values(
                (ROOT / "internal" / "edition" / "license.go").read_text(encoding="utf-8"),
                "LicenseState",
            ),
        )
        assert_same_ordered_values(
            "Go license operations",
            contract["licenseOperations"],
            go_typed_constant_values(
                (ROOT / "internal" / "edition" / "license.go").read_text(encoding="utf-8"),
                "LicenseOperation",
            ),
        )

        assert_same_ordered_values(
            "Web editions",
            contract["editions"],
            typescript_union_values(web_types, "DeploymentEdition"),
        )
        assert_same_ordered_values(
            "Web operational modes",
            contract["operationalModes"],
            typescript_union_values(web_types, "EditionOperationalMode"),
        )
        assert_same_ordered_values(
            "Web restriction reasons",
            contract["restrictionReasons"],
            typescript_union_values(web_types, "EditionRestrictionReason"),
        )
        assert_same_ordered_values(
            "Web feature keys",
            contract["featureKeys"],
            typescript_union_values(web_types, "EditionFeatureKey"),
        )
        assert_same_ordered_values(
            "Web denial codes",
            contract["denialCodes"],
            typescript_union_values(web_types, "EntitlementDenialCode"),
        )

        assert_same_ordered_values(
            "OpenAPI editions",
            contract["editions"],
            openapi_enum(openapi, "SystemEdition", "deploymentEdition"),
        )
        assert_same_ordered_values(
            "OpenAPI operational modes",
            contract["operationalModes"],
            openapi_enum(openapi, "SystemEdition", "operationalMode"),
        )
        assert_same_ordered_values(
            "OpenAPI restriction reasons",
            contract["restrictionReasons"],
            openapi_enum(openapi, "EditionRestrictionReason"),
        )
        assert_same_ordered_values(
            "OpenAPI feature keys",
            contract["featureKeys"],
            openapi_enum(openapi, "EditionFeatureKey"),
        )
        assert_same_ordered_values(
            "OpenAPI denial codes",
            contract["denialCodes"],
            openapi_enum(openapi, "EntitlementDenialCode"),
        )

        for interface_name, expected_methods in contract["goInterfaces"].items():
            assert_same_ordered_values(
                f"Go interface {interface_name}",
                expected_methods,
                interface_methods(go_contract, interface_name),
            )
        assert_same_ordered_values(
            "Web EditionEntry slots",
            contract["webEditionEntrySlots"],
            web_entry_slots(web_entry),
        )
        assert_same_ordered_values(
            "Go APIModuleRegistration fields",
            contract["apiModuleRegistration"]["requiredFields"],
            go_struct_fields(go_contract, "APIModuleRegistration"),
        )
        api_module_source = API_MODULE_PATH.read_text(encoding="utf-8")
        require(
            "s.withAuth" in api_module_source,
            "commercial API module registration must use Core authentication",
        )
        require(
            api_module_source.index("entitlements.Evaluate")
            < api_module_source.index("authorizer.Authorize"),
            "commercial API entitlement must be evaluated before RBAC",
        )
        register_start = api_module_source.index("func (s *Server) registerEditionAPIModules")
        authorize_call = api_module_source.index("authorizeEditionAPIModule(", register_start)
        handler_call = api_module_source.index("registration.Handler(", register_start)
        require(
            authorize_call < handler_call,
            "commercial API authorization must run before the private handler",
        )

        formula = contract["effectiveSpendAuthorization"]
        require(
            formula.get("sponsorshipCreatesRBACPermission") is False,
            "sponsorship must not create RBAC permission",
        )
        for fact in (
            formula["allOf"][:-1]
            + formula["organizationAccountScopeCondition"]
            + formula["personalAccountScopeCondition"]
        ):
            require(
                fact in go_contract,
                f"effective spend authorization fact {fact} is missing from the Go contract",
            )
        require(
            "EvaluateEffectiveSpendAuthorization" in go_authorization,
            "effective spend authorization evaluator is missing",
        )
        for fact in sum(contract["billingAuthorityIsolation"].values(), []):
            require(
                fact in go_contract,
                f"Billing Authority isolation fact {fact} is missing from the Go contract",
            )
        require(
            "EvaluateBillingAuthorityIsolation" in go_authorization,
            "Billing Authority isolation evaluator is missing",
        )
    except (KeyError, OSError, ValueError, json.JSONDecodeError, yaml.YAMLError) as exc:
        print(f"Edition contract check failed: {exc}", file=sys.stderr)
        return 1

    print("Edition v1 contract matches Go, Web, OpenAPI, and authorization formula.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

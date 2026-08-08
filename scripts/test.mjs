import { spawnSync } from "node:child_process";

const isWindows = process.platform === "win32";

const commands = [
  ["go", ["test", "./..."]],
  ["go", ["run", "./cmd/cineweave-migrate", "validate"]],
  ["go", ["run", "./cmd/cineweave-migration-bundle", "verify"]],
  ["go", ["run", "./cmd/cineweave-seed", "validate"]],
  ["go", ["run", "./cmd/project-control-contracts", "-check"]],
  ["python", ["scripts/check-project-control-contracts.py", "--production"]],
  ["pnpm", ["--filter", "@cineweave/web", "test"]],
  ["pnpm", ["--filter", "@cineweave/web", "typecheck"]],
  ["pnpm", ["--filter", "@cineweave/web", "lint"]],
  [
    "python",
    [
      "-c",
      "import pathlib, yaml; yaml.safe_load(pathlib.Path('packages/openapi/openapi.yaml').read_text(encoding='utf-8'))",
    ],
  ],
  ["python", ["scripts/check-openapi-routes.py"]],
  ["python", ["scripts/test-openapi-route-checker.py"]],
  ["python", ["scripts/test-commercial-contract-assembly.py"]],
  ["python", ["scripts/test-new-api-upstream-evidence.py"]],
  ["python", ["scripts/test-new-api-runtime-image.py"]],
  ["python", ["scripts/check-edition-contract.py"]],
  ["python", ["scripts/check-commercial-assembly-contract.py", "--schema-only"]],
  ["python", ["scripts/test-commercial-assembly-contract.py"]],
  ["python", ["scripts/check-release-manifest.py", "--contract-only"]],
  ["python", ["scripts/test-release-manifest.py"]],
  ["python", ["scripts/test-source-licensing-audit.py"]],
  [
    "python",
    [
      "scripts/audit-source-licensing.py",
      "--output",
      "tmp/source-licensing-audit.json",
    ],
  ],
  ["python", ["scripts/test_ce_release_audit.py"]],
  [
    "python",
    [
      "scripts/ce_release_audit.py",
      "--policy",
      "packages/edition/ce-release-policy.v1.json",
      "history",
      "--repo",
      ".",
    ],
  ],
  ["python", ["scripts/check-commerce-development-contract.py"]],
  ["pwsh", ["-NoProfile", "-File", "scripts/test-commerce-smoke-script.ps1"]],
  ["pwsh", ["-NoProfile", "-File", "scripts/test-commerce-deploy-script.ps1"]],
  ["pwsh", ["-NoProfile", "-File", "scripts/test-commercial-assembly-script.ps1"]],
  ["pwsh", ["-NoProfile", "-File", "scripts/test-release-evidence.ps1"]],
  ["pwsh", ["-NoProfile", "-File", "scripts/test-release-build-cache.ps1"]],
  ["pwsh", ["-NoProfile", "-File", "scripts/test-provider-data-guard.ps1"]],
  ["docker", ["compose", "-f", "compose.yml", "config", "--quiet"]],
];

for (const [command, args] of commands) {
  console.log(`\n> ${command} ${args.join(" ")}`);
  const result =
    isWindows && command === "pnpm"
      ? spawnSync(`${command} ${args.join(" ")}`, { stdio: "inherit", shell: true })
      : spawnSync(command, args, { stdio: "inherit" });
  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}

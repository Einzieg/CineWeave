import { spawnSync } from "node:child_process";

const isWindows = process.platform === "win32";

const commands = [
  ["go", ["test", "./..."]],
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

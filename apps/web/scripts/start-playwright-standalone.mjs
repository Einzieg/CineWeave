import { spawn } from "node:child_process";
import { cpSync, existsSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const standaloneWebRoot = join(webRoot, ".next", "standalone", "apps", "web");
const serverEntry = join(standaloneWebRoot, "server.js");
const port = Number(process.argv[2] ?? "19395");

if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error(`invalid Playwright server port: ${process.argv[2] ?? ""}`);
}
if (!existsSync(serverEntry)) {
  throw new Error("standalone Web build is missing; run pnpm --filter @cineweave/web build first");
}

copyBuildDirectory(join(webRoot, ".next", "static"), join(standaloneWebRoot, ".next", "static"));
copyBuildDirectory(join(webRoot, "public"), join(standaloneWebRoot, "public"));

const child = spawn(process.execPath, [serverEntry], {
  cwd: standaloneWebRoot,
  env: {
    ...process.env,
    HOSTNAME: "127.0.0.1",
    PORT: String(port),
  },
  stdio: "inherit",
});

let forwardedSignal = false;
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.once(signal, () => {
    forwardedSignal = true;
    if (!child.killed) child.kill(signal);
  });
}

child.on("error", (error) => {
  console.error(error);
  process.exitCode = 1;
});
child.on("exit", (code) => {
  process.exit(code ?? (forwardedSignal ? 0 : 1));
});

function copyBuildDirectory(source, target) {
  if (!existsSync(source)) {
    throw new Error(`required Web build directory is missing: ${source}`);
  }
  rmSync(target, { recursive: true, force: true });
  cpSync(source, target, { recursive: true });
}

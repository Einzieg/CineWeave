import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const webRoot = fileURLToPath(new URL("../..", import.meta.url));

test("production layout does not fetch remote fonts at build time", () => {
  const layout = readFileSync(join(webRoot, "app", "layout.tsx"), "utf8");
  const globals = readFileSync(join(webRoot, "app", "globals.css"), "utf8");

  assert.doesNotMatch(layout, /next\/font\/google/);
  assert.doesNotMatch(layout, /fonts\.googleapis\.com/);
  assert.doesNotMatch(globals, /fonts\.googleapis\.com/);
});

import assert from "node:assert/strict";
import test from "node:test";

import { resolveBuildId } from "../../build-id.mjs";

test("production Web build ID follows the immutable release ID", () => {
  assert.equal(resolveBuildId("cw-01234567-89abcdef"), "cw-01234567-89abcdef");
});

test("local builds keep Next.js default build ID generation", () => {
  assert.equal(resolveBuildId(""), null);
  assert.equal(resolveBuildId("   "), null);
});

test("mutable or malformed release IDs fail closed", () => {
  for (const value of [
    "latest",
    "main",
    "master",
    "dev",
    "development",
    "local-dev",
    "short",
    "release id",
  ]) {
    assert.throws(() => resolveBuildId(value), /immutable release identifier/);
  }
});

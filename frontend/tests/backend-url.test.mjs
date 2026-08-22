import assert from "node:assert/strict";
import test from "node:test";

const { backendUrl } = await import("../src/middle/backend-url.ts");

test("configured origin wins and trailing slashes are trimmed", () => {
  process.env.CHAINTRACE_BACKEND_URL = "https://api.chaintrace.test//";
  assert.equal(backendUrl(), "https://api.chaintrace.test");

  process.env.CHAINTRACE_BACKEND_URL = "  http://localhost:9999  ";
  assert.equal(backendUrl(), "http://localhost:9999");
});

test("an unset origin falls back to the local Go API port outside production", () => {
  const previousNodeEnv = process.env.NODE_ENV;
  delete process.env.CHAINTRACE_BACKEND_URL;
  process.env.NODE_ENV = "development";
  assert.equal(backendUrl(), "http://localhost:7794");

  process.env.NODE_ENV = "production";
  assert.throws(() => backendUrl(), /CHAINTRACE_BACKEND_URL is not configured/);

  if (previousNodeEnv === undefined) delete process.env.NODE_ENV;
  else process.env.NODE_ENV = previousNodeEnv;
});

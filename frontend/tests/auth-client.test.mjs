import assert from "node:assert/strict";
import test from "node:test";

test("login client exposes the 202 check-email state", async () => {
  const { loginOwner } = await import("../src/auth/client.ts");
  const requests = [];
  const result = await loginOwner(
    { identifier: "owner@example.com", password: "correct horse" },
    async (input, init) => {
      requests.push({ input, init });
      return Response.json({ status: "waiting" }, { status: 202 });
    },
  );

  assert.deepEqual(result, { status: "waiting" });
  assert.equal(requests[0].input, "/api/auth/login");
  assert.equal(requests[0].init.method, "POST");
  assert.deepEqual(JSON.parse(requests[0].init.body), {
    identifier: "owner@example.com",
    password: "correct horse",
  });
});

test("credential cookie permits non-TLS local development", async () => {
  const { credentialCookie } = await import("../src/auth/session.ts");
  const previousNodeEnv = process.env.NODE_ENV;
  process.env.NODE_ENV = "development";

  try {
    const cookie = credentialCookie("local.jwt");
    assert.match(cookie, /HttpOnly/i);
    assert.match(cookie, /SameSite=Lax/i);
    assert.doesNotMatch(cookie, /Secure/i);
  } finally {
    if (previousNodeEnv === undefined) delete process.env.NODE_ENV;
    else process.env.NODE_ENV = previousNodeEnv;
  }
});

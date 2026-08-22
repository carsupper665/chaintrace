import assert from "node:assert/strict";
import { createServer } from "node:http";
import test from "node:test";

async function startBackend(handler) {
  const server = createServer(handler);
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();

  return {
    url: `http://127.0.0.1:${address.port}`,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}

async function fetchApp(path, init) {
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  workerUrl.searchParams.set("auth-test", `${process.pid}-${Date.now()}`);
  const { default: worker } = await import(workerUrl.href);

  return worker.fetch(
    new Request(`http://localhost${path}`, init),
    {
      ASSETS: {
        fetch: async () => new Response("Not found", { status: 404 }),
      },
    },
    {
      waitUntil() {},
      passThroughOnException() {},
    },
  );
}

test("login page offers Owner credentials and a stable callback error", async () => {
  const response = await fetchApp("/login?error=callback", {
    headers: { Accept: "text/html" },
  });

  assert.equal(response.status, 200);
  const html = await response.text();
  assert.match(html, /Email 或使用者名稱/);
  assert.match(html, /type="password"/);
  assert.match(html, /登入並寄送驗證郵件/);
  assert.match(html, /驗證連結無效或已過期，請重新登入/);
  assert.doesNotMatch(html, /register|註冊/i);
});

test("login BFF accepts either an email or username", async (t) => {
  const received = [];
  const backend = await startBackend(async (request, response) => {
    let body = "";
    for await (const chunk of request) body += chunk;
    received.push({ path: request.url, body: JSON.parse(body) });
    response.writeHead(202, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ message: "challenge sent" }));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  for (const identifier of ["owner@example.com", "owner-name"]) {
    const response = await fetchApp("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ identifier, password: "correct horse" }),
    });

    assert.equal(response.status, 202);
    assert.equal(response.headers.get("cache-control"), "no-store");
  }

  assert.deepEqual(received, [
    {
      path: "/Authentication/login",
      body: { email: "owner@example.com", password: "correct horse" },
    },
    {
      path: "/Authentication/login",
      body: { username: "owner-name", password: "correct horse" },
    },
  ]);
});

test("login BFF does not expose whether an Owner account exists", async (t) => {
  let requestNumber = 0;
  const backend = await startBackend((_request, response) => {
    const backendErrors = [
      { error: "User not found: missing@example.com" },
      { error: "Invalid password for existing-owner" },
    ];
    response.writeHead(401, { "Content-Type": "application/json" });
    response.end(JSON.stringify(backendErrors[requestNumber++]));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const errors = [];
  for (const identifier of ["missing@example.com", "existing-owner"]) {
    const response = await fetchApp("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ identifier, password: "incorrect" }),
    });
    errors.push({ status: response.status, body: await response.json() });
  }

  assert.deepEqual(errors, [
    {
      status: 401,
      body: { error: "登入資料無效，請確認後再試一次" },
    },
    {
      status: 401,
      body: { error: "登入資料無效，請確認後再試一次" },
    },
  ]);
});

test("login BFF distinguishes rate limits and backend outages", async (t) => {
  let requestNumber = 0;
  const backend = await startBackend((_request, response) => {
    response.writeHead(requestNumber++ === 0 ? 429 : 500, {
      "Content-Type": "application/json",
    });
    response.end(JSON.stringify({ error: "backend detail" }));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const responses = [];
  for (const identifier of ["rate-limited", "backend-outage"]) {
    const response = await fetchApp("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ identifier, password: "not-exposed" }),
    });
    responses.push({ status: response.status, body: await response.json() });
  }

  assert.deepEqual(responses, [
    {
      status: 429,
      body: { error: "登入嘗試次數過多，請稍後再試" },
    },
    {
      status: 503,
      body: { error: "登入服務暫時無法使用，請稍後再試" },
    },
  ]);
});

test("callback exchanges its code for a production-safe credential cookie", async (t) => {
  const received = [];
  const backend = await startBackend((request, response) => {
    received.push(request.url);
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ token: "header.payload.signature" }));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/login/callback?code=one-time-code&id=owner-7");

  assert.equal(response.status, 303);
  assert.equal(response.headers.get("location"), "http://localhost/");
  assert.deepEqual(received, [
    "/Authentication/challenge?code=one-time-code&id=owner-7",
  ]);
  const cookie = response.headers.get("set-cookie") ?? "";
  assert.match(cookie, /^chaintrace_credential=header\.payload\.signature;/);
  assert.match(cookie, /HttpOnly/i);
  assert.match(cookie, /SameSite=Lax/i);
  assert.match(cookie, /Path=\//i);
  assert.match(cookie, /Secure/i);
  assert.doesNotMatch(await response.text(), /header\.payload\.signature/);
});

test("callback rejects missing parameters without contacting the backend", async (t) => {
  let backendCalls = 0;
  const backend = await startBackend((_request, response) => {
    backendCalls += 1;
    response.writeHead(500).end();
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/login/callback?code=missing-id");

  assert.equal(response.status, 303);
  assert.equal(
    response.headers.get("location"),
    "http://localhost/login?error=callback",
  );
  assert.equal(response.headers.get("set-cookie"), null);
  assert.equal(backendCalls, 0);
});

test("callback returns a stable login error when exchange fails", async (t) => {
  const backend = await startBackend((_request, response) => {
    response.writeHead(401, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ error: "expired auth code for owner@example.com" }));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/login/callback?code=expired&id=owner-7");

  assert.equal(response.status, 303);
  assert.equal(
    response.headers.get("location"),
    "http://localhost/login?error=callback",
  );
  assert.equal(response.headers.get("set-cookie"), null);
  assert.doesNotMatch(
    response.headers.get("location") ?? "",
    /expired|owner%40example/i,
  );
});

test("identity BFF resolves the Owner with the credential cookie", async (t) => {
  const received = [];
  const owner = {
    id: 7,
    username: "real-owner",
    display_name: "Real Owner",
    email: "owner@example.com",
  };
  const backend = await startBackend((request, response) => {
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
    });
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify(owner));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/api/auth/me", {
    headers: { Cookie: "chaintrace_credential=owner.jwt" },
  });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(await response.json(), owner);
  assert.deepEqual(received, [
    {
      path: "/api/v1/me",
      method: "GET",
      authorization: "Bearer owner.jwt",
    },
  ]);
});

test("workspace redirects an unauthenticated browser to login", async () => {
  const response = await fetchApp("/", { headers: { Accept: "text/html" } });

  assert.match(String(response.status), /^30[2378]$/);
  assert.equal(response.headers.get("location"), "http://localhost/login");
});

test("workspace redirects when the backend rejects the identity", async (t) => {
  const authorizations = [];
  const backend = await startBackend((request, response) => {
    authorizations.push(request.headers.authorization);
    response.writeHead(401, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ error: "revoked" }));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/", {
    headers: {
      Accept: "text/html",
      Cookie: "chaintrace_credential=revoked.jwt",
    },
  });

  assert.match(String(response.status), /^30[2378]$/);
  assert.equal(response.headers.get("location"), "http://localhost/login");
  assert.deepEqual(authorizations, ["Bearer revoked.jwt"]);
});

test("workspace renders the authenticated Owner and logout trigger", async (t) => {
  const backend = await startBackend((_request, response) => {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        id: 7,
        username: "real-owner",
        display_name: "Lin Yu",
        email: "owner@example.com",
      }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/", {
    headers: {
      Accept: "text/html",
      Cookie: "chaintrace_credential=valid.jwt",
    },
  });

  assert.equal(response.status, 200);
  const html = await response.text();
  assert.match(html, /Lin Yu/);
  assert.match(html, /@(?:<!-- -->)?real-owner/);
  assert.match(html, /action="\/api\/auth\/logout"/);
  assert.match(html, />登出</);
  assert.doesNotMatch(html, /shlee/i);
});

test("logout revokes the backend credential and clears its cookie", async (t) => {
  const received = [];
  const backend = await startBackend((request, response) => {
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
    });
    response.writeHead(204).end();
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/api/auth/logout", {
    method: "POST",
    headers: { Cookie: "chaintrace_credential=owner.jwt" },
  });

  assert.equal(response.status, 303);
  assert.equal(response.headers.get("location"), "http://localhost/login");
  assert.deepEqual(received, [
    {
      path: "/api/v1/logout",
      method: "POST",
      authorization: "Bearer owner.jwt",
    },
  ]);
  const cookie = response.headers.get("set-cookie") ?? "";
  assert.match(cookie, /^chaintrace_credential=;/);
  assert.match(cookie, /Max-Age=0/i);
  assert.match(cookie, /HttpOnly/i);
  assert.match(cookie, /SameSite=Lax/i);
  assert.match(cookie, /Secure/i);
});

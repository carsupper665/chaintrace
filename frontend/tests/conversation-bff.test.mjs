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
  workerUrl.searchParams.set(
    "conversation-test",
    `${process.pid}-${Date.now()}-${Math.random()}`,
  );
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

test("conversation BFF requires Owner authentication without contacting the backend", async (t) => {
  let calls = 0;
  const backend = await startBackend((_request, response) => {
    calls += 1;
    response.writeHead(500).end();
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp(
    "/api/investigations/inv-09/conversation?cursor=private&pageSize=25",
  );

  assert.equal(response.status, 401);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(await response.json(), {
    code: "unauthorized",
    error: "Authentication required",
  });
  const malformedPost = await fetchApp(
    "/api/investigations/inv-09/conversation",
    { method: "POST", body: "not-json" },
  );
  assert.equal(malformedPost.status, 401);
  assert.equal(malformedPost.headers.get("cache-control"), "no-store");
  assert.deepEqual(await malformedPost.json(), {
    code: "unauthorized",
    error: "Authentication required",
  });
  assert.equal(calls, 0);
});

test("conversation BFF proxies GET pagination and preserves non-enumerating not-found", async (t) => {
  const received = [];
  const backend = await startBackend((request, response) => {
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
    });
    response.writeHead(404, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        code: "investigation_not_found",
        error: "owner-scoped record not found",
      }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp(
    "/api/investigations/other-owner/conversation?cursor=before%2F2&pageSize=25&target=must-not-forward",
    { headers: { Cookie: "chaintrace_credential=owner.jwt" } },
  );

  assert.equal(response.status, 404);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(await response.json(), {
    code: "investigation_not_found",
    error: "owner-scoped record not found",
  });
  assert.deepEqual(received, [
    {
      path: "/api/v1/investigations/other-owner/conversation?cursor=before%2F2&pageSize=25",
      method: "GET",
      authorization: "Bearer owner.jwt",
    },
  ]);
});

test("conversation BFF forwards only the command contract and preserves persisted unavailable body", async (t) => {
  const received = [];
  const messages = [
    {
      id: "message-user",
      role: "user",
      content: "追蹤這筆交易 🧭",
      createdAt: "2026-08-04T03:00:00Z",
    },
    {
      id: "message-system",
      role: "system",
      content: "Agent provider is unavailable.",
      createdAt: "2026-08-04T03:00:00.001Z",
    },
  ];
  const backend = await startBackend(async (request, response) => {
    let body = "";
    for await (const chunk of request) body += chunk;
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
      body: JSON.parse(body),
    });
    response.writeHead(503, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({ code: "agent_unavailable", persisted: true, messages }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp(
    "/api/investigations/inv-09/conversation?cursor=must-not-forward&pageSize=1",
    {
      method: "POST",
      headers: {
        Cookie: "chaintrace_credential=owner.jwt",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        idempotencyKey: "command-key-09",
        message: "追蹤這筆交易 🧭",
        target: "TDoNotTrustBrowserSnapshot",
        score: 99,
        graph: { edges: ["browser-owned"] },
      }),
    },
  );

  assert.equal(response.status, 503);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(await response.json(), {
    code: "agent_unavailable",
    persisted: true,
    messages,
  });
  assert.deepEqual(received, [
    {
      path: "/api/v1/investigations/inv-09/conversation",
      method: "POST",
      authorization: "Bearer owner.jwt",
      body: {
        idempotencyKey: "command-key-09",
        message: "追蹤這筆交易 🧭",
      },
    },
  ]);
});

test("removed agent chat endpoint is no longer an active route", async () => {
  const response = await fetchApp("/api/middle/agent/chat", {
    method: "POST",
    headers: {
      Cookie: "chaintrace_credential=owner.jwt",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ message: "must not use old route" }),
  });

  assert.equal(response.status, 404);
});

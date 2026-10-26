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
    "claims-test",
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

test("Claim extract BFF rejects unauthenticated uploads before the backend", async (t) => {
  let calls = 0;
  const backend = await startBackend((_request, response) => {
    calls += 1;
    response.writeHead(500).end();
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const body = new FormData();
  body.append("image", new Blob([Uint8Array.from([137, 80, 78, 71])], { type: "image/png" }), "shot.png");
  const response = await fetchApp("/api/claims/extract", {
    method: "POST",
    body,
  });

  assert.equal(response.status, 401);
  assert.deepEqual(await response.json(), {
    code: "unauthorized",
    error: "Authentication required",
  });
  assert.equal(calls, 0);
});

test("Claim extract BFF forwards multipart with Owner credential", async (t) => {
  const received = [];
  const backend = await startBackend(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
      contentType: request.headers["content-type"],
      bodyLength: Buffer.concat(chunks).length,
    });
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        network: "tron",
        claims: [
          {
            kind: "address",
            value: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
            role: "to",
            confidence: 0.9,
            status: "unverified",
          },
        ],
      }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const body = new FormData();
  body.append(
    "image",
    new Blob([Uint8Array.from([137, 80, 78, 71, 13, 10, 26, 10])], {
      type: "image/png",
    }),
    "shot.png",
  );
  const response = await fetchApp("/api/claims/extract", {
    method: "POST",
    headers: { Cookie: "chaintrace_credential=owner.jwt" },
    body,
  });

  assert.equal(response.status, 200);
  assert.equal((await response.json()).claims[0].status, "unverified");
  assert.equal(received.length, 1);
  assert.equal(received[0].path, "/api/v1/claims/extract");
  assert.equal(received[0].method, "POST");
  assert.equal(received[0].authorization, "Bearer owner.jwt");
  assert.match(String(received[0].contentType), /^multipart\/form-data/);
  assert.ok(received[0].bodyLength > 0);
});

test("Claim verify BFF forwards JSON verification requests", async (t) => {
  const received = [];
  const backend = await startBackend((request, response) => {
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
    });
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
        network: "tron",
        status: "verified",
      }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/api/claims/verify-address", {
    method: "POST",
    headers: {
      Cookie: "chaintrace_credential=owner.jwt",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
    }),
  });

  assert.equal(response.status, 200);
  assert.equal((await response.json()).status, "verified");
  assert.deepEqual(received, [
    {
      path: "/api/v1/claims/verify-address",
      method: "POST",
      authorization: "Bearer owner.jwt",
    },
  ]);
});

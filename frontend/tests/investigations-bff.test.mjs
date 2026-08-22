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
    "investigations-test",
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

test("Investigation BFF requires the auth cookie without contacting the backend", async (t) => {
  let calls = 0;
  const backend = await startBackend((_request, response) => {
    calls += 1;
    response.writeHead(500).end();
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/api/investigations");

  assert.equal(response.status, 401);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(await response.json(), {
    code: "unauthorized",
    error: "Authentication required",
  });
  assert.equal(calls, 0);
});

test("Investigation BFF forwards the Owner credential and preserves backend errors", async (t) => {
  const received = [];
  const backend = await startBackend((request, response) => {
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
    });
    response.writeHead(409, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        code: "immutable_investigation_target",
        error: "target cannot be changed",
      }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp("/api/investigations/inv-locked", {
    method: "PATCH",
    headers: {
      Cookie: "chaintrace_credential=owner.jwt",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
    }),
  });

  assert.equal(response.status, 409);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(await response.json(), {
    code: "immutable_investigation_target",
    error: "target cannot be changed",
  });
  assert.deepEqual(received, [
    {
      path: "/api/v1/investigations/inv-locked",
      method: "PATCH",
      authorization: "Bearer owner.jwt",
    },
  ]);
});

test("Investigation BFF proxies collection and detail CRUD without exposing the backend URL", async (t) => {
  const received = [];
  const backend = await startBackend(async (request, response) => {
    let body = "";
    for await (const chunk of request) body += chunk;
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
      body: body ? JSON.parse(body) : null,
    });
    if (request.method === "DELETE") {
      response.writeHead(204).end();
      return;
    }
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ id: "inv-1" }));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const headers = {
    Cookie: "chaintrace_credential=owner.jwt",
    "Content-Type": "application/json",
  };
  const responses = [];
  responses.push(
    await fetchApp("/api/investigations?query=TRON", {
      headers: { Cookie: headers.Cookie },
    }),
  );
  responses.push(
    await fetchApp("/api/investigations", {
      method: "POST",
      headers,
      body: JSON.stringify({ title: "TRON flow" }),
    }),
  );
  responses.push(
    await fetchApp("/api/investigations/inv-1", {
      headers: { Cookie: headers.Cookie },
    }),
  );
  responses.push(
    await fetchApp("/api/investigations/inv-1", {
      method: "PATCH",
      headers,
      body: JSON.stringify({ title: "Renamed flow" }),
    }),
  );
  responses.push(
    await fetchApp("/api/investigations/inv-1", {
      method: "DELETE",
      headers: { Cookie: headers.Cookie },
    }),
  );

  assert.deepEqual(
    responses.map((response) => response.status),
    [200, 200, 200, 200, 204],
  );
  assert.ok(
    responses.every(
      (response) => response.headers.get("cache-control") === "no-store",
    ),
  );
  assert.deepEqual(received, [
    {
      path: "/api/v1/investigations?query=TRON",
      method: "GET",
      authorization: "Bearer owner.jwt",
      body: null,
    },
    {
      path: "/api/v1/investigations",
      method: "POST",
      authorization: "Bearer owner.jwt",
      body: { title: "TRON flow" },
    },
    {
      path: "/api/v1/investigations/inv-1",
      method: "GET",
      authorization: "Bearer owner.jwt",
      body: null,
    },
    {
      path: "/api/v1/investigations/inv-1",
      method: "PATCH",
      authorization: "Bearer owner.jwt",
      body: { title: "Renamed flow" },
    },
    {
      path: "/api/v1/investigations/inv-1",
      method: "DELETE",
      authorization: "Bearer owner.jwt",
      body: null,
    },
  ]);
});

test("Analysis BFF forwards scope body and preserves run and current-result responses", async (t) => {
  const received = [];
  const backend = await startBackend(async (request, response) => {
    let body = "";
    for await (const chunk of request) body += chunk;
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
      body: body ? JSON.parse(body) : null,
    });
    if (request.url?.endsWith("/missing")) {
      response.writeHead(404, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({
          code: "investigation_not_found",
          error: "owner-scoped record not found",
        }),
      );
      return;
    }
    if (request.url?.endsWith("/lost")) {
      response.writeHead(404, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({
          code: "run_lost",
          error: "in-memory run no longer exists",
        }),
      );
      return;
    }
    if (request.method === "DELETE") {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({
          id: "run-1",
          investigationId: "inv-1",
          status: "cancelled",
        }),
      );
      return;
    }
    if (request.method === "POST") {
      response.writeHead(202, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({
          id: "run-1",
          investigationId: "inv-1",
          status: "queued",
        }),
      );
      return;
    }
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ id: "run-1", status: "running" }));
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const unauthorized = await fetchApp(
    "/api/investigations/inv-1/analysis-runs",
    { method: "POST" },
  );
  assert.equal(unauthorized.status, 401);
  assert.equal(unauthorized.headers.get("cache-control"), "no-store");
  assert.equal(received.length, 0);
  const unauthorizedCancel = await fetchApp(
    "/api/investigations/inv-1/analysis-runs/run-1",
    { method: "DELETE" },
  );
  assert.equal(unauthorizedCancel.status, 401);
  assert.equal(received.length, 0);

  const headers = {
    Cookie: "chaintrace_credential=owner.jwt",
    "Content-Type": "application/json",
  };
  const started = await fetchApp(
    "/api/investigations/inv-1/analysis-runs",
    {
      method: "POST",
      headers,
      body: JSON.stringify({ transferLimit: 500, traversalDepth: 2 }),
    },
  );
  const polled = await fetchApp(
    "/api/investigations/inv-1/analysis-runs/run-1",
    { headers },
  );
  const current = await fetchApp(
    "/api/investigations/inv-1/current-result",
    { headers },
  );
  const missing = await fetchApp(
    "/api/investigations/inv-1/analysis-runs/missing",
    { headers },
  );
  const lost = await fetchApp(
    "/api/investigations/inv-1/analysis-runs/lost",
    { headers },
  );
  const cancelled = await fetchApp(
    "/api/investigations/inv-1/analysis-runs/run-1",
    { method: "DELETE", headers },
  );

  assert.equal(started.status, 202);
  assert.equal(polled.status, 200);
  assert.equal(current.status, 200);
  assert.equal(missing.status, 404);
  assert.equal(lost.status, 404);
  assert.equal(cancelled.status, 200);
  assert.ok(
    [started, polled, current, missing, lost, cancelled].every(
      (response) => response.headers.get("cache-control") === "no-store",
    ),
  );
  assert.deepEqual(await missing.json(), {
    code: "investigation_not_found",
    error: "owner-scoped record not found",
  });
  assert.deepEqual(await lost.json(), {
    code: "run_lost",
    error: "in-memory run no longer exists",
  });
  assert.deepEqual(await cancelled.json(), {
    id: "run-1",
    investigationId: "inv-1",
    status: "cancelled",
  });
  assert.deepEqual(received, [
    {
      path: "/api/v1/investigations/inv-1/analysis-runs",
      method: "POST",
      authorization: "Bearer owner.jwt",
      body: { transferLimit: 500, traversalDepth: 2 },
    },
    {
      path: "/api/v1/investigations/inv-1/analysis-runs/run-1",
      method: "GET",
      authorization: "Bearer owner.jwt",
      body: null,
    },
    {
      path: "/api/v1/investigations/inv-1/current-result",
      method: "GET",
      authorization: "Bearer owner.jwt",
      body: null,
    },
    {
      path: "/api/v1/investigations/inv-1/analysis-runs/missing",
      method: "GET",
      authorization: "Bearer owner.jwt",
      body: null,
    },
    {
      path: "/api/v1/investigations/inv-1/analysis-runs/lost",
      method: "GET",
      authorization: "Bearer owner.jwt",
      body: null,
    },
    {
      path: "/api/v1/investigations/inv-1/analysis-runs/run-1",
      method: "DELETE",
      authorization: "Bearer owner.jwt",
      body: null,
    },
  ]);
});

test("Analysis cancel BFF preserves backend conflict details", async (t) => {
  const backend = await startBackend((_request, response) => {
    response.writeHead(409, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        code: "analysis_run_not_cancellable",
        error: "completion already won",
      }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp(
    "/api/investigations/inv-1/analysis-runs/run-finished",
    {
      method: "DELETE",
      headers: { Cookie: "chaintrace_credential=owner.jwt" },
    },
  );

  assert.equal(response.status, 409);
  assert.deepEqual(await response.json(), {
    code: "analysis_run_not_cancellable",
    error: "completion already won",
  });
});

test("Analysis BFF preserves the backend invalid-scope status and stable code", async (t) => {
  const backend = await startBackend((_request, response) => {
    response.writeHead(400, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        code: "invalid_analysis_scope",
        message: "backend remains authoritative",
      }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp(
    "/api/investigations/inv-1/analysis-runs",
    {
      method: "POST",
      headers: {
        Cookie: "chaintrace_credential=owner.jwt",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ transferLimit: 5001, traversalDepth: 2 }),
    },
  );

  assert.equal(response.status, 400);
  assert.deepEqual(await response.json(), {
    code: "invalid_analysis_scope",
    message: "backend remains authoritative",
  });
});

test("Graph BFF requires Owner authentication without contacting the backend", async (t) => {
  let calls = 0;
  const backend = await startBackend((_request, response) => {
    calls += 1;
    response.writeHead(500).end();
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp(
    "/api/investigations/inv-1/graph?datasetId=dataset-07&pageSize=100",
  );

  assert.equal(response.status, 401);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(await response.json(), {
    code: "unauthorized",
    error: "Authentication required",
  });
  assert.equal(calls, 0);
});

test("Graph BFF forwards only graph query fields and preserves stable backend errors", async (t) => {
  const received = [];
  const backend = await startBackend((request, response) => {
    received.push({
      path: request.url,
      method: request.method,
      authorization: request.headers.authorization,
    });
    response.writeHead(409, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        code: "stale_dataset",
        error: "dataset is no longer current",
      }),
    );
  });
  t.after(() => backend.close());
  process.env.CHAINTRACE_BACKEND_URL = backend.url;

  const response = await fetchApp(
    "/api/investigations/inv%2F07/graph?datasetId=dataset-07&cursor=next%2Fpage&pageSize=75&anchor=TAnchor&ignored=secret",
    { headers: { Cookie: "chaintrace_credential=owner.jwt" } },
  );

  assert.equal(response.status, 409);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.deepEqual(await response.json(), {
    code: "stale_dataset",
    error: "dataset is no longer current",
  });
  assert.deepEqual(received, [
    {
      path: "/api/v1/investigations/inv%2F07/graph?datasetId=dataset-07&cursor=next%2Fpage&pageSize=75&anchor=TAnchor",
      method: "GET",
      authorization: "Bearer owner.jwt",
    },
  ]);
});

test("legacy address-based metrics and assessment endpoints are not active", async () => {
  for (const path of [
    "/api/middle/risk-score?address=TAddress&network=TRON_MAINNET",
    "/api/middle/investigation-metrics?address=TAddress&network=TRON_MAINNET",
    "/api/middle/anomaly-analysis",
  ]) {
    const response = await fetchApp(path, {
      method: path.endsWith("anomaly-analysis") ? "POST" : "GET",
      headers: { Cookie: "chaintrace_credential=owner.jwt" },
    });
    assert.equal(response.status, 404, path);
  }
});

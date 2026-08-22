import assert from "node:assert/strict";
import test from "node:test";

const validTronAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE";

function investigation(overrides = {}) {
  return {
    id: "inv-1",
    title: "TRON flow",
    address: null,
    network: "TRON_MAINNET",
    status: "待處理",
    risk: null,
    relatedNodes: 0,
    totalFlow: 0,
    flowAsset: null,
    transactionCount: 0,
    targetLocked: false,
    currentResult: null,
    createdAt: "2026-08-04T01:00:00Z",
    updatedAt: "2026-08-04T01:00:00Z",
    ...overrides,
  };
}

function createInvestigationApi() {
  let records = [];
  let sequence = 0;
  const requests = [];

  return {
    requests,
    async fetch(input, init = {}) {
      const url = new URL(String(input), "http://browser.test");
      const method = init.method ?? "GET";
      const body = init.body ? JSON.parse(String(init.body)) : null;
      requests.push({ path: url.pathname, method, body });

      if (url.pathname === "/api/investigations" && method === "GET") {
        return Response.json(records);
      }
      if (url.pathname === "/api/investigations" && method === "POST") {
        const created = investigation({
          id: `inv-${++sequence}`,
          title: body.title,
          address: body.address ?? null,
          network: "TRON_MAINNET",
        });
        records = [...records, created];
        return Response.json(created, { status: 201 });
      }

      const id = decodeURIComponent(url.pathname.split("/").at(-1));
      const selected = records.find((item) => item.id === id);
      if (!selected) {
        return Response.json(
          { code: "INVESTIGATION_NOT_FOUND", error: "database row absent" },
          { status: 404 },
        );
      }
      if (method === "GET") return Response.json(selected);
      if (method === "PATCH") {
        const updated = {
          ...selected,
          ...body,
          updatedAt: "2026-08-04T02:00:00Z",
        };
        records = records.map((item) => (item.id === id ? updated : item));
        return Response.json(updated);
      }
      if (method === "DELETE") {
        records = records.filter((item) => item.id !== id);
        return new Response(null, { status: 204 });
      }
      return Response.json({ code: "UNEXPECTED" }, { status: 500 });
    },
  };
}

test("browser Investigation flow handles empty list, create, reload, search, select, rename, and delete", async () => {
  const {
    createInvestigation,
    deleteInvestigation,
    filterInvestigations,
    getInvestigation,
    listInvestigations,
    renameInvestigation,
  } = await import("../src/middle/investigation-client.ts");
  const api = createInvestigationApi();

  assert.deepEqual(await listInvestigations(api.fetch), []);

  const first = await createInvestigation("Alpha target", api.fetch);
  const second = await createInvestigation("Beta target", api.fetch);
  const reloaded = await listInvestigations(api.fetch);

  assert.deepEqual(
    reloaded.map((item) => item.id),
    [first.id, second.id],
  );
  assert.deepEqual(api.requests[1], {
    path: "/api/investigations",
    method: "POST",
    body: { title: "Alpha target" },
  });
  assert.deepEqual(
    filterInvestigations(reloaded, "alpha").map((item) => item.id),
    [first.id],
  );
  assert.equal((await getInvestigation(second.id, api.fetch)).title, "Beta target");

  const renamed = await renameInvestigation(first.id, "Alpha renamed", api.fetch);
  assert.equal(renamed.title, "Alpha renamed");

  await deleteInvestigation(second.id, api.fetch);
  assert.deepEqual(
    (await listInvestigations(api.fetch)).map((item) => item.id),
    [first.id],
  );
});

test("browser validates TRON Base58Check before updating a pending target", async () => {
  const {
    InvestigationClientError,
    setInvestigationTarget,
    validateTronAddress,
  } = await import("../src/middle/investigation-client.ts");
  const api = createInvestigationApi();
  const created = investigation({ id: "inv-1" });
  api.fetch = async (input, init = {}) => {
    const body = init.body ? JSON.parse(String(init.body)) : null;
    api.requests.push({ input: String(input), body });
    return Response.json({ ...created, ...body });
  };

  assert.equal(await validateTronAddress(validTronAddress), true);
  assert.equal(await validateTronAddress(`${validTronAddress.slice(0, -1)}F`), false);
  assert.equal(
    await validateTronAddress("0x52908400098527886E0F7030069857D2E4169EE7"),
    false,
  );

  await assert.rejects(
    setInvestigationTarget("inv-1", "T-invalid", api.fetch),
    (error) =>
      error instanceof InvestigationClientError &&
      error.code === "invalid_tron_target" &&
      error.status === 400,
  );
  assert.equal(api.requests.length, 0);

  const updated = await setInvestigationTarget(
    "inv-1",
    validTronAddress,
    api.fetch,
  );
  assert.equal(updated.address, validTronAddress);
  assert.deepEqual(api.requests[0], {
    input: "/api/investigations/inv-1",
    body: { address: validTronAddress },
  });
});

test("browser maps non-enumerating not-found responses to a stable UI error", async () => {
  const { getInvestigation, InvestigationClientError } = await import(
    "../src/middle/investigation-client.ts"
  );
  const fakeFetch = async () =>
    Response.json(
      {
        code: "investigation_not_found",
        error: "cross-owner database details must not reach the UI",
      },
      { status: 404 },
    );

  await assert.rejects(
    getInvestigation("other-owner-id", fakeFetch),
    (error) => {
      assert.ok(error instanceof InvestigationClientError);
      assert.equal(error.status, 404);
      assert.equal(error.code, "investigation_not_found");
      assert.equal(error.message, "找不到這筆調查，可能已被刪除或無權存取。");
      assert.doesNotMatch(error.message, /database|owner/i);
      return true;
    },
  );
});

test("browser treats omitted result summaries as no backend result", async () => {
  const { listInvestigations } = await import(
    "../src/middle/investigation-client.ts"
  );
  const responseWithoutResult = {
    id: "inv-pending",
    title: "Pending target",
    address: null,
    network: "TRON_MAINNET",
    status: "待處理",
    relatedNodes: 0,
    transactionCount: 0,
    targetLocked: false,
    currentResult: null,
    createdAt: "2026-08-04T01:00:00Z",
    updatedAt: "2026-08-04T01:00:00Z",
  };

  const [result] = await listInvestigations(async () =>
    Response.json([responseWithoutResult]),
  );

  assert.deepEqual(
    {
      risk: result.risk,
      relatedNodes: result.relatedNodes,
      totalFlow: result.totalFlow,
      flowAsset: result.flowAsset,
      transactionCount: result.transactionCount,
    },
    {
      risk: null,
      relatedNodes: null,
      totalFlow: null,
      flowAsset: null,
      transactionCount: null,
    },
  );
});

test("visual preferences ignore a legacy domain workspace snapshot", async () => {
  const { loadVisualPreferences } = await import(
    "../src/services/visualPreferences.ts"
  );
  const values = new Map([
    ["chaintrace-theme", "light"],
    ["chaintrace-sidebar-width", "340"],
    ["chaintrace-graph-zoom", "1.25"],
    ["chaintrace-graph-spread", "1.5"],
    ["chaintrace-graph-offset-x", "10"],
    ["chaintrace-graph-offset-y", "-5"],
    [
      "chaintrace-investigation-workspace",
      JSON.stringify({
        investigations: [investigation({ title: "Legacy demo" })],
        target: validTronAddress,
        graph: { nodes: ["legacy"] },
        metrics: { totalFlow: 999 },
        assessment: { risk: 1 },
        chat: ["legacy"],
      }),
    ],
  ]);
  const reads = [];
  const storage = {
    getItem(key) {
      reads.push(key);
      return values.get(key) ?? null;
    },
  };

  assert.deepEqual(loadVisualPreferences(storage), {
    isLightMode: true,
    isSidebarCollapsed: null,
    isAnalysisCollapsed: null,
    sidebarWidth: 340,
    analysisWidth: 420,
    graphZoom: 1.25,
    graphSpread: 1.5,
    graphOffset: { x: 10, y: -5 },
  });
  assert.doesNotMatch(reads.join(" "), /investigation-workspace/);
});

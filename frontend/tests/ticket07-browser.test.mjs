import assert from "node:assert/strict";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const exactAmount = {
  smallestUnit: "9007199254740993123456789",
  decimals: 6,
  asset: "USDT",
};

function result(datasetId = "dataset-07") {
  return {
    dataset: { id: datasetId },
    metrics: {
      relatedNodes: 9,
      transferCount: 42,
      totalFlow: exactAmount,
    },
    assessment: {
      source: "rules-v1",
      updatedAt: "2026-08-04T02:00:00Z",
    },
  };
}

function context(datasetId = "dataset-07") {
  return {
    investigationId: "inv-07",
    address: "TFocus",
    network: "TRON_MAINNET",
    result: result(datasetId),
  };
}

function edge(id, eventIdentity, from = "focus", to = "peer-a") {
  return {
    id,
    transactionHash: "shared-transaction",
    eventIdentity,
    from,
    to,
    amount: exactAmount,
    timestamp: "2026-08-04T02:00:00Z",
  };
}

function page(overrides = {}) {
  return {
    datasetId: "dataset-07",
    nodes: [
      { id: "focus", address: "TFocus", type: "focus" },
      { id: "peer-a", address: "TPeerA", type: "normal" },
    ],
    edges: [edge("event-1", "log-0")],
    nextCursor: "page/2",
    hasMore: true,
    ...overrides,
  };
}

test("browser loads cursor pages, owns deterministic layout, dedupes IDs, and preserves Dataset metrics", async () => {
  const { createTransactionGraphBrowser } = await import(
    "../src/services/transactionGraphBrowser.ts"
  );
  const calls = [];
  const fetchImpl = async (input) => {
    const url = new URL(String(input), "http://browser.test");
    calls.push(url);
    if (url.searchParams.get("cursor") === "page/2") {
      return Response.json(
        page({
          nodes: [
            { id: "peer-a", address: "TPeerA", type: "normal" },
            { id: "peer-b", address: "TPeerB", type: "contract" },
          ],
          edges: [
            edge("event-1", "log-0"),
            edge("event-2", "log-1", "peer-a", "peer-b"),
          ],
          nextCursor: null,
          hasMore: false,
        }),
      );
    }
    return Response.json(page());
  };
  const browser = createTransactionGraphBrowser(fetchImpl, 2);

  const first = await browser.load(context());
  const firstPositions = first.nodes.map(({ id, x, y, group }) => ({ id, x, y, group }));
  assert.deepEqual(calls[0].searchParams.entries().toArray(), [
    ["datasetId", "dataset-07"],
    ["pageSize", "2"],
  ]);
  assert.deepEqual(firstPositions[0], { id: "focus", x: 50, y: 50, group: 0 });
  assert.ok(firstPositions.every((node) => Number.isFinite(node.x) && Number.isFinite(node.y)));
  assert.ok(first.nodes.every((node) => "group" in node));

  const merged = await browser.loadMore();
  assert.equal(calls[1].searchParams.get("cursor"), "page/2");
  assert.equal(merged.nodes.length, 3);
  assert.equal(merged.edges.length, 2);
  assert.equal(merged.edges[0].transactionHash, merged.edges[1].transactionHash);
  assert.notEqual(merged.edges[0].eventIdentity, merged.edges[1].eventIdentity);
  assert.deepEqual(
    merged.nodes.find((node) => node.id === "peer-a"),
    first.nodes.find((node) => node.id === "peer-a"),
  );
  assert.equal(merged.transactionCount, 42);
  assert.deepEqual(merged.totalFlow, exactAmount);
  assert.equal(merged.hasMore, false);

  const replay = createTransactionGraphBrowser(async () => Response.json(page()), 2);
  const replayed = await replay.load(context());
  assert.deepEqual(
    replayed.nodes.map(({ id, x, y, group }) => ({ id, x, y, group })),
    firstPositions,
  );
});

test("browser anchor expansion merges only same-Dataset relationships through the graph route", async () => {
  const { createTransactionGraphBrowser } = await import(
    "../src/services/transactionGraphBrowser.ts"
  );
  const ledger = [];
  const fetchImpl = async (input) => {
    const url = new URL(String(input), "http://browser.test");
    ledger.push(url);
    if (url.searchParams.get("anchor")) {
      return Response.json(
        page({
          nodes: [
            { id: "peer-a", address: "TPeerA", type: "normal" },
            { id: "peer-c", address: "TPeerC", type: "normal" },
          ],
          edges: [edge("event-3", "log-2", "peer-a", "peer-c")],
          nextCursor: null,
          hasMore: false,
        }),
      );
    }
    return Response.json(page({ nextCursor: null, hasMore: false }));
  };
  const browser = createTransactionGraphBrowser(fetchImpl, 100);
  await browser.load(context());

  const expanded = await browser.expand("peer-a");

  assert.equal(ledger[1].searchParams.get("datasetId"), "dataset-07");
  assert.equal(ledger[1].searchParams.get("anchor"), "peer-a");
  assert.ok(ledger.every((url) => url.pathname === "/api/investigations/inv-07/graph"));
  assert.ok(
    ledger.every(
      (url) => !/provider|analysis-runs|current-result|risk|anomaly|evaluator/.test(url.href),
    ),
  );
  assert.equal(expanded.nodes.length, 3);
  assert.equal(expanded.edges.length, 2);
  assert.equal(expanded.nodes.find((node) => node.id === "peer-c").group, 1);
  assert.equal(expanded.edges.find((item) => item.id === "event-3").group, 1);
  assert.equal(expanded.transactionCount, 42);
});

test("switching Dataset aborts and discards a stale graph response while clearing view state", async () => {
  const { createTransactionGraphBrowser } = await import(
    "../src/services/transactionGraphBrowser.ts"
  );
  let resolveOld;
  let oldSignal;
  const oldResponse = new Promise((resolve) => {
    resolveOld = resolve;
  });
  const fetchImpl = async (input, init) => {
    const url = new URL(String(input), "http://browser.test");
    const datasetId = url.searchParams.get("datasetId");
    if (datasetId === "dataset-old") {
      oldSignal = init.signal;
      return oldResponse;
    }
    return Response.json(
      page({
        datasetId: "dataset-new",
        nodes: [{ id: "new-focus", address: "TNew", type: "focus" }],
        edges: [],
        nextCursor: null,
        hasMore: false,
      }),
    );
  };
  const browser = createTransactionGraphBrowser(fetchImpl);

  const oldLoad = browser.load(context("dataset-old"));
  const newLoad = browser.load(context("dataset-new"));
  assert.equal(browser.getGraph(), null);
  const current = await newLoad;
  resolveOld(
    Response.json(
      page({
        datasetId: "dataset-old",
        nodes: [{ id: "old-focus", address: "TOld", type: "focus" }],
        edges: [],
        nextCursor: null,
        hasMore: false,
      }),
    ),
  );
  await oldLoad;

  assert.equal(oldSignal.aborted, true);
  assert.equal(current.datasetId, "dataset-new");
  assert.equal(browser.getGraph().datasetId, "dataset-new");
  assert.deepEqual(browser.getGraph().nodes.map((node) => node.id), ["new-focus"]);
});

test("graph rendering displays exact BigInt amounts and separates Dataset totals from visible counts", async () => {
  const { TransactionGraphAmount, TransactionGraphCounts } = await import(
    "../src/ui/AnalysisPanel.tsx"
  );
  const metrics = result().metrics;
  const before = structuredClone(metrics);
  const html = renderToStaticMarkup(
    createElement(
      "div",
      null,
      createElement(TransactionGraphAmount, { amount: exactAmount }),
      createElement(TransactionGraphCounts, {
        metrics,
        loadedNodes: 3,
        loadedEdges: 2,
        visibleEdges: 1,
      }),
    ),
  );

  assert.match(html, /9,007,199,254,740,993,123\.456789 USDT/);
  assert.match(html, /Dataset 總計：9 節點 · 42 Transfer/);
  assert.match(html, /目前顯示：1 \/ 已載入 2 Transfer/);
  assert.deepEqual(metrics, before);
});

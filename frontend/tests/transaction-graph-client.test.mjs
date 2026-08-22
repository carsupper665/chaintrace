import assert from "node:assert/strict";
import test from "node:test";

const exactAmount = {
  smallestUnit: "9007199254740993123456789",
  decimals: 6,
  asset: "USDT",
};

function graphPage(overrides = {}) {
  return {
    datasetId: "dataset-07",
    nodes: [
      { id: "node-focus", address: "TFocus", type: "focus" },
      { id: "node-peer", address: "TPeer", type: "normal" },
    ],
    edges: [
      {
        id: "transfer-1",
        transactionHash: "transaction-shared",
        eventIdentity: "log-0",
        from: "node-focus",
        to: "node-peer",
        amount: exactAmount,
        timestamp: "2026-08-04T02:00:00Z",
      },
      {
        id: "transfer-2",
        transactionHash: "transaction-shared",
        eventIdentity: "log-1",
        from: "node-peer",
        to: "node-focus",
        amount: { ...exactAmount, smallestUnit: "1" },
        timestamp: "2026-08-04T02:00:01Z",
      },
    ],
    nextCursor: "cursor/2",
    hasMore: true,
    ...overrides,
  };
}

test("graph client requests a Dataset-bound first page and preserves exact multi-event Transfers", async () => {
  const { getTransactionGraphPage } = await import(
    "../src/middle/transaction-graph-client.ts"
  );
  const requests = [];
  const fetchImpl = async (input, init) => {
    requests.push({ input: String(input), init });
    return Response.json(graphPage());
  };

  const page = await getTransactionGraphPage(
    "inv/07",
    { datasetId: "dataset-07", pageSize: 100 },
    fetchImpl,
  );

  assert.equal(
    requests[0].input,
    "/api/investigations/inv%2F07/graph?datasetId=dataset-07&pageSize=100",
  );
  assert.equal(requests[0].init.cache, "no-store");
  assert.equal(requests[0].init.headers.Accept, "application/json");
  assert.equal(page.edges.length, 2);
  assert.equal(page.edges[0].transactionHash, page.edges[1].transactionHash);
  assert.notEqual(page.edges[0].eventIdentity, page.edges[1].eventIdentity);
  assert.equal(page.edges[0].amount.smallestUnit, exactAmount.smallestUnit);
  assert.equal(typeof page.edges[0].amount.smallestUnit, "string");
  assert.equal("x" in page.nodes[0], false);
  assert.equal("group" in page.nodes[0], false);
});

test("graph client emits cursor and anchor queries against only the graph route", async () => {
  const { getTransactionGraphPage } = await import(
    "../src/middle/transaction-graph-client.ts"
  );
  const calls = [];
  const fetchImpl = async (input) => {
    calls.push(String(input));
    return Response.json(graphPage({ nextCursor: null, hasMore: false }));
  };

  await getTransactionGraphPage(
    "inv-1",
    {
      datasetId: "dataset-07",
      cursor: "cursor/2",
      pageSize: 75,
      anchor: "node-peer",
    },
    fetchImpl,
  );

  assert.deepEqual(calls, [
    "/api/investigations/inv-1/graph?datasetId=dataset-07&cursor=cursor%2F2&pageSize=75&anchor=node-peer",
  ]);
  assert.ok(calls.every((path) => path.includes("/graph?")));
  assert.ok(calls.every((path) => !/provider|analysis-runs|current-result|risk|anomaly/.test(path)));
});

test("graph client rejects invalid contracts and maps stale Dataset errors stably", async () => {
  const { getTransactionGraphPage, TransactionGraphClientError } = await import(
    "../src/middle/transaction-graph-client.ts"
  );

  await assert.rejects(
    getTransactionGraphPage(
      "inv-1",
      { datasetId: "dataset-07", pageSize: 100 },
      async () =>
        Response.json(
          graphPage({
            nodes: [
              {
                id: "node",
                address: "TNode",
                type: "normal",
                x: 4,
                y: 8,
                group: 1,
              },
            ],
          }),
        ),
    ),
    (error) => {
      assert.ok(error instanceof TransactionGraphClientError);
      assert.equal(error.status, 502);
      assert.equal(error.code, "invalid_graph_response");
      return true;
    },
  );

  await assert.rejects(
    getTransactionGraphPage(
      "inv-1",
      { datasetId: "dataset-old", pageSize: 100 },
      async () =>
        Response.json(
          { code: "stale_dataset", error: "internal dataset detail" },
          { status: 409 },
        ),
    ),
    (error) => {
      assert.ok(error instanceof TransactionGraphClientError);
      assert.equal(error.status, 409);
      assert.equal(error.code, "stale_dataset");
      assert.equal(error.message, "分析資料集已更新，請重新載入目前結果。");
      assert.doesNotMatch(error.message, /internal/);
      return true;
    },
  );
});

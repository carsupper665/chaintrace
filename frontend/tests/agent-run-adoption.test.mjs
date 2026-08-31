import assert from "node:assert/strict";
import test from "node:test";

// A run the Agent started belongs to the Owner just as much as one they started
// themselves. These cover the seam that makes it watchable: the run id travels
// on the Investigation, and the runner can attach to an id it never issued.

function completedResult() {
  return {
    dataset: { id: "ds-adopted", collectedTransfers: 3, partial: false },
    metrics: {
      relatedNodes: 4,
      transferCount: 3,
      totalFlow: { smallestUnit: "3000000", decimals: 6, asset: "USDT" },
    },
    assessment: {
      score: 15,
      level: "low",
      reasons: ["fan_in"],
      nodeAssessments: [],
      source: "deterministic",
      updatedAt: "2026-08-31T00:00:00Z",
    },
  };
}

function runFeed(states) {
  const remaining = [...states];
  const seen = [];
  return {
    seen,
    async fetchImpl(input) {
      const url = new URL(String(input), "http://browser.test");
      seen.push(url.pathname);
      if (url.pathname.endsWith("/current-result")) {
        return Response.json(completedResult());
      }
      const next = remaining.length > 1 ? remaining.shift() : remaining[0];
      if (next.code) return Response.json(next, { status: 409 });
      return Response.json(next);
    },
  };
}

test("the runner attaches to a run it did not start and loads what that run published", async () => {
  const { createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );
  const feed = runFeed([
    {
      id: "run-agent",
      investigationId: "inv-adopt",
      status: "running",
      phase: "collecting",
      collectedTransfers: 2,
    },
    {
      id: "run-agent",
      investigationId: "inv-adopt",
      status: "completed",
      phase: "published",
      collectedTransfers: 3,
      resultId: "ds-adopted",
    },
  ]);
  const runner = createAnalysisRunner(feed.fetchImpl, 1);
  const progress = [];

  const outcome = await runner.attach("inv-adopt", "run-agent", (run) =>
    progress.push(run.status),
  );

  assert.equal(outcome.outcome, "completed");
  assert.equal(outcome.result.dataset.id, "ds-adopted");
  assert.deepEqual(progress, ["running", "completed"]);
  // Attaching must never create a second run: no POST path is ever requested.
  assert.ok(
    feed.seen.every((path) => !path.endsWith("/analysis-runs")),
    `attach requested a start path: ${feed.seen.join(", ")}`,
  );
});

test("attaching twice to the same Investigation watches one run, not two", async () => {
  const { createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );
  const feed = runFeed([
    {
      id: "run-agent",
      investigationId: "inv-adopt",
      status: "completed",
      phase: "published",
      collectedTransfers: 1,
      resultId: "ds-adopted",
    },
  ]);
  const runner = createAnalysisRunner(feed.fetchImpl, 1);

  const first = runner.attach("inv-adopt", "run-agent");
  const second = runner.attach("inv-adopt", "run-agent");

  assert.equal(first, second);
  await first;
  assert.equal(
    feed.seen.filter((path) => path.includes("/analysis-runs/")).length,
    1,
  );
});

test("an attached run lost to a backend restart reports run_lost rather than failing", async () => {
  const { createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );
  const feed = runFeed([{ code: "run_lost", message: "Run lost" }]);
  const runner = createAnalysisRunner(feed.fetchImpl, 1);

  const outcome = await runner.attach("inv-adopt", "run-agent");

  assert.equal(outcome.outcome, "run_lost");
  assert.equal(outcome.runId, "run-agent");
});

test("an attached run can still be cancelled, because its id is known immediately", async () => {
  const { createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );
  const methods = [];
  const fetchImpl = async (input, init = {}) => {
    const url = new URL(String(input), "http://browser.test");
    methods.push(`${init.method ?? "GET"} ${url.pathname}`);
    if ((init.method ?? "GET") === "DELETE") {
      return Response.json({
        id: "run-agent",
        investigationId: "inv-adopt",
        status: "cancelled",
      });
    }
    return Response.json({
      id: "run-agent",
      investigationId: "inv-adopt",
      status: "running",
      phase: "collecting",
      collectedTransfers: 0,
    });
  };
  const runner = createAnalysisRunner(fetchImpl, 5);
  void runner.attach("inv-adopt", "run-agent").catch(() => {});

  const cancelled = await runner.cancel();

  assert.equal(cancelled.status, "cancelled");
  assert.ok(
    methods.includes("DELETE /api/investigations/inv-adopt/analysis-runs/run-agent"),
    `cancel did not reach the run: ${methods.join(", ")}`,
  );
});

test("the Investigation carries the active run id and drops it once a result is published", async () => {
  const { listInvestigations, mergeCurrentAnalysisResult } = await import(
    "../src/middle/investigation-client.ts"
  );
  const fetchImpl = async () =>
    Response.json([
      {
        id: "inv-adopt",
        title: "Adopted",
        address: "TAdopt00000000000000000000000000001",
        network: "TRON_MAINNET",
        status: "分析中",
        risk: null,
        relatedNodes: 0,
        totalFlow: null,
        flowAsset: null,
        transactionCount: 0,
        targetLocked: true,
        currentResult: null,
        activeRun: "run-agent",
        createdAt: "2026-08-31T00:00:00Z",
        updatedAt: "2026-08-31T00:00:00Z",
      },
    ]);

  const [investigation] = await listInvestigations(fetchImpl);
  assert.equal(investigation.activeRun, "run-agent");

  const published = mergeCurrentAnalysisResult(
    investigation,
    completedResult(),
    "ds-adopted",
  );
  assert.equal(published.activeRun, null);
  assert.equal(published.currentResult, "ds-adopted");
});

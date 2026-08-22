import assert from "node:assert/strict";
import test from "node:test";

const validTronAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE";
const exactTotal = {
  smallestUnit: "9007199254740993123456",
  decimals: 6,
  asset: "USDT",
};

function investigation(overrides = {}) {
  return {
    id: "inv-1",
    title: "TRON flow",
    address: null,
    network: "TRON_MAINNET",
    status: "待處理",
    risk: null,
    relatedNodes: null,
    totalFlow: null,
    flowAsset: null,
    transactionCount: null,
    targetLocked: false,
    currentResult: null,
    createdAt: "2026-08-04T01:00:00Z",
    updatedAt: "2026-08-04T01:00:00Z",
    ...overrides,
  };
}

function currentResult() {
  return {
    dataset: {
      id: "dataset-1",
      network: "TRON_MAINNET",
      asset: "USDT",
      windowStart: "2026-07-05T02:00:00Z",
      windowEnd: "2026-08-04T02:00:00Z",
      transferLimit: 500,
      traversalDepth: 2,
      collectedTransfers: 1,
      reachedDepth: 1,
      partial: false,
      confidence: 100,
      stopReason: "source_exhausted",
      createdAt: "2026-08-04T02:00:00Z",
    },
    metrics: {
      relatedNodes: 1,
      transferCount: 1,
      totalFlow: exactTotal,
    },
    assessment: {
      score: 25,
      level: "medium",
      reasons: ["rapid_forwarding"],
      nodeAssessments: [],
      source: "rules-v1",
      updatedAt: "2026-08-04T02:00:00Z",
    },
  };
}

function currentResultWithDataset(datasetId, score) {
  const result = currentResult();
  return {
    ...result,
    dataset: { ...result.dataset, id: datasetId },
    assessment: { ...result.assessment, score },
  };
}

function createAnalysisApi({ terminalStatus = "completed", errorCode } = {}) {
  let record = investigation();
  let pollCount = 0;
  const requests = [];

  return {
    requests,
    get record() {
      return record;
    },
    async fetch(input, init = {}) {
      const url = new URL(String(input), "http://browser.test");
      const method = init.method ?? "GET";
      const body = init.body ? JSON.parse(String(init.body)) : null;
      requests.push({ path: url.pathname, method, body });

      if (url.pathname === "/api/investigations/inv-1" && method === "PATCH") {
        record = { ...record, ...body };
        return Response.json(record);
      }
      if (url.pathname === "/api/investigations/inv-1" && method === "GET") {
        return Response.json(record);
      }
      if (
        url.pathname === "/api/investigations/inv-1/analysis-runs" &&
        method === "POST"
      ) {
        record = { ...record, status: "分析中" };
        return Response.json(
          { id: "run-1", investigationId: "inv-1", status: "queued" },
          { status: 202 },
        );
      }
      if (
        url.pathname === "/api/investigations/inv-1/analysis-runs/run-1" &&
        method === "GET"
      ) {
        pollCount += 1;
        const status = pollCount === 1 ? "running" : terminalStatus;
        if (status === "completed") {
          record = {
            ...record,
            status: "已完成",
            risk: 25,
            relatedNodes: 1,
            totalFlow: exactTotal,
            flowAsset: "USDT",
            transactionCount: 1,
            targetLocked: true,
            currentResult: "dataset-1",
          };
        } else if (status === "failed") {
          record = { ...record, status: "待處理" };
        }
        return Response.json({
          id: "run-1",
          investigationId: "inv-1",
          status,
          phase: status === "running" ? "collecting" : "finished",
          collectedTransfers: status === "running" ? 1 : 1,
          ...(status === "completed" ? { resultId: "dataset-1" } : {}),
          ...(errorCode ? { errorCode } : {}),
        });
      }
      if (
        url.pathname === "/api/investigations/inv-1/current-result" &&
        method === "GET"
      ) {
        return Response.json(currentResult());
      }
      return Response.json({ code: "UNEXPECTED" }, { status: 500 });
    },
  };
}

test("browser sends the default bounded scope, starts once, polls to completed, and reloads authoritative exact results", async () => {
  const {
    createAnalysisRunner,
    formatExactAmount,
    getInvestigation,
    mergeCurrentAnalysisResult,
    setInvestigationTarget,
  } = await import("../src/middle/investigation-client.ts");
  const api = createAnalysisApi();
  const statuses = [];

  const saved = await setInvestigationTarget(
    "inv-1",
    validTronAddress,
    api.fetch,
  );
  assert.equal(saved.address, validTronAddress);

  const runner = createAnalysisRunner(api.fetch, 0);
  const first = runner.start("inv-1", {}, (run) => statuses.push(run.status));
  const duplicate = runner.start("inv-1", {}, (run) => statuses.push(run.status));
  assert.strictEqual(duplicate, first);

  const completed = await first;
  const displayed = mergeCurrentAnalysisResult(
    saved,
    completed.result,
    completed.run.resultId,
  );
  const reloaded = await getInvestigation("inv-1", api.fetch);

  assert.deepEqual(statuses, ["queued", "running", "completed"]);
  assert.equal(completed.run.resultId, "dataset-1");
  assert.equal(completed.result.assessment.source, "rules-v1");
  assert.deepEqual(
    {
      status: displayed.status,
      risk: displayed.risk,
      relatedNodes: displayed.relatedNodes,
      totalFlow: displayed.totalFlow,
      flowAsset: displayed.flowAsset,
      transactionCount: displayed.transactionCount,
      currentResult: displayed.currentResult,
    },
    {
      status: "已完成",
      risk: 25,
      relatedNodes: 1,
      totalFlow: exactTotal,
      flowAsset: "USDT",
      transactionCount: 1,
      currentResult: "dataset-1",
    },
  );
  assert.deepEqual(reloaded.totalFlow, exactTotal);
  assert.equal(
    formatExactAmount(completed.result.metrics.totalFlow),
    "9,007,199,254,740,993.123456",
  );
  assert.equal(
    api.requests.filter(
      (request) =>
        request.path === "/api/investigations/inv-1/analysis-runs" &&
        request.method === "POST",
    ).length,
    1,
  );
  assert.deepEqual(
    api.requests.find(
      (request) =>
        request.path === "/api/investigations/inv-1/analysis-runs" &&
        request.method === "POST",
    ),
    {
      path: "/api/investigations/inv-1/analysis-runs",
      method: "POST",
      body: { transferLimit: 500, traversalDepth: 2 },
    },
  );
  assert.equal(
    api.requests.at(-2).path,
    "/api/investigations/inv-1/current-result",
  );
});

test("analysis client sends maximum scope and rejects invalid scope before fetch", async () => {
  const {
    InvestigationClientError,
    startAnalysisRun,
  } = await import("../src/middle/investigation-client.ts");
  const requests = [];
  const fetchAccepted = async (input, init = {}) => {
    requests.push({
      path: new URL(String(input), "http://browser.test").pathname,
      body: JSON.parse(String(init.body)),
    });
    return Response.json(
      { id: "run-max", investigationId: "inv-1", status: "queued" },
      { status: 202 },
    );
  };

  await startAnalysisRun(
    "inv-1",
    { transferLimit: 5000, traversalDepth: 4 },
    fetchAccepted,
  );
  assert.deepEqual(requests, [
    {
      path: "/api/investigations/inv-1/analysis-runs",
      body: { transferLimit: 5000, traversalDepth: 4 },
    },
  ]);

  for (const scope of [
    { transferLimit: 0, traversalDepth: 2 },
    { transferLimit: 5001, traversalDepth: 2 },
    { transferLimit: 500, traversalDepth: 0 },
    { transferLimit: 500, traversalDepth: 5 },
    { transferLimit: 1.5, traversalDepth: 2 },
  ]) {
    await assert.rejects(
      startAnalysisRun("inv-1", scope, fetchAccepted),
      (error) => {
        assert.ok(error instanceof InvestigationClientError);
        assert.equal(error.status, 400);
        assert.equal(error.code, "invalid_analysis_scope");
        assert.equal(
          error.message,
          "分析範圍無效：Transfer 上限須為 1–5000，追蹤深度須為 1–4。",
        );
        return true;
      },
    );
  }
  assert.equal(requests.length, 1);

  await assert.rejects(
    startAnalysisRun(
      "inv-1",
      { transferLimit: 500, traversalDepth: 2 },
      async () =>
        Response.json(
          {
            code: "invalid_analysis_scope",
            message: "backend policy rejected the scope",
          },
          { status: 400 },
        ),
    ),
    (error) => {
      assert.ok(error instanceof InvestigationClientError);
      assert.equal(error.status, 400);
      assert.equal(error.code, "invalid_analysis_scope");
      assert.equal(
        error.message,
        "分析範圍無效：Transfer 上限須為 1–5000，追蹤深度須為 1–4。",
      );
      return true;
    },
  );
});

test("analysis client cancels a run through DELETE and preserves stable auth and backend errors", async () => {
  const { cancelAnalysisRun, InvestigationClientError } = await import(
    "../src/middle/investigation-client.ts"
  );
  const requests = [];
  const cancelled = await cancelAnalysisRun("inv/1", "run/1", async (input, init) => {
    requests.push({
      path: new URL(String(input), "http://browser.test").pathname,
      method: init.method,
    });
    return Response.json({
      id: "run/1",
      investigationId: "inv/1",
      status: "cancelled",
    });
  });

  assert.deepEqual(cancelled, {
    id: "run/1",
    investigationId: "inv/1",
    status: "cancelled",
  });
  assert.deepEqual(requests, [
    {
      path: "/api/investigations/inv%2F1/analysis-runs/run%2F1",
      method: "DELETE",
    },
  ]);

  for (const response of [
    Response.json({ code: "unauthorized" }, { status: 401 }),
    Response.json(
      { code: "analysis_run_not_cancellable", error: "completion already won" },
      { status: 409 },
    ),
  ]) {
    await assert.rejects(
      cancelAnalysisRun("inv-1", "run-1", async () => response.clone()),
      (error) => {
        assert.ok(error instanceof InvestigationClientError);
        assert.equal(error.status, response.status);
        assert.equal(
          error.code,
          response.status === 401
            ? "unauthorized"
            : "analysis_run_not_cancellable",
        );
        return true;
      },
    );
  }
});

test("browser runner cancels queued and running work and aborts its local poll", async () => {
  const { createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );

  for (const cancelAt of ["queued", "running"]) {
    const requests = [];
    let cancelPromise;
    const fetchImpl = async (input, init = {}) => {
      const path = new URL(String(input), "http://browser.test").pathname;
      const method = init.method ?? "GET";
      requests.push({ path, method });
      if (method === "POST") {
        return Response.json(
          { id: `run-${cancelAt}`, investigationId: "inv-1", status: "queued" },
          { status: 202 },
        );
      }
      if (method === "DELETE") {
        return Response.json({
          id: `run-${cancelAt}`,
          investigationId: "inv-1",
          status: "cancelled",
        });
      }
      if (cancelAt === "running" && method === "GET") {
        return Response.json({
          id: "run-running",
          investigationId: "inv-1",
          status: "running",
          phase: "collecting",
          collectedTransfers: 7,
        });
      }
      await new Promise((resolve, reject) => {
        if (init.signal?.aborted) {
          reject(init.signal.reason);
          return;
        }
        init.signal?.addEventListener(
          "abort",
          () => reject(init.signal.reason),
          { once: true },
        );
      });
    };
    const runner = createAnalysisRunner(fetchImpl, 60_000);
    const flow = runner.start("inv-1", {}, (run) => {
      if (run.status === cancelAt && !cancelPromise) {
        cancelPromise = runner.cancel();
      }
    });

    await new Promise((resolve) => setTimeout(resolve, 0));
    assert.ok(cancelPromise, `${cancelAt} progress should expose cancellation`);
    const cancelled = await cancelPromise;
    await assert.rejects(flow, (error) => error.name === "AbortError");
    assert.equal(cancelled.status, "cancelled");
    assert.deepEqual(
      requests.filter((request) => request.method === "DELETE"),
      [
        {
          path: `/api/investigations/inv-1/analysis-runs/run-${cancelAt}`,
          method: "DELETE",
        },
      ],
    );
  }
});

test("run_lost is a recoverable runner outcome and retry can publish a new result", async () => {
  const { createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );
  let attempt = 0;
  const requests = [];
  const fetchImpl = async (input, init = {}) => {
    const path = new URL(String(input), "http://browser.test").pathname;
    const method = init.method ?? "GET";
    requests.push({ path, method });
    if (method === "POST") {
      attempt += 1;
      return Response.json(
        {
          id: attempt === 1 ? "run-lost" : "run-retry",
          investigationId: "inv-1",
          status: "queued",
        },
        { status: 202 },
      );
    }
    if (path.endsWith("/run-lost")) {
      return Response.json(
        { code: "run_lost", error: "registry restarted" },
        { status: 404 },
      );
    }
    if (path.endsWith("/run-retry")) {
      return Response.json({
        id: "run-retry",
        investigationId: "inv-1",
        status: "completed",
        phase: "finished",
        collectedTransfers: 1,
        resultId: "dataset-retry",
      });
    }
    if (path.endsWith("/current-result")) {
      return Response.json(
        currentResultWithDataset("dataset-retry", 31),
      );
    }
    return Response.json({ code: "UNEXPECTED" }, { status: 500 });
  };
  const runner = createAnalysisRunner(fetchImpl, 0);

  const lost = await runner.start("inv-1");
  assert.deepEqual(lost, {
    outcome: "run_lost",
    investigationId: "inv-1",
    runId: "run-lost",
  });
  assert.equal(
    requests.some((request) => request.path.endsWith("/current-result")),
    false,
  );

  const retried = await runner.start("inv-1");
  assert.equal(retried.outcome, "completed");
  assert.equal(retried.run.id, "run-retry");
  assert.equal(retried.result.dataset.id, "dataset-retry");
  assert.equal(attempt, 2);
});

test("stable recovery reloads pending state without a result and completed state with its previous result", async () => {
  const { reloadStableAnalysis } = await import(
    "../src/middle/investigation-client.ts"
  );

  for (const hasPreviousResult of [false, true]) {
    const requests = [];
    const stableInvestigation = investigation(
      hasPreviousResult
        ? {
            status: "已完成",
            currentResult: "dataset-old",
            targetLocked: true,
            risk: 25,
          }
        : {},
    );
    const restored = await reloadStableAnalysis("inv-1", async (input) => {
      const path = new URL(String(input), "http://browser.test").pathname;
      requests.push(path);
      if (path.endsWith("/current-result")) {
        return Response.json(currentResultWithDataset("dataset-old", 25));
      }
      return Response.json(stableInvestigation);
    });

    assert.equal(restored.investigation.status, hasPreviousResult ? "已完成" : "待處理");
    assert.equal(restored.result?.dataset.id ?? null, hasPreviousResult ? "dataset-old" : null);
    assert.deepEqual(
      requests,
      hasPreviousResult
        ? [
            "/api/investigations/inv-1",
            "/api/investigations/inv-1/current-result",
          ]
        : ["/api/investigations/inv-1"],
    );
  }
});

test("re-analysis keeps the previous stable result until the completed result is fetched", async () => {
  const { createAnalysisRunner, mergeCurrentAnalysisResult } = await import(
    "../src/middle/investigation-client.ts"
  );
  let pollCount = 0;
  let stableInvestigation = investigation({
    status: "已完成",
    currentResult: "dataset-old",
    targetLocked: true,
    risk: 25,
  });
  let stableAnalysisResult = currentResultWithDataset("dataset-old", 25);
  const fetchImpl = async (input, init = {}) => {
    const path = new URL(String(input), "http://browser.test").pathname;
    if ((init.method ?? "GET") === "POST") {
      return Response.json(
        { id: "run-new", investigationId: "inv-1", status: "queued" },
        { status: 202 },
      );
    }
    if (path.endsWith("/run-new")) {
      pollCount += 1;
      return Response.json({
        id: "run-new",
        investigationId: "inv-1",
        status: pollCount === 1 ? "running" : "completed",
        phase: pollCount === 1 ? "collecting" : "finished",
        collectedTransfers: pollCount,
        ...(pollCount === 1 ? {} : { resultId: "dataset-new" }),
      });
    }
    return Response.json(currentResultWithDataset("dataset-new", 45));
  };
  const runner = createAnalysisRunner(fetchImpl, 0);

  const completed = await runner.start("inv-1", {}, () => {
    assert.equal(stableInvestigation.currentResult, "dataset-old");
    assert.equal(stableAnalysisResult.dataset.id, "dataset-old");
  });
  assert.equal(stableInvestigation.currentResult, "dataset-old");
  assert.equal(stableAnalysisResult.dataset.id, "dataset-old");

  stableInvestigation = mergeCurrentAnalysisResult(
    stableInvestigation,
    completed.result,
    completed.run.resultId,
  );
  stableAnalysisResult = completed.result;
  assert.equal(stableInvestigation.currentResult, "dataset-new");
  assert.equal(stableAnalysisResult.dataset.id, "dataset-new");
  assert.equal(stableInvestigation.risk, 45);
});

test("a stale poll response cannot publish over a newer run", async () => {
  const { createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );
  let postCount = 0;
  let resolveOldPoll;
  let oldPollSignal;
  const oldPoll = new Promise((resolve) => {
    resolveOldPoll = resolve;
  });
  const progress = [];
  const fetchImpl = async (input, init = {}) => {
    const path = new URL(String(input), "http://browser.test").pathname;
    const method = init.method ?? "GET";
    if (method === "POST") {
      postCount += 1;
      return Response.json(
        {
          id: postCount === 1 ? "run-old" : "run-new",
          investigationId: "inv-1",
          status: "queued",
        },
        { status: 202 },
      );
    }
    if (path.endsWith("/run-old")) {
      oldPollSignal = init.signal;
      return oldPoll;
    }
    if (path.endsWith("/run-new")) {
      return Response.json({
        id: "run-new",
        investigationId: "inv-1",
        status: "completed",
        phase: "finished",
        collectedTransfers: 2,
        resultId: "dataset-new",
      });
    }
    if (path.endsWith("/current-result")) {
      return Response.json(currentResultWithDataset("dataset-new", 40));
    }
    return Response.json({ code: "UNEXPECTED" }, { status: 500 });
  };
  const runner = createAnalysisRunner(fetchImpl, 0);

  const oldFlow = runner.start("inv-1", {}, (run) => {
    progress.push(`${run.id}:${run.status}`);
  });
  await new Promise((resolve) => setTimeout(resolve, 0));
  runner.stop();
  const current = await runner.start("inv-1", {}, (run) => {
    progress.push(`${run.id}:${run.status}`);
  });
  resolveOldPoll(
    Response.json({
      id: "run-old",
      investigationId: "inv-1",
      status: "completed",
      phase: "finished",
      collectedTransfers: 99,
      resultId: "dataset-old-late",
    }),
  );

  await assert.rejects(oldFlow, (error) => error.name === "AbortError");
  assert.equal(oldPollSignal.aborted, true);
  assert.equal(current.outcome, "completed");
  assert.equal(current.result.dataset.id, "dataset-new");
  assert.ok(!progress.includes("run-old:completed"));
});

test("active Investigation delete uses the normal DELETE contract before stopping local polling", async () => {
  const { createAnalysisRunner, InvestigationClientError } = await import(
    "../src/middle/investigation-client.ts"
  );
  let pollSignal;
  let signalWasAbortedDuringDelete;
  const requests = [];
  const fetchImpl = async (input, init = {}) => {
    const path = new URL(String(input), "http://browser.test").pathname;
    const method = init.method ?? "GET";
    requests.push({ path, method });
    if (method === "POST") {
      return Response.json(
        { id: "run-active", investigationId: "inv-1", status: "queued" },
        { status: 202 },
      );
    }
    if (method === "DELETE" && path === "/api/investigations/inv-1") {
      signalWasAbortedDuringDelete = pollSignal.aborted;
      return new Response(null, { status: 204 });
    }
    pollSignal = init.signal;
    await new Promise((resolve, reject) => {
      init.signal.addEventListener(
        "abort",
        () => reject(init.signal.reason),
        { once: true },
      );
    });
  };
  const runner = createAnalysisRunner(fetchImpl, 60_000);
  const flow = runner.start("inv-1");
  await new Promise((resolve) => setTimeout(resolve, 0));

  await runner.deleteInvestigation("inv-1");
  await assert.rejects(flow, (error) => error.name === "AbortError");
  assert.equal(signalWasAbortedDuringDelete, false);
  assert.equal(pollSignal.aborted, true);
  assert.deepEqual(requests.at(-1), {
    path: "/api/investigations/inv-1",
    method: "DELETE",
  });

  const failedRunner = createAnalysisRunner(
    async () => Response.json({ code: "delete_failed" }, { status: 500 }),
    0,
  );
  await assert.rejects(failedRunner.deleteInvestigation("inv-1"), (error) => {
    assert.ok(error instanceof InvestigationClientError);
    assert.equal(error.code, "delete_failed");
    return true;
  });
});

test("browser analysis flow reports a stable failure code and does not fetch a result", async () => {
  const { AnalysisRunClientError, createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );
  const api = createAnalysisApi({
    terminalStatus: "failed",
    errorCode: "PROVIDER_UNAVAILABLE",
  });
  const runner = createAnalysisRunner(api.fetch, 0);

  await assert.rejects(runner.start("inv-1"), (error) => {
    assert.ok(error instanceof AnalysisRunClientError);
    assert.equal(error.code, "PROVIDER_UNAVAILABLE");
    assert.match(error.message, /PROVIDER_UNAVAILABLE/);
    return true;
  });
  assert.equal(
    api.requests.some((request) => request.path.endsWith("/current-result")),
    false,
  );
});

test("browser analysis client maps owner-scoped not-found without exposing backend details", async () => {
  const { InvestigationClientError, startAnalysisRun } = await import(
    "../src/middle/investigation-client.ts"
  );
  const fetchNotFound = async () =>
    Response.json(
      {
        code: "investigation_not_found",
        error: "owner 42 has no database row",
      },
      { status: 404 },
    );

  await assert.rejects(startAnalysisRun("other-owner", {}, fetchNotFound), (error) => {
    assert.ok(error instanceof InvestigationClientError);
    assert.equal(error.code, "investigation_not_found");
    assert.equal(error.message, "找不到這筆調查，可能已被刪除或無權存取。");
    assert.doesNotMatch(error.message, /owner|database/i);
    return true;
  });
});

test("switching Investigation or stopping the browser runner aborts the old poll", async () => {
  const { createAnalysisRunner } = await import(
    "../src/middle/investigation-client.ts"
  );
  const requests = [];
  const fetchUntilAborted = async (input, init = {}) => {
    const path = new URL(String(input), "http://browser.test").pathname;
    requests.push(path);
    if (path.endsWith("/analysis-runs")) {
      const investigationId = path.split("/").at(-2);
      return Response.json(
        { id: `run-${investigationId}`, investigationId, status: "queued" },
        { status: 202 },
      );
    }
    await new Promise((resolve, reject) => {
      init.signal?.addEventListener(
        "abort",
        () => reject(new DOMException("Aborted", "AbortError")),
        { once: true },
      );
    });
  };
  const runner = createAnalysisRunner(fetchUntilAborted, 0);

  const oldFlow = runner.start("inv-old");
  await new Promise((resolve) => setTimeout(resolve, 0));
  const nextFlow = runner.start("inv-next");
  await assert.rejects(oldFlow, (error) => error.name === "AbortError");
  runner.stop();
  await assert.rejects(nextFlow, (error) => error.name === "AbortError");
  assert.ok(requests.some((path) => path.includes("inv-old")));
  assert.ok(requests.some((path) => path.includes("inv-next")));
});

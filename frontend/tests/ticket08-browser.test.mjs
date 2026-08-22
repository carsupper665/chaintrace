import assert from "node:assert/strict";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const exactTotal = {
  smallestUnit: "25000000",
  decimals: 6,
  asset: "USDT",
};

function investigation(overrides = {}) {
  return {
    id: "inv-08",
    title: "Lifecycle investigation",
    address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
    network: "TRON_MAINNET",
    status: "已完成",
    risk: 25,
    relatedNodes: 3,
    totalFlow: exactTotal,
    flowAsset: "USDT",
    transactionCount: 4,
    targetLocked: true,
    currentResult: "dataset-stable",
    createdAt: "2026-08-04T01:00:00Z",
    updatedAt: "2026-08-04T02:00:00Z",
    ...overrides,
  };
}

function stableResult(datasetId = "dataset-stable", score = 25) {
  return {
    dataset: {
      id: datasetId,
      network: "TRON_MAINNET",
      asset: "USDT",
      windowStart: "2026-07-05T02:00:00Z",
      windowEnd: "2026-08-04T02:00:00Z",
      transferLimit: 500,
      traversalDepth: 2,
      collectedTransfers: 4,
      reachedDepth: 2,
      partial: false,
      confidence: 100,
      stopReason: "source_exhausted",
      createdAt: "2026-08-04T02:00:00Z",
    },
    metrics: {
      relatedNodes: 3,
      transferCount: 4,
      totalFlow: exactTotal,
    },
    assessment: {
      score,
      level: "medium",
      reasons: ["rapid_forwarding"],
      nodeAssessments: [],
      source: "rules-v1",
      updatedAt: "2026-08-04T02:00:00Z",
    },
  };
}

function controller(overrides = {}) {
  return {
    active: investigation(),
    addressDraft: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
    analysisError: "",
    analysisOutcome: null,
    analysisRun: null,
    analysisScope: { transferLimit: 500, traversalDepth: 2 },
    chatMessages: [],
    currentAnalysisResult: stableResult(),
    graphError: "",
    isGraphLoading: false,
    isLightMode: false,
    isRunning: false,
    query: "",
    targetMessage: "",
    cancelAnalysis() {},
    confirmInvestigationAddress() {},
    runInvestigation() {},
    setAddressDraft() {},
    setAnalysisScope() {},
    setQuery() {},
    startAnalysis() {},
    toggleTheme() {},
    ...overrides,
  };
}

test("locked Target is read-only, explains creating a new Investigation, and re-analysis remains available", async () => {
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");
  const html = renderToStaticMarkup(createElement(AgentPanel, { ui: controller() }));

  assert.match(
    html,
    /<input(?=[^>]*id="investigation-address")(?=[^>]*readOnly="")[^>]*>/,
  );
  assert.match(html, /第一次成功分析後，調查目標已永久鎖定/);
  assert.match(html, /新地址需建立新的 Investigation/);
  assert.match(html, />重新分析</);

  const { InvestigationClientError, setInvestigationTarget } = await import(
    "../src/middle/investigation-client.ts"
  );
  await assert.rejects(
    setInvestigationTarget(
      "inv-08",
      "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
      async () =>
        Response.json(
          {
            code: "immutable_investigation_target",
            error: "server rejected bypass",
          },
          { status: 409 },
        ),
    ),
    (error) => {
      assert.ok(error instanceof InvestigationClientError);
      assert.equal(error.status, 409);
      assert.equal(error.code, "immutable_investigation_target");
      assert.match(error.message, /目標已鎖定/);
      assert.doesNotMatch(error.message, /server rejected bypass/);
      return true;
    },
  );
});

test("re-analysis keeps the stable result visible and offers cancellation until cutover", async () => {
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");
  const html = renderToStaticMarkup(
    createElement(AgentPanel, {
      ui: controller({
        active: investigation({ status: "分析中" }),
        analysisRun: {
          id: "run-new",
          investigationId: "inv-08",
          status: "running",
          phase: "collecting",
          collectedTransfers: 2,
        },
      }),
    }),
  );

  assert.match(html, /重新分析進行中，既有結果會保留到新結果發布/);
  assert.match(html, />取消分析</);
  assert.match(html, />25</);
  assert.match(html, /25\.000000/);
  assert.match(html, /4/);
});

test("cancel, failure, and run_lost outcomes expose retry actions with an explicit restart message", async () => {
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");

  for (const [analysisOutcome, analysisError, expectedAction] of [
    ["cancelled", "分析已取消，未發布新結果。", "重試分析"],
    ["failed", "分析未完成（PROVIDER_UNAVAILABLE）", "重試分析"],
    [
      "run_lost",
      "後端重新啟動後已遺失這次分析工作；既有結果已恢復。",
      "重新送出分析",
    ],
  ]) {
    const html = renderToStaticMarkup(
      createElement(AgentPanel, {
        ui: controller({ analysisOutcome, analysisError }),
      }),
    );
    assert.match(html, new RegExp(expectedAction));
    assert.match(html, new RegExp(analysisError.replace(/[（）]/g, ".")));
  }
});

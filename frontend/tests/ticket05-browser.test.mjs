import assert from "node:assert/strict";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const exactTotal = {
  smallestUnit: "9007199254740993123456",
  decimals: 6,
  asset: "USDT",
};

function result(overrides = {}) {
  const base = {
    dataset: {
      id: "dataset-05",
      network: "TRON_MAINNET",
      asset: "USDT",
      windowStart: "2026-07-05T02:00:00Z",
      windowEnd: "2026-08-04T02:00:00Z",
      transferLimit: 500,
      traversalDepth: 2,
      collectedTransfers: 42,
      reachedDepth: 2,
      partial: false,
      confidence: 100,
      stopReason: "source_exhausted",
      createdAt: "2026-08-04T02:00:00Z",
    },
    metrics: {
      relatedNodes: 17,
      transferCount: 42,
      totalFlow: exactTotal,
    },
    assessment: {
      score: 45,
      level: "medium",
      reasons: ["fan_out", "fan_in"],
      nodeAssessments: [
        {
          address: "TPeer11111111111111111111111111111",
          score: 25,
          level: "medium",
          reasons: ["rapid_forwarding"],
        },
      ],
      source: "rules-v1",
      updatedAt: "2026-08-04T02:00:00Z",
    },
  };
  return {
    ...base,
    ...overrides,
    dataset: { ...base.dataset, ...overrides.dataset },
    metrics: { ...base.metrics, ...overrides.metrics },
    assessment: { ...base.assessment, ...overrides.assessment },
  };
}

test("browser renders bounded scope controls with defaults, maxima, and a fixed 30-day window", async () => {
  const { AnalysisScopeControls } = await import(
    "../src/ui/AnalysisScopeControls.tsx"
  );
  const html = renderToStaticMarkup(
    createElement(AnalysisScopeControls, {
      scope: { transferLimit: 500, traversalDepth: 2 },
      disabled: false,
      onChange() {},
    }),
  );

  assert.match(
    html,
    /<input(?=[^>]*name="transferLimit")(?=[^>]*min="1")(?=[^>]*max="5000")[^>]*>/,
  );
  assert.match(
    html,
    /<input(?=[^>]*name="traversalDepth")(?=[^>]*min="1")(?=[^>]*max="4")[^>]*>/,
  );
  assert.match(html, /value="500"/);
  assert.match(html, /value="2"/);
  assert.match(html, /固定 30 天/);
  assert.doesNotMatch(html, /type="date"/);
});

test("browser renders normal authoritative metrics, rules, coverage, and node assessments exactly", async () => {
  const { AnalysisResultDetails } = await import(
    "../src/ui/AnalysisResultDetails.tsx"
  );
  const html = renderToStaticMarkup(
    createElement(AnalysisResultDetails, { result: result() }),
  );

  assert.match(html, /9,007,199,254,740,993\.123456 USDT/);
  assert.match(html, /42 筆 Transfer/);
  assert.match(html, /17 個關聯地址/);
  assert.match(html, /45\/100/);
  assert.match(html, /medium/);
  assert.match(html, /fan_out/);
  assert.match(html, /fan_in/);
  assert.match(html, /rules-v1/);
  assert.match(html, /上限 500/);
  assert.match(html, /深度 2/);
  assert.match(html, /已收集 42 \/ 500/);
  assert.match(html, /已到達 2 \/ 2/);
  assert.match(html, /2026-07-05/);
  assert.match(html, /2026-08-04/);
  assert.match(html, /固定 30 天/);
  assert.match(html, /100%/);
  assert.match(html, /source_exhausted/);
  assert.match(html, /TPeer11111111111111111111111111111/);
  assert.match(html, /rapid_forwarding/);
});

test("browser gives partial results a prominent warning and keeps the stable result visible after failure", async () => {
  const { AnalysisResultDetails } = await import(
    "../src/ui/AnalysisResultDetails.tsx"
  );
  const partial = result({
    dataset: {
      collectedTransfers: 40,
      reachedDepth: 1,
      partial: true,
      confidence: 8,
      stopReason: "provider_rate_limited",
    },
  });
  const html = renderToStaticMarkup(
    createElement(AnalysisResultDetails, {
      result: partial,
      attemptError: "分析未完成（PROVIDER_UNAVAILABLE）",
    }),
  );

  assert.match(html, /role="alert"/);
  assert.match(html, /部分分析/);
  assert.match(html, /provider_rate_limited/);
  assert.match(html, /8%/);
  assert.match(html, /既有穩定結果仍保留/);
  assert.match(html, /PROVIDER_UNAVAILABLE/);
  assert.match(html, /9,007,199,254,740,993\.123456 USDT/);
  assert.match(html, /45\/100/);
});

test("browser renders zero evidence as insufficient evidence, never zero or low risk", async () => {
  const { AnalysisResultDetails } = await import(
    "../src/ui/AnalysisResultDetails.tsx"
  );
  const zero = result({
    dataset: {
      collectedTransfers: 0,
      reachedDepth: 0,
      confidence: 0,
      stopReason: "no_eligible_transfers",
    },
    metrics: {
      relatedNodes: 0,
      transferCount: 0,
      totalFlow: { smallestUnit: "0", decimals: 6, asset: "USDT" },
    },
    assessment: {
      score: null,
      level: null,
      reasons: [],
      nodeAssessments: [],
    },
  });
  const html = renderToStaticMarkup(
    createElement(AnalysisResultDetails, { result: zero }),
  );

  assert.match(html, /insufficient evidence/);
  assert.doesNotMatch(html, /0\/100/);
  assert.doesNotMatch(html, />low</);
  assert.doesNotMatch(html, /低風險/);
  assert.match(html, /no_eligible_transfers/);
});

import assert from "node:assert/strict";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

function panelUi(overrides = {}) {
  const { active: activeOverrides, ...rest } = overrides;
  return {
    active: {
      id: "inv-summary",
      title: "Summary panel",
      address: "TSummaryTarget0000000000000000001",
      network: "TRON_MAINNET",
      status: "待處理",
      risk: null,
      relatedNodes: null,
      totalFlow: null,
      transactionCount: null,
      targetLocked: false,
      currentResult: null,
      ...activeOverrides,
    },
    addressDraft: "",
    analysisError: "",
    analysisOutcome: null,
    analysisRun: null,
    analysisScope: { transferLimit: 500, traversalDepth: 2 },
    chatMessages: [],
    conversationError: "",
    currentAnalysisResult: null,
    graphError: "",
    hasMoreConversation: false,
    isConversationLoading: false,
    isGraphLoading: false,
    isLightMode: false,
    isRunning: false,
    isSummarizing: false,
    query: "",
    summaryNotice: "",
    targetMessage: "",
    cancelAnalysis() {},
    confirmInvestigationAddress() {},
    generateSummary() {},
    loadMoreConversation() {},
    runInvestigation() {},
    setAddressDraft() {},
    setAnalysisScope() {},
    setQuery() {},
    startAnalysis() {},
    toggleTheme() {},
    ...rest,
  };
}

async function render(overrides) {
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");
  return renderToStaticMarkup(
    createElement(AgentPanel, { ui: panelUi(overrides) }),
  );
}

// The summary describes an Analysis Dataset, so without one there is nothing
// to summarise and the control must say so rather than fail on press.
test("the summary button is disabled until an analysis has completed", async () => {
  const html = await render({ active: { currentResult: null } });

  assert.match(html, /產生調查摘要/);
  assert.match(html, /完成一次分析後才能產生摘要。/);
  const button = html.slice(html.indexOf("generate-summary-button"));
  assert.match(button.slice(0, 200), /disabled/);
});

test("a completed analysis enables the summary button", async () => {
  const html = await render({ active: { currentResult: "ds-1" } });

  const button = html.slice(html.indexOf("generate-summary-button"));
  assert.doesNotMatch(button.slice(0, 200), /disabled/);
  assert.match(html, /摘要依目前分析結果產生，同一份結果只會產生一次。/);
});

test("the button reports progress and stays disabled while summarising", async () => {
  const html = await render({
    active: { currentResult: "ds-1" },
    isSummarizing: true,
  });

  assert.match(html, /產生摘要中…/);
  const button = html.slice(html.indexOf("generate-summary-button"));
  assert.match(button.slice(0, 200), /disabled/);
});

test("a running command blocks the summary button", async () => {
  const html = await render({
    active: { currentResult: "ds-1" },
    isRunning: true,
  });

  const button = html.slice(html.indexOf("generate-summary-button"));
  assert.match(button.slice(0, 200), /disabled/);
});

test("reusing a stored summary is disclosed rather than silently repeated", async () => {
  const html = await render({
    active: { currentResult: "ds-1" },
    summaryNotice: "這份分析結果的摘要先前已產生，直接沿用。",
  });

  assert.match(html, /這份分析結果的摘要先前已產生，直接沿用。/);
});

test("the generated summary renders as an agent message", async () => {
  const html = await render({
    active: { currentResult: "ds-1" },
    chatMessages: [
      {
        id: "agent-summary",
        role: "agent",
        content: "### 範圍聲明\n本次分析未涵蓋完整範圍。",
        createdAt: "2026-08-30T03:00:00Z",
      },
    ],
  });

  assert.match(html, /本次分析未涵蓋完整範圍。/);
  assert.match(html, /chat-message agent/);
});

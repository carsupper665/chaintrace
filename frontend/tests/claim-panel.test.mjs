import assert from "node:assert/strict";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const validAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE";

function claimUi(overrides = {}) {
  const { active: activeOverrides, ...rest } = overrides;
  return {
    active: {
      id: "inv-1",
      title: "Claim case",
      address: null,
      network: "TRON_MAINNET",
      targetLocked: false,
      status: "待處理",
      risk: null,
      relatedNodes: 0,
      transactionCount: 0,
      totalFlow: null,
      currentResult: null,
      ...activeOverrides,
    },
    addressDraft: "",
    pendingClaim: {
      kind: "address",
      value: validAddress,
      role: "to",
      confidence: 0.91,
      status: "unverified",
    },
    claimMessage: "已抽出待驗證地址 Claim；請確認後再驗證鏈上活動。",
    claimError: "",
    isClaimExtracting: false,
    isClaimVerifying: false,
    isGraphLoading: false,
    graphError: "",
    targetMessage: "",
    analysisRun: null,
    analysisError: "",
    analysisOutcome: null,
    analysisScope: { transferLimit: 500, traversalDepth: 2 },
    isAnalysisLoading: false,
    isAnalysisCancelling: false,
    currentAnalysisResult: null,
    chatMessages: [],
    conversationError: "",
    streamingReply: null,
    hasMoreConversation: false,
    isConversationLoading: false,
    isLightMode: true,
    isRunning: false,
    isSummarizing: false,
    agentModels: [],
    selectedModel: "",
    query: "",
    summaryNotice: "",
    setAddressDraft() {},
    setAnalysisScope() {},
    setSelectedModel() {},
    setQuery() {},
    confirmInvestigationAddress() {},
    startAnalysis() {},
    cancelAnalysis() {},
    extractClaimFromScreenshot() {},
    verifyPendingClaim() {},
    clearPendingClaim() {},
    generateSummary() {},
    loadMoreConversation() {},
    runInvestigation() {},
    toggleTheme() {},
    ...rest,
  };
}

test("AgentPanel shows pending Claim review controls for unlocked targets", async () => {
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");
  const html = renderToStaticMarkup(
    createElement(AgentPanel, { ui: claimUi() }),
  );

  assert.match(html, /截圖 Claim/);
  assert.match(html, /accept="image\/png"/);
  assert.match(html, /待驗證地址/);
  assert.match(html, new RegExp(validAddress));
  assert.match(html, /待驗證/);
  assert.match(html, /驗證鏈上活動/);
});

test("AgentPanel hides Claim upload when the Investigation target is locked", async () => {
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");
  const html = renderToStaticMarkup(
    createElement(AgentPanel, {
      ui: claimUi({
        active: {
          targetLocked: true,
          address: validAddress,
        },
        pendingClaim: null,
        claimMessage: "",
      }),
    }),
  );

  assert.doesNotMatch(html, /截圖 Claim/);
  assert.doesNotMatch(html, /驗證鏈上活動/);
});

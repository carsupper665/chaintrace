import assert from "node:assert/strict";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const validAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE";
const peerAddress = "TMwFHYXLJaRUPeW6421aqXL4ZEzPRFGkGT";

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
    pendingClaims: [
      {
        id: "c1",
        kind: "address",
        value: validAddress,
        role: "to",
        confidence: 0.91,
        status: "unverified",
      },
      {
        id: "c2",
        kind: "address",
        value: peerAddress,
        role: "from",
        confidence: 0.55,
        status: "unverified",
      },
      {
        id: "c3",
        kind: "amount",
        value: "1500000",
        asset: "USDT",
        confidence: 0.8,
        status: "unverified",
      },
    ],
    selectedClaimId: "c1",
    claimPreviewUrl: "blob:preview",
    claimMessage: "已抽出 3 筆待驗證 Claim；請確認後再驗證鏈上結果。",
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
    updatePendingClaim() {},
    removePendingClaim() {},
    selectPendingClaim() {},
    clearPendingClaim() {},
    generateSummary() {},
    loadMoreConversation() {},
    runInvestigation() {},
    toggleTheme() {},
    ...rest,
  };
}

test("AgentPanel shows multi-claim review controls for unlocked targets", async () => {
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");
  const html = renderToStaticMarkup(
    createElement(AgentPanel, { ui: claimUi() }),
  );

  assert.match(html, /截圖 Claim/);
  assert.match(html, /accept="image\/png,image\/jpeg,image\/webp"/);
  assert.match(html, /待驗證 Claim/);
  assert.match(html, new RegExp(validAddress));
  assert.match(html, new RegExp(peerAddress));
  assert.match(html, /低信心/);
  assert.match(html, /選為目標候選|已選為目標/);
  assert.match(html, /驗證鏈上結果/);
  assert.match(html, /claim-preview/);
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
        pendingClaims: [],
        selectedClaimId: null,
        claimPreviewUrl: null,
        claimMessage: "",
      }),
    }),
  );

  assert.doesNotMatch(html, /截圖 Claim/);
  assert.doesNotMatch(html, /驗證鏈上結果/);
});

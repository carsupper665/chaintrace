"use client";

import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { ConversationClientError } from "@/src/middle/conversation-client";
import {
  AnalysisRunClientError,
  createAnalysisRunner,
  createInvestigation as createInvestigationRecord,
  filterInvestigations,
  getCurrentAnalysisResult,
  getInvestigation,
  InvestigationClientError,
  listInvestigations,
  mergeCurrentAnalysisResult,
  reloadStableAnalysis,
  renameInvestigation as renameInvestigationRecord,
  setInvestigationTarget,
} from "@/src/middle/investigation-client";
import type {
  AnalysisScope,
  AnalysisRunProgress,
  CurrentAnalysisResult,
} from "@/src/middle/investigation-contract";
import {
  DEFAULT_TRANSFER_LIMIT,
  DEFAULT_TRAVERSAL_DEPTH,
} from "@/src/middle/investigation-contract";
import type { TransactionGraphResponse } from "@/src/middle/transaction-graph-contract";
import { TransactionGraphClientError } from "@/src/middle/transaction-graph-client";
import {
  AnalysisExportError,
  createAnalysisExporter,
} from "@/src/services/analysisExportBrowser";
import { createConversationBrowser } from "@/src/services/conversationBrowser";
import { createTransactionGraphBrowser } from "@/src/services/transactionGraphBrowser";
import { loadVisualPreferences } from "@/src/services/visualPreferences";
import type {
  ChatMessage,
  ContextMenuState,
  Investigation,
} from "@/src/models/chaintraceTypes";

type InvestigationSessionState = {
  addressDraft: string;
  graphError: string;
  analysisError: string;
  query: string;
};

export function useChainTrace() {
  const [investigations, setInvestigations] = useState<Investigation[]>([]);
  const [activeId, setActiveId] = useState("");
  const [query, setQuery] = useState("");
  const [addressDraft, setAddressDraft] = useState("");
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([]);
  const [conversationError, setConversationError] = useState("");
  const [hasMoreConversation, setHasMoreConversation] = useState(false);
  const [isConversationLoading, setIsConversationLoading] = useState(false);
  const [conversationReloadToken, setConversationReloadToken] = useState(0);
  const [search, setSearch] = useState("");
  const [isWorkspaceLoading, setIsWorkspaceLoading] = useState(true);
  const [isWorkspaceMutating, setIsWorkspaceMutating] = useState(false);
  const [workspaceError, setWorkspaceError] = useState("");
  const [targetMessage, setTargetMessage] = useState("");
  const [isRunning, setIsRunning] = useState(false);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);
  const [isAnalysisCollapsed, setIsAnalysisCollapsed] = useState(false);
  const [isSidebarHovered, setIsSidebarHovered] = useState(false);
  const [isAnalysisHovered, setIsAnalysisHovered] = useState(false);
  const [isSidebarClosing, setIsSidebarClosing] = useState(false);
  const [isAnalysisClosing, setIsAnalysisClosing] = useState(false);
  const [isLightMode, setIsLightMode] = useState(true);
  const [isVisualPreferencesHydrated, setIsVisualPreferencesHydrated] =
    useState(false);
  const [graphZoom, setGraphZoom] = useState(1);
  const [graphSpread, setGraphSpread] = useState(1);
  const [graphOffset, setGraphOffset] = useState({ x: 0, y: 0 });
  const [isGraphDragging, setIsGraphDragging] = useState(false);
  const [isGraphFullscreen, setIsGraphFullscreen] = useState(false);
  const [sidebarWidth, setSidebarWidth] = useState(280);
  const [analysisWidth, setAnalysisWidth] = useState(420);
  const [resizingPanel, setResizingPanel] = useState<
    "sidebar" | "analysis" | null
  >(null);
  const [isExportingPdf, setIsExportingPdf] = useState(false);
  const [isExportingCsv, setIsExportingCsv] = useState(false);
  const [exportError, setExportError] = useState("");
  const [isGraphLoading, setIsGraphLoading] = useState(false);
  const [graphError, setGraphError] = useState("");
  const [isGraphPageLoading, setIsGraphPageLoading] = useState(false);
  const [graphPageError, setGraphPageError] = useState("");
  const [graphReloadToken, setGraphReloadToken] = useState(0);
  const [isAnalysisLoading, setIsAnalysisLoading] = useState(false);
  const [analysisError, setAnalysisError] = useState("");
  const [analysisRun, setAnalysisRun] = useState<AnalysisRunProgress | null>(
    null,
  );
  const [analysisOutcome, setAnalysisOutcome] = useState<
    "cancelled" | "failed" | "run_lost" | null
  >(null);
  const [isAnalysisCancelling, setIsAnalysisCancelling] = useState(false);
  const [analysisScope, setAnalysisScope] = useState<AnalysisScope>({
    transferLimit: DEFAULT_TRANSFER_LIMIT,
    traversalDepth: DEFAULT_TRAVERSAL_DEPTH,
  });
  const [currentAnalysisResult, setCurrentAnalysisResult] =
    useState<CurrentAnalysisResult | null>(null);
  const [analysisRunner] = useState(() => createAnalysisRunner());
  const [analysisExporter] = useState(() => createAnalysisExporter());
  const [conversationBrowser] = useState(() => createConversationBrowser());
  const [graphBrowser] = useState(() => createTransactionGraphBrowser());
  const [transactionGraph, setTransactionGraph] =
    useState<TransactionGraphResponse | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [expandedNodeIds, setExpandedNodeIds] = useState<Set<string>>(
    () => new Set(),
  );
  const [expandingNodeId, setExpandingNodeId] = useState<string | null>(null);
  const [expansionError, setExpansionError] = useState("");
  const [graphHistoryDepth, setGraphHistoryDepth] = useState(0);
  const [contextMenu, setContextMenu] = useState<ContextMenuState>(null);
  const [renameTarget, setRenameTarget] = useState<Investigation | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<Investigation | null>(null);

  const graphCanvasRef = useRef<HTMLDivElement | null>(null);
  const graphCardRef = useRef<HTMLElement | null>(null);
  const analysisPanelRef = useRef<HTMLElement | null>(null);
  const graphDragStart = useRef({ x: 0, y: 0, offsetX: 0, offsetY: 0 });
  const panelResizeStart = useRef({
    clientX: 0,
    sidebarWidth: 280,
    analysisWidth: 420,
  });
  const panelResizeCurrent = useRef({
    sidebarWidth: 280,
    analysisWidth: 420,
  });
  const sidebarCloseTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const analysisCloseTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const graphHistory = useRef<
    Array<{
      graph: TransactionGraphResponse;
      spread: number;
      offset: { x: number; y: number };
      expandedNodeIds: Set<string>;
    }>
  >([]);
  const investigationSessions = useRef(
    new Map<string, InvestigationSessionState>(),
  );
  const loadedAnalysisResult = useRef("");
  const currentGraphDataset = useRef("");
  const analysisAttempt = useRef(0);
  const conversationAttempt = useRef(0);
  const selectAttempt = useRef(0);
  const exportInProgress = useRef(false);

  const active =
    investigations.find((item) => item.id === activeId) ||
    investigations[0] ||
    null;
  const filtered = useMemo(
    () => filterInvestigations(investigations, search),
    [investigations, search],
  );

  useEffect(() => {
    const closeMenu = () => setContextMenu(null);
    window.addEventListener("click", closeMenu);
    window.addEventListener("blur", closeMenu);
    return () => {
      window.removeEventListener("click", closeMenu);
      window.removeEventListener("blur", closeMenu);
    };
  }, []);

  useEffect(() => {
    const preferences = loadVisualPreferences(window.localStorage);
    let cancelled = false;
    window.queueMicrotask(() => {
      if (cancelled) return;
      setIsLightMode(preferences.isLightMode);
      if (preferences.isSidebarCollapsed !== null) {
        setIsSidebarCollapsed(preferences.isSidebarCollapsed);
      }
      if (preferences.isAnalysisCollapsed !== null) {
        setIsAnalysisCollapsed(preferences.isAnalysisCollapsed);
      }
      setSidebarWidth(preferences.sidebarWidth);
      setAnalysisWidth(preferences.analysisWidth);
      setGraphZoom(preferences.graphZoom);
      setGraphSpread(preferences.graphSpread);
      setGraphOffset(preferences.graphOffset);
      setIsVisualPreferencesHydrated(true);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!isVisualPreferencesHydrated) return;
    window.localStorage.setItem("chaintrace-graph-zoom", String(graphZoom));
    window.localStorage.setItem("chaintrace-graph-spread", String(graphSpread));
    window.localStorage.setItem(
      "chaintrace-graph-offset-x",
      String(graphOffset.x),
    );
    window.localStorage.setItem(
      "chaintrace-graph-offset-y",
      String(graphOffset.y),
    );
  }, [graphOffset, graphSpread, graphZoom, isVisualPreferencesHydrated]);

  useEffect(() => {
    let cancelled = false;
    async function loadWorkspace() {
      setIsWorkspaceLoading(true);
      setWorkspaceError("");
      try {
        const records = await listInvestigations();
        if (cancelled) return;
        const selected = records[0];
        setInvestigations(records);
        setActiveId(selected?.id || "");
        setAddressDraft(selected?.address || "");
      } catch (error) {
        if (cancelled) return;
        setWorkspaceError(
          error instanceof InvestigationClientError
            ? error.message
            : "無法載入調查工作區，請稍後再試。",
        );
      } finally {
        if (!cancelled) setIsWorkspaceLoading(false);
      }
    }
    void loadWorkspace();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => () => analysisRunner.stop(), [analysisRunner]);

  useEffect(() => {
    const investigationId = active?.id;
    let cancelled = false;

    if (!investigationId) {
      conversationBrowser.clear();
      window.queueMicrotask(() => {
        if (cancelled) return;
        setChatMessages([]);
        setConversationError("");
        setHasMoreConversation(false);
        setIsConversationLoading(false);
      });
      return () => {
        cancelled = true;
      };
    }

    window.queueMicrotask(() => {
      if (cancelled) return;
      setChatMessages([]);
      setConversationError("");
      setHasMoreConversation(false);
      setIsConversationLoading(true);
    });
    void conversationBrowser
      .load(investigationId)
      .then((conversation) => {
        if (cancelled) return;
        setChatMessages(conversation.messages);
        setHasMoreConversation(Boolean(conversation.nextCursor));
      })
      .catch((error) => {
        if (
          cancelled ||
          (error instanceof DOMException && error.name === "AbortError")
        ) {
          return;
        }
        setConversationError(
          error instanceof ConversationClientError
            ? error.message
            : "無法載入已保存的對話，請稍後再試。",
        );
      })
      .finally(() => {
        if (!cancelled) setIsConversationLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [active?.id, conversationBrowser, conversationReloadToken]);

  useEffect(() => () => conversationBrowser.clear(), [conversationBrowser]);

  useEffect(() => {
    const investigationId = active?.id;
    const resultId = active?.currentResult;
    const key = investigationId && resultId ? `${investigationId}:${resultId}` : "";
    let cancelled = false;
    const controller = new AbortController();

    if (!key) {
      window.queueMicrotask(() => {
        if (cancelled) return;
        loadedAnalysisResult.current = "";
        setCurrentAnalysisResult(null);
      });
      return () => {
        cancelled = true;
        controller.abort();
      };
    }
    if (loadedAnalysisResult.current === key) {
      return () => controller.abort();
    }

    window.queueMicrotask(() => {
      if (!cancelled) setIsAnalysisLoading(true);
    });
    void getCurrentAnalysisResult(investigationId, fetch, controller.signal)
      .then((result) => {
        if (cancelled) return;
        loadedAnalysisResult.current = key;
        setCurrentAnalysisResult(result);
        setAnalysisError("");
      })
      .catch((error) => {
        if (
          cancelled ||
          (error instanceof DOMException && error.name === "AbortError")
        ) {
          return;
        }
        setAnalysisError(
          error instanceof InvestigationClientError
            ? error.message
            : "無法載入目前分析結果，請稍後再試。",
        );
      })
      .finally(() => {
        if (!cancelled) setIsAnalysisLoading(false);
      });
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [active?.currentResult, active?.id]);

  useEffect(() => {
    const investigationId = active?.id;
    const address = active?.address;
    const network = active?.network;
    const result = currentAnalysisResult;
    let cancelled = false;

    currentGraphDataset.current = result?.dataset.id ?? "";
    graphBrowser.clear();
    graphHistory.current = [];
    window.queueMicrotask(() => {
      if (cancelled) return;
      setTransactionGraph(null);
      setSelectedNodeId(null);
      setExpandedNodeIds(new Set());
      setGraphHistoryDepth(0);
      setGraphSpread(1);
      setGraphZoom(1);
      setGraphOffset({ x: 0, y: 0 });
      setExpansionError("");
      setGraphPageError("");
      setIsGraphPageLoading(Boolean(investigationId && address && result));
    });

    if (!investigationId || !address || !network || !result) {
      return () => {
        cancelled = true;
        graphBrowser.clear();
      };
    }

    void graphBrowser
      .load({ investigationId, address, network, result })
      .then((graph) => {
        if (cancelled || graph?.datasetId !== result.dataset.id) return;
        setTransactionGraph(graph);
      })
      .catch((error) => {
        if (cancelled) return;
        setGraphPageError(
          error instanceof TransactionGraphClientError
            ? error.message
            : "無法載入交易圖譜，請稍後再試。",
        );
      })
      .finally(() => {
        if (!cancelled) setIsGraphPageLoading(false);
      });

    return () => {
      cancelled = true;
      graphBrowser.clear();
    };
  }, [
    active?.address,
    active?.id,
    active?.network,
    currentAnalysisResult,
    graphBrowser,
    graphReloadToken,
  ]);

  useEffect(() => {
    const closeFullscreen = (event: KeyboardEvent) => {
      if (event.key === "Escape") setIsGraphFullscreen(false);
    };
    window.addEventListener("keydown", closeFullscreen);
    return () => window.removeEventListener("keydown", closeFullscreen);
  }, []);

  useEffect(() => {
    const canvas = graphCanvasRef.current;
    if (!canvas) return;
    const handleGraphWheel = (event: WheelEvent) => {
      event.preventDefault();
      event.stopPropagation();
      const direction = event.deltaY < 0 ? 0.1 : -0.1;
      setGraphZoom((current) =>
        Math.min(1.8, Math.max(0.6, current + direction)),
      );
    };
    canvas.addEventListener("wheel", handleGraphWheel, { passive: false });
    return () => canvas.removeEventListener("wheel", handleGraphWheel);
  }, [active?.id, isGraphFullscreen]);

  useEffect(
    () => () => {
      if (sidebarCloseTimer.current) clearTimeout(sidebarCloseTimer.current);
      if (analysisCloseTimer.current) clearTimeout(analysisCloseTimer.current);
    },
    [],
  );

  function enterSidebar() {
    if (sidebarCloseTimer.current) clearTimeout(sidebarCloseTimer.current);
    setIsSidebarClosing(false);
    setIsSidebarHovered(true);
  }

  function leaveSidebar() {
    if (!isSidebarCollapsed) return;
    setIsSidebarClosing(true);
    sidebarCloseTimer.current = setTimeout(() => {
      setIsSidebarHovered(false);
      setIsSidebarClosing(false);
    }, 360);
  }

  function enterAnalysis() {
    if (analysisCloseTimer.current) clearTimeout(analysisCloseTimer.current);
    setIsAnalysisClosing(false);
    setIsAnalysisHovered(true);
  }

  function leaveAnalysis() {
    if (!isAnalysisCollapsed) return;
    setIsAnalysisClosing(true);
    analysisCloseTimer.current = setTimeout(() => {
      setIsAnalysisHovered(false);
      setIsAnalysisClosing(false);
    }, 360);
  }

  function toggleSidebarPin() {
    if (sidebarCloseTimer.current) clearTimeout(sidebarCloseTimer.current);
    setIsSidebarClosing(false);
    setIsSidebarCollapsed((current) => {
      const next = !current;
      window.localStorage.setItem(
        "chaintrace-sidebar-collapsed",
        String(next),
      );
      return next;
    });
  }

  function toggleAnalysisPin() {
    if (analysisCloseTimer.current) clearTimeout(analysisCloseTimer.current);
    setIsAnalysisClosing(false);
    setIsAnalysisCollapsed((current) => {
      const next = !current;
      window.localStorage.setItem(
        "chaintrace-analysis-collapsed",
        String(next),
      );
      return next;
    });
  }

  function toggleTheme() {
    setIsLightMode((current) => {
      const next = !current;
      window.localStorage.setItem(
        "chaintrace-theme",
        next ? "light" : "dark",
      );
      return next;
    });
  }

  function resetGraphView() {
    setGraphZoom(1);
    setGraphOffset({ x: 0, y: 0 });
  }

  function moveGraphNode(nodeId: string, x: number, y: number) {
    setTransactionGraph((current) => {
      if (!current) return current;
      const next = {
        ...current,
        nodes: current.nodes.map((node) =>
          node.id === nodeId
            ? {
                ...node,
                x: Math.min(96, Math.max(4, x)),
                y: Math.min(94, Math.max(6, y)),
              }
            : node,
        ),
      };
      graphBrowser.replaceGraph(next);
      return next;
    });
  }

  async function toggleGraphFullscreen() {
    setIsGraphFullscreen((current) => {
      const next = !current;
      if (next) {
        if (analysisCloseTimer.current) clearTimeout(analysisCloseTimer.current);
        setIsAnalysisClosing(false);
        setIsAnalysisHovered(true);
      }
      return next;
    });
  }

  function beginPanelResize(
    panel: "sidebar" | "analysis",
    clientX: number,
  ) {
    panelResizeStart.current = { clientX, sidebarWidth, analysisWidth };
    panelResizeCurrent.current = { sidebarWidth, analysisWidth };
    setResizingPanel(panel);
  }

  function updatePanelResize(clientX: number) {
    const start = panelResizeStart.current;
    const centerMinimum = 500;
    if (resizingPanel === "sidebar") {
      const maximum = Math.max(
        220,
        window.innerWidth - analysisWidth - centerMinimum,
      );
      const rawWidth = start.sidebarWidth + clientX - start.clientX;
      if (rawWidth < 170) {
        setSidebarWidth(start.sidebarWidth);
        setIsSidebarCollapsed(true);
        window.localStorage.setItem("chaintrace-sidebar-collapsed", "true");
        setIsSidebarHovered(false);
        setResizingPanel(null);
        return;
      }
      const nextWidth = Math.min(
        maximum,
        Math.max(220, rawWidth),
      );
      panelResizeCurrent.current.sidebarWidth = nextWidth;
      setSidebarWidth(nextWidth);
    }
    if (resizingPanel === "analysis") {
      const maximum = Math.max(
        320,
        window.innerWidth - sidebarWidth - centerMinimum,
      );
      const rawWidth = start.analysisWidth - clientX + start.clientX;
      if (rawWidth < 240) {
        setAnalysisWidth(start.analysisWidth);
        setIsAnalysisCollapsed(true);
        window.localStorage.setItem("chaintrace-analysis-collapsed", "true");
        setIsAnalysisHovered(false);
        setResizingPanel(null);
        return;
      }
      const nextWidth = Math.min(
        maximum,
        Math.max(320, rawWidth),
      );
      panelResizeCurrent.current.analysisWidth = nextWidth;
      setAnalysisWidth(nextWidth);
    }
  }

  function endPanelResize() {
    if (resizingPanel === "sidebar") {
      const width = panelResizeCurrent.current.sidebarWidth;
      const settledWidth = Math.max(220, width);
      setSidebarWidth(settledWidth);
      window.localStorage.setItem(
        "chaintrace-sidebar-width",
        String(settledWidth),
      );
    }
    if (resizingPanel === "analysis") {
      const width = panelResizeCurrent.current.analysisWidth;
      const settledWidth = Math.max(320, width);
      setAnalysisWidth(settledWidth);
      window.localStorage.setItem(
        "chaintrace-analysis-width",
        String(settledWidth),
      );
    }
    setResizingPanel(null);
  }

  function resetPanelWidth(panel: "sidebar" | "analysis") {
    if (panel === "sidebar") {
      setSidebarWidth(280);
      window.localStorage.setItem("chaintrace-sidebar-width", "280");
    } else {
      setAnalysisWidth(420);
      window.localStorage.setItem("chaintrace-analysis-width", "420");
    }
  }

  function adjustPanelWidth(
    panel: "sidebar" | "analysis",
    delta: number,
  ) {
    const centerMinimum = 500;
    if (panel === "sidebar") {
      const maximum = Math.max(
        220,
        window.innerWidth - analysisWidth - centerMinimum,
      );
      const nextWidth = Math.min(
        maximum,
        Math.max(220, sidebarWidth + delta),
      );
      setSidebarWidth(nextWidth);
      window.localStorage.setItem("chaintrace-sidebar-width", String(nextWidth));
      return;
    }

    const maximum = Math.max(
      320,
      window.innerWidth - sidebarWidth - centerMinimum,
    );
    const nextWidth = Math.min(
      maximum,
      Math.max(320, analysisWidth + delta),
    );
    setAnalysisWidth(nextWidth);
    window.localStorage.setItem("chaintrace-analysis-width", String(nextWidth));
  }

  async function confirmInvestigationAddress() {
    const address = addressDraft.trim();
    if (!active || isGraphLoading) return;
    setGraphError("");
    setTargetMessage("");
    setAnalysisError("");
    setIsGraphLoading(true);
    try {
      const updated = await setInvestigationTarget(active.id, address);
      setInvestigations((current) =>
        current.map((item) =>
          item.id === updated.id ? updated : item,
        ),
      );
      setAddressDraft(updated.address || "");
      graphBrowser.clear();
      setTransactionGraph(null);
      setCurrentAnalysisResult(null);
      setSelectedNodeId(null);
      setExpandedNodeIds(new Set());
      setTargetMessage("TRON 調查目標已儲存，可以開始分析。");
    } catch (error) {
      setGraphError(
        error instanceof InvestigationClientError
          ? error.message
          : "無法儲存 TRON 調查目標，請稍後再試。",
      );
    } finally {
      setIsGraphLoading(false);
    }
  }

  async function expandGraphNode(nodeId: string) {
    if (
      !transactionGraph ||
      expandingNodeId ||
      expandedNodeIds.has(nodeId)
    ) {
      return;
    }
    setSelectedNodeId(nodeId);
    setExpandingNodeId(nodeId);
    setExpansionError("");
    const previous = transactionGraph;
    try {
      const expanded = await graphBrowser.expand(nodeId);
      if (!expanded || expanded.datasetId !== previous.datasetId) return;
      graphHistory.current.push({
        graph: previous,
        spread: graphSpread,
        offset: graphOffset,
        expandedNodeIds: new Set(expandedNodeIds),
      });
      setTransactionGraph(expanded);
      setExpandedNodeIds((current) => new Set(current).add(nodeId));
      setGraphHistoryDepth(graphHistory.current.length);
      setGraphSpread((current) => Math.min(2.4, current + 0.18));
    } catch (error) {
      setExpansionError(
        error instanceof TransactionGraphClientError
          ? error.message
          : "無法展開此節點的交易關係，請稍後再試。",
      );
    } finally {
      setExpandingNodeId(null);
    }
  }

  async function loadMoreGraph() {
    if (!transactionGraph?.hasMore || isGraphPageLoading) return;
    const datasetId = transactionGraph.datasetId;
    setIsGraphPageLoading(true);
    setGraphPageError("");
    try {
      const merged = await graphBrowser.loadMore();
      if (merged?.datasetId === datasetId) setTransactionGraph(merged);
    } catch (error) {
      setGraphPageError(
        error instanceof TransactionGraphClientError
          ? error.message
          : "無法載入更多交易關係，請稍後再試。",
      );
    } finally {
      if (currentGraphDataset.current === datasetId) {
        setIsGraphPageLoading(false);
      }
    }
  }

  function reloadTransactionGraph() {
    setGraphReloadToken((current) => current + 1);
  }

  function undoGraphExpansion() {
    const previous = graphHistory.current.pop();
    if (!previous) return;
    setTransactionGraph(previous.graph);
    setGraphSpread(previous.spread);
    setGraphOffset(previous.offset);
    setExpandedNodeIds(previous.expandedNodeIds);
    graphBrowser.replaceGraph(previous.graph);
    setSelectedNodeId(null);
    setExpansionError("");
    setGraphHistoryDepth(graphHistory.current.length);
  }

  async function exportAnalysis(format: "csv" | "pdf") {
    if (
      exportInProgress.current ||
      isExportingCsv ||
      isExportingPdf ||
      !active?.address ||
      !active.currentResult ||
      !currentAnalysisResult
    ) {
      return;
    }
    exportInProgress.current = true;
    const input = {
      investigation: {
        id: active.id,
        title: active.title,
        address: active.address,
        network: active.network,
        currentResult: active.currentResult,
      },
      result: currentAnalysisResult,
      graphView: transactionGraph,
      selectedNodeId,
    };
    setExportError("");
    if (format === "csv") setIsExportingCsv(true);
    else setIsExportingPdf(true);
    try {
      if (format === "csv") await analysisExporter.exportCsv(input);
      else await analysisExporter.exportPdf(input);
    } catch (error) {
      setExportError(
        error instanceof TransactionGraphClientError ||
          error instanceof AnalysisExportError
          ? error.message
          : `${format.toUpperCase()} 產生失敗，未下載任何檔案，請稍後再試。`,
      );
    } finally {
      exportInProgress.current = false;
      if (format === "csv") setIsExportingCsv(false);
      else setIsExportingPdf(false);
    }
  }

  function exportTransactionsCsv() {
    return exportAnalysis("csv");
  }

  function exportAnalysisPdf() {
    return exportAnalysis("pdf");
  }

  function showInvestigation(nextInvestigation: Investigation) {
    analysisAttempt.current += 1;
    conversationAttempt.current += 1;
    analysisRunner.stop();
    graphBrowser.clear();
    loadedAnalysisResult.current = "";
    const saved = investigationSessions.current.get(nextInvestigation.id);
    graphHistory.current = [];
    setGraphHistoryDepth(0);
    setActiveId(nextInvestigation.id);
    setAddressDraft(saved?.addressDraft ?? nextInvestigation.address ?? "");
    setTransactionGraph(null);
    setSelectedNodeId(null);
    setExpandedNodeIds(new Set());
    setGraphSpread(1);
    setGraphZoom(1);
    setGraphOffset({ x: 0, y: 0 });
    setCurrentAnalysisResult(null);
    setAnalysisRun(null);
    setAnalysisOutcome(null);
    setIsAnalysisCancelling(false);
    setGraphError(saved?.graphError ?? "");
    setAnalysisError(saved?.analysisError ?? "");
    setExpansionError("");
    setGraphPageError("");
    setExportError("");
    setChatMessages([]);
    setConversationError("");
    setHasMoreConversation(false);
    setIsConversationLoading(true);
    setIsRunning(false);
    setConversationReloadToken((current) => current + 1);
    setQuery(saved?.query ?? "");
    setTargetMessage("");
    setExpandingNodeId(null);
    setIsGraphLoading(false);
    setIsAnalysisLoading(false);
  }

  async function selectInvestigation(nextId: string) {
    if (nextId === activeId) return;
    const attempt = (selectAttempt.current += 1);

    if (activeId) {
      investigationSessions.current.set(activeId, {
        addressDraft,
        graphError,
        analysisError,
        query,
      });
    }

    analysisRunner.stop();
    analysisAttempt.current += 1;
    setAnalysisRun(null);
    setAnalysisOutcome(null);
    setIsAnalysisCancelling(false);
    setIsAnalysisLoading(false);
    setWorkspaceError("");
    try {
      const selected = await getInvestigation(nextId);
      if (selectAttempt.current !== attempt) return;
      setInvestigations((current) =>
        current.map((item) => (item.id === selected.id ? selected : item)),
      );
      showInvestigation(selected);
    } catch (error) {
      if (selectAttempt.current !== attempt) return;
      setWorkspaceError(
        error instanceof InvestigationClientError
          ? error.message
          : "無法開啟這筆調查，請稍後再試。",
      );
    }
  }

  async function reloadInvestigations() {
    if (isWorkspaceLoading) return;
    setIsWorkspaceLoading(true);
    setWorkspaceError("");
    try {
      const records = await listInvestigations();
      const selected =
        records.find((item) => item.id === activeId) || records[0] || null;
      setInvestigations(records);
      if (selected) showInvestigation(selected);
      else {
        setActiveId("");
        setAddressDraft("");
      }
    } catch (error) {
      setWorkspaceError(
        error instanceof InvestigationClientError
          ? error.message
          : "無法重新載入調查工作區，請稍後再試。",
      );
    } finally {
      setIsWorkspaceLoading(false);
    }
  }

  async function createInvestigation() {
    if (isWorkspaceMutating) return;
    setIsWorkspaceMutating(true);
    setWorkspaceError("");
    try {
      const created = await createInvestigationRecord("新調查任務");
      setInvestigations((current) => [...current, created]);
      showInvestigation(created);
    } catch (error) {
      setWorkspaceError(
        error instanceof InvestigationClientError
          ? error.message
          : "無法建立調查，請稍後再試。",
      );
    } finally {
      setIsWorkspaceMutating(false);
    }
  }

  async function runInvestigation(text?: string) {
    const command = (text || query).trim();
    if (!active || !command || isRunning || isConversationLoading) return;
    const investigationId = active.id;
    const attempt = ++conversationAttempt.current;
    setQuery("");
    setConversationError("");
    setIsRunning(true);
    try {
      await conversationBrowser.submit(command);
      if (conversationAttempt.current !== attempt) return;
      const conversation = conversationBrowser.getState();
      if (conversation.investigationId !== investigationId) return;
      setChatMessages(conversation.messages);
      setHasMoreConversation(Boolean(conversation.nextCursor));
    } catch (error) {
      if (conversationAttempt.current !== attempt) return;
      setQuery((current) => current || command);
      setConversationError(
        error instanceof ConversationClientError
          ? error.message
          : "訊息結果不明，請重試；重試會沿用相同的 command identity。",
      );
    } finally {
      if (conversationAttempt.current === attempt) setIsRunning(false);
    }
  }

  async function loadMoreConversation() {
    if (!active || isConversationLoading || !hasMoreConversation) return;
    const investigationId = active.id;
    const attempt = ++conversationAttempt.current;
    setConversationError("");
    setIsConversationLoading(true);
    try {
      const conversation = await conversationBrowser.loadMore();
      if (
        conversationAttempt.current !== attempt ||
        conversation.investigationId !== investigationId
      ) {
        return;
      }
      setChatMessages(conversation.messages);
      setHasMoreConversation(Boolean(conversation.nextCursor));
    } catch (error) {
      if (conversationAttempt.current !== attempt) return;
      setConversationError(
        error instanceof ConversationClientError
          ? error.message
          : "無法載入更多對話，請稍後再試。",
      );
    } finally {
      if (conversationAttempt.current === attempt) {
        setIsConversationLoading(false);
      }
    }
  }

  function openRename(item: Investigation) {
    setRenameTarget(item);
    setRenameValue(item.title);
    setContextMenu(null);
  }

  async function renameInvestigation(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const title = renameValue.trim();
    if (!renameTarget || !title || isWorkspaceMutating) return;
    setIsWorkspaceMutating(true);
    setWorkspaceError("");
    try {
      const updated = await renameInvestigationRecord(renameTarget.id, title);
      setInvestigations((current) =>
        current.map((item) => (item.id === updated.id ? updated : item)),
      );
      setRenameTarget(null);
    } catch (error) {
      setWorkspaceError(
        error instanceof InvestigationClientError
          ? error.message
          : "無法變更調查名稱，請稍後再試。",
      );
    } finally {
      setIsWorkspaceMutating(false);
    }
  }

  async function restoreStableState(investigationId: string, attempt: number) {
    try {
      const restored = await reloadStableAnalysis(investigationId);
      if (analysisAttempt.current !== attempt) return;
      setInvestigations((current) =>
        current.map((item) =>
          item.id === restored.investigation.id
            ? restored.investigation
            : item,
        ),
      );
      if (restored.result) {
        loadedAnalysisResult.current = `${investigationId}:${restored.result.dataset.id}`;
        setCurrentAnalysisResult(restored.result);
      } else {
        loadedAnalysisResult.current = "";
        setCurrentAnalysisResult(null);
      }
    } catch {
      if (analysisAttempt.current === attempt) {
        setAnalysisError((current) =>
          `${current || "分析未完成。"} 無法重新載入後端穩定結果，請重新載入工作區。`,
        );
      }
    }
  }

  async function startAnalysis() {
    if (
      !active?.address ||
      isAnalysisLoading ||
      analysisRun?.status === "queued" ||
      analysisRun?.status === "running"
    ) {
      return;
    }
    const investigationId = active.id;
    const attempt = analysisAttempt.current + 1;
    analysisAttempt.current = attempt;
    setAnalysisError("");
    setAnalysisOutcome(null);
    setAnalysisRun(null);
    setTargetMessage("");
    setIsAnalysisLoading(true);
    try {
      const completed = await analysisRunner.start(investigationId, analysisScope, (run) => {
        if (analysisAttempt.current !== attempt) return;
        setAnalysisRun(run);
        setIsAnalysisLoading(
          run.status === "queued" || run.status === "running",
        );
        if (run.status === "queued" || run.status === "running") {
          setInvestigations((current) =>
            current.map((item) =>
              item.id === investigationId
                ? { ...item, status: "分析中" }
                : item,
            ),
          );
        }
      });
      if (analysisAttempt.current !== attempt) return;
      if (completed.outcome === "run_lost") {
        setAnalysisRun(null);
        setAnalysisOutcome("run_lost");
        setIsAnalysisLoading(false);
        setAnalysisError(
          "後端重新啟動後已遺失這次分析工作；既有結果已恢復，請重新送出分析。",
        );
        await restoreStableState(investigationId, attempt);
        return;
      }
      if (completed.outcome === "cancelled") {
        setAnalysisRun(completed.run);
        setAnalysisOutcome("cancelled");
        setIsAnalysisLoading(false);
        setAnalysisError("分析已取消，未發布新結果；既有結果保持不變。");
        await restoreStableState(investigationId, attempt);
        return;
      }
      const resultId = completed.run.resultId || completed.result.dataset.id;
      loadedAnalysisResult.current = `${investigationId}:${resultId}`;
      setCurrentAnalysisResult(completed.result);
      setInvestigations((current) =>
        current.map((item) =>
          item.id === investigationId
            ? mergeCurrentAnalysisResult(item, completed.result, resultId)
            : item,
        ),
      );
      setIsAnalysisLoading(false);
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      if (analysisAttempt.current !== attempt) return;
      setAnalysisRun(null);
      setIsAnalysisLoading(false);
      setAnalysisOutcome("failed");
      setAnalysisError(
        error instanceof AnalysisRunClientError ||
          error instanceof InvestigationClientError
          ? error.message
          : "分析服務目前無法使用，請稍後再試。",
      );
      await restoreStableState(investigationId, attempt);
    }
  }

  async function cancelAnalysis() {
    if (isAnalysisCancelling) return;
    const attempt = analysisAttempt.current;
    setIsAnalysisCancelling(true);
    setAnalysisError("");
    try {
      const cancelled = await analysisRunner.cancel();
      if (!cancelled || analysisAttempt.current !== attempt) return;
      setAnalysisRun(cancelled);
      setAnalysisOutcome("cancelled");
      setIsAnalysisLoading(false);
      setAnalysisError("分析已取消，未發布新結果；既有結果保持不變。");
      await restoreStableState(cancelled.investigationId, attempt);
    } catch (error) {
      if (analysisAttempt.current !== attempt) return;
      setAnalysisError(
        error instanceof InvestigationClientError
          ? error.message
          : "無法取消分析，後端工作可能仍在進行。",
      );
    } finally {
      if (analysisAttempt.current === attempt) setIsAnalysisCancelling(false);
    }
  }

  async function deleteInvestigation() {
    if (!deleteTarget || isWorkspaceMutating) return;
    setIsWorkspaceMutating(true);
    setWorkspaceError("");
    try {
      await analysisRunner.deleteInvestigation(deleteTarget.id);
      if (activeId === deleteTarget.id) analysisAttempt.current += 1;
      const remaining = investigations.filter(
        (item) => item.id !== deleteTarget.id,
      );
      setInvestigations((current) =>
        current.filter((item) => item.id !== deleteTarget.id),
      );
      investigationSessions.current.delete(deleteTarget.id);
      if (activeId === deleteTarget.id) {
        const next = remaining[0];
        if (next) showInvestigation(next);
        else {
          conversationAttempt.current += 1;
          setActiveId("");
          setAddressDraft("");
          setTransactionGraph(null);
          setCurrentAnalysisResult(null);
          setChatMessages([]);
          setConversationError("");
          setHasMoreConversation(false);
          setIsConversationLoading(false);
          setQuery("");
        }
      }
      setDeleteTarget(null);
    } catch (error) {
      setWorkspaceError(
        error instanceof InvestigationClientError
          ? error.message
          : "無法刪除調查，請稍後再試。",
      );
    } finally {
      setIsWorkspaceMutating(false);
    }
  }

  return {
    active,
    activeId,
    addressDraft,
    analysisError,
    analysisOutcome,
    analysisRun,
    analysisScope,
    analysisPanelRef,
    chatMessages,
    conversationError,
    contextMenu,
    currentAnalysisResult,
    deleteTarget,
    filtered,
    graphCanvasRef,
    graphCardRef,
    graphDragStart,
    graphOffset,
    graphSpread,
    graphError,
    graphPageError,
    graphHistoryDepth,
    expandedNodeIds,
    expandingNodeId,
    expansionError,
    graphZoom,
    hasMoreConversation,
    investigations,
    isAnalysisClosing,
    isAnalysisCollapsed,
    isAnalysisHovered,
    isAnalysisLoading,
    isAnalysisCancelling,
    isConversationLoading,
    isExportingCsv,
    isExportingPdf,
    exportError,
    isGraphDragging,
    isGraphFullscreen,
    isGraphLoading,
    isGraphPageLoading,
    isLightMode,
    isRunning,
    isWorkspaceLoading,
    isWorkspaceMutating,
    isSidebarClosing,
    isSidebarCollapsed,
    isSidebarHovered,
    query,
    renameTarget,
    renameValue,
    resizingPanel,
    search,
    selectedNodeId,
    sidebarWidth,
    analysisWidth,
    transactionGraph,
    targetMessage,
    workspaceError,
    beginPanelResize,
    adjustPanelWidth,
    confirmInvestigationAddress,
    cancelAnalysis,
    createInvestigation,
    deleteInvestigation,
    enterAnalysis,
    enterSidebar,
    exportTransactionsCsv,
    exportAnalysisPdf,
    expandGraphNode,
    leaveAnalysis,
    leaveSidebar,
    loadMoreConversation,
    loadMoreGraph,
    openRename,
    renameInvestigation,
    reloadInvestigations,
    reloadTransactionGraph,
    resetPanelWidth,
    resetGraphView,
    moveGraphNode,
    runInvestigation,
    startAnalysis,
    setActiveId: selectInvestigation,
    setAddressDraft,
    setAnalysisScope,
    setContextMenu,
    setDeleteTarget,
    setGraphOffset,
    setGraphZoom,
    setIsGraphDragging,
    setIsGraphFullscreen,
    setQuery,
    setRenameTarget,
    setRenameValue,
    setSearch,
    setSelectedNodeId,
    toggleAnalysisPin,
    toggleGraphFullscreen,
    toggleSidebarPin,
    toggleTheme,
    undoGraphExpansion,
    updatePanelResize,
    endPanelResize,
  };
}

export type ChainTraceController = ReturnType<typeof useChainTrace>;

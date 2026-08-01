"use client";

import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import {
  AgentChatClientError,
  sendAgentMessage,
} from "@/src/middle/agent-chat-client";
import { getInvestigationMetrics } from "@/src/middle/investigation-metrics-client";
import { getRiskScore } from "@/src/middle/risk-score-client";
import {
  AnomalyAnalysisClientError,
  requestAnomalyAnalysis,
} from "@/src/middle/anomaly-analysis-client";
import type { AnomalyAnalysisResponse } from "@/src/middle/anomaly-analysis-contract";
import {
  getTransactionGraph,
  TransactionGraphClientError,
} from "@/src/middle/transaction-graph-client";
import type { TransactionGraphResponse } from "@/src/middle/transaction-graph-contract";
import { initialInvestigations } from "@/src/models/chaintraceData";
import { downloadAnalysisPdf } from "@/src/services/downloadAnalysisPdf";
import {
  loadInvestigationWorkspace,
  saveInvestigationWorkspace,
} from "@/src/services/investigationStorage";
import type {
  ChatMessage,
  ContextMenuState,
  Investigation,
} from "@/src/models/chaintraceTypes";

type InvestigationSessionState = {
  addressDraft: string;
  transactionGraph: TransactionGraphResponse | null;
  selectedNodeId: string | null;
  expandedNodeIds: Set<string>;
  graphSpread: number;
  graphZoom: number;
  graphOffset: { x: number; y: number };
  anomalyAnalysis: AnomalyAnalysisResponse | null;
  graphError: string;
  analysisError: string;
  expansionError: string;
  chatMessages: ChatMessage[];
  query: string;
};

function mergeTransactionGraphs(
  current: TransactionGraphResponse,
  expansion: TransactionGraphResponse,
  centerId: string,
): TransactionGraphResponse {
  const originalCenter = current.nodes.find((node) => node.id === centerId);
  if (!originalCenter) return current;
  const shiftX =
    originalCenter.x < 28
      ? 28 - originalCenter.x
      : originalCenter.x > 72
        ? 72 - originalCenter.x
        : 0;
  const shiftY =
    originalCenter.y < 28
      ? 28 - originalCenter.y
      : originalCenter.y > 72
        ? 72 - originalCenter.y
        : 0;
  const shiftedCurrent = {
    ...current,
    nodes: current.nodes.map((node) => ({
      ...node,
      x: Math.min(96, Math.max(4, node.x + shiftX)),
      y: Math.min(96, Math.max(4, node.y + shiftY)),
    })),
  };
  const center = shiftedCurrent.nodes.find((node) => node.id === centerId)!;
  const nextGroup =
    Math.max(0, ...shiftedCurrent.nodes.map((node) => node.group ?? 0)) + 1;
  const existingIds = new Set(shiftedCurrent.nodes.map((node) => node.id));
  const candidates = expansion.nodes.filter(
    (node) => node.id !== centerId && !existingIds.has(node.id),
  );
  const positionedNodes = candidates.map((node, index) => {
    const angle =
      (index / Math.max(candidates.length, 1)) * Math.PI * 2 - Math.PI / 2;
    const seed = [...node.id].reduce(
      (total, character) => total + character.charCodeAt(0),
      0,
    );
    const radius = 15 + (seed % 20);
    return {
      ...node,
      group: nextGroup,
      type: node.type === "focus" ? ("normal" as const) : node.type,
      x: Math.min(94, Math.max(6, center.x + Math.cos(angle) * radius)),
      y: Math.min(94, Math.max(6, center.y + Math.sin(angle) * radius)),
    };
  });
  const nodes = [...shiftedCurrent.nodes, ...positionedNodes];
  const visibleIds = new Set(nodes.map((node) => node.id));
  const edgeIds = new Set(shiftedCurrent.edges.map((edge) => edge.id));
  const edges = [
    ...shiftedCurrent.edges,
    ...expansion.edges
      .filter(
        (edge) =>
          !edgeIds.has(edge.id) &&
          visibleIds.has(edge.from) &&
          visibleIds.has(edge.to),
      )
      .map((edge) => ({ ...edge, group: nextGroup })),
  ];
  return {
    ...shiftedCurrent,
    nodes,
    edges,
    transactionCount: edges.length,
    totalFlow: edges.reduce((sum, edge) => sum + edge.value, 0),
    source: `${current.source}+expanded`,
    updatedAt: new Date().toISOString(),
  };
}

export function useChainTrace() {
  const [investigations, setInvestigations] = useState(initialInvestigations);
  const [activeId, setActiveId] = useState("CT-2041");
  const [query, setQuery] = useState("");
  const [addressDraft, setAddressDraft] = useState(
    initialInvestigations[0]?.address || "",
  );
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([]);
  const [search, setSearch] = useState("");
  const [isRunning, setIsRunning] = useState(false);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(true);
  const [isAnalysisCollapsed, setIsAnalysisCollapsed] = useState(true);
  const [isSidebarHovered, setIsSidebarHovered] = useState(false);
  const [isAnalysisHovered, setIsAnalysisHovered] = useState(false);
  const [isSidebarClosing, setIsSidebarClosing] = useState(false);
  const [isAnalysisClosing, setIsAnalysisClosing] = useState(false);
  const [isLightMode, setIsLightMode] = useState(false);
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
  const [isGraphLoading, setIsGraphLoading] = useState(false);
  const [graphError, setGraphError] = useState("");
  const [isAnalysisLoading, setIsAnalysisLoading] = useState(false);
  const [analysisError, setAnalysisError] = useState("");
  const [anomalyAnalysis, setAnomalyAnalysis] =
    useState<AnomalyAnalysisResponse | null>(null);
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
  const [isStorageHydrated, setIsStorageHydrated] = useState(false);

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
  const hasLoadedInvestigationData = useRef(false);

  const active =
    investigations.find((item) => item.id === activeId) || investigations[0];
  const filtered = useMemo(
    () =>
      investigations.filter((item) =>
        `${item.title} ${item.address}`
          .toLowerCase()
          .includes(search.toLowerCase()),
      ),
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
    setIsLightMode(window.localStorage.getItem("chaintrace-theme") === "light");
    const storedSidebarCollapsed = window.localStorage.getItem(
      "chaintrace-sidebar-collapsed",
    );
    const storedAnalysisCollapsed = window.localStorage.getItem(
      "chaintrace-analysis-collapsed",
    );
    if (storedSidebarCollapsed !== null) {
      setIsSidebarCollapsed(storedSidebarCollapsed === "true");
    }
    if (storedAnalysisCollapsed !== null) {
      setIsAnalysisCollapsed(storedAnalysisCollapsed === "true");
    }
    setSidebarWidth(
      Number(window.localStorage.getItem("chaintrace-sidebar-width")) || 280,
    );
    setAnalysisWidth(
      Number(window.localStorage.getItem("chaintrace-analysis-width")) || 420,
    );

    const snapshot = loadInvestigationWorkspace();
    if (snapshot) {
      const restoredActiveId = snapshot.investigations.some(
        (item) => item.id === snapshot.activeId,
      )
        ? snapshot.activeId
        : snapshot.investigations[0].id;
      const restoredSessions = new Map<string, InvestigationSessionState>();
      for (const [id, session] of Object.entries(snapshot.sessions)) {
        restoredSessions.set(id, {
          ...session,
          expandedNodeIds: new Set(session.expandedNodeIds || []),
          chatMessages: Array.isArray(session.chatMessages)
            ? session.chatMessages
            : [],
          query: session.query || "",
        });
      }
      investigationSessions.current = restoredSessions;
      const activeSession = restoredSessions.get(restoredActiveId);
      const activeInvestigation = snapshot.investigations.find(
        (item) => item.id === restoredActiveId,
      );

      setInvestigations(snapshot.investigations);
      setActiveId(restoredActiveId);
      setAddressDraft(
        activeSession?.addressDraft ?? activeInvestigation?.address ?? "",
      );
      setTransactionGraph(activeSession?.transactionGraph ?? null);
      setSelectedNodeId(activeSession?.selectedNodeId ?? null);
      setExpandedNodeIds(
        new Set(activeSession?.expandedNodeIds || []),
      );
      setGraphSpread(activeSession?.graphSpread ?? 1);
      setGraphZoom(activeSession?.graphZoom ?? 1);
      setGraphOffset(activeSession?.graphOffset ?? { x: 0, y: 0 });
      setAnomalyAnalysis(activeSession?.anomalyAnalysis ?? null);
      setGraphError(activeSession?.graphError ?? "");
      setAnalysisError(activeSession?.analysisError ?? "");
      setExpansionError(activeSession?.expansionError ?? "");
      setChatMessages(activeSession?.chatMessages ?? []);
      setQuery(activeSession?.query ?? "");
    }
    setIsStorageHydrated(true);
  }, []);

  useEffect(() => {
    if (!isStorageHydrated) return;
    const timeout = window.setTimeout(() => {
      const sessions = new Map(investigationSessions.current);
      sessions.set(activeId, {
        addressDraft,
        transactionGraph,
        selectedNodeId,
        expandedNodeIds: new Set(expandedNodeIds),
        graphSpread,
        graphZoom,
        graphOffset,
        anomalyAnalysis,
        graphError,
        analysisError,
        expansionError,
        chatMessages,
        query,
      });
      investigationSessions.current = sessions;
      saveInvestigationWorkspace({
        activeId,
        investigations,
        sessions: Object.fromEntries(
          [...sessions.entries()].map(([id, session]) => [
            id,
            {
              ...session,
              expandedNodeIds: [...session.expandedNodeIds],
            },
          ]),
        ),
      });
    }, 250);
    return () => window.clearTimeout(timeout);
  }, [
    activeId,
    addressDraft,
    analysisError,
    anomalyAnalysis,
    chatMessages,
    expandedNodeIds,
    expansionError,
    graphError,
    graphOffset,
    graphSpread,
    graphZoom,
    investigations,
    isStorageHydrated,
    query,
    selectedNodeId,
    transactionGraph,
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
  }, []);

  useEffect(() => {
    if (!isStorageHydrated || hasLoadedInvestigationData.current) return;
    hasLoadedInvestigationData.current = true;
    let cancelled = false;
    async function loadInvestigationData() {
      const results = await Promise.all(
        investigations
          .filter((item) => item.address)
          .map(async (item) => ({
            id: item.id,
            risk: await getRiskScore({
              address: item.address,
              network: item.network,
            }),
            metrics: await getInvestigationMetrics({
              address: item.address,
              network: item.network,
            }),
          })),
      );
      if (cancelled) return;
      setInvestigations((current) =>
        current.map((item) => {
          const match = results.find((entry) => entry.id === item.id);
          if (!match) return item;
          return {
            ...item,
            ...(match.risk.status === "ready"
              ? { risk: match.risk.score }
              : {}),
            ...(match.metrics.status === "ready"
              ? {
                  relatedNodes: match.metrics.relatedNodes,
                  totalFlow: match.metrics.totalFlow,
                  flowAsset: match.metrics.flowAsset,
                  transactionCount: match.metrics.transactionCount,
                }
              : {}),
          };
        }),
      );
    }
    void loadInvestigationData();
    return () => {
      cancelled = true;
    };
  }, [isStorageHydrated]);

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
    setTransactionGraph((current) =>
      current
        ? {
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
          }
        : current,
    );
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
    if (isGraphLoading) return;
    if (!/^0x[a-fA-F0-9]{40}$/.test(address)) {
      setGraphError("請輸入有效的 Ethereum 錢包地址");
      return;
    }

    setGraphError("");
    setAnalysisError("");
    setAnomalyAnalysis(null);
    setIsGraphLoading(true);
    setTransactionGraph(null);
    setSelectedNodeId(null);
    setExpandedNodeIds(new Set());
    graphHistory.current = [];
    setGraphHistoryDepth(0);
    setExpansionError("");
    setGraphSpread(1);
    resetGraphView();
    try {
      const graph = await getTransactionGraph(address, active.network);
      const layeredGraph = {
        ...graph,
        nodes: graph.nodes.map((node) => ({ ...node, group: 0 })),
        edges: graph.edges.map((edge) => ({ ...edge, group: 0 })),
      };
      setTransactionGraph(layeredGraph);
      setSelectedNodeId(
        graph.nodes.find((node) => node.type === "focus")?.id ||
          graph.nodes[0]?.id ||
          null,
      );
      setExpandedNodeIds(new Set([address.toLowerCase()]));
      setInvestigations((current) =>
        current.map((item) =>
          item.id === activeId
            ? {
                ...item,
                address,
                relatedNodes: Math.max(0, graph.nodes.length - 1),
                transactionCount: graph.transactionCount,
                totalFlow: graph.totalFlow,
                flowAsset: graph.flowAsset,
                status: "分析中",
              }
            : item,
        ),
      );

      setIsAnalysisLoading(true);
      try {
        const analysis = await requestAnomalyAnalysis({
          sessionId: active.id,
          address,
          network: active.network,
          graph: {
            nodes: layeredGraph.nodes,
            edges: layeredGraph.edges,
            transactionCount: graph.transactionCount,
            totalFlow: graph.totalFlow,
            flowAsset: graph.flowAsset,
          },
        });
        setAnomalyAnalysis(analysis);
        setInvestigations((current) =>
          current.map((item) =>
            item.id === activeId
              ? { ...item, risk: Math.min(100, Math.max(0, analysis.score)) }
              : item,
          ),
        );
      } catch (error) {
        setAnalysisError(
          error instanceof AnomalyAnalysisClientError
            ? error.message
            : "交易資料已取得，但異常分析服務目前無法使用",
        );
      } finally {
        setIsAnalysisLoading(false);
      }
    } catch (error) {
      setGraphError(
        error instanceof TransactionGraphClientError
          ? error.message
          : "實際鏈上資料抓取失敗，請稍後再試",
      );
    } finally {
      setIsGraphLoading(false);
    }
  }

  async function expandGraphNode(nodeId: string) {
    setSelectedNodeId(nodeId);
    const currentGraph = transactionGraph;
    const node = currentGraph?.nodes.find((candidate) => candidate.id === nodeId);
    if (
      !currentGraph ||
      !node ||
      expandedNodeIds.has(nodeId) ||
      expandingNodeId
    ) {
      return;
    }

    setExpansionError("");
    setExpandingNodeId(nodeId);
    try {
      const expansion = await getTransactionGraph(node.address, active.network);
      const merged = mergeTransactionGraphs(currentGraph, expansion, nodeId);
      graphHistory.current.push({
        graph: currentGraph,
        spread: graphSpread,
        offset: graphOffset,
        expandedNodeIds: new Set(expandedNodeIds),
      });
      setGraphHistoryDepth(graphHistory.current.length);
      setTransactionGraph(merged);
      setGraphSpread((current) => {
        const next = Math.min(6, current + 0.7);
        const expandedCenter = merged.nodes.find(
          (candidate) => candidate.id === nodeId,
        );
        const canvas = graphCanvasRef.current;
        if (expandedCenter && canvas) {
          const rect = canvas.getBoundingClientRect();
          setGraphOffset({
            x: ((50 - expandedCenter.x) / 100) * rect.width * next,
            y: ((50 - expandedCenter.y) / 100) * rect.height * next,
          });
        }
        return next;
      });
      setExpandedNodeIds((current) => new Set(current).add(nodeId));
      setInvestigations((current) =>
        current.map((item) =>
          item.id === activeId
            ? {
                ...item,
                relatedNodes: Math.max(0, merged.nodes.length - 1),
                transactionCount: merged.transactionCount,
                totalFlow: merged.totalFlow,
                flowAsset: merged.flowAsset,
              }
            : item,
        ),
      );
    } catch (error) {
      setExpansionError(
        error instanceof TransactionGraphClientError
          ? error.message
          : "無法展開這個節點的鏈上交易",
      );
    } finally {
      setExpandingNodeId(null);
    }
  }

  function undoGraphExpansion() {
    const previous = graphHistory.current.pop();
    if (!previous) return;
    setTransactionGraph(previous.graph);
    setGraphSpread(previous.spread);
    setGraphOffset(previous.offset);
    setExpandedNodeIds(previous.expandedNodeIds);
    setSelectedNodeId(null);
    setExpansionError("");
    setGraphHistoryDepth(graphHistory.current.length);
    setInvestigations((current) =>
      current.map((item) =>
        item.id === activeId
          ? {
              ...item,
              relatedNodes: Math.max(0, previous.graph.nodes.length - 1),
              transactionCount: previous.graph.transactionCount,
              totalFlow: previous.graph.totalFlow,
              flowAsset: previous.graph.flowAsset,
            }
          : item,
      ),
    );
  }

  async function exportAnalysisPdf() {
    if (isExportingPdf || !transactionGraph) return;
    setIsExportingPdf(true);
    try {
      await downloadAnalysisPdf({
        caseId: active.id,
        caseTitle: active.title,
        address: active.address,
        network: active.network,
        graph: transactionGraph,
        selectedNode:
          transactionGraph.nodes.find((node) => node.id === selectedNodeId) ||
          null,
        anomalyAnalysis,
      });
    } catch (error) {
      console.error("PDF export failed", error);
      window.alert("PDF 產生失敗，請稍後再試。");
    } finally {
      setIsExportingPdf(false);
    }
  }

  function selectInvestigation(nextId: string) {
    if (nextId === activeId) return;

    investigationSessions.current.set(activeId, {
      addressDraft,
      transactionGraph,
      selectedNodeId,
      expandedNodeIds: new Set(expandedNodeIds),
      graphSpread,
      graphZoom,
      graphOffset,
      anomalyAnalysis,
      graphError,
      analysisError,
      expansionError,
      chatMessages,
      query,
    });

    const nextInvestigation = investigations.find(
      (item) => item.id === nextId,
    );
    const saved = investigationSessions.current.get(nextId);
    graphHistory.current = [];
    setGraphHistoryDepth(0);
    setActiveId(nextId);
    setAddressDraft(saved?.addressDraft ?? nextInvestigation?.address ?? "");
    setTransactionGraph(saved?.transactionGraph ?? null);
    setSelectedNodeId(saved?.selectedNodeId ?? null);
    setExpandedNodeIds(
      saved ? new Set(saved.expandedNodeIds) : new Set(),
    );
    setGraphSpread(saved?.graphSpread ?? 1);
    setGraphZoom(saved?.graphZoom ?? 1);
    setGraphOffset(saved?.graphOffset ?? { x: 0, y: 0 });
    setAnomalyAnalysis(saved?.anomalyAnalysis ?? null);
    setGraphError(saved?.graphError ?? "");
    setAnalysisError(saved?.analysisError ?? "");
    setExpansionError(saved?.expansionError ?? "");
    setChatMessages(saved?.chatMessages ?? []);
    setQuery(saved?.query ?? "");
    setExpandingNodeId(null);
    setIsGraphLoading(false);
    setIsAnalysisLoading(false);
  }

  function createInvestigation() {
    const id = `CT-${2042 + investigations.length}`;
    const item: Investigation = {
      id,
      title: `新調查任務 #${id.slice(3)}`,
      address: "",
      network: "Ethereum",
      risk: 0,
      relatedNodes: 0,
      totalFlow: 0,
      flowAsset: "",
      transactionCount: 0,
      status: "待處理",
    };
    setInvestigations((current) => [item, ...current]);
    selectInvestigation(id);
    setQuery("");
  }

  async function runInvestigation(text?: string) {
    const command = (text || query).trim();
    if (!command || isRunning) return;
    const messageId = Date.now();
    setQuery("");
    setChatMessages((current) => [
      ...current,
      { id: messageId, role: "user", content: command },
    ]);
    setIsRunning(true);
    setInvestigations((current) =>
      current.map((item) =>
        item.id === activeId ? { ...item, status: "分析中" } : item,
      ),
    );
    try {
      const result = await sendAgentMessage({
        sessionId: active.id,
        message: command,
        investigation: {
          id: active.id,
          address: active.address || null,
          network: active.network,
        },
      });
      setChatMessages((current) => [
        ...current,
        { id: messageId + 1, role: "agent", content: result.message },
      ]);
    } catch (error) {
      const message =
        error instanceof AgentChatClientError
          ? `${error.message}（${error.code}）`
          : "AI Agent 接口呼叫失敗";
      setChatMessages((current) => [
        ...current,
        { id: messageId + 1, role: "system", content: message },
      ]);
    } finally {
      setIsRunning(false);
    }
  }

  function openRename(item: Investigation) {
    setRenameTarget(item);
    setRenameValue(item.title);
    setContextMenu(null);
  }

  function renameInvestigation(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const title = renameValue.trim();
    if (!renameTarget || !title) return;
    setInvestigations((current) =>
      current.map((item) =>
        item.id === renameTarget.id ? { ...item, title } : item,
      ),
    );
    setRenameTarget(null);
  }

  function deleteInvestigation() {
    if (!deleteTarget || investigations.length === 1) return;
    const remaining = investigations.filter(
      (item) => item.id !== deleteTarget.id,
    );
    setInvestigations(remaining);
    if (activeId === deleteTarget.id) {
      selectInvestigation(remaining[0].id);
      investigationSessions.current.delete(deleteTarget.id);
      setQuery("");
    } else {
      investigationSessions.current.delete(deleteTarget.id);
    }
    setDeleteTarget(null);
  }

  return {
    active,
    activeId,
    addressDraft,
    analysisError,
    anomalyAnalysis,
    analysisPanelRef,
    chatMessages,
    contextMenu,
    deleteTarget,
    filtered,
    graphCanvasRef,
    graphCardRef,
    graphDragStart,
    graphOffset,
    graphSpread,
    graphError,
    graphHistoryDepth,
    expandedNodeIds,
    expandingNodeId,
    expansionError,
    graphZoom,
    investigations,
    isAnalysisClosing,
    isAnalysisCollapsed,
    isAnalysisHovered,
    isAnalysisLoading,
    isExportingPdf,
    isGraphDragging,
    isGraphFullscreen,
    isGraphLoading,
    isLightMode,
    isRunning,
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
    beginPanelResize,
    adjustPanelWidth,
    confirmInvestigationAddress,
    createInvestigation,
    deleteInvestigation,
    enterAnalysis,
    enterSidebar,
    exportAnalysisPdf,
    expandGraphNode,
    leaveAnalysis,
    leaveSidebar,
    openRename,
    renameInvestigation,
    resetPanelWidth,
    resetGraphView,
    moveGraphNode,
    runInvestigation,
    setActiveId: selectInvestigation,
    setAddressDraft,
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

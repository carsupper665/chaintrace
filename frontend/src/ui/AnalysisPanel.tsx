import React, { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ChainTraceController } from "@/src/hooks/useChainTrace";
import type { TransactionGraphEdge } from "@/src/middle/transaction-graph-contract";
import { getRiskTone } from "@/src/utils/riskTone";

export function AnalysisPanel({ ui }: { ui: ChainTraceController }) {
  const [selectedDate, setSelectedDate] = useState("");
  const [isCompactGraph, setIsCompactGraph] = useState(false);
  const [hiddenGraphGroups, setHiddenGraphGroups] = useState<Set<number>>(
    () => new Set(),
  );
  const [directionFilter, setDirectionFilter] = useState<
    "all" | "inbound" | "outbound"
  >("all");
  const [hoveredNode, setHoveredNode] = useState<{
    id: string;
    x: number;
    y: number;
  } | null>(null);
  const [copyFeedback, setCopyFeedback] = useState<{
    address: string;
    status: "success" | "error";
  } | null>(null);
  const [nodeSearch, setNodeSearch] = useState("");
  const [nodeSearchMessage, setNodeSearchMessage] = useState("");
  const [hoveredEdge, setHoveredEdge] = useState<{
    edge: TransactionGraphEdge;
    x: number;
    y: number;
  } | null>(null);
  const copyFeedbackTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const nodeDrag = useRef<{
    id: string;
    clientX: number;
    clientY: number;
    x: number;
    y: number;
  } | null>(null);
  const nodeWasDragged = useRef(false);
  const graphNodes = ui.transactionGraph?.nodes || [];
  const anomalyTone = ui.anomalyAnalysis
    ? getRiskTone(ui.anomalyAnalysis.score)
    : "pending";
  const graphEdges = ui.transactionGraph?.edges || [];
  const graphUpdatedLabel = ui.transactionGraph?.updatedAt
    ? new Date(ui.transactionGraph.updatedAt).toLocaleString("zh-TW", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
      })
    : "";
  useEffect(() => {
    setSelectedDate("");
    setHiddenGraphGroups(new Set());
    setDirectionFilter("all");
  }, [ui.transactionGraph?.address]);
  useEffect(
    () => () => {
      if (copyFeedbackTimer.current) clearTimeout(copyFeedbackTimer.current);
    },
    [],
  );
  const transactionDates = useMemo(
    () =>
      [...new Set(
        graphEdges
          .map((edge) => edge.timestamp?.slice(0, 10))
          .filter((date): date is string => Boolean(date)),
      )].sort((a, b) => b.localeCompare(a)),
    [graphEdges],
  );
  const matchingEdges = useMemo(
    () =>
      selectedDate
        ? new Set(
            graphEdges
              .filter((edge) => edge.timestamp?.slice(0, 10) === selectedDate)
              .map((edge) => edge.id),
          )
        : null,
    [graphEdges, selectedDate],
  );
  const matchingNodes = useMemo(() => {
    if (!matchingEdges) return null;
    const ids = new Set<string>();
    for (const edge of graphEdges) {
      if (matchingEdges.has(edge.id)) {
        ids.add(edge.from);
        ids.add(edge.to);
      }
    }
    return ids;
  }, [graphEdges, matchingEdges]);
  const layerColors = [
    "#25c978",
    "#4f91ff",
    "#ae66f5",
    "#f0a43b",
    "#e95b72",
    "#1fb6b2",
  ];
  const layerColor = (group = 0) =>
    layerColors[group % layerColors.length];
  const graphGroups = [...new Set(graphNodes.map((node) => node.group ?? 0))]
    .sort((a, b) => a - b);
  const nodeById = new Map(graphNodes.map((node) => [node.id, node]));
  const investigationAddress = ui.transactionGraph?.address.toLowerCase();
  const investigationNodeId =
    graphNodes.find(
      (node) =>
        investigationAddress &&
        node.address.toLowerCase() === investigationAddress,
    )?.id ??
    graphNodes.find((node) => node.type === "focus")?.id ??
    null;
  const groupCenterByGroup = new Map<number, string>();
  if (investigationNodeId) groupCenterByGroup.set(0, investigationNodeId);
  for (const group of graphGroups.filter((value) => value > 0)) {
    const centerCandidates = new Map<string, number>();
    for (const edge of graphEdges.filter(
      (candidate) => (candidate.group ?? 0) === group,
    )) {
      for (const nodeId of [edge.from, edge.to]) {
        const node = nodeById.get(nodeId);
        if (node && (node.group ?? 0) < group) {
          centerCandidates.set(
            nodeId,
            (centerCandidates.get(nodeId) ?? 0) + 1,
          );
        }
      }
    }
    const centerId = [...centerCandidates.entries()].sort(
      (left, right) => right[1] - left[1],
    )[0]?.[0];
    if (centerId) groupCenterByGroup.set(group, centerId);
  }
  const edgeDirection = (edge: TransactionGraphEdge) => {
    const centerId =
      groupCenterByGroup.get(edge.group ?? 0) ?? investigationNodeId;
    if (edge.to === centerId) return "inbound";
    if (edge.from === centerId) return "outbound";
    return "neutral";
  };
  const visibleGraphEdges = graphEdges.filter((edge) => {
    if (hiddenGraphGroups.has(edge.group ?? 0)) return false;
    if (directionFilter === "all") return true;
    return edgeDirection(edge) === directionFilter;
  });
  const visibleEdgeIds = new Set(visibleGraphEdges.map((edge) => edge.id));
  const hasActiveGraphFilters =
    directionFilter !== "all" ||
    hiddenGraphGroups.size > 0 ||
    Boolean(selectedDate) ||
    isCompactGraph;
  const visibleNodeIds = new Set<string>();
  for (const edge of visibleGraphEdges) {
    visibleNodeIds.add(edge.from);
    visibleNodeIds.add(edge.to);
  }
  if (investigationNodeId) visibleNodeIds.add(investigationNodeId);
  const dateMarkers = useMemo(() => {
    const relationshipMarkers = new Map<
      string,
      { edge: (typeof graphEdges)[number]; dates: Set<string> }
    >();
    for (const edge of graphEdges) {
      const date = edge.timestamp?.slice(0, 10);
      if (!date || edge.from === edge.to) continue;
      const pair = [edge.from, edge.to].sort().join(":");
      const existing = relationshipMarkers.get(pair);
      if (existing) {
        existing.dates.add(date);
      } else {
        relationshipMarkers.set(pair, {
          edge,
          dates: new Set([date]),
        });
      }
    }

    const nodesById = new Map(graphNodes.map((node) => [node.id, node]));

    return [...relationshipMarkers.values()].flatMap(
      (marker) => {
      const from = nodesById.get(marker.edge.from);
      const to = nodesById.get(marker.edge.to);
      if (!from || !to) return [];

      const midpoint = {
        x: (from.x + to.x) / 2,
        y: (from.y + to.y) / 2,
      };
      const dates = [...marker.dates].sort();
      const firstDate = dates[0];
      const lastDate = dates.at(-1) || firstDate;
      const dateLabel =
        firstDate === lastDate
          ? firstDate
          : firstDate.slice(0, 4) === lastDate.slice(0, 4)
            ? `${firstDate}～${lastDate.slice(5)}`
            : `${firstDate}～${lastDate}`;
      return [{ ...marker, dates, dateLabel, ...midpoint }];
    },
    );
  }, [graphEdges, graphNodes]);
  const selectedNode =
    graphNodes.find((node) => node.id === ui.selectedNodeId) || null;
  const selectedEdges = selectedNode
    ? graphEdges.filter(
        (edge) => edge.from === selectedNode.id || edge.to === selectedNode.id,
      )
    : [];
  const incomingEdges = selectedNode
    ? selectedEdges.filter((edge) => edge.to === selectedNode.id)
    : [];
  const outgoingEdges = selectedNode
    ? selectedEdges.filter((edge) => edge.from === selectedNode.id)
    : [];
  const counterparties = selectedNode
    ? new Set(
        selectedEdges.map((edge) =>
          edge.from === selectedNode.id ? edge.to : edge.from,
        ),
      ).size
    : 0;
  const selectedAssessment = selectedNode
    ? ui.anomalyAnalysis?.nodeAssessments?.find(
        (assessment) => assessment.nodeId.toLowerCase() === selectedNode.id,
      )
    : null;
  const hoverNode = hoveredNode
    ? graphNodes.find((node) => node.id === hoveredNode.id) || null
    : null;
  const hoverEdges = hoverNode
    ? graphEdges.filter(
        (edge) => edge.from === hoverNode.id || edge.to === hoverNode.id,
      )
    : [];
  const hoverIncomingEdges = hoverNode
    ? hoverEdges.filter((edge) => edge.to === hoverNode.id)
    : [];
  const hoverOutgoingEdges = hoverNode
    ? hoverEdges.filter((edge) => edge.from === hoverNode.id)
    : [];
  const hoverCounterparties = hoverNode
    ? new Set(
        hoverEdges.map((edge) =>
          edge.from === hoverNode.id ? edge.to : edge.from,
        ),
      ).size
    : 0;
  const summarizeFlow = (edges: TransactionGraphEdge[]) => {
    const totals = new Map<string, number>();
    for (const edge of edges) {
      totals.set(edge.asset, (totals.get(edge.asset) ?? 0) + edge.value);
    }
    if (totals.size === 0) return "0";
    return [...totals.entries()]
      .map(
        ([asset, amount]) =>
          `${amount.toLocaleString(undefined, {
            maximumFractionDigits: 6,
          })} ${asset}`,
      )
      .join(" + ");
  };

  async function copyNodeAddress(address: string) {
    let copied = false;
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(address);
        copied = true;
      }
    } catch {
      copied = false;
    }
    if (!copied) {
      const textarea = document.createElement("textarea");
      textarea.value = address;
      textarea.setAttribute("readonly", "");
      textarea.style.position = "fixed";
      textarea.style.opacity = "0";
      document.body.appendChild(textarea);
      textarea.select();
      copied = document.execCommand("copy");
      textarea.remove();
    }
    setCopyFeedback({ address, status: copied ? "success" : "error" });
    if (copyFeedbackTimer.current) clearTimeout(copyFeedbackTimer.current);
    copyFeedbackTimer.current = setTimeout(() => setCopyFeedback(null), 1800);
  }

  function locateGraphNode() {
    const normalized = nodeSearch.trim().toLowerCase();
    if (!normalized) return;
    const node = graphNodes.find(
      (candidate) =>
        candidate.address.toLowerCase().includes(normalized) ||
        candidate.label.toLowerCase().includes(normalized),
    );
    if (!node) {
      setNodeSearchMessage("找不到此節點");
      return;
    }
    const canvas = ui.graphCanvasRef.current;
    if (canvas) {
      const rect = canvas.getBoundingClientRect();
      ui.setGraphOffset({
        x:
          -((node.x - 50) / 100) *
          rect.width *
          ui.graphSpread *
          ui.graphZoom,
        y:
          -((node.y - 50) / 100) *
          rect.height *
          ui.graphSpread *
          ui.graphZoom,
      });
    }
    ui.setSelectedNodeId(node.id);
    setNodeSearchMessage("已定位節點");
  }

  function exportTransactionsCsv() {
    if (!ui.transactionGraph) return;
    const escapeCell = (value: string | number | null) =>
      `"${String(value ?? "").replaceAll('"', '""')}"`;
    const rows = [
      ["交易識別碼", "日期時間", "發送方", "接收方", "金額", "資產"],
      ...graphEdges.map((edge) => [
        edge.id,
        edge.timestamp,
        nodeById.get(edge.from)?.address || edge.from,
        nodeById.get(edge.to)?.address || edge.to,
        edge.value,
        edge.asset,
      ]),
    ];
    const csv =
      "\uFEFF" +
      rows.map((row) => row.map(escapeCell).join(",")).join("\r\n");
    const url = URL.createObjectURL(
      new Blob([csv], { type: "text/csv;charset=utf-8" }),
    );
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `chaintrace-${ui.transactionGraph.address.slice(0, 10)}-transactions.csv`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  const copyFeedbackToast =
    copyFeedback && typeof document !== "undefined"
      ? createPortal(
          <div
            className={`copy-feedback-toast ${copyFeedback.status}`}
            role="status"
            aria-live="polite"
          >
            <span>{copyFeedback.status === "success" ? "✓" : "!"}</span>
            {copyFeedback.status === "success"
              ? "錢包地址已複製"
              : "複製失敗，請確認剪貼簿權限"}
          </div>,
          document.body,
        )
      : null;

  return (
    <>
    <aside
      ref={ui.analysisPanelRef}
      className={`analysis-panel ${
        ui.isGraphFullscreen ? "graph-overlay-active" : ""
      }`}
      onMouseEnter={() => {
        if (!ui.isGraphFullscreen) ui.enterAnalysis();
      }}
      onMouseLeave={() => {
        if (!ui.isGraphFullscreen) ui.leaveAnalysis();
      }}
    >
      {!ui.isAnalysisCollapsed && (
        <div
          className="panel-resize-handle analysis-resize-handle"
          role="separator"
          aria-label="調整分析與解釋工作區寬度"
          aria-orientation="vertical"
          tabIndex={0}
          onPointerDown={(event) => {
            event.preventDefault();
            event.stopPropagation();
            event.currentTarget.setPointerCapture(event.pointerId);
            ui.beginPanelResize("analysis", event.clientX);
          }}
          onPointerMove={(event) => {
            if (ui.resizingPanel === "analysis") {
              ui.updatePanelResize(event.clientX);
            }
          }}
          onPointerUp={(event) => {
            if (event.currentTarget.hasPointerCapture(event.pointerId)) {
              event.currentTarget.releasePointerCapture(event.pointerId);
            }
            ui.endPanelResize();
          }}
          onPointerCancel={ui.endPanelResize}
          onDoubleClick={() => ui.resetPanelWidth("analysis")}
          onKeyDown={(event) => {
            if (event.key === "ArrowLeft") {
              event.preventDefault();
              ui.adjustPanelWidth("analysis", 20);
            } else if (event.key === "ArrowRight") {
              event.preventDefault();
              ui.adjustPanelWidth("analysis", -20);
            } else if (event.key === "Home") {
              event.preventDefault();
              ui.toggleAnalysisPin();
            } else if (event.key === "End") {
              event.preventDefault();
              ui.resetPanelWidth("analysis");
            }
          }}
        />
      )}
      <header className="analysis-header">
        <div>
          <span className="eyebrow">ANALYSIS</span>
          <h2>分析與解釋</h2>
        </div>
        <div className="analysis-header-actions">
          <button
            className={`icon-button pin-button ${
              !ui.isAnalysisCollapsed ? "pinned" : ""
            }`}
            aria-label={
              ui.isAnalysisCollapsed
                ? "釘選分析與解釋面板"
                : "取消釘選分析與解釋面板"
            }
            title={
              ui.isAnalysisCollapsed
                ? "釘選分析與解釋面板"
                : "取消釘選分析與解釋面板"
            }
            onClick={ui.toggleAnalysisPin}
          >
            ⌖
          </button>
          <button
            className="icon-button analysis-toggle"
            aria-label={
              ui.isAnalysisCollapsed
                ? "展開並釘選分析與解釋面板"
                : "收合分析與解釋面板"
            }
            aria-expanded={!ui.isAnalysisCollapsed}
            onClick={ui.toggleAnalysisPin}
          >
            <span className="panel-toggle-glyph panel-toggle-right" />
          </button>
        </div>
      </header>

      <section
        ref={ui.graphCardRef}
        className={`graph-card ${ui.isGraphFullscreen ? "graph-fullscreen" : ""}`}
      >
        <div className="card-heading">
          <div>
            <h3>交易關係圖譜</h3>
            <small>
              {ui.active.relatedNodes} 節點 · {ui.active.transactionCount} 交易
              {ui.graphSpread > 1 &&
                ` · 畫布 ${Math.round(ui.graphSpread * 100)}%`}
            </small>
            {ui.transactionGraph && (
              <div className="graph-data-meta">
                <span>資料來源：{ui.transactionGraph.source}</span>
                <span>更新：{graphUpdatedLabel}</span>
              </div>
            )}
          </div>
          <div className="graph-controls">
            <label className="graph-node-search">
              <span className="sr-only">搜尋圖譜節點</span>
              <input
                value={nodeSearch}
                onChange={(event) => {
                  setNodeSearch(event.target.value);
                  setNodeSearchMessage("");
                }}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    locateGraphNode();
                  }
                }}
                placeholder="搜尋地址"
                aria-label="搜尋圖譜中的錢包地址"
              />
              <button
                type="button"
                onClick={locateGraphNode}
                disabled={!nodeSearch.trim()}
                aria-label="定位節點"
                title={nodeSearchMessage || "定位節點"}
              >
                ⌕
              </button>
            </label>
            <label className="graph-date-filter">
              <span aria-hidden="true">◷</span>
              <select
                value={selectedDate}
                onChange={(event) => setSelectedDate(event.target.value)}
                aria-label="依交易日期篩選圖譜"
              >
                <option value="">全部日期</option>
                {transactionDates.map((date) => (
                  <option key={date} value={date}>
                    {date}
                  </option>
                ))}
              </select>
            </label>
            <div
              className="graph-view-switch"
              role="group"
              aria-label="圖譜標籤顯示方式"
            >
              <span>標籤</span>
              <button
                type="button"
                className={!isCompactGraph ? "active" : ""}
                onClick={() => setIsCompactGraph(false)}
                aria-pressed={!isCompactGraph}
              >
                完整
              </button>
              <button
                type="button"
                className={isCompactGraph ? "active" : ""}
                onClick={() => setIsCompactGraph(true)}
                aria-pressed={isCompactGraph}
              >
                簡潔
              </button>
            </div>
            <button
              type="button"
              className="graph-csv-button"
              onClick={exportTransactionsCsv}
              disabled={!ui.transactionGraph}
              aria-label="下載交易 CSV"
              title="下載交易 CSV"
            >
              CSV
            </button>
            <button
              aria-label="放大"
              title={`放大（${Math.round(ui.graphZoom * 100)}%）`}
              onClick={() =>
                ui.setGraphZoom((current) => Math.min(1.8, current + 0.2))
              }
              disabled={ui.graphZoom >= 1.8}
            >
              ＋
            </button>
            <button
              aria-label="縮小"
              title={`縮小（${Math.round(ui.graphZoom * 100)}%）`}
              onClick={() =>
                ui.setGraphZoom((current) => Math.max(0.6, current - 0.2))
              }
              disabled={ui.graphZoom <= 0.6}
            >
              −
            </button>
            <button
              aria-label="重設視圖"
              title="重設為 100%"
              onClick={ui.resetGraphView}
            >
              ⌖
            </button>
          </div>
        </div>
        {ui.transactionGraph && (
          <div className="graph-filter-bar" aria-label="圖譜顯示篩選">
            <div className="graph-filter-group">
              <span>方向</span>
              {(["all", "inbound", "outbound"] as const).map((value) => (
                <button
                  type="button"
                  key={value}
                  className={directionFilter === value ? "active" : ""}
                  onClick={() => setDirectionFilter(value)}
                  aria-pressed={directionFilter === value}
                >
                  {value === "all"
                    ? "全部"
                    : value === "inbound"
                      ? "轉入"
                      : "轉出"}
                </button>
              ))}
            </div>
            <div className="graph-filter-group layer-filter-group">
              <span>圖層</span>
              {graphGroups.map((group) => (
                <button
                  type="button"
                  key={group}
                  className={!hiddenGraphGroups.has(group) ? "active" : ""}
                  onClick={() =>
                    setHiddenGraphGroups((current) => {
                      const next = new Set(current);
                      if (next.has(group)) next.delete(group);
                      else next.add(group);
                      return next;
                    })
                  }
                  aria-pressed={!hiddenGraphGroups.has(group)}
                >
                  {String.fromCharCode(65 + Math.min(group, 25))}
                </button>
              ))}
            </div>
            <button
              type="button"
              className="graph-undo-button"
              onClick={ui.undoGraphExpansion}
              disabled={ui.graphHistoryDepth === 0}
              title="復原上一次節點展開"
            >
              ↶ 上一步
            </button>
            <div className="graph-filter-summary" role="status">
              <strong>{visibleGraphEdges.length}</strong>
              <span>/ {graphEdges.length} 筆交易</span>
              {selectedDate && matchingEdges && (
                <span>· 日期符合 {matchingEdges.size} 筆</span>
              )}
            </div>
            <button
              type="button"
              className="graph-clear-filter"
              onClick={() => {
                setDirectionFilter("all");
                setHiddenGraphGroups(new Set());
                setSelectedDate("");
                setIsCompactGraph(false);
              }}
              disabled={!hasActiveGraphFilters}
            >
              清除篩選
            </button>
          </div>
        )}
        <div
          ref={ui.graphCanvasRef}
          className={`graph-canvas ${ui.isGraphDragging ? "dragging" : ""}`}
          aria-label="可拖曳與縮放的交易關係圖"
          tabIndex={0}
          onKeyDown={(event) => {
            if (event.key === "+" || event.key === "=") {
              event.preventDefault();
              ui.setGraphZoom((current) => Math.min(1.8, current + 0.2));
            } else if (event.key === "-") {
              event.preventDefault();
              ui.setGraphZoom((current) => Math.max(0.6, current - 0.2));
            } else if (event.key === "0") {
              event.preventDefault();
              ui.resetGraphView();
            }
          }}
          onPointerDown={(event) => {
            if (
              (event.target as HTMLElement).closest(
                ".graph-node, button, .graph-legend",
              )
            ) {
              return;
            }
            event.currentTarget.setPointerCapture(event.pointerId);
            ui.graphDragStart.current = {
              x: event.clientX,
              y: event.clientY,
              offsetX: ui.graphOffset.x,
              offsetY: ui.graphOffset.y,
            };
            ui.setIsGraphDragging(true);
          }}
          onPointerMove={(event) => {
            if (!ui.isGraphDragging) return;
            ui.setGraphOffset({
              x:
                ui.graphDragStart.current.offsetX +
                event.clientX -
                ui.graphDragStart.current.x,
              y:
                ui.graphDragStart.current.offsetY +
                event.clientY -
                ui.graphDragStart.current.y,
            });
          }}
          onPointerUp={(event) => {
            if (event.currentTarget.hasPointerCapture(event.pointerId)) {
              event.currentTarget.releasePointerCapture(event.pointerId);
            }
            ui.setIsGraphDragging(false);
          }}
          onPointerCancel={() => ui.setIsGraphDragging(false)}
        >
          {!ui.transactionGraph && (
            <div className="graph-empty-state">
              {ui.isGraphLoading ? (
                <>
                  <i />
                  <strong>正在抓取實際鏈上交易</strong>
                  <span>取得資料後會自動生成交易關係圖譜</span>
                </>
              ) : ui.graphError ? (
                <>
                  <strong>無法生成圖譜</strong>
                  <span>{ui.graphError}</span>
                  <button
                    type="button"
                    className="graph-retry-button"
                    onClick={() => void ui.confirmInvestigationAddress()}
                    disabled={ui.isGraphLoading}
                  >
                    重新取得資料
                  </button>
                </>
              ) : (
                <>
                  <strong>尚未指定調查目標</strong>
                  <span>請在中間上方輸入 Ethereum 錢包地址並按下確認</span>
                </>
              )}
            </div>
          )}
          <div
            className="graph-scene"
            style={{
              inset: "auto",
              left: "50%",
              top: "50%",
              width: `${ui.graphSpread * 100}%`,
              height: `${ui.graphSpread * 100}%`,
              transform: `translate(-50%, -50%) translate(${ui.graphOffset.x}px, ${ui.graphOffset.y}px) scale(${ui.graphZoom})`,
              opacity: ui.transactionGraph ? 1 : 0,
            }}
          >
            <div className="graph-grid" />
            <svg
              className="graph-edges"
              viewBox="0 0 100 100"
              preserveAspectRatio="none"
              aria-hidden="true"
            >
              <defs>
                {layerColors.map((color, index) => (
                  <marker
                    key={color}
                    id={`graph-arrow-${index}`}
                    viewBox="0 0 12 12"
                    refX="8"
                    refY="6"
                    markerWidth={ui.isGraphFullscreen ? 13 : 6}
                    markerHeight={ui.isGraphFullscreen ? 13 : 6}
                    orient="auto-start-reverse"
                    overflow="visible"
                  >
                    <path d="M 0 0 L 12 6 L 0 12 z" fill={color} />
                  </marker>
                ))}
              </defs>
              {visibleGraphEdges.map((edge) => {
                const a = nodeById.get(edge.from);
                const b = nodeById.get(edge.to);
                if (!a || !b) return null;
                const arrowStart = {
                  x: a.x + (b.x - a.x) * 0.68,
                  y: a.y + (b.y - a.y) * 0.68,
                };
                const arrowEnd = {
                  x: a.x + (b.x - a.x) * 0.75,
                  y: a.y + (b.y - a.y) * 0.75,
                };
                const dateClass =
                  matchingEdges && !matchingEdges.has(edge.id)
                    ? "date-muted"
                    : "date-matched";
                const groupCenterId =
                  groupCenterByGroup.get(edge.group ?? 0) ??
                  investigationNodeId;
                const directionClass =
                  edge.to === groupCenterId
                    ? "direction-inbound"
                    : edge.from === groupCenterId
                      ? "direction-outbound"
                      : "direction-neutral";
                return (
                  <React.Fragment key={edge.id}>
                  <line
                    className={`${dateClass} ${directionClass}`}
                    x1={a.x}
                    y1={a.y}
                    x2={b.x}
                    y2={b.y}
                    vectorEffect="non-scaling-stroke"
                    style={{ stroke: layerColor(edge.group) }}
                  >
                    <title>
                      {edge.timestamp?.slice(0, 10) || "日期未知"} ·{" "}
                      {edge.value.toLocaleString()} {edge.asset}
                    </title>
                  </line>
                  <line
                    className="graph-edge-arrow"
                    x1={arrowStart.x}
                    y1={arrowStart.y}
                    x2={arrowEnd.x}
                    y2={arrowEnd.y}
                    vectorEffect="non-scaling-stroke"
                    style={{ stroke: layerColor(edge.group) }}
                    markerEnd={`url(#graph-arrow-${(edge.group ?? 0) % layerColors.length})`}
                  />
                  <line
                    className="graph-edge-hitarea"
                    x1={a.x}
                    y1={a.y}
                    x2={b.x}
                    y2={b.y}
                    vectorEffect="non-scaling-stroke"
                    onPointerDown={(event) => event.stopPropagation()}
                    onPointerMove={(event) => {
                      const canvas = ui.graphCanvasRef.current;
                      if (!canvas) return;
                      const rect = canvas.getBoundingClientRect();
                      setHoveredEdge({
                        edge,
                        x: event.clientX - rect.left + 12,
                        y: event.clientY - rect.top + 12,
                      });
                    }}
                    onMouseMove={(event) => {
                      const canvas = ui.graphCanvasRef.current;
                      if (!canvas) return;
                      const rect = canvas.getBoundingClientRect();
                      setHoveredEdge({
                        edge,
                        x: event.clientX - rect.left + 12,
                        y: event.clientY - rect.top + 12,
                      });
                    }}
                    onMouseEnter={(event) => {
                      const canvas = ui.graphCanvasRef.current;
                      if (!canvas) return;
                      const rect = canvas.getBoundingClientRect();
                      setHoveredEdge({
                        edge,
                        x: event.clientX - rect.left + 12,
                        y: event.clientY - rect.top + 12,
                      });
                    }}
                    onPointerLeave={() => setHoveredEdge(null)}
                    onMouseLeave={() => setHoveredEdge(null)}
                  />
                  </React.Fragment>
                );
              })}
            </svg>
            {!isCompactGraph && dateMarkers.map(({ edge, dates, dateLabel, x, y }) => {
              if (!visibleEdgeIds.has(edge.id)) return null;
              return (
                <span
                  key={`date-${edge.from}-${edge.to}-${edge.group ?? 0}`}
                  className={`graph-edge-date ${
                    selectedDate && !dates.includes(selectedDate)
                      ? "date-muted"
                      : ""
                  }`}
                  style={{
                    left: `${x}%`,
                    top: `${y}%`,
                    borderColor: layerColor(edge.group),
                  }}
                >
                  {dateLabel}
                </span>
              );
            })}
            {graphNodes.map((node) => visibleNodeIds.has(node.id) && (
              <div
                key={node.id}
                className={`graph-node ${node.type} ${
                  ui.selectedNodeId === node.id ? "selected" : ""
                } ${ui.expandingNodeId === node.id ? "expanding" : ""} ${
                  ui.expandedNodeIds.has(node.id) ? "expanded" : ""
                } ${
                  matchingNodes && !matchingNodes.has(node.id)
                    ? "date-muted"
                    : "date-matched"
                }`}
                style={
                  {
                    left: `${node.x}%`,
                    top: `${node.y}%`,
                    "--layer-color": layerColor(node.group),
                  } as React.CSSProperties
                }
                role="button"
                tabIndex={0}
                aria-label={`選取並查看節點 ${node.address}`}
                title="單擊查看資料，雙擊展開交易關係"
                onPointerEnter={(event) => {
                  if (!ui.isGraphFullscreen || nodeDrag.current) return;
                  const canvas = ui.graphCanvasRef.current;
                  if (!canvas) return;
                  const rect = canvas.getBoundingClientRect();
                  const cardWidth = 286;
                  const x =
                    event.clientX - rect.left + cardWidth + 24 > rect.width
                      ? event.clientX - rect.left - cardWidth - 14
                      : event.clientX - rect.left + 14;
                  setHoveredNode({
                    id: node.id,
                    x: Math.max(12, x),
                    y: Math.min(
                      rect.height - 150,
                      Math.max(12, event.clientY - rect.top - 30),
                    ),
                  });
                }}
                onPointerLeave={() => setHoveredNode(null)}
                onPointerDown={(event) => {
                  event.stopPropagation();
                  event.currentTarget.setPointerCapture(event.pointerId);
                  nodeWasDragged.current = false;
                  nodeDrag.current = {
                    id: node.id,
                    clientX: event.clientX,
                    clientY: event.clientY,
                    x: node.x,
                    y: node.y,
                  };
                }}
                onPointerMove={(event) => {
                  const drag = nodeDrag.current;
                  if (!drag || drag.id !== node.id) return;
                  const dx = event.clientX - drag.clientX;
                  const dy = event.clientY - drag.clientY;
                  if (Math.hypot(dx, dy) > 3) nodeWasDragged.current = true;
                  const canvas = ui.graphCanvasRef.current;
                  if (!canvas) return;
                  const rect = canvas.getBoundingClientRect();
                  ui.moveGraphNode(
                    node.id,
                    drag.x +
                      (dx /
                        (rect.width * ui.graphSpread * ui.graphZoom)) *
                        100,
                    drag.y +
                      (dy /
                        (rect.height * ui.graphSpread * ui.graphZoom)) *
                        100,
                  );
                }}
                onPointerUp={(event) => {
                  if (event.currentTarget.hasPointerCapture(event.pointerId)) {
                    event.currentTarget.releasePointerCapture(event.pointerId);
                  }
                  nodeDrag.current = null;
                }}
                onPointerCancel={() => {
                  nodeDrag.current = null;
                }}
                onClick={(event) => {
                  event.stopPropagation();
                  if (nodeWasDragged.current) {
                    nodeWasDragged.current = false;
                    ui.setSelectedNodeId(node.id);
                    return;
                  }
                  ui.setSelectedNodeId(node.id);
                }}
                onDoubleClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  if (!nodeWasDragged.current) {
                    void ui.expandGraphNode(node.id);
                  }
                }}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    ui.setSelectedNodeId(node.id);
                  }
                }}
              >
                <span>{node.type === "focus" ? "◎" : node.type === "contract" ? "C" : "•"}</span>
                <small>{node.label}</small>
              </div>
            ))}
            {hoveredEdge && (
              <div
                className="graph-edge-tooltip"
                style={{ left: hoveredEdge.x, top: hoveredEdge.y }}
              >
                <strong>
                  {hoveredEdge.edge.value.toLocaleString()}{" "}
                  {hoveredEdge.edge.asset}
                </strong>
                <span>
                  {hoveredEdge.edge.timestamp
                    ? new Date(hoveredEdge.edge.timestamp).toLocaleString(
                        "zh-TW",
                      )
                    : "日期未提供"}
                </span>
                <small>{hoveredEdge.edge.id}</small>
              </div>
            )}
          </div>
          {ui.isGraphFullscreen && hoverNode && hoveredNode && (
            <div
              className="graph-node-popover"
              style={{ left: hoveredNode.x, top: hoveredNode.y }}
            >
              <span className="eyebrow">節點預覽</span>
              <strong>{hoverNode.label}</strong>
              <small>{hoverNode.address}</small>
              <div className="graph-node-popover-tags">
                <span>{hoverNode.type === "contract" ? "合約" : "錢包"}</span>
                <span>{hoverEdges.length} 筆關聯交易</span>
                <span className="flow-in">
                  轉入 {summarizeFlow(hoverIncomingEdges)}
                </span>
                <span className="flow-out">
                  轉出 {summarizeFlow(hoverOutgoingEdges)}
                </span>
              </div>
              <p>
                轉入 {hoverIncomingEdges.length} 筆 · 轉出{" "}
                {hoverOutgoingEdges.length} 筆 · 交易對手：
                {hoverCounterparties} 個節點
              </p>
            </div>
          )}
          {ui.transactionGraph && (
            <div className="graph-layer-legend">
              {graphGroups.map((group) => (
                <span key={group}>
                  <i style={{ background: layerColor(group) }} />
                  {group === 0
                    ? "A 起始交易"
                    : `${String.fromCharCode(65 + Math.min(group, 25))} 第 ${group} 次展開`}
                </span>
              ))}
              <span className="graph-direction-key">
                <b className="graph-direction-line direction-outbound" />
                實線＝由目標轉出
              </span>
              <span className="graph-direction-key">
                <b className="graph-direction-line direction-inbound" />
                虛線＝轉入目標
              </span>
            </div>
          )}
          {ui.transactionGraph && <div className="graph-legend">
            <span>
              <i className="legend-high" /> 調查目標
            </span>
            <span>
              <i className="legend-medium" /> 合約
            </span>
            <span>
              <i className="legend-normal" /> 錢包
            </span>
          </div>}
          <button
            type="button"
            className="graph-fullscreen-button"
            onPointerDown={(event) => event.stopPropagation()}
            onPointerUp={(event) => event.stopPropagation()}
            onClick={() => void ui.toggleGraphFullscreen()}
            aria-label={ui.isGraphFullscreen ? "退出全螢幕圖譜" : "全螢幕顯示圖譜"}
            title={ui.isGraphFullscreen ? "退出全螢幕（Esc）" : "全螢幕顯示圖譜"}
          >
            {ui.isGraphFullscreen ? "↙" : "⛶"}
          </button>
        </div>

        {ui.isGraphFullscreen && (
          <aside className="fullscreen-analysis-dock">
            <div className="fullscreen-dock-heading">
              <div>
                <span className="eyebrow">NODE ANALYSIS</span>
                <h3>節點分析</h3>
              </div>
              <small>點選圖譜節點查看</small>
            </div>

            <div className="fullscreen-dock-card selected-node-dock">
              <span className="eyebrow">SELECTED NODE</span>
              {selectedNode ? (
                <>
                  <h4>{selectedNode.label}</h4>
                      <button
                        className="dock-address"
                        onClick={() => void copyNodeAddress(selectedNode.address)}
                        title="複製地址"
                      >
                        {selectedNode.address}
                        <span>
                          {copyFeedback?.address === selectedNode.address &&
                          copyFeedback.status === "success"
                            ? "已複製"
                            : "複製"}
                        </span>
                  </button>
                  {!ui.expandedNodeIds.has(selectedNode.id) && (
                    <button
                      className="expand-node-button"
                      onClick={() => void ui.expandGraphNode(selectedNode.id)}
                      disabled={ui.expandingNodeId !== null}
                    >
                      {ui.expandingNodeId === selectedNode.id
                        ? "正在抓取並展開..."
                        : "展開此節點的交易關係"}
                      <span>↗</span>
                    </button>
                  )}
                  <div className="dock-stat-grid">
                    <div>
                      <small>關聯交易</small>
                      <strong>{selectedEdges.length}</strong>
                    </div>
                    <div>
                      <small>轉入</small>
                      <strong>
                        {incomingEdges
                          .reduce((sum, edge) => sum + edge.value, 0)
                          .toLocaleString(undefined, {
                            maximumFractionDigits: 4,
                          })}
                      </strong>
                    </div>
                    <div>
                      <small>轉出</small>
                      <strong>
                        {outgoingEdges
                          .reduce((sum, edge) => sum + edge.value, 0)
                          .toLocaleString(undefined, {
                            maximumFractionDigits: 4,
                          })}
                      </strong>
                    </div>
                    <div>
                      <small>交易對手</small>
                      <strong>{counterparties}</strong>
                    </div>
                  </div>
                  <div
                    className={`dock-safety ${
                      selectedAssessment
                        ? selectedAssessment.level
                        : "pending"
                    }`}
                  >
                    <span>安全判讀</span>
                    <strong>
                      {selectedAssessment
                        ? `${selectedAssessment.score}/100`
                        : "尚未評估"}
                    </strong>
                    <p>
                      {selectedAssessment
                        ? selectedAssessment.summary
                        : "此處僅顯示後端模型回傳的節點風險，目前尚無判讀資料。"}
                    </p>
                  </div>
                </>
              ) : (
                <p className="dock-empty">
                  將滑鼠移到節點可先查看摘要；點選節點後，詳細資料會顯示在這裡。
                </p>
              )}
            </div>

            <div className="fullscreen-dock-card anomaly-dock">
              <span className="eyebrow">ANOMALY SCORE</span>
              <div>
                <strong className={`risk-text risk-${anomalyTone}`}>
                  {ui.anomalyAnalysis?.score ?? "—"}
                </strong>
                <h4>異常分析摘要</h4>
              </div>
              <p>
                {ui.isAnalysisLoading
                  ? "正在等待異常分析結果..."
                  : ui.anomalyAnalysis?.summary ||
                    "交易資料已取得，等待異常分析後端接口回傳。"}
              </p>
            </div>

            <div className="fullscreen-dock-card ai-dock">
              <div className="fullscreen-ai-heading">
                <div>
                  <span>✦</span>
                  <h4>AI 調查判讀</h4>
                </div>
                <button
                  className="pdf-export-button"
                  onClick={() => void ui.exportAnalysisPdf()}
                  disabled={ui.isExportingPdf || !ui.transactionGraph}
                >
                  ↓ PDF
                </button>
              </div>
              <p>
                {ui.isAnalysisLoading
                  ? "正在分析交易資料..."
                  : ui.anomalyAnalysis?.interpretation ||
                    "取得交易資料並完成模型分析後顯示。"}
              </p>
            </div>
          </aside>
        )}

        {selectedNode && (
          <div className="node-inspector">
            <div className="node-inspector-heading">
              <div>
                <span className="eyebrow">SELECTED NODE</span>
                <h4>{selectedNode.label}</h4>
              </div>
              <span className={`node-kind ${selectedNode.type}`}>
                {ui.expandingNodeId === selectedNode.id
                  ? "展開中…"
                  : ui.expandedNodeIds.has(selectedNode.id)
                    ? "已展開"
                    : "點擊展開"}
              </span>
            </div>
                <button
                  className="node-address"
                  title="複製完整地址"
                  onClick={() => void copyNodeAddress(selectedNode.address)}
                >
                  {selectedNode.address}
                  <span>
                    {copyFeedback?.address === selectedNode.address &&
                    copyFeedback.status === "success"
                      ? "已複製"
                      : "複製"}
                  </span>
                </button>
            {!ui.expandedNodeIds.has(selectedNode.id) && (
              <button
                className="expand-node-button"
                onClick={() => void ui.expandGraphNode(selectedNode.id)}
                disabled={ui.expandingNodeId !== null}
              >
                {ui.expandingNodeId === selectedNode.id
                  ? "正在抓取並展開…"
                  : "展開此節點的交易關係"}
                <span>↗</span>
              </button>
            )}
            <div className="node-stat-grid">
              <div>
                <small>圖中交易</small>
                <strong>{selectedEdges.length}</strong>
              </div>
              <div>
                <small>轉入</small>
                <strong>
                  {incomingEdges
                    .reduce((sum, edge) => sum + edge.value, 0)
                    .toLocaleString(undefined, { maximumFractionDigits: 6 })}
                </strong>
                <span>ETH · {incomingEdges.length} 筆</span>
              </div>
              <div>
                <small>轉出</small>
                <strong>
                  {outgoingEdges
                    .reduce((sum, edge) => sum + edge.value, 0)
                    .toLocaleString(undefined, { maximumFractionDigits: 6 })}
                </strong>
                <span>ETH · {outgoingEdges.length} 筆</span>
              </div>
              <div>
                <small>交易對手</small>
                <strong>{counterparties}</strong>
                <span>個節點</span>
              </div>
            </div>
            <div
              className={`node-safety ${
                selectedAssessment ? selectedAssessment.level : "pending"
              }`}
            >
              <span>安全判讀</span>
              <strong>
                {selectedAssessment
                  ? `${selectedAssessment.score}/100 · ${
                      selectedAssessment.level === "critical"
                        ? "極高風險"
                        : selectedAssessment.level === "high"
                          ? "高風險"
                          : selectedAssessment.level === "medium"
                            ? "中風險"
                            : "低風險"
                    }`
                  : "尚未評估"}
              </strong>
              <small>
                {selectedAssessment
                  ? `${selectedAssessment.summary}（來源：${selectedAssessment.source}）`
                  : "此處只顯示真實模型回傳的節點風險；目前後端尚未提供節點安全判讀。"}
              </small>
            </div>
            {ui.expansionError && (
              <div className="node-expansion-error">{ui.expansionError}</div>
            )}
          </div>
        )}
      </section>

      <section className="risk-card">
        <div className="risk-score">
          <div
            className={`score-ring risk-${anomalyTone}`}
            style={
              {
                "--score": `${ui.anomalyAnalysis?.score || 0}%`,
              } as React.CSSProperties
            }
          >
            <span>{ui.anomalyAnalysis?.score ?? "—"}</span>
            <small>
              {ui.anomalyAnalysis
                ? ui.anomalyAnalysis.level === "critical"
                  ? "極高風險"
                  : ui.anomalyAnalysis.level === "high"
                    ? "高風險"
                    : ui.anomalyAnalysis.level === "medium"
                      ? "中風險"
                      : "低風險"
                : "待分析"}
            </small>
          </div>
          <div>
            <span className="eyebrow">ANOMALY SCORE</span>
            <h3>異常分析摘要</h3>
            <p>
              {ui.isAnalysisLoading
                ? "交易圖譜已取得，正在等待異常模型完成判讀…"
                : ui.anomalyAnalysis
                  ? ui.anomalyAnalysis.summary
                  : ui.analysisError ||
                    (ui.transactionGraph
                      ? "交易圖譜已取得，尚未產生異常分析結果。"
                      : "輸入調查地址並取得交易資料後，才會開始異常分析。")}
            </p>
          </div>
        </div>

        <div className="risk-scale" aria-label="風險分數區間">
          <span className="risk-safe">0–29 安全</span>
          <span className="risk-caution">30–69 注意</span>
          <span className="risk-danger">70–100 危險</span>
        </div>

        {ui.anomalyAnalysis && ui.anomalyAnalysis.reasons.length > 0 && (
          <div className="risk-reasons">
            {ui.anomalyAnalysis.reasons.map((reason) => (
              <div key={reason.code}>
                <span className="reason-icon">◇</span>
                <div>
                  <strong>{reason.title}</strong>
                  <small>{reason.description}</small>
                </div>
                {reason.contribution !== null && (
                  <b>
                    {reason.contribution > 0 ? "+" : ""}
                    {reason.contribution}
                  </b>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="insight-card">
        <div className="insight-heading">
          <span>✦</span>
            <div>
              <h3>AI 調查判讀</h3>
              <small>
                {ui.anomalyAnalysis
                  ? `來源：${ui.anomalyAnalysis.source}`
                  : "取得交易資料並完成模型分析後顯示"}
              </small>
            </div>
          <button
            className="pdf-export-button"
            onClick={() => void ui.exportAnalysisPdf()}
            title="下載 PDF 報告"
            disabled={ui.isExportingPdf || !ui.transactionGraph}
          >
            {ui.isExportingPdf ? "產生中…" : "↓ PDF"}
          </button>
        </div>
        <p>
          {ui.isAnalysisLoading
            ? "正在產生可解釋的調查判讀…"
            : ui.anomalyAnalysis
              ? ui.anomalyAnalysis.interpretation
              : ui.analysisError ||
                "目前沒有模型判讀結果，不會顯示預設或模擬內容。"}
        </p>
        {ui.anomalyAnalysis?.recommendations.map((recommendation) => (
          <button
            key={recommendation}
            onClick={() => ui.runInvestigation(recommendation)}
          >
            延伸追蹤建議
            <span>{recommendation} →</span>
          </button>
        ))}
          </section>
    </aside>
    {copyFeedbackToast}
    </>
  );
}

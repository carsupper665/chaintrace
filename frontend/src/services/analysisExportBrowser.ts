import { formatExactAmount } from "@/src/middle/investigation-client";
import type {
  CurrentAnalysisResult,
  Investigation,
} from "@/src/middle/investigation-contract";
import {
  getTransactionGraphPage,
  TransactionGraphClientError,
} from "@/src/middle/transaction-graph-client";
import {
  TRANSACTION_GRAPH_PAGE_SIZE,
  type SavedTransactionGraphEdge,
  type SavedTransactionGraphNode,
  type TransactionGraphResponse,
} from "@/src/middle/transaction-graph-contract";
import {
  downloadAnalysisPdf,
  type AnalysisPdfReport,
} from "@/src/services/downloadAnalysisPdf";

type Fetch = typeof fetch;

export type AnalysisExportInput = {
  investigation: Pick<
    Investigation,
    "id" | "title" | "address" | "network" | "currentResult"
  >;
  result: CurrentAnalysisResult;
  graphView: Pick<
    TransactionGraphResponse,
    "datasetId" | "nodes" | "edges"
  > | null;
  selectedNodeId: string | null;
};

type CompleteDatasetGraph = {
  nodes: SavedTransactionGraphNode[];
  edges: SavedTransactionGraphEdge[];
};

type AnalysisExporterOptions = {
  fetchImpl?: Fetch;
  pageSize?: number;
  downloadBlob?: (blob: Blob, filename: string) => void;
  downloadPdf?: (report: AnalysisPdfReport) => Promise<void>;
};

export class AnalysisExportError extends Error {
  readonly code: string;

  constructor(message: string, code: string) {
    super(message);
    this.name = "AnalysisExportError";
    this.code = code;
  }
}

function browserDownloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

function snapshotInput(input: AnalysisExportInput): AnalysisExportInput {
  return structuredClone(input);
}

function invalidPagination() {
  return new AnalysisExportError(
    "交易圖譜分頁狀態異常，未產生匯出檔案。",
    "invalid_graph_pagination",
  );
}

async function loadCompleteDatasetGraph(
  snapshot: AnalysisExportInput,
  fetchImpl: Fetch,
  pageSize: number,
): Promise<CompleteDatasetGraph> {
  const datasetId = snapshot.result.dataset.id;
  const nodes = new Map<string, SavedTransactionGraphNode>();
  const edges = new Map<string, SavedTransactionGraphEdge>();
  const requestedCursors = new Set<string>();
  const maximumPages =
    Math.ceil(snapshot.result.dataset.transferLimit / Math.max(1, pageSize)) + 2;
  let pageCount = 0;
  let cursor: string | undefined;

  while (true) {
    pageCount += 1;
    if (pageCount > maximumPages) throw invalidPagination();
    if (cursor) {
      if (requestedCursors.has(cursor)) throw invalidPagination();
      requestedCursors.add(cursor);
    }
    const page = await getTransactionGraphPage(
      snapshot.investigation.id,
      { datasetId, cursor, pageSize },
      fetchImpl,
    );
    if (page.datasetId !== datasetId) {
      throw new TransactionGraphClientError(
        "分析資料集已更新，請重新載入目前結果。",
        409,
        "stale_dataset",
      );
    }
    for (const node of page.nodes) {
      if (!nodes.has(node.id)) nodes.set(node.id, node);
    }
    for (const edge of page.edges) {
      if (!edges.has(edge.id)) edges.set(edge.id, edge);
    }
    if (!page.hasMore) break;
    if (!page.nextCursor || requestedCursors.has(page.nextCursor)) {
      throw invalidPagination();
    }
    cursor = page.nextCursor;
  }

  if (
    edges.size !== snapshot.result.metrics.transferCount ||
    edges.size !== snapshot.result.dataset.collectedTransfers
  ) {
    throw new AnalysisExportError(
      "交易圖譜頁面不完整，未產生匯出檔案。",
      "incomplete_graph_export",
    );
  }
  return { nodes: [...nodes.values()], edges: [...edges.values()] };
}

function csvCell(value: string | number | boolean | null, exactText = false) {
  let text = String(value ?? "");
  if (
    exactText ||
    /^[\u0000-\u0020]*[=+\-@]/.test(text) ||
    /^[\t\r\n]/.test(text)
  ) {
    text = `'${text}`;
  }
  return `"${text.replaceAll('"', '""')}"`;
}

function createCsv(snapshot: AnalysisExportInput, graph: CompleteDatasetGraph) {
  const { dataset, assessment } = snapshot.result;
  const headers = [
    "dataset_id",
    "result_id",
    "transaction_hash",
    "event_identity",
    "timestamp",
    "from",
    "to",
    "asset",
    "amount_smallest_unit",
    "decimals",
    "amount_formatted",
    "partial",
    "confidence",
    "stop_reason",
    "network",
    "target_address",
    "window_start",
    "window_end",
    "transfer_limit",
    "traversal_depth",
    "collected_transfers",
    "reached_depth",
    "evaluator_source",
  ];
  const rows = graph.edges.map((edge) => {
    const values: Array<[string | number | boolean | null, boolean?]> = [
      [dataset.id],
      [snapshot.investigation.currentResult],
      [edge.transactionHash],
      [edge.eventIdentity],
      [edge.timestamp],
      [edge.from],
      [edge.to],
      [edge.amount.asset],
      [edge.amount.smallestUnit, true],
      [edge.amount.decimals],
      [formatExactAmount(edge.amount), true],
      [dataset.partial],
      [dataset.confidence],
      [dataset.stopReason],
      [snapshot.investigation.network],
      [snapshot.investigation.address],
      [dataset.windowStart],
      [dataset.windowEnd],
      [dataset.transferLimit],
      [dataset.traversalDepth],
      [dataset.collectedTransfers],
      [dataset.reachedDepth],
      [assessment.source],
    ];
    return values.map(([value, exact]) => csvCell(value, exact)).join(",");
  });
  return `\uFEFF${headers.map((header) => csvCell(header)).join(",")}\r\n${rows.join("\r\n")}`;
}

function safeFilenamePart(value: string) {
  return value.replace(/[^a-zA-Z0-9._-]+/g, "-").replace(/^-+|-+$/g, "") || "export";
}

export function createAnalysisExporter(options: AnalysisExporterOptions = {}) {
  const fetchImpl = options.fetchImpl ?? fetch;
  const pageSize = options.pageSize ?? TRANSACTION_GRAPH_PAGE_SIZE;
  const downloadBlob = options.downloadBlob ?? browserDownloadBlob;
  const downloadPdf = options.downloadPdf ?? downloadAnalysisPdf;

  return {
    async exportCsv(input: AnalysisExportInput) {
      const snapshot = snapshotInput(input);
      if (!snapshot.investigation.currentResult) {
        throw new AnalysisExportError(
          "目前沒有可匯出的穩定分析結果。",
          "analysis_result_unavailable",
        );
      }
      const graph = await loadCompleteDatasetGraph(
        snapshot,
        fetchImpl,
        pageSize,
      );
      const csv = createCsv(snapshot, graph);
      downloadBlob(
        new Blob([csv], { type: "text/csv;charset=utf-8" }),
        `chaintrace-${safeFilenamePart(snapshot.investigation.id)}-${safeFilenamePart(snapshot.result.dataset.id)}.csv`,
      );
    },
    async exportPdf(input: AnalysisExportInput) {
      const snapshot = snapshotInput(input);
      if (!snapshot.investigation.currentResult) {
        throw new AnalysisExportError(
          "目前沒有可匯出的穩定分析結果。",
          "analysis_result_unavailable",
        );
      }
      const graph = await loadCompleteDatasetGraph(
        snapshot,
        fetchImpl,
        pageSize,
      );
      const selectedNode =
        snapshot.graphView?.datasetId === snapshot.result.dataset.id
          ? snapshot.graphView.nodes.find(
              (node) => node.id === snapshot.selectedNodeId,
            ) ?? null
          : null;
      await downloadPdf({
        investigation: snapshot.investigation,
        result: snapshot.result,
        graph,
        selectedNode,
      });
    },
  };
}

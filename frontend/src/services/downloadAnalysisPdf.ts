import type {
  CurrentAnalysisResult,
  Investigation,
} from "@/src/middle/investigation-contract";
import { formatExactAmount } from "@/src/middle/investigation-client";
import type {
  SavedTransactionGraphEdge,
  SavedTransactionGraphNode,
  TransactionGraphNode,
} from "@/src/middle/transaction-graph-contract";

type Fetch = typeof fetch;
type JsPdfModule = Pick<typeof import("jspdf"), "jsPDF">;

export type AnalysisPdfReport = {
  investigation: Pick<
    Investigation,
    "id" | "title" | "address" | "network" | "currentResult"
  >;
  result: CurrentAnalysisResult;
  graph: {
    nodes: SavedTransactionGraphNode[];
    edges: SavedTransactionGraphEdge[];
  };
  selectedNode: TransactionGraphNode | null;
};

export type AnalysisPdfRuntime = {
  fetchImpl?: Fetch;
  loadJsPdf?: () => Promise<JsPdfModule>;
};

function arrayBufferToBase64(buffer: ArrayBuffer) {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  const chunkSize = 0x8000;
  for (let index = 0; index < bytes.length; index += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
  }
  return globalThis.btoa(binary);
}

function safeFilenamePart(value: string) {
  return value.replace(/[^a-zA-Z0-9._-]+/g, "-").replace(/^-+|-+$/g, "") || "report";
}

export async function downloadAnalysisPdf(
  report: AnalysisPdfReport,
  runtime: AnalysisPdfRuntime = {},
) {
  const fetchImpl = runtime.fetchImpl ?? fetch;
  const loadJsPdf = runtime.loadJsPdf ?? (() => import("jspdf"));
  const { jsPDF } = await loadJsPdf();
  const fontResponse = await fetchImpl("/fonts/NotoSansTC.ttf");
  if (!fontResponse.ok) throw new Error("Unable to load PDF font");
  const fontBase64 = arrayBufferToBase64(await fontResponse.arrayBuffer());

  const pdf = new jsPDF({
    orientation: "portrait",
    unit: "mm",
    format: "a4",
    compress: true,
  });
  pdf.addFileToVFS("NotoSansTC.ttf", fontBase64);
  pdf.addFont("NotoSansTC.ttf", "NotoSansTC", "normal");
  pdf.setFont("NotoSansTC", "normal");

  const pageWidth = pdf.internal.pageSize.getWidth();
  const pageHeight = pdf.internal.pageSize.getHeight();
  const margin = 16;
  const contentWidth = pageWidth - margin * 2;
  let y = 18;

  const addPage = () => {
    pdf.addPage();
    pdf.setFont("NotoSansTC", "normal");
    y = 18;
  };
  const ensureSpace = (height: number) => {
    if (y + height > pageHeight - 18) addPage();
  };
  const addText = (
    text: string,
    options: { size?: number; color?: [number, number, number]; gap?: number } = {},
  ) => {
    const size = options.size ?? 10;
    const gap = options.gap ?? 2;
    pdf.setFontSize(size);
    pdf.setTextColor(...(options.color ?? [47, 58, 52]));
    const lines = pdf.splitTextToSize(text || "-", contentWidth) as string[];
    const lineHeight = size * 0.42 + 1.3;
    ensureSpace(lines.length * lineHeight + gap);
    pdf.text(lines, margin, y);
    y += lines.length * lineHeight + gap;
  };
  const addSection = (title: string) => {
    ensureSpace(14);
    y += 6;
    pdf.setFillColor(36, 201, 120);
    pdf.roundedRect(margin, y - 5.2, 1.3, 6.4, 0.6, 0.6, "F");
    pdf.setFontSize(14);
    pdf.setTextColor(18, 117, 70);
    pdf.text(title, margin + 4.5, y);
    y += 9;
  };
  const addKeyValue = (label: string, value: string) => {
    addText(`${label}: ${value}`, { size: 10, gap: 1.5 });
  };

  const { assessment, dataset, metrics } = report.result;
  const isZeroEvidence =
    dataset.collectedTransfers === 0 || assessment.score === null;

  pdf.setFillColor(14, 80, 51);
  pdf.rect(0, 0, pageWidth, 38, "F");
  pdf.setTextColor(255, 255, 255);
  pdf.setFontSize(20);
  pdf.text("ChainTrace 區塊鏈調查報告", margin, 17);
  pdf.setFontSize(9);
  pdf.text(
    `Investigation ${report.investigation.id} / ${report.investigation.title}`,
    margin,
    27,
  );
  y = 48;

  addSection("一、調查與結果識別");
  addKeyValue("調查目標", report.investigation.address || "尚未指定");
  addKeyValue("網路", report.investigation.network);
  addKeyValue("Result ID", report.investigation.currentResult || "未提供");
  addKeyValue("Dataset ID", dataset.id);
  addKeyValue("Evaluator source", assessment.source);
  addKeyValue("Result updated at", assessment.updatedAt);

  addSection("二、有界證據與 Coverage");
  addText("本報告只描述指定 Analysis Window 與 scope 內已收集的有界證據，不代表完整鏈上歷史。");
  addKeyValue("Analysis Window", `${dataset.windowStart} 至 ${dataset.windowEnd}`);
  addKeyValue(
    "Requested scope",
    `Transfer 上限 ${dataset.transferLimit} / Traversal depth ${dataset.traversalDepth}`,
  );
  addKeyValue(
    "Collected coverage",
    `${dataset.collectedTransfers} / ${dataset.transferLimit} Transfer；到達深度 ${dataset.reachedDepth} / ${dataset.traversalDepth}`,
  );
  addKeyValue("Confidence", `${dataset.confidence}%`);
  addKeyValue("Stop reason", dataset.stopReason);
  if (isZeroEvidence) {
    addText("證據不足（zero evidence / insufficient evidence）：未提供風險分數或安全結論。", {
      color: [163, 92, 34],
    });
  } else if (dataset.partial) {
    addText("部分分析（Partial Analysis）：證據收集中途停止，評估只適用於已收集資料。", {
      color: [163, 92, 34],
    });
  } else {
    addText("正常完成的有界分析；停止原因與 scope 仍以上述 Coverage 為準。");
  }

  addSection("三、Authoritative Metrics");
  addKeyValue("關聯地址", String(metrics.relatedNodes));
  addKeyValue("Transfer events", String(metrics.transferCount));
  addKeyValue(
    "總資金流",
    `${formatExactAmount(metrics.totalFlow)} ${metrics.totalFlow.asset}`,
  );

  addSection("四、確定性規則評估");
  addKeyValue("Evaluator source", assessment.source);
  if (assessment.score === null) {
    addKeyValue("風險評估", "證據不足，不提供風險分數或風險等級");
  } else {
    addKeyValue("風險分數", `${assessment.score}/100`);
    addKeyValue("風險等級", assessment.level || "未提供");
  }
  addKeyValue(
    "命中原因",
    assessment.reasons.length > 0 ? assessment.reasons.join("、") : "無",
  );
  addText("本節只列示後端 evaluator 的確定性規則輸出，不加入額外判讀或建議。");

  addSection("五、Dataset Graph Summary");
  addKeyValue("Dataset 節點", String(report.graph.nodes.length));
  addKeyValue("Dataset Transfer edges", String(report.graph.edges.length));
  if (report.selectedNode) {
    addKeyValue("Selected node label", report.selectedNode.label);
    addKeyValue("Selected node address", report.selectedNode.address);
    addKeyValue("Selected node type", report.selectedNode.type);
  } else {
    addText("目前 graph view 未選定節點。");
  }

  const pageCount = pdf.getNumberOfPages();
  for (let page = 1; page <= pageCount; page += 1) {
    pdf.setPage(page);
    pdf.setFont("NotoSansTC", "normal");
    pdf.setFontSize(8);
    pdf.setTextColor(126, 139, 131);
    pdf.text(
      `ChainTrace / ${dataset.id} / 第 ${page} / ${pageCount} 頁`,
      pageWidth / 2,
      pageHeight - 8,
      { align: "center" },
    );
  }

  pdf.save(
    `chaintrace-${safeFilenamePart(report.investigation.id)}-${safeFilenamePart(dataset.id)}.pdf`,
  );
}

import type { AnomalyAnalysisResponse } from "@/src/middle/anomaly-analysis-contract";
import type {
  TransactionGraphNode,
  TransactionGraphResponse,
} from "@/src/middle/transaction-graph-contract";

export type AnalysisPdfReport = {
  caseId: string;
  caseTitle: string;
  address: string;
  network: string;
  graph: TransactionGraphResponse;
  selectedNode: TransactionGraphNode | null;
  anomalyAnalysis: AnomalyAnalysisResponse | null;
};

function arrayBufferToBase64(buffer: ArrayBuffer) {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  const chunkSize = 0x8000;
  for (let index = 0; index < bytes.length; index += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
  }
  return window.btoa(binary);
}

export async function downloadAnalysisPdf(report: AnalysisPdfReport) {
  const { jsPDF } = await import("jspdf");
  const fontResponse = await fetch("/fonts/NotoSansTC.ttf");
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
    const lines = pdf.splitTextToSize(text || "—", contentWidth) as string[];
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
    addText(`${label}：${value}`, { size: 10, gap: 1.5 });
  };

  pdf.setFillColor(14, 80, 51);
  pdf.rect(0, 0, pageWidth, 38, "F");
  pdf.setTextColor(255, 255, 255);
  pdf.setFontSize(20);
  pdf.text("ChainTrace 區塊鏈調查報告", margin, 17);
  pdf.setFontSize(9);
  pdf.text(`案件 ${report.caseId} · ${report.caseTitle}`, margin, 27);
  y = 48;

  addSection("一、案件資訊");
  addKeyValue("調查目標", report.address || "尚未指定");
  addKeyValue("網路", report.network);
  addKeyValue("報告產生時間", new Date().toLocaleString("zh-TW"));
  addKeyValue("資料來源", report.graph.source);
  addKeyValue("資料更新時間", report.graph.updatedAt);

  addSection("二、交易圖譜摘要");
  addKeyValue("節點數", String(report.graph.nodes.length));
  addKeyValue("交易數", String(report.graph.transactionCount));
  addKeyValue(
    "總金流",
    `${report.graph.totalFlow.toLocaleString(undefined, {
      maximumFractionDigits: 8,
    })} ${report.graph.flowAsset}`,
  );
  const dateCounts = new Map<string, number>();
  for (const edge of report.graph.edges) {
    const date = edge.timestamp?.slice(0, 10) || "日期未知";
    dateCounts.set(date, (dateCounts.get(date) || 0) + 1);
  }
  addText(
    [...dateCounts.entries()]
      .sort(([a], [b]) => b.localeCompare(a))
      .map(([date, count]) => `${date}：${count} 筆`)
      .join("\n") || "沒有可用的交易日期",
  );

  addSection("三、目前選定節點");
  if (report.selectedNode) {
    addKeyValue("標籤", report.selectedNode.label);
    addKeyValue("地址", report.selectedNode.address);
    addKeyValue(
      "類型",
      report.selectedNode.type === "contract" ? "智能合約" : "錢包",
    );
    addKeyValue("追蹤層級", String(report.selectedNode.group ?? 0));
  } else {
    addText("尚未選定節點。");
  }

  addSection("四、異常分析摘要");
  if (report.anomalyAnalysis) {
    addKeyValue("異常分數", `${report.anomalyAnalysis.score}/100`);
    addKeyValue("風險等級", report.anomalyAnalysis.level);
    addText(report.anomalyAnalysis.summary);
    if (report.anomalyAnalysis.reasons.length > 0) {
      addText(
        report.anomalyAnalysis.reasons
          .map(
            (reason, index) =>
              `${index + 1}. ${reason.title}\n${reason.description}`,
          )
          .join("\n"),
      );
    }
  } else {
    addText("交易資料已取得，異常分析結果尚待後端模型回傳。");
  }

  addSection("五、AI 調查判讀");
  if (report.anomalyAnalysis) {
    addText(report.anomalyAnalysis.interpretation);
    if (report.anomalyAnalysis.recommendations.length > 0) {
      addText(
        `建議後續調查：\n${report.anomalyAnalysis.recommendations
          .map((item, index) => `${index + 1}. ${item}`)
          .join("\n")}`,
      );
    }
  } else {
    addText("尚待 AI Agent／異常分析後端完成判讀。本報告未產生模擬結論。");
  }

  const pageCount = pdf.getNumberOfPages();
  for (let page = 1; page <= pageCount; page += 1) {
    pdf.setPage(page);
    pdf.setFont("NotoSansTC", "normal");
    pdf.setFontSize(8);
    pdf.setTextColor(126, 139, 131);
    pdf.text(
      `ChainTrace · ${report.caseId} · 第 ${page} / ${pageCount} 頁`,
      pageWidth / 2,
      pageHeight - 8,
      { align: "center" },
    );
  }

  pdf.save(`ChainTrace-${report.caseId}-analysis.pdf`);
}

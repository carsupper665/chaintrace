import assert from "node:assert/strict";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const targetAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE";

function analysisResult(overrides = {}) {
  const base = {
    dataset: {
      id: "dataset-10",
      network: "TRON_MAINNET",
      asset: "USDT",
      windowStart: "2026-07-05T02:00:00Z",
      windowEnd: "2026-08-04T02:00:00Z",
      transferLimit: 500,
      traversalDepth: 2,
      collectedTransfers: 3,
      reachedDepth: 2,
      partial: false,
      confidence: 100,
      stopReason: "source_exhausted",
      createdAt: "2026-08-04T02:00:00Z",
    },
    metrics: {
      relatedNodes: 3,
      transferCount: 3,
      totalFlow: {
        smallestUnit: "9007199254740993123456791",
        decimals: 6,
        asset: "USDT",
      },
    },
    assessment: {
      score: 25,
      level: "medium",
      reasons: ["rapid_forwarding"],
      nodeAssessments: [],
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

function transfer(
  id,
  eventIdentity,
  smallestUnit,
  overrides = {},
) {
  return {
    id,
    transactionHash: "shared-transaction",
    eventIdentity,
    from: targetAddress,
    to: "TPeer11111111111111111111111111111",
    amount: { smallestUnit, decimals: 6, asset: "USDT" },
    timestamp: "2026-08-04T02:00:00Z",
    ...overrides,
  };
}

function graphPage(overrides = {}) {
  return {
    datasetId: "dataset-10",
    nodes: [
      { id: targetAddress, address: targetAddress, type: "focus" },
      {
        id: "TPeer11111111111111111111111111111",
        address: "TPeer11111111111111111111111111111",
        type: "normal",
      },
    ],
    edges: [],
    nextCursor: null,
    hasMore: false,
    ...overrides,
  };
}

function exportInput(overrides = {}) {
  const result = overrides.result || analysisResult();
  return {
    investigation: {
      id: "inv-10",
      title: "CSV, formula-safe investigation",
      address: targetAddress,
      network: "TRON_MAINNET",
      currentResult: "result-10",
    },
    result,
    graphView: {
      datasetId: result.dataset.id,
      nodes: [],
      edges: [
        // This deliberately represents only a visible/filter subset.
        transfer("visible-only", "visible-only", "42"),
      ],
    },
    selectedNodeId: null,
    ...overrides,
  };
}

test("CSV export walks every fixed-Dataset page, ignores the visible subset, and preserves exact Transfer events", async () => {
  const { createAnalysisExporter } = await import(
    "../src/services/analysisExportBrowser.ts"
  );
  const requests = [];
  const downloads = [];
  const firstEvent = transfer(
    "edge-1",
    "log-0",
    "9007199254740993123456789",
  );
  const secondEvent = transfer("edge-2", "log-1", "1");
  const thirdEvent = transfer("edge-3", "log-2", "1", {
    transactionHash: "=HYPERLINK(\"https://attacker.invalid\")",
    from: "+formula-sender",
    to: "@formula-recipient",
    timestamp: "2026-08-04T02:00:02Z",
  });
  const fetchImpl = async (input) => {
    const url = new URL(String(input), "http://browser.test");
    requests.push(url);
    if (url.searchParams.get("cursor") === "cursor/2") {
      return Response.json(
        graphPage({
          edges: [firstEvent, thirdEvent],
        }),
      );
    }
    return Response.json(
      graphPage({
        edges: [firstEvent, secondEvent],
        nextCursor: "cursor/2",
        hasMore: true,
      }),
    );
  };
  const exporter = createAnalysisExporter({
    fetchImpl,
    downloadBlob(blob, filename) {
      downloads.push({ blob, filename });
    },
  });

  await exporter.exportCsv(exportInput());

  assert.deepEqual(
    requests.map((url) => [
      url.pathname,
      url.searchParams.get("datasetId"),
      url.searchParams.get("cursor"),
    ]),
    [
      ["/api/investigations/inv-10/graph", "dataset-10", null],
      ["/api/investigations/inv-10/graph", "dataset-10", "cursor/2"],
    ],
  );
  assert.equal(downloads.length, 1);
  assert.ok(downloads[0].blob instanceof Blob);
  assert.match(downloads[0].blob.type, /^text\/csv/);
  assert.deepEqual(
    [...new Uint8Array(await downloads[0].blob.arrayBuffer()).slice(0, 3)],
    [0xef, 0xbb, 0xbf],
  );
  const csv = await downloads[0].blob.text();
  assert.match(
    csv,
    /^"dataset_id","result_id","transaction_hash","event_identity","timestamp","from","to","asset","amount_smallest_unit","decimals","amount_formatted","partial","confidence","stop_reason"/,
  );
  assert.equal(csv.split("\r\n").length, 4);
  assert.match(csv, /"log-0"/);
  assert.match(csv, /"log-1"/);
  assert.match(csv, /"log-2"/);
  assert.doesNotMatch(csv, /visible-only/);
  assert.match(csv, /"'9007199254740993123456789"/);
  assert.match(csv, /"'1"/);
  assert.match(csv, /"'=HYPERLINK\(""https:\/\/attacker\.invalid""\)"/);
  assert.match(csv, /"'\+formula-sender"/);
  assert.match(csv, /"'@formula-recipient"/);
  assert.match(csv, /"rules-v1"/);
  assert.match(downloads[0].filename, /dataset-10.*\.csv$/);
});

test("export snapshots the result identity before a concurrently completed result updates the UI", async () => {
  const { createAnalysisExporter } = await import(
    "../src/services/analysisExportBrowser.ts"
  );
  let resolvePage;
  const pageResponse = new Promise((resolve) => {
    resolvePage = resolve;
  });
  const requests = [];
  const downloads = [];
  const oldResult = analysisResult({
    dataset: { collectedTransfers: 1 },
    metrics: { transferCount: 1 },
  });
  const input = exportInput({ result: oldResult });
  const exporter = createAnalysisExporter({
    fetchImpl: async (request) => {
      requests.push(new URL(String(request), "http://browser.test"));
      return pageResponse;
    },
    downloadBlob(blob, filename) {
      downloads.push({ blob, filename });
    },
  });

  const exporting = exporter.exportCsv(input);
  input.investigation.currentResult = "result-new";
  input.result.dataset.id = "dataset-new";
  input.graphView.datasetId = "dataset-new";
  resolvePage(
    Response.json(
      graphPage({ edges: [transfer("edge-old", "old-event", "1")] }),
    ),
  );
  await exporting;

  assert.equal(requests[0].searchParams.get("datasetId"), "dataset-10");
  assert.equal(downloads.length, 1);
  assert.match(downloads[0].filename, /dataset-10/);
  const csv = await downloads[0].blob.text();
  assert.match(csv, /"dataset-10","result-10"/);
  assert.doesNotMatch(csv, /dataset-new|result-new/);
});

test("stale, unauthorized, deleted, and failed pages never download a partial CSV", async (t) => {
  const { createAnalysisExporter } = await import(
    "../src/services/analysisExportBrowser.ts"
  );
  for (const failure of [
    { name: "stale", status: 409, code: "stale_dataset" },
    { name: "unauthorized", status: 401, code: "unauthorized" },
    { name: "deleted", status: 404, code: "investigation_not_found" },
    { name: "page error", status: 503, code: "provider_unavailable" },
  ]) {
    await t.test(failure.name, async () => {
      const downloads = [];
      let requestCount = 0;
      const exporter = createAnalysisExporter({
        fetchImpl: async () => {
          requestCount += 1;
          if (requestCount === 1) {
            return Response.json(
              graphPage({
                edges: [transfer("edge-1", "log-0", "1")],
                nextCursor: "cursor/2",
                hasMore: true,
              }),
            );
          }
          return Response.json(
            { code: failure.code, error: "internal export detail" },
            { status: failure.status },
          );
        },
        downloadBlob(blob) {
          downloads.push(blob);
        },
      });

      await assert.rejects(exporter.exportCsv(exportInput()));
      assert.equal(downloads.length, 0);
    });
  }
});

test("cursor cycles and invalid hasMore contracts abort before browser download", async (t) => {
  const { createAnalysisExporter, AnalysisExportError } = await import(
    "../src/services/analysisExportBrowser.ts"
  );
  await t.test("cursor cycle", async () => {
    const downloads = [];
    let requestCount = 0;
    const exporter = createAnalysisExporter({
      fetchImpl: async () => {
        requestCount += 1;
        return Response.json(
          graphPage({
            edges: [
              transfer(`edge-${requestCount}`, `log-${requestCount}`, "1"),
            ],
            nextCursor: "cursor/2",
            hasMore: true,
          }),
        );
      },
      downloadBlob(blob) {
        downloads.push(blob);
      },
    });

    await assert.rejects(
      exporter.exportCsv(exportInput()),
      (error) => {
        assert.ok(error instanceof AnalysisExportError);
        assert.equal(error.code, "invalid_graph_pagination");
        return true;
      },
    );
    assert.equal(downloads.length, 0);
  });

  await t.test("hasMore without a cursor", async () => {
    const downloads = [];
    const exporter = createAnalysisExporter({
      fetchImpl: async () =>
        Response.json(
          graphPage({
            edges: [transfer("edge-1", "log-0", "1")],
            nextCursor: null,
            hasMore: true,
          }),
        ),
      downloadBlob(blob) {
        downloads.push(blob);
      },
    });

    await assert.rejects(exporter.exportCsv(exportInput()));
    assert.equal(downloads.length, 0);
  });

  await t.test("hasMore exceeds the bounded Dataset page count", async () => {
    const downloads = [];
    let requestCount = 0;
    const oneTransferResult = analysisResult({
      dataset: { transferLimit: 1, collectedTransfers: 1 },
      metrics: { transferCount: 1 },
    });
    const exporter = createAnalysisExporter({
      fetchImpl: async () => {
        requestCount += 1;
        return Response.json(
          graphPage({
            edges: [transfer("edge-1", "log-0", "1")],
            nextCursor: requestCount < 4 ? `cursor/${requestCount + 1}` : null,
            hasMore: requestCount < 4,
          }),
        );
      },
      downloadBlob(blob) {
        downloads.push(blob);
      },
    });

    await assert.rejects(
      exporter.exportCsv(
        exportInput({
          result: oneTransferResult,
          graphView: {
            datasetId: "dataset-10",
            nodes: [],
            edges: [],
          },
        }),
      ),
      (error) => {
        assert.ok(error instanceof AnalysisExportError);
        assert.equal(error.code, "invalid_graph_pagination");
        return true;
      },
    );
    assert.equal(downloads.length, 0);
  });
});

test("PDF export passes the complete fixed Dataset, authoritative result, and selected graph node to its browser renderer", async () => {
  const { createAnalysisExporter } = await import(
    "../src/services/analysisExportBrowser.ts"
  );
  const reports = [];
  const selectedNode = {
    id: targetAddress,
    address: targetAddress,
    type: "focus",
    label: "Target",
    x: 50,
    y: 50,
    group: 0,
  };
  const input = exportInput({
    result: analysisResult({
      dataset: {
        partial: true,
        confidence: 8,
        stopReason: "provider_rate_limited",
      },
    }),
    graphView: {
      datasetId: "dataset-10",
      nodes: [selectedNode],
      edges: [transfer("visible-only", "visible-only", "42")],
    },
    selectedNodeId: targetAddress,
  });
  const firstEvent = transfer("edge-1", "log-0", "2");
  const secondEvent = transfer("edge-2", "log-1", "3");
  const thirdEvent = transfer("edge-3", "log-2", "4");
  const exporter = createAnalysisExporter({
    fetchImpl: async (request) => {
      const url = new URL(String(request), "http://browser.test");
      if (url.searchParams.get("cursor")) {
        return Response.json(graphPage({ edges: [thirdEvent] }));
      }
      return Response.json(
        graphPage({
          edges: [firstEvent, secondEvent],
          nextCursor: "cursor/2",
          hasMore: true,
        }),
      );
    },
    async downloadPdf(report) {
      reports.push(report);
    },
  });

  await exporter.exportPdf(input);

  assert.equal(reports.length, 1);
  assert.equal(reports[0].investigation.currentResult, "result-10");
  assert.equal(reports[0].result.dataset.id, "dataset-10");
  assert.equal(reports[0].result.dataset.partial, true);
  assert.equal(reports[0].result.dataset.confidence, 8);
  assert.equal(reports[0].result.dataset.stopReason, "provider_rate_limited");
  assert.equal(reports[0].result.assessment.source, "rules-v1");
  assert.deepEqual(
    reports[0].graph.edges.map((edge) => edge.id),
    ["edge-1", "edge-2", "edge-3"],
  );
  assert.equal(reports[0].selectedNode.id, targetAddress);
  assert.notEqual(reports[0].selectedNode, selectedNode);
  assert.equal(
    reports[0].graph.edges.some((edge) => edge.id === "visible-only"),
    false,
  );
});

test("PDF page failure does not invoke the client-side renderer or save a partial report", async () => {
  const { createAnalysisExporter } = await import(
    "../src/services/analysisExportBrowser.ts"
  );
  let requestCount = 0;
  let renderCount = 0;
  const exporter = createAnalysisExporter({
    fetchImpl: async () => {
      requestCount += 1;
      if (requestCount === 1) {
        return Response.json(
          graphPage({
            edges: [transfer("edge-1", "log-0", "1")],
            nextCursor: "cursor/2",
            hasMore: true,
          }),
        );
      }
      return Response.json(
        { code: "stale_dataset", error: "new result published" },
        { status: 409 },
      );
    },
    async downloadPdf() {
      renderCount += 1;
    },
  });

  await assert.rejects(exporter.exportPdf(exportInput()));
  assert.equal(renderCount, 0);
});

test("client-side jsPDF renders only authoritative bounded facts with honest partial and zero-evidence labels", async () => {
  const { downloadAnalysisPdf } = await import(
    "../src/services/downloadAnalysisPdf.ts"
  );
  const documents = [];
  class FakePdf {
    constructor(options) {
      this.options = options;
      this.textRuns = [];
      this.savedAs = "";
      this.internal = {
        pageSize: {
          getWidth: () => 210,
          getHeight: () => 297,
        },
      };
      documents.push(this);
    }

    addFileToVFS() {}
    addFont() {}
    setFont() {}
    addPage() {}
    setFontSize() {}
    setTextColor() {}
    setFillColor() {}
    rect() {}
    roundedRect() {}
    setPage() {}
    splitTextToSize(text) {
      return String(text).split("\n");
    }
    text(value) {
      this.textRuns.push(Array.isArray(value) ? value.join("\n") : String(value));
    }
    getNumberOfPages() {
      return 1;
    }
    save(filename) {
      this.savedAs = filename;
    }
  }
  const selectedNode = {
    id: targetAddress,
    address: targetAddress,
    type: "focus",
    label: "Target node",
    x: 50,
    y: 50,
    group: 0,
  };
  const partialReport = {
    investigation: exportInput().investigation,
    result: analysisResult({
      dataset: {
        partial: true,
        confidence: 8,
        stopReason: "provider_rate_limited",
      },
    }),
    graph: {
      nodes: graphPage().nodes,
      edges: [
        transfer("edge-1", "log-0", "9007199254740993123456789"),
        transfer("edge-2", "log-1", "1"),
        transfer("edge-3", "log-2", "1"),
      ],
    },
    selectedNode,
  };
  const runtime = {
    fetchImpl: async (input) => {
      assert.equal(String(input), "/fonts/NotoSansTC.ttf");
      return new Response(new Uint8Array([1, 2, 3]));
    },
    loadJsPdf: async () => ({ jsPDF: FakePdf }),
  };

  await downloadAnalysisPdf(partialReport, runtime);

  assert.equal(documents.length, 1);
  assert.equal(documents[0].options.format, "a4");
  assert.match(documents[0].savedAs, /dataset-10\.pdf$/);
  const partialText = documents[0].textRuns.join("\n");
  assert.match(partialText, /TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE/);
  assert.match(partialText, /Result ID: result-10/);
  assert.match(partialText, /Dataset ID: dataset-10/);
  assert.match(partialText, /Evaluator source: rules-v1/);
  assert.match(partialText, /2026-07-05T02:00:00Z.*2026-08-04T02:00:00Z/);
  assert.match(partialText, /3 \/ 500 Transfer/);
  assert.match(partialText, /Confidence: 8%/);
  assert.match(partialText, /Stop reason: provider_rate_limited/);
  assert.match(partialText, /部分分析（Partial Analysis）/);
  assert.match(partialText, /9,007,199,254,740,993,123\.456791 USDT/);
  assert.match(partialText, /Dataset Transfer edges: 3/);
  assert.match(partialText, /Selected node address: TQn9Y2/);
  assert.doesNotMatch(
    partialText,
    /\bAI\b|LLM|Interpretation|Agent|recommendation/i,
  );

  const zeroResult = analysisResult({
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
    },
  });
  await downloadAnalysisPdf(
    {
      ...partialReport,
      result: zeroResult,
      graph: { nodes: [], edges: [] },
      selectedNode: null,
    },
    runtime,
  );

  const zeroText = documents[1].textRuns.join("\n");
  assert.match(zeroText, /證據不足（zero evidence \/ insufficient evidence）/);
  assert.match(zeroText, /no_eligible_transfers/);
  assert.doesNotMatch(zeroText, /0\/100|低風險/);
  assert.doesNotMatch(
    zeroText,
    /\bAI\b|LLM|Interpretation|Agent|recommendation/i,
  );
});

test("responsive browser export actions expose CSV and PDF triggers with shared loading and error states", async () => {
  const { AnalysisExportActions } = await import(
    "../src/ui/AnalysisPanel.tsx"
  );
  const ready = renderToStaticMarkup(
    createElement(AnalysisExportActions, {
      disabled: false,
      isExportingCsv: false,
      isExportingPdf: false,
      error: "",
      onExportCsv() {},
      onExportPdf() {},
    }),
  );
  assert.match(ready, /aria-label="下載完整 Dataset CSV"/);
  assert.match(ready, /aria-label="下載目前分析 PDF"/);
  assert.match(ready, />CSV</);
  assert.match(ready, />PDF</);

  const busy = renderToStaticMarkup(
    createElement(AnalysisExportActions, {
      disabled: false,
      isExportingCsv: true,
      isExportingPdf: false,
      error: "分頁讀取失敗，未下載檔案。",
      onExportCsv() {},
      onExportPdf() {},
    }),
  );
  assert.equal((busy.match(/disabled=""/g) || []).length, 2);
  assert.match(busy, /CSV 產生中/);
  assert.match(busy, /role="alert"/);
  assert.match(busy, /分頁讀取失敗，未下載檔案。/);
});

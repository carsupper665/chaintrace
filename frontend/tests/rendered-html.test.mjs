import assert from "node:assert/strict";
import { access, readFile } from "node:fs/promises";
import { createServer } from "node:http";
import test from "node:test";

async function render() {
  const backend = createServer((_request, response) => {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        id: 7,
        username: "render-owner",
        display_name: "Rendered Owner",
        email: "render@example.com",
        role: 1,
      }),
    );
  });
  await new Promise((resolve) => backend.listen(0, "127.0.0.1", resolve));
  const address = backend.address();
  process.env.CHAINTRACE_BACKEND_URL = `http://127.0.0.1:${address.port}`;
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}`);
  const { default: worker } = await import(workerUrl.href);

  try {
    return await worker.fetch(
      new Request("http://localhost/", {
        headers: {
          accept: "text/html",
          cookie: "chaintrace_credential=render.jwt",
        },
      }),
      {
        ASSETS: {
          fetch: async () => new Response("Not found", { status: 404 }),
        },
      },
      {
        waitUntil() {},
        passThroughOnException() {},
      },
    );
  } finally {
    await new Promise((resolve) => backend.close(resolve));
  }
}

test("server-renders the ChainTrace investigation workspace", async () => {
  const response = await render();
  assert.equal(response.status, 200);
  assert.match(response.headers.get("content-type") ?? "", /^text\/html\b/i);

  const html = await response.text();
  assert.match(html, /<title>ChainTrace \| AI 區塊鏈異常調查<\/title>/i);
  assert.match(html, /調查工作區/);
  assert.match(html, /正在載入調查工作區/);
  assert.match(html, /TRON mainnet/);
  assert.match(html, /Rendered Owner/);
  assert.doesNotMatch(html, /CT-20(?:17|29|38|41)|ETH 錢包追蹤|Ethereum|Bitcoin/i);
  assert.doesNotMatch(html, /codex-preview|react-loading-skeleton/i);
});

test("keeps the route entry thin and implementation under src", async () => {
  const [
    page,
    feature,
    styles,
    architecture,
    controller,
    analysis,
    agentPanel,
    pdfService,
  ] =
    await Promise.all([
    readFile(new URL("../app/page.tsx", import.meta.url), "utf8"),
    readFile(
      new URL("../src/features/chaintrace/ChainTraceApp.tsx", import.meta.url),
      "utf8",
    ),
    readFile(new URL("../src/styles/index.css", import.meta.url), "utf8"),
    readFile(new URL("../ARCHITECTURE.md", import.meta.url), "utf8"),
    readFile(new URL("../src/hooks/useChainTrace.ts", import.meta.url), "utf8"),
    readFile(new URL("../src/ui/AnalysisPanel.tsx", import.meta.url), "utf8"),
    readFile(new URL("../src/ui/AgentPanel.tsx", import.meta.url), "utf8"),
    readFile(
      new URL("../src/services/downloadAnalysisPdf.ts", import.meta.url),
      "utf8",
    ),
  ]);

  assert.match(page, /<ChainTraceApp owner=\{owner\} \/>/);
  assert.doesNotMatch(page, /useState|useEffect|fetch\(/);
  assert.match(feature, /useChainTrace/);
  assert.match(feature, /WorkspacePanel/);
  assert.match(feature, /AgentPanel/);
  assert.match(feature, /AnalysisPanel/);
  assert.match(styles, /@import "\.\/chaintrace\.css"/);
  assert.match(architecture, /src\/middle/);
  assert.match(controller, /chaintrace-analysis-collapsed/);
  assert.match(controller, /chaintrace-sidebar-collapsed/);
  assert.match(analysis, /dateMarkers\.map/);
  assert.doesNotMatch(analysis, /!ui\.isGraphFullscreen && !selectedDate/);
  assert.match(analysis, /edge\.from === edge\.to/);
  assert.match(analysis, /const reach = 0\.46 \+ 0\.08 \* \(index % 5\)/);
  assert.match(analysis, /x: from\.x \+ \(to\.x - from\.x\) \* reach/);
  assert.match(analysis, /relationshipMarkers\.get\(pair\)/);
  assert.match(analysis, /relationshipMarkers/);
  assert.match(analysis, /dateLabel/);
  assert.match(analysis, /dates\.includes\(selectedDate\)/);
  assert.match(analysis, /markerEnd=/);
  assert.match(analysis, /graph-edge-hitarea/);
  assert.match(analysis, /graph-edge-tooltip/);
  assert.match(analysis, /direction-inbound/);
  assert.match(analysis, /groupCenterByGroup/);
  assert.match(analysis, /ui\.isGraphFullscreen \? 13 : 6/);
  assert.match(analysis, /className="flow-in"/);
  assert.match(analysis, /isCompactGraph/);
  assert.match(analysis, /graph-view-switch/);
  assert.match(analysis, /graph-filter-bar/);
  assert.match(analysis, /graph-data-meta/);
  assert.match(analysis, /graph-filter-summary/);
  assert.match(analysis, /graph-clear-filter/);
  assert.match(analysis, /hasActiveGraphFilters/);
  assert.match(analysis, /event\.key === "0"/);
  assert.match(analysis, /directionFilter/);
  assert.match(analysis, /hiddenGraphGroups/);
  assert.match(analysis, /!isCompactGraph && dateMarkers\.map/);
  assert.doesNotMatch(analysis, /title=\{node\.address\}/);
  assert.doesNotMatch(analysis, /summarizeFlow|reduce\(\(sum, edge\)/);
  assert.match(agentPanel, /className="mini-stat-value"/);
  assert.match(agentPanel, /className="mini-stat total-flow-stat"/);
  assert.match(agentPanel, /className="total-flow-value"/);
  assert.match(controller, /function adjustPanelWidth/);
  assert.match(controller, /function undoGraphExpansion/);
  assert.match(controller, /chaintrace-sidebar-width/);
  assert.match(analysis, /實線＝由目標轉出/);
  assert.match(pdfService, /roundedRect\(margin, y - 5\.2/);
  assert.match(pdfService, /pdf\.text\(title, margin \+ 4\.5, y\)/);
  assert.doesNotMatch(pdfService, /pdf\.line\(margin, y - 2/);

  await access(
    new URL("../src/middle/transaction-graph-contract.ts", import.meta.url),
  );
});

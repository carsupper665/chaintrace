import assert from "node:assert/strict";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

function message(id, role, content, createdAt) {
  return { id, role, content, createdAt };
}

test("browser conversation switches Investigation, reloads, and appends later pages in durable order", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  let bReload = 0;
  const requests = [];
  const fetchImpl = async (input) => {
    const url = new URL(String(input), "http://browser.test");
    requests.push(url);
    const investigationId = decodeURIComponent(url.pathname.split("/")[3]);
    const cursor = url.searchParams.get("cursor");
    if (investigationId === "inv-a" && cursor === "later-a") {
      return Response.json({
        messages: [message("a-2", "system", "A later", "2026-08-04T02:00:00Z")],
        nextCursor: null,
      });
    }
    if (investigationId === "inv-a") {
      return Response.json({
        messages: [
          message("a-1", "user", "A oldest", "2026-08-04T01:00:00Z"),
        ],
        nextCursor: "later-a",
      });
    }
    bReload += 1;
    return Response.json({
      messages: [
        message(
          `b-${bReload}`,
          "user",
          bReload === 1 ? "B initial" : "B restored after reload",
          `2026-08-04T0${bReload}:00:00Z`,
        ),
      ],
      nextCursor: null,
    });
  };
  const browser = createConversationBrowser(fetchImpl, 25);

  await browser.load("inv-a");
  await browser.loadMore();
  assert.deepEqual(
    browser.getState().messages.map((item) => item.id),
    ["a-1", "a-2"],
  );
  assert.equal(requests[0].searchParams.get("pageSize"), "25");
  assert.equal(requests[1].searchParams.get("cursor"), "later-a");

  await browser.load("inv-b");
  assert.deepEqual(
    browser.getState().messages.map((item) => item.content),
    ["B initial"],
  );
  await browser.load("inv-b");
  assert.deepEqual(
    browser.getState().messages.map((item) => item.content),
    ["B restored after reload"],
  );
  assert.ok(browser.getState().messages.every((item) => !item.id.startsWith("a-")));
});

test("persisted unavailable renders user and system messages, preserves Unicode, and idempotent retry does not duplicate", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");
  const command = `${"追蹤跨鏈資金流🌐\n".repeat(400)}終點：測試地址`;
  const saved = [
    message("unicode-user", "user", command, "2026-08-04T03:00:00Z"),
    message(
      "unicode-system",
      "system",
      "Agent 目前無法使用；訊息已保存。",
      "2026-08-04T03:00:00.001Z",
    ),
  ];
  const requests = [];
  const fetchImpl = async (_input, init = {}) => {
    if ((init.method ?? "GET") === "GET") {
      return Response.json({ messages: [], nextCursor: null });
    }
    requests.push(JSON.parse(String(init.body)));
    return Response.json(
      { code: "agent_unavailable", persisted: true, messages: saved },
      { status: 503 },
    );
  };
  const browser = createConversationBrowser(fetchImpl);
  await browser.load("inv-09");

  await browser.submit(command, "same-command-key");
  await browser.submit(command, "same-command-key");

  assert.deepEqual(requests, [
    { idempotencyKey: "same-command-key", message: command },
    { idempotencyKey: "same-command-key", message: command },
  ]);
  assert.deepEqual(
    browser.getState().messages.map((item) => item.id),
    ["unicode-user", "unicode-system"],
  );

  const investigation = {
    id: "inv-09",
    title: "Conversation truthfulness",
    address: null,
    network: "TRON_MAINNET",
    status: "待處理",
    risk: null,
    relatedNodes: null,
    totalFlow: null,
    transactionCount: null,
    targetLocked: false,
    currentResult: null,
  };
  const html = renderToStaticMarkup(
    createElement(AgentPanel, {
      ui: {
        active: investigation,
        addressDraft: "",
        analysisError: "",
        analysisOutcome: null,
        analysisRun: null,
        analysisScope: { transferLimit: 500, traversalDepth: 2 },
        chatMessages: browser.getState().messages,
        conversationError: "",
        currentAnalysisResult: null,
        graphError: "",
        hasMoreConversation: false,
        isConversationLoading: false,
        isGraphLoading: false,
        isLightMode: false,
        isRunning: false,
        query: "",
        targetMessage: "",
        cancelAnalysis() {},
        confirmInvestigationAddress() {},
        loadMoreConversation() {},
        runInvestigation() {},
        setAddressDraft() {},
        setAnalysisScope() {},
        setQuery() {},
        startAnalysis() {},
        toggleTheme() {},
      },
    }),
  );

  assert.match(html, /終點：測試地址/);
  assert.match(html, /Agent 目前無法使用；訊息已保存。/);
  assert.doesNotMatch(html, /chat-message agent/);
  assert.equal(investigation.status, "待處理");
});

test("browser reuses its generated idempotency key after an unknown network outcome", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  const submittedKeys = [];
  let attempts = 0;
  const fetchImpl = async (_input, init = {}) => {
    if ((init.method ?? "GET") === "GET") {
      return Response.json({ messages: [], nextCursor: null });
    }
    const body = JSON.parse(String(init.body));
    submittedKeys.push(body.idempotencyKey);
    attempts += 1;
    if (attempts === 1) throw new TypeError("response was lost");
    return Response.json(
      {
        code: "agent_unavailable",
        persisted: true,
        messages: [
          message("retry-user", "user", body.message, "2026-08-04T04:00:00Z"),
          message(
            "retry-system",
            "system",
            '{"code":"agent_unavailable"}',
            "2026-08-04T04:00:00.001Z",
          ),
        ],
      },
      { status: 503 },
    );
  };
  const browser = createConversationBrowser(fetchImpl);
  await browser.load("inv-retry");

  await assert.rejects(browser.submit("retry me"), /response was lost/);
  await browser.submit("retry me");

  assert.equal(submittedKeys.length, 2);
  assert.equal(submittedKeys[0], submittedKeys[1]);
  assert.match(submittedKeys[0], /^conversation-|^[0-9a-f-]{36}$/i);
  assert.deepEqual(
    browser.getState().messages.map((item) => item.id),
    ["retry-user", "retry-system"],
  );
});

test("submitting discloses the pages between the loaded window and the new messages", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  const fetchImpl = async (input, init = {}) => {
    if ((init.method ?? "GET") === "POST") {
      return Response.json(
        {
          code: "agent_unavailable",
          persisted: true,
          messages: [
            message("m-4", "user", "newest", "2026-08-04T04:00:00Z"),
            message(
              "m-5",
              "system",
              '{"code":"agent_unavailable"}',
              "2026-08-04T04:00:00.001Z",
            ),
          ],
        },
        { status: 503 },
      );
    }
    const cursor = new URL(String(input), "http://browser.test").searchParams.get(
      "cursor",
    );
    if (cursor === "after-2") {
      return Response.json({
        messages: [message("m-3", "user", "third", "2026-08-04T03:00:00Z")],
        nextCursor: null,
      });
    }
    return Response.json({
      messages: [
        message("m-1", "user", "first", "2026-08-04T01:00:00Z"),
        message("m-2", "system", "second", "2026-08-04T02:00:00Z"),
      ],
      nextCursor: "after-2",
    });
  };
  const browser = createConversationBrowser(fetchImpl, 2);

  await browser.load("inv-drain");
  assert.equal(browser.getState().nextCursor, "after-2");

  await browser.submit("newest");

  assert.deepEqual(
    browser.getState().messages.map((item) => item.id),
    ["m-1", "m-2", "m-3", "m-4", "m-5"],
  );
  assert.equal(browser.getState().nextCursor, null);
});

test("AgentPanel presents structured unavailable events as system outcomes and offers undisclosed history", async () => {
  const { AgentPanel } = await import("../src/ui/AgentPanel.tsx");
  const html = renderToStaticMarkup(
    createElement(AgentPanel, {
      ui: {
        active: {
          id: "inv-structured",
          title: "Structured event",
          address: null,
          network: "TRON_MAINNET",
          status: "待處理",
          risk: null,
          relatedNodes: null,
          totalFlow: null,
          transactionCount: null,
          targetLocked: false,
          currentResult: null,
        },
        addressDraft: "",
        analysisError: "",
        analysisOutcome: null,
        analysisRun: null,
        analysisScope: { transferLimit: 500, traversalDepth: 2 },
        chatMessages: [
          message(
            "structured-system",
            "system",
            '{"code":"agent_unavailable"}',
            "2026-08-04T04:00:00Z",
          ),
        ],
        conversationError: "",
        currentAnalysisResult: null,
        graphError: "",
        hasMoreConversation: true,
        isConversationLoading: false,
        isGraphLoading: false,
        isLightMode: false,
        isRunning: false,
        query: "",
        targetMessage: "",
        cancelAnalysis() {},
        confirmInvestigationAddress() {},
        loadMoreConversation() {},
        runInvestigation() {},
        setAddressDraft() {},
        setAnalysisScope() {},
        setQuery() {},
        startAnalysis() {},
        toggleTheme() {},
      },
    }),
  );

  assert.match(html, /載入更多訊息/);
  assert.match(html, /Agent 目前無法使用；訊息已保存。/);
  assert.doesNotMatch(html, /\{&quot;code&quot;:/);
});

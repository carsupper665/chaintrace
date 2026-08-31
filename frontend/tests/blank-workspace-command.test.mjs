import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

// Sending a command from a blank workspace has to open an Investigation for it.
// These cover the two halves: the bar is reachable with nothing open, and the
// conversation session it starts submits without first fetching an empty page.

function commandBarUi(overrides = {}) {
  return {
    active: null,
    query: "幫我調查 TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t",
    isRunning: false,
    isConversationLoading: false,
    runInvestigation() {},
    setQuery() {},
    ...overrides,
  };
}

async function renderCommandBar(overrides) {
  const { CommandBar } = await import("../src/ui/CommandBar.tsx");
  return renderToStaticMarkup(
    createElement(CommandBar, { ui: commandBarUi(overrides) }),
  );
}

test("with nothing open the command bar invites an address and says a new Investigation follows", async () => {
  const html = await renderCommandBar();

  assert.match(html, /貼上 TRON 地址並說明需求/);
  assert.match(html, /Enter 送出後會自動建立調查/);
  const send = html.slice(html.indexOf("send-button"));
  assert.doesNotMatch(send.slice(0, 200), /disabled/);
});

test("with an Investigation open the bar keeps its ordinary prompt", async () => {
  const html = await renderCommandBar({ active: { id: "inv-1" } });

  assert.match(html, /追蹤此地址流向混幣服務的所有路徑/);
  assert.match(html, /Enter 執行 · Shift\+Enter 換行/);
});

test("an empty command cannot be sent from either state", async () => {
  for (const active of [null, { id: "inv-1" }]) {
    const html = await renderCommandBar({ active, query: "   " });
    const send = html.slice(html.indexOf("send-button"));
    assert.match(send.slice(0, 200), /disabled/);
  }
});

test("a conversation opened for a new Investigation submits without fetching an empty page first", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  const requests = [];
  const fetchImpl = async (input, init = {}) => {
    const url = new URL(String(input), "http://browser.test");
    requests.push(`${init.method ?? "GET"} ${url.pathname}`);
    return Response.json({
      code: "agent_replied",
      persisted: true,
      messages: [
        {
          id: "m-user",
          role: "user",
          content: "幫我調查 TR7NHq",
          createdAt: "2026-08-31T00:00:00Z",
        },
        {
          id: "m-agent",
          role: "agent",
          content: "已設定調查目標並啟動鏈上資料蒐集。",
          createdAt: "2026-08-31T00:00:01Z",
        },
      ],
    });
  };
  const browser = createConversationBrowser(fetchImpl);

  browser.startFresh("inv-new");
  assert.deepEqual(requests, []);

  await browser.submit("幫我調查 TR7NHq", "fresh-key");

  assert.deepEqual(requests, ["POST /api/investigations/inv-new/conversation"]);
  assert.deepEqual(
    browser.getState().messages.map((item) => item.id),
    ["m-user", "m-agent"],
  );
  assert.equal(browser.getState().investigationId, "inv-new");
});

test("adopting a fresh conversation discards whatever the previous Investigation held", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  const fetchImpl = async () =>
    Response.json({
      messages: [
        {
          id: "old-1",
          role: "user",
          content: "old",
          createdAt: "2026-08-30T00:00:00Z",
        },
      ],
      nextCursor: null,
    });
  const browser = createConversationBrowser(fetchImpl);
  await browser.load("inv-old");
  assert.equal(browser.getState().messages.length, 1);

  browser.startFresh("inv-new");

  assert.deepEqual(browser.getState(), {
    investigationId: "inv-new",
    messages: [],
    nextCursor: null,
  });
});

test("the empty workspace is not hidden behind a class the app never applies", async () => {
  const [css, shell] = await Promise.all([
    readFile(new URL("../src/styles/chaintrace.css", import.meta.url), "utf8"),
    readFile(
      new URL("../src/features/chaintrace/ChainTraceApp.tsx", import.meta.url),
      "utf8",
    ),
  ]);

  // The empty state used to be `display: none` unless `.has-no-investigations`
  // was set, and nothing ever set it: an Owner with no Investigation saw a
  // blank page, and the command bar that opens one was unreachable.
  assert.doesNotMatch(css, /has-no-investigations/);
  assert.doesNotMatch(shell, /has-no-investigations/);
  const block = css.slice(
    css.indexOf(".empty-investigation-state {"),
    css.indexOf(".empty-investigation-state h3"),
  );
  assert.match(block, /display: flex;/);
  assert.doesNotMatch(block, /display: none;/);
  assert.match(shell, /<CommandBar ui=\{ui\} \/>/);
});

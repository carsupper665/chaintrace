import assert from "node:assert/strict";
import test from "node:test";

const summaryMessages = [
  {
    id: "agent-1",
    role: "agent",
    content: "### 範圍聲明\n本次分析涵蓋 30 天。",
    createdAt: "2026-08-30T03:00:00Z",
  },
];

function jsonFetch(body, status) {
  const requests = [];
  const fetchImpl = async (input, init) => {
    requests.push({ input: String(input), init });
    return Response.json(body, { status });
  };
  return { requests, fetchImpl };
}

test("a 201 reports a freshly generated summary", async () => {
  const { postAgentSummary } = await import(
    "../src/middle/conversation-client.ts"
  );
  const { requests, fetchImpl } = jsonFetch(
    { datasetId: "ds-1", messages: summaryMessages },
    201,
  );

  const outcome = await postAgentSummary("inv/09", fetchImpl);

  assert.equal(outcome.created, true);
  assert.equal(outcome.datasetId, "ds-1");
  assert.deepEqual(outcome.messages, summaryMessages);
  assert.equal(requests.length, 1);
  assert.equal(
    requests[0].input,
    "/api/investigations/inv%2F09/agent/summary",
    "the investigation id must be encoded into the path",
  );
  assert.equal(requests[0].init.method, "POST");
});

test("a 200 reports the stored summary being reused", async () => {
  const { postAgentSummary } = await import(
    "../src/middle/conversation-client.ts"
  );
  const { fetchImpl } = jsonFetch(
    { datasetId: "ds-1", messages: summaryMessages },
    200,
  );

  const outcome = await postAgentSummary("inv-09", fetchImpl);

  // Same dataset, same summary: the backend did not spend tokens again.
  assert.equal(outcome.created, false);
  assert.deepEqual(outcome.messages, summaryMessages);
});

test("an un-analysed investigation is explained, not blamed on the agent", async () => {
  const { postAgentSummary, ConversationClientError } = await import(
    "../src/middle/conversation-client.ts"
  );
  const { fetchImpl } = jsonFetch({ code: "current_result_not_found" }, 404);

  await assert.rejects(
    () => postAgentSummary("inv-09", fetchImpl),
    (error) => {
      assert.ok(error instanceof ConversationClientError);
      assert.equal(error.code, "current_result_not_found");
      assert.match(error.message, /請先執行分析/);
      return true;
    },
  );
});

test("an unavailable agent surfaces as an agent problem", async () => {
  const { postAgentSummary, ConversationClientError } = await import(
    "../src/middle/conversation-client.ts"
  );
  const { fetchImpl } = jsonFetch({ code: "agent_unavailable" }, 503);

  await assert.rejects(
    () => postAgentSummary("inv-09", fetchImpl),
    (error) => {
      assert.ok(error instanceof ConversationClientError);
      assert.equal(error.code, "agent_unavailable");
      return true;
    },
  );
});

test("a malformed payload is rejected rather than rendered", async () => {
  const { postAgentSummary } = await import(
    "../src/middle/conversation-client.ts"
  );
  const { fetchImpl } = jsonFetch(
    { datasetId: "ds-1", messages: [{ id: "x" }] },
    201,
  );

  await assert.rejects(() => postAgentSummary("inv-09", fetchImpl));
});

test("requestSummary adds the summary to the loaded conversation", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  const existing = {
    id: "user-1",
    role: "user",
    content: "先前的問題",
    createdAt: "2026-08-30T02:00:00Z",
  };
  const fetchImpl = async (input, init) => {
    const url = String(input);
    if (url.endsWith("/agent/summary")) {
      return Response.json(
        { datasetId: "ds-1", messages: summaryMessages },
        { status: 201 },
      );
    }
    assert.equal(init?.method ?? "GET", "GET");
    return Response.json({ messages: [existing], nextCursor: null });
  };

  const browser = createConversationBrowser(fetchImpl);
  await browser.load("inv-09");
  const outcome = await browser.requestSummary();

  assert.equal(outcome.created, true);
  assert.deepEqual(
    browser.getState().messages.map((message) => message.id),
    ["user-1", "agent-1"],
    "the summary joins the existing transcript instead of replacing it",
  );
});

test("requestSummary refuses before a conversation is loaded", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  const browser = createConversationBrowser(async () => {
    throw new Error("must not reach the network");
  });

  await assert.rejects(() => browser.requestSummary());
});

import assert from "node:assert/strict";
import test from "node:test";

function message(id, role, content, createdAt) {
  return { id, role, content, createdAt };
}

// Reproduces the real sequence a user goes through: open an Investigation,
// press "produce summary", then type a question. The chat POST must actually
// reach the backend.
test("a chat message still reaches the backend after a summary was produced", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  const calls = [];
  const fetchImpl = async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    calls.push(`${method} ${new URL(url, "http://test").pathname}`);

    if (url.endsWith("/agent/summary")) {
      return Response.json(
        {
          datasetId: "ds-1",
          messages: [message("agent-1", "agent", "摘要", "2026-08-30T03:00:00Z")],
        },
        { status: 201 },
      );
    }
    if (method === "POST") {
      return Response.json({
        messages: [
          message("user-2", "user", "問題", "2026-08-30T04:00:00Z"),
          message("agent-2", "agent", "回答", "2026-08-30T04:00:01Z"),
        ],
      });
    }
    return Response.json({
      messages: [message("user-1", "user", "先前", "2026-08-30T02:00:00Z")],
      nextCursor: null,
    });
  };

  const browser = createConversationBrowser(fetchImpl);
  await browser.load("inv-1");
  await browser.requestSummary();
  const outcome = await browser.submit("問題");

  assert.equal(outcome.code, "agent_replied");
  assert.ok(
    calls.includes("POST /api/investigations/inv-1/conversation"),
    `the chat POST never happened; calls were: ${calls.join(", ")}`,
  );
});

// The same sequence, but the Investigation was never loaded first — which is
// what happens if the workspace hands a message to an unloaded conversation.
test("submitting without a loaded conversation fails loudly, not silently", async () => {
  const { createConversationBrowser } = await import(
    "../src/services/conversationBrowser.ts"
  );
  let reached = false;
  const browser = createConversationBrowser(async () => {
    reached = true;
    return Response.json({ messages: [] });
  });

  await assert.rejects(() => browser.submit("問題"));
  assert.equal(reached, false, "no request should be attempted");
});

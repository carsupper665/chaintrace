import assert from "node:assert/strict";
import test from "node:test";

const unavailableMessages = [
  {
    id: "user-1",
    role: "user",
    content: "解釋這組資金流",
    createdAt: "2026-08-04T03:00:00Z",
  },
  {
    id: "system-1",
    role: "system",
    content: "Agent provider is unavailable.",
    createdAt: "2026-08-04T03:00:00.001Z",
  },
];

test("client treats persisted agent_unavailable as a truthful saved outcome", async () => {
  const { postConversationMessage } = await import(
    "../src/middle/conversation-client.ts"
  );
  const requests = [];
  const fetchImpl = async (input, init) => {
    requests.push({ input: String(input), init });
    return Response.json(
      {
        code: "agent_unavailable",
        persisted: true,
        messages: unavailableMessages,
      },
      { status: 503 },
    );
  };

  const outcome = await postConversationMessage(
    "inv/09",
    "stable-command-key",
    "解釋這組資金流",
    fetchImpl,
  );

  assert.deepEqual(outcome, {
    code: "agent_unavailable",
    persisted: true,
    messages: unavailableMessages,
  });
  assert.equal(requests[0].input, "/api/investigations/inv%2F09/conversation");
  assert.equal(requests[0].init.cache, "no-store");
  assert.deepEqual(JSON.parse(requests[0].init.body), {
    idempotencyKey: "stable-command-key",
    message: "解釋這組資金流",
  });
});

test("client maps cross-Owner and absent conversations to the same stable error", async () => {
  const { ConversationClientError, getConversationPage } = await import(
    "../src/middle/conversation-client.ts"
  );
  const fetchImpl = async () =>
    Response.json(
      {
        code: "investigation_not_found",
        error: "sensitive database ownership detail",
      },
      { status: 404 },
    );

  await assert.rejects(
    getConversationPage("other-owner", {}, fetchImpl),
    (error) => {
      assert.ok(error instanceof ConversationClientError);
      assert.equal(error.status, 404);
      assert.equal(error.code, "investigation_not_found");
      assert.equal(error.message, "找不到這筆調查，可能已被刪除或無權存取。");
      assert.doesNotMatch(error.message, /database|ownership/i);
      return true;
    },
  );
});

test("client forwards cursor pagination and uses crypto.randomUUID for command identity", async () => {
  const { createConversationIdempotencyKey, getConversationPage } = await import(
    "../src/middle/conversation-client.ts"
  );
  let requested;
  const fetchImpl = async (input, init) => {
    requested = { input: String(input), init };
    return Response.json({ messages: [], nextCursor: null });
  };

  const result = await getConversationPage(
    "inv-09",
    { cursor: "earlier/page", pageSize: 25 },
    fetchImpl,
  );

  assert.deepEqual(result, { messages: [], nextCursor: null });
  assert.equal(
    requested.input,
    "/api/investigations/inv-09/conversation?cursor=earlier%2Fpage&pageSize=25",
  );
  assert.equal(requested.init.cache, "no-store");
  assert.equal(
    createConversationIdempotencyKey({ randomUUID: () => "uuid-from-crypto" }),
    "uuid-from-crypto",
  );
});

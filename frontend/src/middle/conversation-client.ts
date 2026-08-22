import type {
  AgentUnavailableOutcome,
  ConversationErrorResponse,
  ConversationMessage,
  ConversationPage,
  ConversationSubmitOutcome,
} from "./conversation-contract.ts";
import { expireSession } from "@/src/auth/expire";

type Fetch = typeof fetch;
type RandomUuidSource = { randomUUID?: () => string };

let fallbackKeySequence = 0;

export class ConversationClientError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly code: string,
  ) {
    super(message);
    this.name = "ConversationClientError";
  }
}

function conversationPath(investigationId: string) {
  return `/api/investigations/${encodeURIComponent(investigationId)}/conversation`;
}

function stableErrorMessage(status: number, code: string) {
  if (status === 404 || code === "investigation_not_found") {
    return "找不到這筆調查，可能已被刪除或無權存取。";
  }
  if (status === 401 || code === "unauthorized") {
    expireSession();
    return "登入狀態已失效，請重新登入。";
  }
  if (code === "agent_unavailable" || code === "agent_not_implemented") {
    return "Agent 目前無法使用。";
  }
  if (status === 429 || code === "rate_limited") {
    return "短時間內的請求過多，請稍候再試。";
  }
  return "無法載入或保存對話，請稍後再試。";
}

function isConversationMessage(value: unknown): value is ConversationMessage {
  if (!value || typeof value !== "object") return false;
  const message = value as Partial<ConversationMessage>;
  return (
    typeof message.id === "string" &&
    (message.role === "user" ||
      message.role === "system" ||
      message.role === "agent") &&
    typeof message.content === "string" &&
    typeof message.createdAt === "string"
  );
}

function requireMessages(value: unknown): ConversationMessage[] {
  if (!Array.isArray(value) || !value.every(isConversationMessage)) {
    throw new ConversationClientError(
      "對話服務回傳了無法辨識的資料。",
      502,
      "invalid_conversation_response",
    );
  }
  return value;
}

async function responsePayload(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch {
    return null;
  }
}

function responseError(response: Response, payload: unknown) {
  const error = (payload || {}) as ConversationErrorResponse;
  const code = error.code || `HTTP_${response.status}`;
  return new ConversationClientError(
    stableErrorMessage(response.status, code),
    response.status,
    code,
  );
}

export function createConversationIdempotencyKey(
  source: RandomUuidSource | undefined = globalThis.crypto,
) {
  if (typeof source?.randomUUID === "function") return source.randomUUID();
  fallbackKeySequence += 1;
  return `conversation-${Date.now().toString(36)}-${fallbackKeySequence.toString(36)}`;
}

export async function getConversationPage(
  investigationId: string,
  options: { cursor?: string | null; pageSize?: number } = {},
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
): Promise<ConversationPage> {
  const query = new URLSearchParams();
  if (options.cursor) query.set("cursor", options.cursor);
  if (options.pageSize !== undefined) {
    query.set("pageSize", String(options.pageSize));
  }
  const search = query.size > 0 ? `?${query}` : "";
  const response = await fetchImpl(`${conversationPath(investigationId)}${search}`, {
    headers: { Accept: "application/json" },
    cache: "no-store",
    signal,
  });
  const payload = await responsePayload(response);
  if (!response.ok) throw responseError(response, payload);
  const page = (payload || {}) as Partial<ConversationPage>;
  return {
    messages: requireMessages(page.messages),
    nextCursor: typeof page.nextCursor === "string" ? page.nextCursor : null,
  };
}

export async function postConversationMessage(
  investigationId: string,
  idempotencyKey: string,
  message: string,
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
): Promise<ConversationSubmitOutcome> {
  const response = await fetchImpl(conversationPath(investigationId), {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ idempotencyKey, message }),
    cache: "no-store",
    signal,
  });
  const payload = await responsePayload(response);
  const outcome = (payload || {}) as Partial<AgentUnavailableOutcome>;

  if (
    response.status === 503 &&
    outcome.code === "agent_unavailable" &&
    outcome.persisted === true
  ) {
    return {
      code: "agent_unavailable",
      persisted: true,
      messages: requireMessages(outcome.messages),
    };
  }
  if (response.ok) {
    return {
      code: "agent_replied",
      persisted: true,
      messages: requireMessages(outcome.messages),
    };
  }
  throw responseError(response, payload);
}

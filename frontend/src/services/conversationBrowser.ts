import {
  createConversationIdempotencyKey,
  getConversationPage,
  postAgentSummary,
  postConversationMessage,
  postConversationMessageStream,
} from "@/src/middle/conversation-client";
import {
  CONVERSATION_PAGE_SIZE,
  type AgentSummaryOutcome,
  type ConversationStreamEvent,
  type ConversationSubmitOutcome,
  type ConversationMessage,
} from "@/src/middle/conversation-contract";

type Fetch = typeof fetch;

type ConversationBrowserState = {
  investigationId: string;
  messages: ConversationMessage[];
  nextCursor: string | null;
};

function mergeMessages(...groups: ConversationMessage[][]) {
  const seen = new Set<string>();
  return groups.flat().filter((message) => {
    if (seen.has(message.id)) return false;
    seen.add(message.id);
    return true;
  });
}

export function createConversationBrowser(
  fetchImpl: Fetch = fetch,
  pageSize = CONVERSATION_PAGE_SIZE,
) {
  let state: ConversationBrowserState = {
    investigationId: "",
    messages: [],
    nextCursor: null,
  };
  let requestVersion = 0;
  let controller: AbortController | null = null;
  let pendingSubmission: {
    investigationId: string;
    message: string;
    idempotencyKey: string;
  } | null = null;

  function beginRequest(investigationId: string) {
    requestVersion += 1;
    controller?.abort();
    controller = new AbortController();
    return {
      investigationId,
      version: requestVersion,
      signal: controller.signal,
    };
  }

  function isCurrent(investigationId: string, version: number) {
    return (
      state.investigationId === investigationId && requestVersion === version
    );
  }

  // The backend cursor walks forward in durable order: the first page is the
  // oldest, and each cursor discloses newer messages. Pages are therefore
  // appended, never prepended.
  async function loadMore() {
    const { investigationId, nextCursor } = state;
    if (!investigationId || !nextCursor) return state;
    const request = beginRequest(investigationId);
    const page = await getConversationPage(
      investigationId,
      { cursor: nextCursor, pageSize },
      fetchImpl,
      request.signal,
    );
    if (!isCurrent(investigationId, request.version)) return state;
    state = {
      investigationId,
      messages: mergeMessages(state.messages, page.messages),
      nextCursor: page.nextCursor,
    };
    return state;
  }

  // Newly persisted messages are the newest durable records, so any page the
  // Owner had not disclosed yet sits between the loaded window and them. Drain
  // forward first, then merge; mergeMessages keeps them unique when the last
  // drained page already contains them. A failed drain must not report a
  // persisted message as lost.
  async function adoptNewMessages(
    investigationId: string,
    version: number,
    messages: ConversationMessage[],
  ) {
    if (!isCurrent(investigationId, version)) return;
    try {
      while (state.investigationId === investigationId && state.nextCursor) {
        const cursorBefore = state.nextCursor;
        await loadMore();
        if (state.nextCursor === cursorBefore) break;
      }
    } catch {
      // Leave the remaining pages for an explicit "load more".
    }
    if (state.investigationId === investigationId) {
      state = { ...state, messages: mergeMessages(state.messages, messages) };
    }
  }

  return {
    getState() {
      return state;
    },

    async load(investigationId: string) {
      const request = beginRequest(investigationId);
      if (pendingSubmission?.investigationId !== investigationId) {
        pendingSubmission = null;
      }
      state = { investigationId, messages: [], nextCursor: null };
      const page = await getConversationPage(
        investigationId,
        { pageSize },
        fetchImpl,
        request.signal,
      );
      if (!isCurrent(investigationId, request.version)) return state;
      state = { investigationId, ...page };
      pendingSubmission = null;
      return state;
    },

    loadMore,

    // A just-created Investigation provably has no conversation, so this
    // session adopts it without a round trip. Fetching an empty page here would
    // only race the first submit and could drop its reply from the thread.
    startFresh(investigationId: string) {
      beginRequest(investigationId);
      pendingSubmission = null;
      state = { investigationId, messages: [], nextCursor: null };
      return state;
    },

    async submit(
      message: string,
      idempotencyKey?: string,
      model?: string,
    ): Promise<ConversationSubmitOutcome> {
      const investigationId = state.investigationId;
      if (!investigationId) {
        throw new Error("An Investigation conversation must be loaded first");
      }
      const commandKey =
        idempotencyKey ||
        (pendingSubmission?.investigationId === investigationId &&
        pendingSubmission.message === message
          ? pendingSubmission.idempotencyKey
          : createConversationIdempotencyKey());
      pendingSubmission = {
        investigationId,
        message,
        idempotencyKey: commandKey,
      };
      const request = beginRequest(investigationId);
      const outcome = await postConversationMessage(
        investigationId,
        commandKey,
        message,
        fetchImpl,
        request.signal,
        model,
      );
      await adoptNewMessages(investigationId, request.version, outcome.messages);
      if (pendingSubmission?.idempotencyKey === commandKey) {
        pendingSubmission = null;
      }
      return outcome;
    },

    // submit's streaming twin: onDelta fires for every text/thinking chunk as
    // it arrives, and the returned outcome (and the reconciliation against
    // any newer pages via adoptNewMessages) is otherwise identical to submit's
    // — a caller can freely switch between the two.
    async submitStream(
      message: string,
      onDelta: (kind: "text" | "thinking", text: string) => void,
      idempotencyKey?: string,
      model?: string,
    ): Promise<ConversationSubmitOutcome> {
      const investigationId = state.investigationId;
      if (!investigationId) {
        throw new Error("An Investigation conversation must be loaded first");
      }
      const commandKey =
        idempotencyKey ||
        (pendingSubmission?.investigationId === investigationId &&
        pendingSubmission.message === message
          ? pendingSubmission.idempotencyKey
          : createConversationIdempotencyKey());
      pendingSubmission = {
        investigationId,
        message,
        idempotencyKey: commandKey,
      };
      const request = beginRequest(investigationId);
      let terminalEvent: ConversationStreamEvent | null = null;
      await postConversationMessageStream(
        investigationId,
        commandKey,
        message,
        (event) => {
          if (event.type === "text_delta") onDelta("text", event.text);
          else if (event.type === "thinking_delta") onDelta("thinking", event.text);
          else terminalEvent = event;
        },
        fetchImpl,
        request.signal,
        model,
      );
      if (!terminalEvent) {
        throw new Error("Agent stream ended without a terminal frame");
      }
      // TS mis-narrows a `let` reassigned inside a closure to `never` here;
      // the null check above already proves this cast safe at runtime, and
      // the closure only ever assigns a "done" or "error" event to it.
      const settled = terminalEvent as Extract<
        ConversationStreamEvent,
        { type: "done" | "error" }
      >;
      const outcome: ConversationSubmitOutcome =
        settled.type === "done"
          ? { code: "agent_replied", persisted: true, messages: settled.messages }
          : { code: "agent_unavailable", persisted: true, messages: settled.messages };
      await adoptNewMessages(investigationId, request.version, outcome.messages);
      if (pendingSubmission?.idempotencyKey === commandKey) {
        pendingSubmission = null;
      }
      return outcome;
    },

    // The summary is generated once per Analysis Dataset. Asking again returns
    // the stored one, so this is safe to press twice.
    async requestSummary(): Promise<AgentSummaryOutcome> {
      const investigationId = state.investigationId;
      if (!investigationId) {
        throw new Error("An Investigation conversation must be loaded first");
      }
      const request = beginRequest(investigationId);
      const outcome = await postAgentSummary(
        investigationId,
        fetchImpl,
        request.signal,
      );
      await adoptNewMessages(investigationId, request.version, outcome.messages);
      return outcome;
    },

    clear() {
      requestVersion += 1;
      controller?.abort();
      controller = null;
      pendingSubmission = null;
      state = { investigationId: "", messages: [], nextCursor: null };
    },
  };
}

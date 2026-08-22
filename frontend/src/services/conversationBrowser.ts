import {
  createConversationIdempotencyKey,
  getConversationPage,
  postConversationMessage,
} from "@/src/middle/conversation-client";
import {
  CONVERSATION_PAGE_SIZE,
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

    async submit(
      message: string,
      idempotencyKey?: string,
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
      );
      if (isCurrent(investigationId, request.version)) {
        // The submitted pair is now the newest durable record, so any page the
        // Owner had not disclosed yet sits between the loaded window and that
        // pair. Drain forward first, then merge; mergeMessages keeps the pair
        // unique when the last drained page already contains it. A failed drain
        // must not report the persisted message as lost.
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
          state = {
            ...state,
            messages: mergeMessages(state.messages, outcome.messages),
          };
        }
      }
      if (pendingSubmission?.idempotencyKey === commandKey) {
        pendingSubmission = null;
      }
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

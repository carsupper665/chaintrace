export type ConversationMessage = {
  id: string;
  role: "user" | "system" | "agent";
  content: string;
  createdAt: string;
};

export type ConversationPage = {
  messages: ConversationMessage[];
  nextCursor: string | null;
};

export type AgentUnavailableOutcome = {
  code: "agent_unavailable";
  persisted: true;
  messages: ConversationMessage[];
};

export type AgentReplyOutcome = {
  code: "agent_replied";
  persisted: true;
  messages: ConversationMessage[];
};

export type ConversationSubmitOutcome =
  | AgentUnavailableOutcome
  | AgentReplyOutcome;

// One SSE frame from POST .../conversation/stream. text_delta/thinking_delta
// arrive zero or more times while a turn is in flight; done or error always
// end the stream. Thinking text is never persisted — it only ever exists as
// these live deltas, discarded once the turn settles.
export type ConversationStreamEvent =
  | { type: "text_delta"; text: string }
  | { type: "thinking_delta"; text: string }
  | { type: "done"; messages: ConversationMessage[] }
  | { type: "error"; code: string; messages: ConversationMessage[] };

// A summary belongs to one Analysis Dataset. Asking again for the same dataset
// returns the stored summary rather than generating a second one, which is what
// `created: false` reports.
export type AgentSummaryOutcome = {
  datasetId: string;
  created: boolean;
  messages: ConversationMessage[];
};

export type ConversationErrorResponse = {
  code?: string;
  error?: string;
  message?: string;
};

export const CONVERSATION_PAGE_SIZE = 50;

// One compatible model the Agent can run, as offered in the command bar. The
// id is what goes back with a message; the display name is for people.
export type AgentModel = {
  id: string;
  displayName: string;
};

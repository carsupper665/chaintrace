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

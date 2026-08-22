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

export type ConversationErrorResponse = {
  code?: string;
  error?: string;
  message?: string;
};

export const CONVERSATION_PAGE_SIZE = 50;

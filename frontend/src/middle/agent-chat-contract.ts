export type AgentChatRequest = {
  sessionId: string;
  message: string;
  investigation: {
    id: string;
    address: string | null;
    network: string;
  };
};

export type AgentChatResponse = {
  taskId: string;
  message: string;
  status: "accepted" | "running" | "completed";
  createdAt: string;
};

export type AgentChatError = {
  error: string;
  code: "AGENT_BACKEND_NOT_CONFIGURED" | "AGENT_BACKEND_UNAVAILABLE";
};

export const AGENT_CHAT_API_PATH = "/api/middle/agent/chat";

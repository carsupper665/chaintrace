import {
  AGENT_CHAT_API_PATH,
  type AgentChatError,
  type AgentChatRequest,
  type AgentChatResponse,
} from "./agent-chat-contract";

export class AgentChatClientError extends Error {
  constructor(
    message: string,
    public readonly code: AgentChatError["code"],
  ) {
    super(message);
  }
}

export async function sendAgentMessage(
  request: AgentChatRequest,
): Promise<AgentChatResponse> {
  const response = await fetch(AGENT_CHAT_API_PATH, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    const error = (await response.json()) as AgentChatError;
    throw new AgentChatClientError(error.error, error.code);
  }

  return (await response.json()) as AgentChatResponse;
}

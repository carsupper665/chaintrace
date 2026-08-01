import type {
  AgentChatRequest,
  AgentChatResponse,
} from "./agent-chat-contract";

export class AgentBackendError extends Error {
  constructor(
    message: string,
    public readonly code:
      | "AGENT_BACKEND_NOT_CONFIGURED"
      | "AGENT_BACKEND_UNAVAILABLE",
  ) {
    super(message);
  }
}

/**
 * Real backend adapter. No model response is generated in this repository.
 *
 * Configure CHAINTRACE_BACKEND_URL when the Go backend implements:
 * POST /api/v1/agent/chat
 */
export async function sendMessageToAgentBackend(
  request: AgentChatRequest,
): Promise<AgentChatResponse> {
  const backendUrl = process.env.CHAINTRACE_BACKEND_URL?.replace(/\/$/, "");

  if (!backendUrl) {
    throw new AgentBackendError(
      "AI Agent 後端尚未設定",
      "AGENT_BACKEND_NOT_CONFIGURED",
    );
  }

  try {
    const response = await fetch(`${backendUrl}/api/v1/agent/chat`, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(request),
    });

    if (!response.ok) {
      throw new Error(`Backend returned ${response.status}`);
    }

    return (await response.json()) as AgentChatResponse;
  } catch (error) {
    if (error instanceof AgentBackendError) throw error;
    throw new AgentBackendError(
      "AI Agent 後端目前無法連線",
      "AGENT_BACKEND_UNAVAILABLE",
    );
  }
}

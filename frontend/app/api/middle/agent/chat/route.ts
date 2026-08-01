import {
  AgentBackendError,
  sendMessageToAgentBackend,
} from "@/src/middle/agent-chat-provider";
import type { AgentChatRequest } from "@/src/middle/agent-chat-contract";

export const dynamic = "force-dynamic";

export async function POST(request: Request) {
  const payload = (await request.json()) as Partial<AgentChatRequest>;

  if (
    !payload.sessionId?.trim() ||
    !payload.message?.trim() ||
    !payload.investigation?.id ||
    !payload.investigation.network
  ) {
    return Response.json(
      { error: "sessionId, message and investigation are required" },
      { status: 400 },
    );
  }

  try {
    const result = await sendMessageToAgentBackend(payload as AgentChatRequest);
    return Response.json(result, {
      headers: { "Cache-Control": "no-store" },
    });
  } catch (error) {
    if (error instanceof AgentBackendError) {
      return Response.json(
        { error: error.message, code: error.code },
        { status: 503 },
      );
    }
    throw error;
  }
}

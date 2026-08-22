export type { Investigation } from "@/src/middle/investigation-contract";
export type { ConversationMessage as ChatMessage } from "@/src/middle/conversation-contract";

export type ContextMenuState = {
  id: string;
  x: number;
  y: number;
} | null;

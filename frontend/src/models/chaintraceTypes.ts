export type Investigation = {
  id: string;
  title: string;
  address: string;
  network: string;
  risk: number;
  relatedNodes: number;
  totalFlow: number;
  flowAsset: string;
  transactionCount: number;
  status: "分析中" | "已完成" | "待處理";
};

export type ChatMessage = {
  id: number;
  role: "user" | "agent" | "system";
  content: string;
};

export type ContextMenuState = {
  id: string;
  x: number;
  y: number;
} | null;

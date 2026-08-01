import type {
  TransactionGraphEdge,
  TransactionGraphNode,
} from "./transaction-graph-contract";

export type AnomalyReason = {
  code: string;
  title: string;
  description: string;
  contribution: number | null;
};

export type NodeSafetyAssessment = {
  nodeId: string;
  score: number;
  level: "low" | "medium" | "high" | "critical";
  summary: string;
  source: string;
};

export type AnomalyAnalysisRequest = {
  sessionId: string;
  address: string;
  network: string;
  graph: {
    nodes: TransactionGraphNode[];
    edges: TransactionGraphEdge[];
    transactionCount: number;
    totalFlow: number;
    flowAsset: string;
  };
};

export type AnomalyAnalysisResponse = {
  score: number;
  level: "low" | "medium" | "high" | "critical";
  summary: string;
  reasons: AnomalyReason[];
  interpretation: string;
  recommendations: string[];
  nodeAssessments?: NodeSafetyAssessment[];
  source: string;
  updatedAt: string;
};

export const ANOMALY_ANALYSIS_API_PATH = "/api/middle/anomaly-analysis";

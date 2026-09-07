export const INVESTIGATIONS_API_PATH = "/api/investigations";
export const TRON_NETWORK = "TRON_MAINNET" as const;
export const DEFAULT_TRANSFER_LIMIT = 500;
export const MAX_TRANSFER_LIMIT = 5000;
export const DEFAULT_TRAVERSAL_DEPTH = 2;
export const MAX_TRAVERSAL_DEPTH = 4;

export type InvestigationStatus = "待處理" | "分析中" | "已完成";
export type AnalysisRunStatus =
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "cancelled";
export type RiskLevel = "low" | "medium" | "high" | "critical";

export type ExactAmount = {
  smallestUnit: string;
  decimals: number;
  asset: string;
};

export type AnalysisScope = {
  transferLimit: number;
  traversalDepth: number;
};

export type AnalysisScopeInput = Partial<AnalysisScope>;

export type StartedAnalysisRun = {
  id: string;
  investigationId: string;
  status: "queued";
};

export type CancelledAnalysisRun = {
  id: string;
  investigationId: string;
  status: "cancelled";
};

export type AnalysisRun = {
  id: string;
  investigationId: string;
  status: AnalysisRunStatus;
  phase: string;
  collectedTransfers: number;
  resultId?: string;
  errorCode?: string;
};

export type AnalysisRunProgress =
  | StartedAnalysisRun
  | AnalysisRun
  | CancelledAnalysisRun;

export type NodeAssessment = {
  address: string;
  score: number;
  level: RiskLevel;
  reasons: string[];
};

export type CurrentAnalysisResult = {
  dataset: {
    id: string;
    network: typeof TRON_NETWORK;
    asset: string;
    windowStart: string;
    windowEnd: string;
    cutoffBlockId: string;
    transferLimit: number;
    traversalDepth: number;
    collectedTransfers: number;
    reachedDepth: number;
    partial: boolean;
    confidence: number;
    stopReason: string;
    createdAt: string;
  };
  metrics: {
    relatedNodes: number;
    transferCount: number;
    totalFlow: ExactAmount;
  };
  assessment: {
    score: number | null;
    level: RiskLevel | "";
    reasons: string[];
    nodeAssessments: NodeAssessment[];
    source: string;
    learnedScore: number | null;
    learnedScoreSource: string;
    updatedAt: string;
  };
};

export type Investigation = {
  id: string;
  title: string;
  address: string | null;
  network: typeof TRON_NETWORK;
  status: InvestigationStatus;
  risk: number | null;
  relatedNodes: number | null;
  totalFlow: ExactAmount | null;
  flowAsset: string | null;
  transactionCount: number | null;
  targetLocked: boolean;
  currentResult: string | null;
  // The Analysis Run currently collecting for this Investigation, if any. The
  // Agent can start one on the Owner's behalf, so the browser needs an id it
  // did not receive from its own start request.
  activeRun: string | null;
  createdAt: string;
  updatedAt: string;
};

export type InvestigationErrorResponse = {
  code?: string;
  error?: string;
  message?: string;
};

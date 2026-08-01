import type { AnomalyAnalysisResponse } from "@/src/middle/anomaly-analysis-contract";
import type { TransactionGraphResponse } from "@/src/middle/transaction-graph-contract";
import type {
  ChatMessage,
  Investigation,
} from "@/src/models/chaintraceTypes";

const STORAGE_KEY = "chaintrace-investigation-workspace";
const STORAGE_VERSION = 1;

export type PersistedInvestigationSession = {
  addressDraft: string;
  transactionGraph: TransactionGraphResponse | null;
  selectedNodeId: string | null;
  expandedNodeIds: string[];
  graphSpread: number;
  graphZoom: number;
  graphOffset: { x: number; y: number };
  anomalyAnalysis: AnomalyAnalysisResponse | null;
  graphError: string;
  analysisError: string;
  expansionError: string;
  chatMessages: ChatMessage[];
  query: string;
};

export type InvestigationWorkspaceSnapshot = {
  version: typeof STORAGE_VERSION;
  activeId: string;
  investigations: Investigation[];
  sessions: Record<string, PersistedInvestigationSession>;
  savedAt: string;
};

export function loadInvestigationWorkspace():
  | InvestigationWorkspaceSnapshot
  | null {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<InvestigationWorkspaceSnapshot>;
    if (
      parsed.version !== STORAGE_VERSION ||
      !parsed.activeId ||
      !Array.isArray(parsed.investigations) ||
      parsed.investigations.length === 0 ||
      !parsed.sessions ||
      typeof parsed.sessions !== "object"
    ) {
      return null;
    }
    return parsed as InvestigationWorkspaceSnapshot;
  } catch {
    return null;
  }
}

export function saveInvestigationWorkspace(
  snapshot: Omit<InvestigationWorkspaceSnapshot, "version" | "savedAt">,
) {
  try {
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        ...snapshot,
        version: STORAGE_VERSION,
        savedAt: new Date().toISOString(),
      }),
    );
  } catch (error) {
    console.warn("Unable to persist ChainTrace workspace", error);
  }
}

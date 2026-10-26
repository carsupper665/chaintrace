export type ClaimKind = "address" | "tx_hash" | "amount";
export type ClaimRole = "from" | "to" | "unknown";
export type ClaimStatus =
  | "unverified"
  | "verified"
  | "no_activity"
  | "not_found"
  | "mismatch";

export type PendingClaim = {
  id: string;
  kind: ClaimKind;
  value: string;
  role?: ClaimRole;
  asset?: "USDT";
  confidence: number;
  status: ClaimStatus;
  actual?: Record<string, unknown>;
};

export type ExtractClaimsResult = {
  network: "tron";
  claims: Array<{
    kind: ClaimKind;
    value: string;
    role?: ClaimRole;
    asset?: "USDT";
    confidence: number;
    status?: ClaimStatus;
  }>;
};

export type VerifyClaimsResult = {
  results: Array<{
    kind: ClaimKind;
    value: string;
    status: ClaimStatus;
    actual?: Record<string, unknown>;
  }>;
  prefillAddress?: string;
};

export type ClaimErrorResponse = {
  code?: string;
  error?: string;
  message?: string;
};

export const CLAIMS_API_PATH = "/api/claims";
export const LOW_CLAIM_CONFIDENCE = 0.7;
export const MAX_CLAIMS = 10;

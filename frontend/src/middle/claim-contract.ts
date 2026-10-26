export type PendingAddressClaim = {
  kind: "address";
  value: string;
  role: "from" | "to" | "unknown";
  confidence: number;
  status: "unverified" | "verified" | "no_activity";
};

export type ExtractClaimsResult = {
  network: "tron";
  claims: PendingAddressClaim[];
};

export type VerifyAddressResult = {
  address: string;
  network: "tron";
  status: "verified" | "no_activity";
};

export type ClaimErrorResponse = {
  code?: string;
  error?: string;
  message?: string;
};

export const CLAIMS_API_PATH = "/api/claims";

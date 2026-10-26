import { expireSession } from "@/src/auth/expire";
import {
  CLAIMS_API_PATH,
  type ClaimErrorResponse,
  type ExtractClaimsResult,
  type PendingAddressClaim,
  type VerifyAddressResult,
} from "./claim-contract.ts";

export type {
  ExtractClaimsResult,
  PendingAddressClaim,
  VerifyAddressResult,
} from "./claim-contract.ts";

type Fetch = typeof fetch;

export class ClaimClientError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(message: string, status: number, code: string) {
    super(message);
    this.name = "ClaimClientError";
    this.status = status;
    this.code = code;
  }
}

function stableErrorMessage(status: number, code: string) {
  if (code === "no_claim") {
    return "截圖中沒有可抽取的 TRON 地址。";
  }
  if (code === "invalid_claim_address" || code === "malformed_claims") {
    return "截圖抽取結果無效。";
  }
  if (code === "invalid_image" || code === "image_too_large") {
    return "請上傳有效且未超過大小限制的 PNG 截圖。";
  }
  if (code === "agent_unavailable" || status === 503 || status === 502) {
    return "Claim 抽取服務目前無法使用。";
  }
  if (code === "provider_unavailable") {
    return "鏈上驗證服務目前無法使用。";
  }
  if (status === 401 || code === "unauthorized") {
    expireSession();
    return "登入狀態已失效，請重新登入。";
  }
  return "無法完成 Claim 請求，請稍後再試。";
}

export function shouldApplyClaimResult(args: {
  requestInvestigationId: string;
  activeInvestigationId: string | null | undefined;
  requestGeneration: number;
  currentGeneration: number;
  requestClaimValue?: string;
  activeClaimValue?: string | null;
}) {
  if (args.requestGeneration !== args.currentGeneration) return false;
  if (args.requestInvestigationId !== args.activeInvestigationId) return false;
  if (
    args.requestClaimValue !== undefined &&
    args.requestClaimValue !== args.activeClaimValue
  ) {
    return false;
  }
  return true;
}

export async function extractClaimsFromPng(
  file: File,
  fetchImpl: Fetch = fetch,
): Promise<ExtractClaimsResult> {
  const body = new FormData();
  body.append("image", file, file.name || "screenshot.png");
  const response = await fetchImpl(`${CLAIMS_API_PATH}/extract`, {
    method: "POST",
    headers: { Accept: "application/json" },
    body,
    cache: "no-store",
  });
  const payload = response.ok
    ? await response.json()
    : await response.json().catch(() => null);
  if (!response.ok) {
    const error = (payload || {}) as ClaimErrorResponse;
    const code = error.code || `HTTP_${response.status}`;
    throw new ClaimClientError(
      stableErrorMessage(response.status, code),
      response.status,
      code,
    );
  }
  const result = payload as ExtractClaimsResult;
  if (
    result.network !== "tron" ||
    !Array.isArray(result.claims) ||
    result.claims.length !== 1
  ) {
    throw new ClaimClientError("截圖抽取結果無效。", 502, "malformed_claims");
  }
  return result;
}

export async function verifyClaimAddress(
  address: string,
  fetchImpl: Fetch = fetch,
): Promise<VerifyAddressResult> {
  const response = await fetchImpl(`${CLAIMS_API_PATH}/verify-address`, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ address: address.trim() }),
    cache: "no-store",
  });
  const payload = response.ok
    ? await response.json()
    : await response.json().catch(() => null);
  if (!response.ok) {
    const error = (payload || {}) as ClaimErrorResponse;
    const code = error.code || `HTTP_${response.status}`;
    throw new ClaimClientError(
      stableErrorMessage(response.status, code),
      response.status,
      code,
    );
  }
  return payload as VerifyAddressResult;
}

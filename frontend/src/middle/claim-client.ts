import { expireSession } from "@/src/auth/expire";
import {
  CLAIMS_API_PATH,
  type ClaimErrorResponse,
  type ExtractClaimsResult,
  type PendingClaim,
  type VerifyClaimsResult,
} from "./claim-contract.ts";

export type {
  ExtractClaimsResult,
  PendingClaim,
  VerifyClaimsResult,
} from "./claim-contract.ts";

export {
  LOW_CLAIM_CONFIDENCE,
  MAX_CLAIMS,
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

export function stableErrorMessage(status: number, code: string) {
  if (code === "no_claim") {
    return "截圖中沒有可抽取的 Claim。";
  }
  if (code === "unsupported_media") {
    return "只接受 PNG、JPEG 或 WebP 截圖。";
  }
  if (code === "invalid_claim_address" || code === "malformed_claims") {
    return "截圖抽取結果無效。";
  }
  if (code === "invalid_image" || code === "image_too_large") {
    return "請上傳有效且未超過大小限制的截圖。";
  }
  if (code === "provider_unavailable") {
    return "鏈上驗證服務目前無法使用。";
  }
  if (code === "agent_unavailable" || status === 503 || status === 502) {
    return "Claim 抽取服務目前無法使用。";
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

export function claimsFromExtractResult(
  result: ExtractClaimsResult,
  idFactory: () => string = () => crypto.randomUUID(),
): PendingClaim[] {
  return result.claims.map((claim) => ({
    id: idFactory(),
    kind: claim.kind,
    value: claim.value,
    role: claim.role,
    asset: claim.asset,
    confidence: claim.confidence,
    status: "unverified",
  }));
}

async function readClaimError(
  response: Response,
): Promise<ClaimClientError> {
  const payload = await response.json().catch(() => null);
  const error = (payload || {}) as ClaimErrorResponse;
  const code = error.code || `HTTP_${response.status}`;
  return new ClaimClientError(
    stableErrorMessage(response.status, code),
    response.status,
    code,
  );
}

export async function extractClaimsFromImage(
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
  if (!response.ok) {
    throw await readClaimError(response);
  }
  const result = (await response.json()) as ExtractClaimsResult;
  if (
    result.network !== "tron" ||
    !Array.isArray(result.claims) ||
    result.claims.length < 1
  ) {
    throw new ClaimClientError("截圖抽取結果無效。", 502, "malformed_claims");
  }
  return result;
}

export async function verifyClaims(
  input: {
    selectedAddress: string;
    claims: Array<{
      kind: string;
      value: string;
      role?: string;
      asset?: string;
      confidence: number;
    }>;
  },
  fetchImpl: Fetch = fetch,
): Promise<VerifyClaimsResult> {
  const response = await fetchImpl(`${CLAIMS_API_PATH}/verify`, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
    cache: "no-store",
  });
  if (!response.ok) {
    throw await readClaimError(response);
  }
  return (await response.json()) as VerifyClaimsResult;
}

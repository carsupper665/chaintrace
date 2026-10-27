import type { PendingClaim } from "@/src/middle/claim-contract";

/** Pick the address Claim the Agent should ask the Owner to confirm. */
export function pickConfirmAddress(claims: PendingClaim[]): PendingClaim | null {
  const addresses = claims.filter((claim) => claim.kind === "address" && claim.value);
  if (addresses.length === 0) return null;
  const preferred =
    addresses.find((claim) => claim.role === "to") ||
    addresses.find((claim) => claim.role !== "from") ||
    addresses[0];
  return preferred;
}

export function claimConfirmPrompt(address: string): string {
  return `截圖裡看到地址 ${address}，要調查這個地址對嗎？我可能看錯，請確認一下。`;
}

export function claimConfirmNoAddressMessage(): string {
  return "這張截圖裡沒有辨識出可調查的 TRON 地址。請再傳一張清楚一點的圖，或直接貼上地址。";
}

export function claimConfirmAcceptedMessage(address: string): string {
  return `已確認地址 ${address}，已設為調查目標。可以開始分析。`;
}

export function claimConfirmRejectedMessage(): string {
  return "好，那先不採用這個地址。請再傳截圖，或直接貼上正確的 TRON 地址。";
}

/** Owner affirms the proposed address reading (Agent may have OCR'd wrong). */
export function isAffirmativeClaimReply(text: string): boolean {
  const normalized = text.trim().toLowerCase().replace(/\s+/g, "");
  if (!normalized) return false;
  return /^(對|對的|對啊|對呀|沒錯|是|是的|好|好的|要|可以|確認|yes|y|ok|okay|correct)[!！.。]*$/.test(
    normalized,
  );
}

export function isNegativeClaimReply(text: string): boolean {
  const normalized = text.trim().toLowerCase().replace(/\s+/g, "");
  if (!normalized) return false;
  return /^(不|不是|不對|不對吧|錯|錯了|不要|否|no|n|wrong)[!！.。]*$/.test(
    normalized,
  );
}

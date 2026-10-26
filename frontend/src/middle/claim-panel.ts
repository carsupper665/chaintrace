import type {
  ExtractClaimsResult,
  PendingClaim,
  VerifyClaimsResult,
} from "./claim-contract.ts";
import { claimsFromExtractResult } from "./claim-client.ts";

export type ClaimPanelState = {
  investigationId: string;
  generation: number;
  pendingClaims: PendingClaim[];
  selectedClaimId: string | null;
  addressDraft: string;
  claimMessage: string;
  claimError: string;
  isExtracting: boolean;
  isVerifying: boolean;
};

export function createClaimPanel(investigationId: string): ClaimPanelState {
  return {
    investigationId,
    generation: 0,
    pendingClaims: [],
    selectedClaimId: null,
    addressDraft: "",
    claimMessage: "",
    claimError: "",
    isExtracting: false,
    isVerifying: false,
  };
}

export function beginClaimExtract(state: ClaimPanelState): ClaimPanelState {
  return {
    ...state,
    generation: state.generation + 1,
    pendingClaims: [],
    selectedClaimId: null,
    claimError: "",
    claimMessage: "",
    isExtracting: true,
    isVerifying: false,
  };
}

export function applyClaimExtractSuccess(
  state: ClaimPanelState,
  request: { investigationId: string; generation: number },
  result: ExtractClaimsResult,
  idFactory?: () => string,
): ClaimPanelState {
  if (
    request.generation !== state.generation ||
    request.investigationId !== state.investigationId
  ) {
    return state;
  }
  const pendingClaims = claimsFromExtractResult(result, idFactory);
  const firstAddress = pendingClaims.find((claim) => claim.kind === "address");
  return {
    ...state,
    pendingClaims,
    selectedClaimId: firstAddress?.id ?? null,
    claimMessage: `已抽出 ${pendingClaims.length} 筆待驗證 Claim；請確認後再驗證鏈上結果。`,
    claimError: "",
    isExtracting: false,
  };
}

export function applyClaimExtractError(
  state: ClaimPanelState,
  request: { investigationId: string; generation: number },
  message: string,
): ClaimPanelState {
  if (
    request.generation !== state.generation ||
    request.investigationId !== state.investigationId
  ) {
    return state;
  }
  return {
    ...state,
    claimError: message,
    claimMessage: "",
    isExtracting: false,
  };
}

export function updateClaimValue(
  state: ClaimPanelState,
  id: string,
  value: string,
): ClaimPanelState {
  const next = value.trim();
  const pendingClaims = state.pendingClaims.map((claim) =>
    claim.id === id
      ? { ...claim, value: next, status: "unverified" as const, actual: undefined }
      : claim,
  );
  return {
    ...state,
    pendingClaims,
    addressDraft: state.selectedClaimId === id ? "" : state.addressDraft,
    claimMessage: "Claim 已修改，需重新驗證。",
    claimError: "",
  };
}

export function removeClaim(state: ClaimPanelState, id: string): ClaimPanelState {
  const pendingClaims = state.pendingClaims.filter((claim) => claim.id !== id);
  let selectedClaimId = state.selectedClaimId;
  let addressDraft = state.addressDraft;
  if (state.selectedClaimId === id) {
    selectedClaimId =
      pendingClaims.find((claim) => claim.kind === "address")?.id ?? null;
    addressDraft = "";
  }
  return {
    ...state,
    pendingClaims,
    selectedClaimId,
    addressDraft,
    claimMessage: "Claim 已刪除；驗證狀態已清除。",
    claimError: "",
  };
}

export function selectClaim(
  state: ClaimPanelState,
  id: string,
): ClaimPanelState {
  const claim = state.pendingClaims.find((item) => item.id === id);
  if (!claim || claim.kind !== "address") return state;
  return {
    ...state,
    selectedClaimId: id,
    addressDraft: claim.status === "verified" ? claim.value : "",
  };
}

export function beginClaimVerify(state: ClaimPanelState): ClaimPanelState {
  return {
    ...state,
    generation: state.generation + 1,
    claimError: "",
    claimMessage: "",
    isVerifying: true,
  };
}

export function applyClaimVerifySuccess(
  state: ClaimPanelState,
  request: {
    investigationId: string;
    generation: number;
    selectedValue: string;
  },
  result: VerifyClaimsResult,
): ClaimPanelState {
  const selected = state.pendingClaims.find(
    (claim) => claim.id === state.selectedClaimId,
  );
  if (
    request.generation !== state.generation ||
    request.investigationId !== state.investigationId ||
    !selected ||
    selected.value !== request.selectedValue
  ) {
    return state;
  }
  const pendingClaims = state.pendingClaims.map((claim) => {
    const match = result.results.find(
      (item) => item.kind === claim.kind && item.value === claim.value,
    );
    return match
      ? { ...claim, status: match.status, actual: match.actual }
      : claim;
  });
  const prefill =
    result.prefillAddress && result.prefillAddress === selected.value
      ? result.prefillAddress
      : "";
  return {
    ...state,
    pendingClaims,
    addressDraft: prefill || state.addressDraft,
    claimMessage: prefill
      ? "地址已通過鏈上驗證，已預填調查目標；請確認後按「儲存目標」。"
      : "驗證完成；目前選定的地址不能預填調查目標。",
    claimError: "",
    isVerifying: false,
  };
}

export function applyClaimVerifyError(
  state: ClaimPanelState,
  request: {
    investigationId: string;
    generation: number;
    selectedValue: string;
  },
  message: string,
): ClaimPanelState {
  const selected = state.pendingClaims.find(
    (claim) => claim.id === state.selectedClaimId,
  );
  if (
    request.generation !== state.generation ||
    request.investigationId !== state.investigationId ||
    !selected ||
    selected.value !== request.selectedValue
  ) {
    return state;
  }
  return {
    ...state,
    claimError: message,
    claimMessage: "",
    isVerifying: false,
  };
}

export function clearClaimPanel(state: ClaimPanelState): ClaimPanelState {
  return {
    ...state,
    generation: state.generation + 1,
    pendingClaims: [],
    selectedClaimId: null,
    claimError: "",
    claimMessage: "",
    isExtracting: false,
    isVerifying: false,
  };
}

export function switchClaimInvestigation(
  state: ClaimPanelState,
  investigationId: string,
): ClaimPanelState {
  return {
    ...createClaimPanel(investigationId),
    generation: state.generation + 1,
  };
}

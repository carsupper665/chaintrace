import assert from "node:assert/strict";
import test from "node:test";

const addressA = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE";
const addressB = "TMwFHYXLJaRUPeW6421aqXL4ZEzPRFGkGT";

test("claim panel journey: extract → edit/delete/select → verify → prefill → stale ignored", async () => {
  const {
    applyClaimExtractError,
    applyClaimExtractSuccess,
    applyClaimVerifyError,
    applyClaimVerifySuccess,
    beginClaimExtract,
    beginClaimVerify,
    clearClaimPanel,
    createClaimPanel,
    removeClaim,
    selectClaim,
    switchClaimInvestigation,
    updateClaimValue,
  } = await import("../src/middle/claim-panel.ts");
  const { extractClaimsFromImage, verifyClaims, ClaimClientError } =
    await import("../src/middle/claim-client.ts");

  let ids = 0;
  const nextId = () => `c${++ids}`;
  let state = createClaimPanel("inv-1");

  // Upload / extract
  state = beginClaimExtract(state);
  const extractGeneration = state.generation;
  const file = new File([Uint8Array.from([137, 80, 78, 71])], "shot.png", {
    type: "image/png",
  });
  const extracted = await extractClaimsFromImage(file, async () =>
    Response.json({
      network: "tron",
      claims: [
        { kind: "address", value: addressA, role: "to", confidence: 0.91 },
        { kind: "address", value: addressB, role: "from", confidence: 0.55 },
        {
          kind: "amount",
          value: "1500000",
          asset: "USDT",
          confidence: 0.8,
        },
      ],
    }),
  );
  state = applyClaimExtractSuccess(
    state,
    { investigationId: "inv-1", generation: extractGeneration },
    extracted,
    nextId,
  );
  assert.equal(state.pendingClaims.length, 3);
  assert.equal(state.selectedClaimId, "c1");
  assert.match(state.claimMessage, /3 筆/);

  // Edit clears verification path and keeps selection
  state = updateClaimValue(state, "c1", `${addressA}`);
  assert.equal(state.pendingClaims[0].status, "unverified");
  assert.equal(state.addressDraft, "");

  // Delete the low-confidence peer; selected stays on addressA
  state = removeClaim(state, "c2");
  assert.equal(state.pendingClaims.length, 2);
  assert.equal(state.selectedClaimId, "c1");

  // Select remains addressA; verify + prefill
  state = selectClaim(state, "c1");
  state = beginClaimVerify(state);
  const verifyGeneration = state.generation;
  const selectedValue = state.pendingClaims.find(
    (claim) => claim.id === state.selectedClaimId,
  ).value;
  const verified = await verifyClaims(
    {
      selectedAddress: selectedValue,
      claims: state.pendingClaims.map((claim) => ({
        kind: claim.kind,
        value: claim.value,
        role: claim.role,
        asset: claim.asset,
        confidence: claim.confidence,
      })),
    },
    async () =>
      Response.json({
        prefillAddress: addressA,
        results: [
          { kind: "address", value: addressA, status: "verified" },
          { kind: "amount", value: "1500000", status: "verified" },
        ],
      }),
  );
  state = applyClaimVerifySuccess(
    state,
    {
      investigationId: "inv-1",
      generation: verifyGeneration,
      selectedValue,
    },
    verified,
  );
  assert.equal(state.addressDraft, addressA);
  assert.match(state.claimMessage, /已預填調查目標/);
  assert.equal(state.pendingClaims[0].status, "verified");

  // Stale verify response after clear must not re-apply
  const stale = beginClaimVerify(state);
  const staleGeneration = stale.generation;
  state = clearClaimPanel(stale);
  state = applyClaimVerifySuccess(
    state,
    {
      investigationId: "inv-1",
      generation: staleGeneration,
      selectedValue: addressA,
    },
    verified,
  );
  assert.equal(state.pendingClaims.length, 0);
  assert.equal(state.addressDraft, addressA); // draft kept; claims cleared
  assert.equal(state.selectedClaimId, null);

  // Switching investigation drops unverified claim state (reload semantics)
  state = switchClaimInvestigation(state, "inv-2");
  assert.equal(state.investigationId, "inv-2");
  assert.equal(state.pendingClaims.length, 0);
  assert.equal(state.addressDraft, "");
  assert.equal(state.claimMessage, "");

  // Provider failure surfaces distinctly from agent failure
  state = beginClaimExtract(state);
  const failGeneration = state.generation;
  try {
    await verifyClaims(
      {
        selectedAddress: addressA,
        claims: [
          {
            kind: "address",
            value: addressA,
            role: "to",
            confidence: 0.9,
          },
        ],
      },
      async () =>
        Response.json(
          { code: "provider_unavailable", message: "down" },
          { status: 502 },
        ),
    );
    assert.fail("expected provider failure");
  } catch (error) {
    assert.ok(error instanceof ClaimClientError);
    assert.equal(error.code, "provider_unavailable");
    state = applyClaimVerifyError(
      state,
      {
        investigationId: "inv-2",
        generation: failGeneration,
        selectedValue: addressA,
      },
      error.message,
    );
    // extract generation was bumped; verify error for wrong selected is ignored
    assert.equal(state.isExtracting, true);
  }

  state = applyClaimExtractError(
    state,
    { investigationId: "inv-2", generation: failGeneration },
    "截圖中沒有可抽取的 Claim。",
  );
  assert.equal(state.isExtracting, false);
  assert.match(state.claimError, /沒有可抽取/);
});

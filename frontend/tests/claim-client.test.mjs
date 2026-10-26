import assert from "node:assert/strict";
import test from "node:test";

const validAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE";

test("shouldApplyClaimResult rejects stale generation and switched investigations", async () => {
  const { shouldApplyClaimResult } = await import(
    "../src/middle/claim-client.ts"
  );

  assert.equal(
    shouldApplyClaimResult({
      requestInvestigationId: "inv-1",
      activeInvestigationId: "inv-1",
      requestGeneration: 2,
      currentGeneration: 2,
    }),
    true,
  );
  assert.equal(
    shouldApplyClaimResult({
      requestInvestigationId: "inv-1",
      activeInvestigationId: "inv-2",
      requestGeneration: 2,
      currentGeneration: 2,
    }),
    false,
  );
  assert.equal(
    shouldApplyClaimResult({
      requestInvestigationId: "inv-1",
      activeInvestigationId: "inv-1",
      requestGeneration: 2,
      currentGeneration: 3,
    }),
    false,
  );
  assert.equal(
    shouldApplyClaimResult({
      requestInvestigationId: "inv-1",
      activeInvestigationId: "inv-1",
      requestGeneration: 2,
      currentGeneration: 2,
      requestClaimValue: validAddress,
      activeClaimValue: "other",
    }),
    false,
  );
});

test("extractClaimsFromPng posts multipart and returns one pending claim", async () => {
  const { extractClaimsFromPng } = await import("../src/middle/claim-client.ts");
  const requests = [];
  const file = new File([Uint8Array.from([137, 80, 78, 71])], "shot.png", {
    type: "image/png",
  });

  const result = await extractClaimsFromPng(file, async (input, init = {}) => {
    requests.push({
      path: String(input),
      method: init.method,
      body: init.body,
    });
    return Response.json({
      network: "tron",
      claims: [
        {
          kind: "address",
          value: validAddress,
          role: "to",
          confidence: 0.9,
          status: "unverified",
        },
      ],
    });
  });

  assert.equal(requests.length, 1);
  assert.equal(requests[0].path, "/api/claims/extract");
  assert.equal(requests[0].method, "POST");
  assert.ok(requests[0].body instanceof FormData);
  assert.equal(result.claims[0].value, validAddress);
});

test("extractClaimsFromPng surfaces no_claim without inventing success", async () => {
  const { ClaimClientError, extractClaimsFromPng } = await import(
    "../src/middle/claim-client.ts"
  );
  const file = new File([Uint8Array.from([1, 2, 3])], "empty.png", {
    type: "image/png",
  });

  await assert.rejects(
    () =>
      extractClaimsFromPng(file, async () =>
        Response.json(
          { code: "no_claim", message: "none" },
          { status: 400 },
        ),
      ),
    (error) =>
      error instanceof ClaimClientError &&
      error.code === "no_claim" &&
      /沒有可抽取/.test(error.message),
  );
});

test("verifyClaimAddress returns verified status for activity probe", async () => {
  const { verifyClaimAddress } = await import("../src/middle/claim-client.ts");
  const result = await verifyClaimAddress(validAddress, async (input, init = {}) => {
    assert.equal(String(input), "/api/claims/verify-address");
    assert.equal(init.method, "POST");
    assert.deepEqual(JSON.parse(String(init.body)), { address: validAddress });
    return Response.json({
      address: validAddress,
      network: "tron",
      status: "verified",
    });
  });
  assert.equal(result.status, "verified");
});

test("verifyClaims posts selected address and bounded claim set", async () => {
  const { claimsFromExtractResult, verifyClaims } = await import(
    "../src/middle/claim-client.ts"
  );
  const pending = claimsFromExtractResult(
    {
      network: "tron",
      claims: [
        {
          kind: "address",
          value: validAddress,
          role: "to",
          confidence: 0.9,
        },
        {
          kind: "amount",
          value: "1500000",
          asset: "USDT",
          confidence: 0.8,
        },
      ],
    },
    () => "id-1",
  );
  assert.equal(pending[0].status, "unverified");
  assert.equal(pending[0].id, "id-1");

  const result = await verifyClaims(
    {
      selectedAddress: validAddress,
      claims: pending.map((claim) => ({
        kind: claim.kind,
        value: claim.value,
        role: claim.role,
        asset: claim.asset,
        confidence: claim.confidence,
      })),
    },
    async (input, init = {}) => {
      assert.equal(String(input), "/api/claims/verify");
      assert.equal(init.method, "POST");
      return Response.json({
        prefillAddress: validAddress,
        results: [
          { kind: "address", value: validAddress, status: "verified" },
          { kind: "amount", value: "1500000", status: "verified" },
        ],
      });
    },
  );
  assert.equal(result.prefillAddress, validAddress);
  assert.equal(result.results[0].status, "verified");
});

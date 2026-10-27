import assert from "node:assert/strict";
import test from "node:test";

test("claim confirm helpers pick address and classify Owner replies", async () => {
  const {
    pickConfirmAddress,
    claimConfirmPrompt,
    isAffirmativeClaimReply,
    isNegativeClaimReply,
  } = await import("../src/middle/claim-confirm.ts");

  const picked = pickConfirmAddress([
    {
      id: "1",
      kind: "amount",
      value: "1",
      asset: "USDT",
      confidence: 0.9,
      status: "unverified",
    },
    {
      id: "2",
      kind: "address",
      value: "TFrom",
      role: "from",
      confidence: 0.8,
      status: "unverified",
    },
    {
      id: "3",
      kind: "address",
      value: "TTo",
      role: "to",
      confidence: 0.7,
      status: "unverified",
    },
  ]);
  assert.equal(picked?.value, "TTo");
  assert.match(claimConfirmPrompt("TTo"), /TTo/);
  assert.match(claimConfirmPrompt("TTo"), /看錯/);

  assert.equal(isAffirmativeClaimReply("對"), true);
  assert.equal(isAffirmativeClaimReply("沒錯"), true);
  assert.equal(isAffirmativeClaimReply("yes"), true);
  assert.equal(isAffirmativeClaimReply("調查一下"), false);
  assert.equal(isNegativeClaimReply("不是"), true);
  assert.equal(isNegativeClaimReply("錯了"), true);
  assert.equal(isNegativeClaimReply("對"), false);
});

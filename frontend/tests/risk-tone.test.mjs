import assert from "node:assert/strict";
import test from "node:test";

import { getRiskTone } from "../src/utils/riskTone.ts";

test("risk tones follow the authoritative severity boundaries", () => {
  assert.deepEqual(
    [24, 25, 49, 50, 74, 75].map((score) => getRiskTone(score)),
    ["safe", "caution", "caution", "danger", "danger", "danger"],
  );
});

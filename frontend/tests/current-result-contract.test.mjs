import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// Mirrors CurrentAnalysisResult in src/middle/investigation-contract.ts. TypeScript
// types are erased at runtime, so this list — not the type — is what actually holds
// the mirror against the backend contract fixture. Update both together, never one.
const MIRRORED_KEYS = [
  "assessment",
  "assessment.level",
  "assessment.nodeAssessments",
  "assessment.nodeAssessments[].address",
  "assessment.nodeAssessments[].level",
  "assessment.nodeAssessments[].reasons",
  "assessment.nodeAssessments[].score",
  "assessment.reasons",
  "assessment.score",
  "assessment.source",
  "assessment.updatedAt",
  "dataset",
  "dataset.asset",
  "dataset.collectedTransfers",
  "dataset.confidence",
  "dataset.createdAt",
  "dataset.cutoffBlockId",
  "dataset.id",
  "dataset.network",
  "dataset.partial",
  "dataset.reachedDepth",
  "dataset.stopReason",
  "dataset.transferLimit",
  "dataset.traversalDepth",
  "dataset.windowEnd",
  "dataset.windowStart",
  "metrics",
  "metrics.relatedNodes",
  "metrics.totalFlow",
  "metrics.totalFlow.asset",
  "metrics.totalFlow.decimals",
  "metrics.totalFlow.smallestUnit",
  "metrics.transferCount",
];

function keyPaths(node, prefix = "", paths = []) {
  if (Array.isArray(node)) {
    if (node.length > 0) keyPaths(node[0], `${prefix}[]`, paths);
    return paths;
  }
  if (node === null || typeof node !== "object") return paths;
  for (const [key, child] of Object.entries(node)) {
    const path = prefix ? `${prefix}.${key}` : key;
    paths.push(path);
    keyPaths(child, path, paths);
  }
  return paths;
}

test("CurrentAnalysisResult mirrors the backend current-result contract", async () => {
  const fixture = JSON.parse(
    await readFile(
      new URL("./fixtures/current-result-contract.json", import.meta.url),
      "utf8",
    ),
  );

  assert.deepEqual(keyPaths(fixture).sort(), MIRRORED_KEYS);
});

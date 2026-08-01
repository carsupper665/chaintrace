export type RiskTone = "safe" | "caution" | "danger";

export function getRiskTone(score: number): RiskTone {
  if (score < 30) return "safe";
  if (score < 70) return "caution";
  return "danger";
}

export function getRiskLabel(score: number) {
  const tone = getRiskTone(score);
  if (tone === "safe") return "低風險";
  if (tone === "caution") return "中度風險";
  return "高風險";
}

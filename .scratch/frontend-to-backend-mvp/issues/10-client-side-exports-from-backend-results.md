# 10 — 以 backend result 產生 client-side CSV 與 PDF

**What to build:** Owner 從已完成 Investigation 匯出時，Web 綁定單一 current Dataset，讀取 PostgreSQL 的全部 Transfer pages、metrics、rules-v1 assessment 與 Coverage，在 browser 產生精確 CSV/PDF，不使用 localStorage、visible subset 或模擬 Interpretation。

**Blocked by:** 05 — 完成 bounded multi-hop Analysis 與 rules-v1; 07 — 分頁呈現 saved Transaction Graph.

**Status:** ready-for-agent

- [ ] CSV 匯出同一 Dataset 的全部 Transfer pages，每列保留 transaction/event identity、timestamp、from/to、Asset、smallest-unit amount、decimals 與 analysis context；visible filters 不遺漏 evidence。
- [ ] Exact amount 以文字輸出並防止 spreadsheet scientific notation 或 JavaScript floating-point 失真；同 transaction 的多 Transfer events 各自成列。
- [ ] PDF 使用 authoritative Target、metrics、rules-v1 assessment、window、Coverage、confidence、partial/stop reason、Dataset ID 與 evaluator version，清楚標示 bounded evidence。
- [ ] 沒有 AgentProvider 時 PDF 不生成或暗示 AI Interpretation；Partial Analysis、zero evidence 與 `AGENT_UNAVAILABLE` 不被包裝成完整安全結論。
- [ ] Export 開始後固定 result identity；新 Analysis Run 同時完成、stale cursor、unauthorized、deleted record 或 page failure 不會混用版本或輸出半套檔案。
- [ ] 完整測試先經 Web analysis 建立 PostgreSQL multi-page Dataset，再觸發 CSV/PDF並驗證內容；涵蓋超大/最小 amount、multi-event transaction、partial labels、版本一致性、mobile/desktop trigger 與 client-side generation。

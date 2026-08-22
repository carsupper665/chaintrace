# 05 — 完成 bounded multi-hop Analysis 與 rules-v1

**What to build:** Owner 可在固定最近 30 天的 Analysis Window 下選擇允許的 Transfer Limit 與 Traversal Depth；backend 建立 bounded multi-hop Dataset、誠實回報 Coverage/Partial Analysis，使用單一可替換 rules-v1 evaluator，Web 顯示 exact metrics、risk、confidence 與停止原因。

**Blocked by:** 04 — 打通單筆 Transfer Analysis tracer bullet.

**Status:** ready-for-agent

- [ ] Analysis Window 固定為 run 開始時 confirmed block cutoff 往前 30 天；default/max Transfer Limit 為 500/5000，default/max Traversal Depth 為 2/4，Web controls 與 API 同時限制無效值。
- [ ] Collector 依穩定 traversal order 分頁、追蹤 visited addresses、以 Transfer event identity 去重並遵守 limit/depth；Graph rendering、filter 或 pagination 不改變 Dataset。
- [ ] 正常 source/window/limit/depth boundary 與 provider/rate/resource interruption 使用不同 stop reasons；有可用 evidence 的非預期中止可發布 `partial=true`，零 usable evidence 不得發布低風險分數。
- [ ] Coverage 回傳 requested/collected Transfers、requested/reached depth、window/cutoff、partial、confidence 與 stop reason；Partial Analysis confidence 低於相同 evidence 的正常 bounded result。
- [ ] 單一 replaceable rules-v1 evaluator 實作 fan-out `>=10/+20`、fan-in `>=10/+15`、60 分鐘內轉出 `>=80%/+25`、24 小時內至少兩 hops 且每 hop 轉出 `>=70%/+25`、2–4 hops 回流 `+15`，總分 cap 100。
- [ ] Severity 為 0–24 low、25–49 medium、50–74 high、75–100 critical；normal confidence 100，partial confidence `min(75, floor(100 * collected/requested))`，零 evidence confidence 0 且 score null；所有 thresholds 集中可替換，source 標示 `rules-v1` 而非 ML。
- [ ] Web list、summary 與 analysis panel 顯示同一 current Dataset 的 exact metrics、score、reasons、node assessments、coverage 與 partial warning，不使用 visible graph 或 pending-zero placeholders 覆寫結果。
- [ ] 完整 fixture journey 從 Web 設定 default/max scope、啟動分析到查驗 PostgreSQL Dataset/Transfers/assessment；測試涵蓋 loops、duplicate pages、multi-event transaction、每條 rule、score cap、partial、零 evidence 與超過 JavaScript safe integer 的 amount。

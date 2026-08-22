# 08 — 完成 Analysis Run 取消、遺失與 Target 鎖定

**What to build:** Owner 可從 Web 取消或重試 Analysis Run；backend restart 使 in-memory run 消失時回 `RUN_LOST` 並恢復前一 stable result。第一次成功 publication 後 Target 永久鎖定，re-analysis、failure 與 delete race 都不破壞 SQL 一致性。

**Blocked by:** 04 — 打通單筆 Transfer Analysis tracer bullet.

**Status:** ready-for-agent

- [ ] Web 可取消 authenticated Owner 的 queued/running run；cancellation 傳遞到 provider/evaluator，cancel 與 publication 競爭只有一個 terminal winner，cancelled run 不發布任何新結果。
- [ ] In-memory run registry 遺失時 polling 回 Owner-scoped `RUN_LOST`；frontend 停止 polling、reload SQL current result，無舊結果回 `待處理`，有舊結果回 `已完成`，並允許重新提交。
- [ ] Re-analysis 期間 Web 繼續顯示前一 stable result；failed/cancelled/lost run 不清除它，成功 publication 才原子切換 current Dataset。
- [ ] 第一次 full 或 usable partial result 成功發布後，PostgreSQL 鎖定 address + Network；Web 顯示 read-only，直接 API bypass 回 target-immutable conflict，新目標必須建立新 Investigation。
- [ ] Delete 有 active run 的 Investigation 時先取消工作並保證 worker 不能發布 orphan result；durable Dataset、Transaction、Transfer、assessment、metrics 與 conversation 都不可再讀。
- [ ] 完整 browser/API/PostgreSQL tests 覆蓋 queued cancel、mid-provider cancel、cancel/publication race、registry loss、retry、previous result、first-completion lock、cross-Owner operation 與 active-delete race。

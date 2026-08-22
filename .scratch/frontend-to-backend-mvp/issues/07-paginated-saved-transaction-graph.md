# 07 — 分頁呈現 saved Transaction Graph

**What to build:** Owner 開啟已完成 Investigation 時，Web 從 PostgreSQL current Analysis Dataset 分頁取得 Transaction Graph；首屏與 Graph Expansion 只揭露同一 Dataset 的更多 Transfer relationships，frontend 負責 coordinates 與 rendering。

**Blocked by:** 04 — 打通單筆 Transfer Analysis tracer bullet.

**Status:** ready-for-agent

- [ ] Graph API 僅讀 authenticated Owner 的 current Dataset，以穩定 Dataset-bound cursor/order 分頁；stale Dataset、deleted Investigation 或跨 Owner request 使用 stable errors。
- [ ] Backend nodes 表示 unique addresses，edges 一對一表示 TRC20 Transfer events並帶 parent transaction/event identity、timestamp、Asset、smallest-unit amount 與 decimals。
- [ ] Backend 不保存或回傳 x/y、visual group、zoom、pan、selection 或 view history；frontend enrich rendering fields 並以 Dataset ID 隔離 local visual state。
- [ ] Double-click/expand 與 load-more 只查 saved Dataset 中的相鄰 relationships，merge 時正確去重，不呼叫 ChainDataProvider、不建立 Dataset、不重跑 evaluator、不改 Investigation Status。
- [ ] Dataset metrics 與 assessment 不因 visible page、filter、undo、layout 或 selection 改變；UI 明確區分 Dataset totals 與 visible counts。
- [ ] 完整測試先透過 Web Analysis Run 建立 PostgreSQL multi-page fixture Dataset，再從 browser load/expand graph；同時斷言 API cursors、SQL rows、exact amounts、stale rejection 及 provider/evaluator 呼叫次數不增加。

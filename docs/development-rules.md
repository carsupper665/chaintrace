# 開發守則

**最高守則：簡單、精簡、快速、邊界清楚。**

任何設計違反這四點，先砍掉重想，不要先寫再說。

## 1. 四條總則

1. **簡單** — 一個函式解決的事，不要開一個 package。看得懂比看起來厲害重要。
2. **精簡** — 不寫沒人呼叫的程式、不加沒人要的設定、不留「以後可能會用到」的抽象。
3. **快速** — 同步路徑不做慢事。慢的事丟背景（如 Analysis Run），或設 timeout 直接放棄。
4. **邊界清楚** — 每個 process 只做自己那層的事。跨界的需求要改合約，不是繞過去。

## 2. 系統邊界

三個 process，職責不重疊：

| Process | 負責 | 絕對不做 |
| --- | --- | --- |
| Next.js BFF `:3000` | 畫面、掛上 Owner 憑證、代理 `/api/*` | 不放商業邏輯、不直接連 DB、不直接打 Python |
| Go API `:7794` | 認證、授權、持久化、鏈上證據、Risk Score、**執行所有資料存取** | 不呼叫 LLM、不組 prompt |
| Python Agent `:7795` | 組 prompt、呼叫 LLM、管理 session 上下文、算學習式風險分數 | 不連 DB、不驗身分、不改對外的 Risk Score、不持有 Owner 資訊 |

**Python 不執行資料存取，只能「請求」。** 它可以說「我要展開這個節點」，但取資料的是 Go，審核的也是 Go。Python 全程沒有 DB 連線、沒有 Owner 身分、沒有憑證。

「不算分數」這條在 [ADR-0015](adr/0015-learned-risk-scoring-alongside-rules.md) 之後有一個明確的例外：學習式風險分數在 Python 算，因為特徵計算的程式碼訓練與線上必須是同一份。但它仍然是純函式 —— 收到 Go 給的轉帳，回一個數字，不碰 DB、不驗身分。**對外公布的 Risk Score 依然只由 Go 的決定性規則決定**（第 9 節那條線沒有動）。

依賴方向：BFF → Go → Python。Python 的回應可以夾帶 tool 請求，但那是回應的一部分，不是它主動發起的呼叫。

## 3. 資料歸屬與快取

**Go 的 PostgreSQL 是唯一 source of truth。** 但不代表每件事都要查 DB。

關鍵性質：**Analysis Dataset 建立後永不改變**。要改就是開新的 Run、產生新的 `dataset_id`。所以任何以 `dataset_id` 為 key 的快取**永遠不需要失效**，只要記憶體滿了淘汰最舊的。

| 東西 | 存哪 | 為什麼 |
| --- | --- | --- |
| 對話訊息 | **每次進 DB** | source of truth，要 append、要持久化 |
| 授權上下文（owner / investigation / dataset_id） | 每個 turn 查 **1 次**，記憶體帶著跑完整個 tool loop | Investigation 會被刪，不可跨 turn 快取 |
| 證據包（evidence pack） | Analysis Run 完成時算好存 DB，再進記憶體 | 不可變，算一次用一輩子 |
| 地址白名單（審核用） | 記憶體 LRU，key = `dataset_id` | 不可變 |
| `expand_node` / `get_address_detail` 結果 | 記憶體 LRU，key = `(dataset_id, address)` | 不可變，且常重複命中 |
| `get_transfers(任意 filter)` | 直接查 DB | filter 千變萬化，快取命中率低，靠 index |

熱路徑下一個 turn 只打 3 次 DB：讀對話、寫對話、查授權。tool call 全在記憶體。

**Python 側的 session 只是快取。** 重開就沒了，這是預期行為不是 bug；不見時不報錯，用 Go 這次帶來的資料重建。不准為了 Python 再開第二個資料庫。

## 4. Session

- `session_id` **就是 Investigation ID**，不另外發一組 ID。
- ID 一律由 Go 產生，Python 只拿來當 key。
- Python session 有 TTL（預設 30 分鐘）與數量上限，滿了淘汰最舊的。記憶體不准無上限成長。

## 5. 網路與安全

- Python **只綁內網位址**（`127.0.0.1` 或 LAN IP），不對公網開放。
- 只有 Go 可以呼叫它，加共用密鑰 header（`X-Agent-Key`），對不上直接 401。
- Python **不做授權判斷**。Go 打過來的請求一律視為已授權。
- 瀏覽器、前端、外部服務永遠不准直接碰到 Python。
- **LLM API key 只放在 Python 這一側。** Go 和前端都不該有，放 `agent/.env`。
- **供應商的型別不得外洩。** LLM SDK 的物件只准活在 `agent/llm/<vendor>.py` 裡；進去出來都是自己的中性型別。換供應商 = 換一個檔案。設計見 `.scratch/llm-agent/adapter-plan.md`。

## 6. Agent 工具與授權

**Agent 可以自己操作工具**，包括讀取證據、展開節點、設定調查目標、啟動 Analysis Run。
使用者說「幫我調查」本身就是同意；再要他按一次確認只是多一步，沒有多一個決定
（見 [ADR-0013](adr/0013-agent-operates-investigation-tools.md)）。

工具由 Go 執行，Agent 只負責提出請求：

| 工具 | 用途 |
| --- | --- |
| `get_address_detail` | 地址的進出統計與觸發規則 |
| `expand_node` | Graph Expansion，只揭露 dataset 內既有關係 |
| `get_transfers` | 依條件查 Transfer |
| `set_investigation_target` | 設定要追查的地址 |
| `start_analysis` | 啟動鏈上資料蒐集 |

**Go 每次執行 tool call 前必查：**

1. Owner 由 Go 從自己的資料庫推導。**Agent 講的一律不信。**
2. 讀取類工具的 address 必須落在 current dataset 內，否則回 `out_of_scope`，不得代為抓取。
3. 回傳筆數有上限 `AGENT_TOOL_MAX_ROWS`。
4. 每 turn tool call 上限 `AGENT_MAX_TOOL_CALLS`、總逾時 `AGENT_TURN_TIMEOUT_MS`。
5. tool 參數是 LLM 生成的字串，**當成不可信輸入驗證**。地址要通過跟使用者輸入同一套 Base58Check 檢查。

寫入類工具另外受既有規則約束，而且是用跟 REST 路由**同一段查詢**執行的，不是另寫一套：

- 目標在第一次分析完成後不可變（[ADR-0004](adr/0004-immutable-investigation-target.md)）
- 同一 Investigation 同時只能有一個 Analysis Run（[ADR-0007](adr/0007-bounded-best-effort-analysis.md)）

**Agent 不能自己調整 Analysis Scope**（transfer 上限、追蹤深度），一律用系統預設。它沒有判斷依據，而調大直接影響時間與額度。

**一個 turn 絕不等待 Analysis Run。** 啟動後這一輪就回答，結果由下一輪給
（見 [ADR-0014](adr/0014-agent-turns-never-await-analysis.md)）。

工具失敗要回結構化的錯誤碼，而且**要說得出下一步該做什麼**：`no_analysis_yet`、
`no_target`、`target_locked`、`analysis_already_running`、`out_of_scope`。
Agent 無法據以行動的拒絕等於死路。

## 7. API 合約

- 端點放在 `/v1/` 底下。破壞相容性就升版號，不要偷改既有欄位語意。
- Go 側型別以 `controller.AgentProvider`（`controller/conversation.go`）為準，JSON 欄位跟著它走。
- 新增欄位一律可選，舊版收到不認得的欄位要能忽略。
- 端點越少越好。目前只需要三個：
  - `POST /v1/agent/chat` — 一個對話回合，回應可含 `tool_calls`
  - `POST /v1/score` — 一批轉帳換一個學習式風險分數（ADR-0015）。刪掉 `scoring/`
    這條路由就消失，agent 其他部分照常運作
  - `GET /healthz` — 活著沒

## 8. 失敗處理

- Go 每次呼叫都要有 timeout（預設 30 秒），逾時視為 Python 不可用。
- Python 掛掉、逾時、回錯 → Go 走現有的 `agent_unavailable` 路徑：使用者訊息照樣存進 DB，前端顯示服務暫時無法使用。**不要假裝成功、不要生假回覆。**
- Python 對 LLM 最多重試一次。快速失敗比較誠實。
- 錯誤要結構化（`{"code": "...", "message": "..."}`），由 Go 決定怎麼降級。

## 9. 不可跨越的線

來自 [ADR-0011](adr/0011-deterministic-risk-with-separate-interpretation.md)：

- LLM **不能改 Risk Score**。分數由 Go 的決定性規則算出來。
- LLM **不能發明證據**。它只能解釋 Go 給它的 Analysis Dataset。
- 每個宣稱都要能指回具體的地址或 transaction hash。指不出來就說沒有。
- `Partial Analysis` 必須在摘要開頭聲明，不得寫得像看完了全部鏈上歷史。

## 10. 測試

**假的 provider 是主力，真 LLM 只做人工評估。**

現成範例：TronGrid 也是付費供應商，但 `analysis/testdata/trongrid/` 有錄好的回應，測試用 `analysis.Options{Provider: fake}` 注入。LLM 照抄這個做法。

| 要測的東西 | 需要真 LLM |
| --- | --- |
| Go 的降級、審核、tool loop 上限、逾時 | ❌ 假 `AgentProvider` |
| Python 的 prompt 組裝、session 管理 | ❌ mock LLM client |
| 摘要品質、tool call 叫得對不對 | ✅ 人工看，不寫成自動化測試 |

開發用的 LLM 是 **Gemini 免費 tier**，透過 OpenAI 相容端點呼叫，換供應商只改 `LLM_BASE_URL`。本機 Ollama 可當離線備援，但小模型的 tool calling 不穩，別拿它的表現當結論。

跨 process 的整合測試可以有，但不是主力。

## 11. 寫程式

- 檔案短、函式短。一個檔案做一件事。
- 命名用 `CONTEXT.md` 的詞彙。詞彙表標 _Avoid_ 的字不要用。
- 不加沒有人要的抽象。

## 12. 動工順序

1. 詞彙沒定義好 → 先補 `CONTEXT.md`
2. 有架構決策 → 先寫 `docs/adr/`
3. 才寫程式

倒著做會產生沒人看得懂的程式。

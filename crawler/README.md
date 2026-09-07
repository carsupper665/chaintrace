# ChainTrace 訓練資料爬蟲

爬一批 TRON 地址的 USDT 轉帳紀錄，給異常偵測模型當訓練資料
（見 `docs/adr/0015-learned-risk-scoring-alongside-rules.md`）。

這個資料夾**不依賴 ChainTrace 的其他任何東西**。整個 `crawler/` 複製到一台有
Python 的機器上就能跑。

## 需求

- Python 3.10 以上
- 沒有第三方套件。不用建 venv，不用 pip install。
- 一把 TronGrid API 金鑰

## 設定

```bash
cp .env.example .env      # 然後把金鑰填進去
```

或直接給環境變數（優先於 .env）：

```bash
export TRONGRID_API_KEY=你的金鑰
```

## 跑

先在前景跑，確認看得到進度：

```bash
python3 crawl.py --addresses 1000 --interval 0.1
```

每一個地址都會印一行，長這樣：

```
[11/1000] TFxrDn8TAEsmfMYYGbdBzaSAqRYPYDeoFs     552 筆       呼叫    176  已跑 4:05  預計還要 6:08:05
[12/1000] TCeywfKPFoB4mossCLnNtQWf1uJWU5oGbk     306 筆       呼叫    179  已跑 4:09  預計還要 5:41:47
```

看到在動之後，Ctrl-C 停掉，再改成背景跑（**續爬不會白費前面的**）：

```bash
nohup python3 crawl.py --addresses 1000 --interval 0.1 > crawl.log 2>&1 &
tail -f crawl.log
```

參數：

| 參數 | 預設 | 說明 |
| --- | --- | --- |
| `--addresses` | 1000 | 總目標地址數（含已完成的） |
| `--out` | `./data` | 輸出資料夾 |
| `--interval` | 0.25 | 每次 API 呼叫間隔秒數。網路快可以調小，被限流就調大 |

## 中斷了怎麼辦

直接用同一個指令再跑一次。它會：

1. 從 `data/manifest.json` 讀回**原本的時間窗口與抽樣起點**
2. 掃 `data/transfers.jsonl.gz` 算出已完成的地址，跳過它們

所以斷線、關機、Ctrl-C 都不會白費。**不要刪 `manifest.json`** —— 窗口一換，
兩批資料算出來的特徵就不能混用了。

## 產出

```
data/
├── transfers.jsonl.gz    一行一個地址
└── manifest.json         窗口、上限、抽樣起點、地址數
```

`transfers.jsonl.gz` 每一行長這樣：

```json
{"address":"T…","truncated":false,
 "transfers":[{"from":"T…","to":"T…","value":"337410000","ts":1788276087000}]}
```

- `value` 是最小單位的字串（USDT 六位小數）。**不要轉成 float**，會失真。
- `truncated` 表示這個地址的轉帳數撞到上限，只拿到一部分。這是模型的特徵之一。

## 資料多大

實測約 **每個地址 70 KB（壓縮後）**：

| 地址數 | 壓縮後 | 解壓後（估） |
| --- | --- | --- |
| 1,000 | ~70 MB | ~500 MB |
| 5,000 | ~350 MB | ~2.5 GB |

## 要跑多久 / 花多少呼叫

實測約 **每個地址 10 次 API 呼叫**（38% 的地址轉帳數超過一頁，要翻很多頁），
1,000 個地址 ≈ 10,000 次呼叫。

在台灣的家用網路實測：每次呼叫約 1.4 秒，其中 **1.1 秒是網路延遲**，只有 0.25
秒是我們自己的間隔。整批約 **5～6 小時**。

所以瓶頸是延遲，不是間隔 —— 換一台離 TronGrid 近、延遲低的機器，並把
`--interval` 調到 0.1，估計可以壓到 1.5～2 小時。

## 拿回來之後

把 `data/` 整個複製回開發機，放到 ChainTrace 專案下的 `agent/scoring/train/data/`，
就可以進入訓練階段（Phase 2）。

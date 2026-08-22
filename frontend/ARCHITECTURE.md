# ChainTrace 專案架構與交接指南

## 設計原則

本專案採「薄路由、功能組裝、畫面與邏輯分離、中介層契約固定」的結構：

- `app` 僅處理 Next.js 路由與 HTTP 入口，不放大型畫面或商業邏輯。
- `src/features` 組合一項完整功能，但不直接實作後端資料來源。
- `src/ui` 只處理 JSX 與使用者互動呈現。
- `src/hooks` 管理 React 狀態、事件、工作區 session 與圖譜操作。
- `src/middle` 是前端與後端唯一需要共同遵守的資料邊界。
- `src/styles` 是 CSS 的唯一放置位置。

這種做法沿用 Next.js 官方允許的 `src` 應用程式目錄與「將專案檔案放在
`app` 外」策略，也符合 React 官方將元件拆檔、把可重用狀態邏輯抽成
Custom Hook 的建議。

## 目錄

```text
chaintrace-frontend/
├─ app/
│  ├─ api/auth/               # 登入、識別、登出的 BFF 路由
│  ├─ api/investigations/     # Investigation-ID 為主的 BFF 代理路由
│  ├─ login/                  # 登入頁與 callback 交換
│  ├─ layout.tsx              # 全站 HTML、metadata、CSS 入口
│  └─ page.tsx                # 首頁入口，驗證身分後渲染 ChainTraceApp
├─ src/
│  ├─ auth/                   # credential cookie 與後端身分呼叫
│  ├─ features/chaintrace/
│  │  └─ ChainTraceApp.tsx    # 三欄介面與全域狀態的組裝層
│  ├─ features/auth/          # 登入表單
│  ├─ ui/                     # Workspace、Agent、Analysis、Dialogs
│  ├─ hooks/
│  │  └─ useChainTrace.ts     # 前端狀態與操作
│  ├─ models/                 # UI 型別與建議文字
│  ├─ services/               # 圖譜分頁、對話、匯出等瀏覽器端服務
│  ├─ utils/                  # 風險色階等純函式
│  ├─ middle/                 # contracts、clients、BFF proxy
│  └─ styles/                 # CSS 入口與 ChainTrace 全站樣式
├─ public/                    # 字型、圖示、OG 圖等靜態檔
├─ tests/                     # BFF/client 契約與建置後驗證
└─ package.json               # 指令與套件版本
```

## 資料流

```text
UI component
  → useChainTrace
  → src/services/*Browser.ts        # 分頁、合併、版本控管
  → src/middle/*-client.ts
  → app/api/{auth,investigations}/  # 同源 BFF，附上 credential cookie
  → src/middle/investigation-backend.ts
  → Go backend /api/v1/*
```

瀏覽器不會直接呼叫 Go 後端或任何鏈上資料來源，也沒有 public chain
fallback。

### 中介層檔案

- `*-contract.ts`：前後端共同資料格式與路由常數。後端串接時應優先確認這裡。
- `*-client.ts`：瀏覽器端呼叫同源 `/api/...`，UI 不應直接拼後端網址。
- `investigation-backend.ts`：伺服器端代理，轉發 bearer credential 並保留
  後端 status 與 machine-readable `code`。

後端同事只要依 `src/middle/README.md` 與各 `contract` 回傳資料，不需要
閱讀 React 元件或猜測畫面變數名稱。

## 新增或修改功能

1. 新增畫面：放在 `src/ui`，由 `ChainTraceApp.tsx` 組裝。
2. 新增狀態或互動：放在 `src/hooks`，透過 Hook 回傳給 UI。
3. 新增後端資料：先定義 `contract`，再實作 `client` 與 BFF route，
   最後才接到 Hook。
4. 新增純計算：放在 `src/utils`，不要塞進 UI 元件。
5. 新增匯出或瀏覽器服務：放在 `src/services`。
6. 新增 CSS：放在 `src/styles`，並由 `index.css` 統一匯入。

## 瀏覽器端資料保存

Domain 資料一律由後端保存。`localStorage` 只保留純視覺偏好：主題、面板
寬度與收合狀態、圖譜縮放與位移（見 `src/services/visualPreferences.ts`）。
調查紀錄、地址、交易圖譜、分析結果與對話都不再寫入瀏覽器，也不會把舊的
本機快照上傳當成可信證據。

節點座標、群組與展開狀態屬於呈現層，由 `transactionGraphBrowser` 依
Dataset ID 產生；Dataset 一改變即整份丟棄，不會把舊座標套到新證據上。

## 命名與匯入規則

- React 元件：`PascalCase.tsx`
- Custom Hook：`usePascalCase.ts`
- 一般函式與資料：`camelCase.ts`
- 中介層檔名維持 `kebab-case`，讓同一 API 的 contract 與 client
  排在一起。
- 跨資料夾使用 `@/src/...` 絕對匯入；同資料夾內才使用相對匯入。
- UI 不得直接呼叫外部 API，BFF proxy 不得匯入 React 或 UI。

## 交接前檢查

```bash
npm install
npm run build
npm test
```

各 API 回應格式請查看 `src/middle/README.md`；環境變數與本機啟動流程請看
專案根目錄的 `docs/local-development.md` 與 `.env.example`。

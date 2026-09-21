# OpenAI 相容模型設定

這個目錄裡**每個 `*.json` 都會被讀**，`providers` / `models` 各自串接起來。
新增一個端點或模型 = 改 JSON，不用寫程式。JSON 不能寫註解，所以說明放這裡。

用法：

```python
from llm.openai_compatible import model_factory

llm = model_factory("Nvidia-Ultra-550B")   # 參數是 models 裡的 id
reply = llm.complete(system="...", messages=[...])
```

`GET /v1/models` 會把 `models` 清單（只有 id 與 display_name）交給 Go，前端拿來做下拉；
選到的 id 隨對話送回來，Python 再用 `model_factory` 建對應的 adapter。

## 格式

```json
{
  "providers": [
    {
      "name": "provider_01",
      "url": "https://example.invalid/v1",
      "token": "sk-put-the-real-token-here"
    }
  ],
  "models": [
    {
      "id": "Nvidia-Ultra-550B",
      "display_name": "Furen-max",
      "provider": "provider_01",
      "level": "medium",
      "timeout": 60,
      "ex_params": {}
    },
    {
      "id": "Nemotron-3-Super-120B",
      "display_name": "Furen-large",
      "provider": "provider_01"
    }
  ]
}
```

多個 JSON 檔的 `providers` / `models` 會各自串接起來。

`providers` — 每個是一個端點：

| 欄位 | 必填 | 說明 |
| --- | --- | --- |
| `name` | ✓ | 給 `models[].provider` 指回來用的名字。 |
| `url` | ✓ | OpenAI 相容端點的 base URL，通常以 `/v1` 結尾。 |
| `token` | ✓ | 這個端點的金鑰。本機端點（Ollama）隨便填一個字串即可。 |

`models` — 每個是一個模型：

| 欄位 | 必填 | 說明 |
| --- | --- | --- |
| `id` | ✓ | 送給 API 的模型名字，也是 `model_factory` 與前端下拉用的 key，必須唯一。 |
| `provider` | ✓ | 對應 `providers[].name`。 |
| `display_name` | | 給人看的名字，跟 `id` 刻意分開：改顯示名稱不會動到請求。沒填就用 `id`。 |
| `level` | | 推理程度，送成 `reasoning_effort`（`low` / `medium` / `high`）。沒填就不送——很多相容端點不認這個參數，會回 400。 |
| `timeout` | | 單次呼叫逾時秒數，預設 60。 |
| `ex_params` | | 原樣併進每次請求的額外參數，最後蓋上去，可以壓過預設的鍵。用來處理各家怪癖，例如 NVIDIA 的 `{"chat_template_kwargs": {"thinking": true}}` 或 `{"temperature": 0}`。 |

## 金鑰

`token` 是真的金鑰，所以這個目錄的 `*.json` 全部在 `.gitignore` 裡，**不會被 commit**；
只有這份 README 會。要給別人設定範本就叫他複製上面那段。

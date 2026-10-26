import {
  INVESTIGATIONS_API_PATH,
  DEFAULT_TRANSFER_LIMIT,
  DEFAULT_TRAVERSAL_DEPTH,
  MAX_TRANSFER_LIMIT,
  MAX_TRAVERSAL_DEPTH,
  type AnalysisRun,
  type AnalysisRunProgress,
  type AnalysisScope,
  type AnalysisScopeInput,
  type CancelledAnalysisRun,
  type CurrentAnalysisResult,
  type ExactAmount,
  type Investigation,
  type InvestigationErrorResponse,
  type StartedAnalysisRun,
} from "./investigation-contract.ts";
import { expireSession } from "@/src/auth/expire";

type Fetch = typeof fetch;

const BASE58_ALPHABET =
  "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";

export class InvestigationClientError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(
    message: string,
    status: number,
    code: string,
  ) {
    super(message);
    this.name = "InvestigationClientError";
    this.status = status;
    this.code = code;
  }
}

// Stable failure codes published by the backend Analysis Run worker.
const ANALYSIS_FAILURE_REASONS: Record<string, string> = {
  provider_unavailable: "後端未設定鏈上資料來源（TronGrid），無法收集證據；請聯絡管理者設定 TRONGRID_API_KEY。",
  provider_failure: "鏈上資料來源回應失敗，未收集到可用證據。",
  provider_rate_limited: "鏈上資料來源達到流量上限，未收集到可用證據。",
  resource_limit_reached: "收集作業超出後端資源上限。",
  evaluation_failed: "風險評估計算失敗。",
  publication_failed: "結果寫入資料庫失敗。",
};

export class AnalysisRunClientError extends Error {
  readonly code: string;

  constructor(code: string) {
    const stableCode = code || "ANALYSIS_FAILED";
    const reason = ANALYSIS_FAILURE_REASONS[stableCode];
    super(
      reason
        ? `分析未完成（${stableCode}）：${reason}既有結果保持不變。`
        : `分析未完成（${stableCode}），既有結果保持不變，請稍後再試。`,
    );
    this.name = "AnalysisRunClientError";
    this.code = stableCode;
  }
}

function stableErrorMessage(
  status: number,
  code: string,
  response: InvestigationErrorResponse,
) {
  if (code === "run_lost") {
    return "後端重新啟動後已遺失這次分析工作；既有結果已恢復，請重新送出分析。";
  }
  if (status === 404 || code === "investigation_not_found") {
    return "找不到這筆調查，可能已被刪除或無權存取。";
  }
  if (status === 401 || code === "unauthorized") {
    expireSession();
    return "登入狀態已失效，請重新登入。";
  }
  if (code === "immutable_investigation_target") {
    return "此調查已有分析結果，目標已鎖定；請建立新調查以分析其他地址。";
  }
  if (code === "invalid_tron_target") {
    return "請輸入有效的 TRON Base58Check 地址。";
  }
  if (code === "invalid_analysis_scope") {
    return "分析範圍無效：Transfer 上限須為 1–5000，追蹤深度須為 1–4。";
  }
  if (status === 429 || code === "rate_limited") {
    return "短時間內的請求過多，請稍候再試。";
  }
  if (code === "analysis_run_active") {
    return "這筆調查已有進行中的分析，請等它結束或先取消。";
  }
  return response.error || response.message || "調查服務目前無法使用，請稍後再試。";
}

async function requestJson<T>(
  path: string,
  init: RequestInit,
  fetchImpl: Fetch,
): Promise<T> {
  const response = await fetchImpl(path, {
    ...init,
    headers: {
      Accept: "application/json",
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...init.headers,
    },
    cache: "no-store",
  });
  if (init.signal?.aborted) {
    throw init.signal.reason ?? new DOMException("Aborted", "AbortError");
  }
  // Rate limits, proxy errors and gateway pages can arrive without a JSON body;
  // the status and any stable code still have to survive.
  const payload =
    response.status === 204
      ? null
      : response.ok
        ? await response.json()
        : await response.json().catch(() => null);
  if (init.signal?.aborted) {
    throw init.signal.reason ?? new DOMException("Aborted", "AbortError");
  }

  if (!response.ok) {
    const error = (payload || {}) as InvestigationErrorResponse;
    const code = error.code || `HTTP_${response.status}`;
    throw new InvestigationClientError(
      stableErrorMessage(response.status, code, error),
      response.status,
      code,
    );
  }
  return payload as T;
}

function normalizeInvestigation(result: Investigation): Investigation {
  const currentResult =
    typeof result.currentResult === "string" ? result.currentResult : null;
  return {
    ...result,
    risk:
      currentResult && typeof result.risk === "number" ? result.risk : null,
    relatedNodes:
      currentResult && typeof result.relatedNodes === "number"
        ? result.relatedNodes
        : null,
    totalFlow: currentResult ? normalizeExactAmount(result.totalFlow) : null,
    flowAsset:
      currentResult && typeof result.flowAsset === "string"
        ? result.flowAsset
        : null,
    transactionCount:
      currentResult && typeof result.transactionCount === "number"
        ? result.transactionCount
        : null,
    currentResult,
    activeRun:
      typeof result.activeRun === "string" && result.activeRun
        ? result.activeRun
        : null,
  };
}

function normalizeExactAmount(value: unknown): ExactAmount | null {
  if (!value || typeof value !== "object") return null;
  const amount = value as Partial<ExactAmount>;
  if (
    typeof amount.smallestUnit !== "string" ||
    !/^\d+$/.test(amount.smallestUnit) ||
    !Number.isInteger(amount.decimals) ||
    (amount.decimals ?? -1) < 0 ||
    typeof amount.asset !== "string"
  ) {
    return null;
  }
  return amount as ExactAmount;
}

export function formatExactAmount(amount: ExactAmount) {
  if (
    !/^\d+$/.test(amount.smallestUnit) ||
    !Number.isInteger(amount.decimals) ||
    amount.decimals < 0
  ) {
    throw new TypeError("Invalid exact amount");
  }
  const decimals = amount.decimals;
  const digits = BigInt(amount.smallestUnit).toString().padStart(decimals + 1, "0");
  const integerEnd = digits.length - decimals;
  const integer = digits.slice(0, integerEnd).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  if (decimals === 0) return integer;
  return `${integer}.${digits.slice(integerEnd)}`;
}

export function mergeCurrentAnalysisResult(
  investigation: Investigation,
  result: CurrentAnalysisResult,
  resultId = result.dataset.id,
): Investigation {
  return {
    ...investigation,
    status: "已完成",
    risk: result.assessment.score,
    relatedNodes: result.metrics.relatedNodes,
    totalFlow: result.metrics.totalFlow,
    flowAsset: result.metrics.totalFlow.asset,
    transactionCount: result.metrics.transferCount,
    targetLocked: true,
    currentResult: resultId,
    activeRun: null,
    updatedAt: result.assessment.updatedAt,
  };
}

export async function listInvestigations(fetchImpl: Fetch = fetch) {
  const result = await requestJson<unknown>(
    INVESTIGATIONS_API_PATH,
    {},
    fetchImpl,
  );
  if (!Array.isArray(result)) {
    throw new InvestigationClientError(
      "調查服務回傳了無法辨識的資料。",
      502,
      "INVALID_INVESTIGATION_RESPONSE",
    );
  }
  return (result as Investigation[]).map(normalizeInvestigation);
}

export function filterInvestigations(
  investigations: Investigation[],
  search: string,
) {
  const normalized = search.trim().toLocaleLowerCase();
  if (!normalized) return investigations;
  return investigations.filter((item) =>
    `${item.title} ${item.address || ""}`.toLocaleLowerCase().includes(normalized),
  );
}

export async function getInvestigation(
  id: string,
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
) {
  return normalizeInvestigation(
    await requestJson<Investigation>(
      `${INVESTIGATIONS_API_PATH}/${encodeURIComponent(id)}`,
      { signal },
      fetchImpl,
    ),
  );
}

export async function createInvestigation(title: string, fetchImpl: Fetch = fetch) {
  return normalizeInvestigation(
    await requestJson<Investigation>(
      INVESTIGATIONS_API_PATH,
      {
        method: "POST",
        body: JSON.stringify({ title: title.trim() }),
      },
      fetchImpl,
    ),
  );
}

export async function renameInvestigation(
  id: string,
  title: string,
  fetchImpl: Fetch = fetch,
) {
  return normalizeInvestigation(
    await requestJson<Investigation>(
      `${INVESTIGATIONS_API_PATH}/${encodeURIComponent(id)}`,
      { method: "PATCH", body: JSON.stringify({ title: title.trim() }) },
      fetchImpl,
    ),
  );
}

export async function deleteInvestigation(
  id: string,
  fetchImpl: Fetch = fetch,
) {
  await requestJson<null>(
    `${INVESTIGATIONS_API_PATH}/${encodeURIComponent(id)}`,
    { method: "DELETE" },
    fetchImpl,
  );
}

function decodeBase58(value: string) {
  const bytes = [0];
  for (const character of value) {
    const digit = BASE58_ALPHABET.indexOf(character);
    if (digit < 0) return null;
    let carry = digit;
    for (let index = 0; index < bytes.length; index += 1) {
      const next = bytes[index] * 58 + carry;
      bytes[index] = next & 0xff;
      carry = next >> 8;
    }
    while (carry > 0) {
      bytes.push(carry & 0xff);
      carry >>= 8;
    }
  }
  for (let index = 0; index < value.length - 1 && value[index] === "1"; index += 1) {
    bytes.push(0);
  }
  return Uint8Array.from(bytes.reverse());
}

async function sha256(value: Uint8Array) {
  const buffer = Uint8Array.from(value).buffer;
  return new Uint8Array(
    await globalThis.crypto.subtle.digest("SHA-256", buffer),
  );
}

export async function validateTronAddress(address: string) {
  const normalized = address.trim();
  if (normalized.length !== 34 || !normalized.startsWith("T")) return false;
  const decoded = decodeBase58(normalized);
  if (!decoded || decoded.length !== 25 || decoded[0] !== 0x41) return false;

  const payload = decoded.slice(0, 21);
  const checksum = decoded.slice(21);
  const expected = (await sha256(await sha256(payload))).slice(0, 4);
  return checksum.every((byte, index) => byte === expected[index]);
}

export async function setInvestigationTarget(
  id: string,
  address: string,
  fetchImpl: Fetch = fetch,
) {
  const normalized = address.trim();
  if (!(await validateTronAddress(normalized))) {
    throw new InvestigationClientError(
      "請輸入有效的 TRON Base58Check 地址。",
      400,
      "invalid_tron_target",
    );
  }
  return normalizeInvestigation(
    await requestJson<Investigation>(
      `${INVESTIGATIONS_API_PATH}/${encodeURIComponent(id)}`,
      {
        method: "PATCH",
        body: JSON.stringify({ address: normalized }),
      },
      fetchImpl,
    ),
  );
}

export function normalizeAnalysisScope(
  scope: AnalysisScopeInput = {},
): AnalysisScope {
  const normalized = {
    transferLimit: scope.transferLimit ?? DEFAULT_TRANSFER_LIMIT,
    traversalDepth: scope.traversalDepth ?? DEFAULT_TRAVERSAL_DEPTH,
  };
  if (
    !Number.isInteger(normalized.transferLimit) ||
    normalized.transferLimit < 1 ||
    normalized.transferLimit > MAX_TRANSFER_LIMIT ||
    !Number.isInteger(normalized.traversalDepth) ||
    normalized.traversalDepth < 1 ||
    normalized.traversalDepth > MAX_TRAVERSAL_DEPTH
  ) {
    throw new InvestigationClientError(
      "分析範圍無效：Transfer 上限須為 1–5000，追蹤深度須為 1–4。",
      400,
      "invalid_analysis_scope",
    );
  }
  return normalized;
}

export async function startAnalysisRun(
  investigationId: string,
  scope: AnalysisScopeInput = {},
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
) {
  const normalizedScope = normalizeAnalysisScope(scope);
  return requestJson<StartedAnalysisRun>(
    `${INVESTIGATIONS_API_PATH}/${encodeURIComponent(investigationId)}/analysis-runs`,
    {
      method: "POST",
      body: JSON.stringify(normalizedScope),
      signal,
    },
    fetchImpl,
  );
}

export function getAnalysisRun(
  investigationId: string,
  runId: string,
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
) {
  return requestJson<AnalysisRun>(
    `${INVESTIGATIONS_API_PATH}/${encodeURIComponent(investigationId)}/analysis-runs/${encodeURIComponent(runId)}`,
    { signal },
    fetchImpl,
  );
}

export function cancelAnalysisRun(
  investigationId: string,
  runId: string,
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
) {
  return requestJson<CancelledAnalysisRun>(
    `${INVESTIGATIONS_API_PATH}/${encodeURIComponent(investigationId)}/analysis-runs/${encodeURIComponent(runId)}`,
    { method: "DELETE", signal },
    fetchImpl,
  );
}

export function getCurrentAnalysisResult(
  investigationId: string,
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
) {
  return requestJson<CurrentAnalysisResult>(
    `${INVESTIGATIONS_API_PATH}/${encodeURIComponent(investigationId)}/current-result`,
    { signal },
    fetchImpl,
  );
}

export async function reloadStableAnalysis(
  investigationId: string,
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
) {
  const investigation = await getInvestigation(
    investigationId,
    fetchImpl,
    signal,
  );
  const result = investigation.currentResult
    ? await getCurrentAnalysisResult(investigationId, fetchImpl, signal)
    : null;
  return { investigation, result };
}

function waitForNextPoll(milliseconds: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
      return;
    }
    const abort = () => {
      clearTimeout(timer);
      reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", abort);
      resolve();
    }, milliseconds);
    signal.addEventListener("abort", abort, { once: true });
  });
}

export const MAX_POLL_INTERVAL_MILLISECONDS = 5000;
const MAX_RATE_LIMITED_POLL_RETRIES = 5;

export async function pollAnalysisRun(
  investigationId: string,
  runId: string,
  onProgress: ((run: AnalysisRun) => void) | undefined,
  fetchImpl: Fetch = fetch,
  intervalMilliseconds = 1000,
  signal = new AbortController().signal,
) {
  let interval = intervalMilliseconds;
  let rateLimitedRetries = 0;
  while (true) {
    let run: AnalysisRun;
    try {
      run = await getAnalysisRun(investigationId, runId, fetchImpl, signal);
    } catch (error) {
      // A shared rate limit is a transient backpressure signal, not a failed run.
      if (
        error instanceof InvestigationClientError &&
        (error.code === "rate_limited" || error.status === 429) &&
        rateLimitedRetries < MAX_RATE_LIMITED_POLL_RETRIES
      ) {
        rateLimitedRetries += 1;
        interval = MAX_POLL_INTERVAL_MILLISECONDS;
        await waitForNextPoll(interval, signal);
        continue;
      }
      throw error;
    }
    rateLimitedRetries = 0;
    onProgress?.(run);
    if (
      run.status === "completed" ||
      run.status === "failed" ||
      run.status === "cancelled"
    ) {
      return run;
    }
    await waitForNextPoll(interval, signal);
    // Long collections should not keep the shared budget at one request/second.
    interval = Math.min(
      MAX_POLL_INTERVAL_MILLISECONDS,
      Math.round(interval * 1.5),
    );
  }
}

export type AnalysisFlowResult =
  | {
      outcome: "completed";
      run: AnalysisRun;
      result: CurrentAnalysisResult;
    }
  | {
      outcome: "cancelled";
      run: AnalysisRun;
    }
  | {
      outcome: "run_lost";
      investigationId: string;
      runId: string;
    };

export function createAnalysisRunner(
  fetchImpl: Fetch = fetch,
  intervalMilliseconds = 1000,
) {
  let active:
    | {
        investigationId: string;
        controller: AbortController;
        promise: Promise<AnalysisFlowResult>;
        runId?: string;
        cancelRequested?: boolean;
      }
    | undefined;

  // follow polls one run to its terminal state and loads whatever it published.
  // start() and attach() share it because who created the run changes nothing
  // about how it is watched.
  async function follow(
    investigationId: string,
    runId: string,
    onProgress: ((run: AnalysisRunProgress) => void) | undefined,
    controller: AbortController,
  ): Promise<AnalysisFlowResult> {
    let run: AnalysisRun;
    try {
      run = await pollAnalysisRun(
        investigationId,
        runId,
        onProgress,
        fetchImpl,
        intervalMilliseconds,
        controller.signal,
      );
    } catch (error) {
      if (
        error instanceof InvestigationClientError &&
        error.code === "run_lost"
      ) {
        return { outcome: "run_lost" as const, investigationId, runId };
      }
      throw error;
    }
    if (run.status === "failed") {
      throw new AnalysisRunClientError(run.errorCode || "ANALYSIS_FAILED");
    }
    if (run.status === "cancelled") {
      return { outcome: "cancelled" as const, run };
    }
    return {
      outcome: "completed" as const,
      run,
      result: await getCurrentAnalysisResult(
        investigationId,
        fetchImpl,
        controller.signal,
      ),
    };
  }

  function track(
    investigationId: string,
    controller: AbortController,
    promise: Promise<AnalysisFlowResult>,
    runId?: string,
  ) {
    active = { investigationId, controller, promise, runId };
    const release = () => {
      if (active?.promise === promise) active = undefined;
    };
    void promise.then(release, release);
    return promise;
  }

  function start(
    investigationId: string,
    scope: AnalysisScopeInput = {},
    onProgress?: (run: AnalysisRunProgress) => void,
  ) {
    if (active?.investigationId === investigationId) return active.promise;
    active?.controller.abort();

    const controller = new AbortController();
    const promise = (async () => {
      const started = await startAnalysisRun(
        investigationId,
        scope,
        fetchImpl,
        controller.signal,
      );
      if (active?.controller === controller) active.runId = started.id;
      if (active?.controller === controller && active.cancelRequested) {
        // Cancel was pressed while this request was still in flight, so the run only
        // became cancellable now. Polling below then observes the cancelled status.
        await cancelAnalysisRun(investigationId, started.id, fetchImpl);
      }
      onProgress?.(started);
      return follow(investigationId, started.id, onProgress, controller);
    })();
    return track(investigationId, controller, promise);
  }

  // attach watches a run this browser did not start. The Agent can start one on
  // the Owner's behalf, and the run id then arrives on the Investigation rather
  // than from a start request.
  function attach(
    investigationId: string,
    runId: string,
    onProgress?: (run: AnalysisRunProgress) => void,
  ) {
    if (active?.investigationId === investigationId) return active.promise;
    active?.controller.abort();
    const controller = new AbortController();
    return track(
      investigationId,
      controller,
      follow(investigationId, runId, onProgress, controller),
      runId,
    );
  }

  return {
    start,
    attach,
    async cancel() {
      const selected = active;
      if (!selected) return undefined;
      if (!selected.runId) {
        // The run does not exist on the backend yet; start() cancels it as soon as
        // the creating request returns its id.
        selected.cancelRequested = true;
        return undefined;
      }
      const cancelled = await cancelAnalysisRun(
        selected.investigationId,
        selected.runId,
        fetchImpl,
      );
      if (active === selected) {
        selected.controller.abort();
        active = undefined;
      }
      return cancelled;
    },
    async deleteInvestigation(investigationId: string) {
      await deleteInvestigation(investigationId, fetchImpl);
      if (active?.investigationId === investigationId) {
        active.controller.abort();
        active = undefined;
      }
    },
    stop() {
      active?.controller.abort();
      active = undefined;
    },
  };
}

import {
  getTransactionGraphPage,
  TransactionGraphClientError,
} from "@/src/middle/transaction-graph-client";
import {
  TRANSACTION_GRAPH_PAGE_SIZE,
  type SavedTransactionGraphNode,
  type TransactionGraphEdge,
  type TransactionGraphNode,
  type TransactionGraphPageRequest,
  type TransactionGraphPageResponse,
  type TransactionGraphResponse,
} from "@/src/middle/transaction-graph-contract";
import type { CurrentAnalysisResult } from "@/src/middle/investigation-contract";

type Fetch = typeof fetch;

export type TransactionGraphBrowserContext = {
  investigationId: string;
  address: string;
  network: string;
  result: {
    dataset: Pick<CurrentAnalysisResult["dataset"], "id">;
    metrics: CurrentAnalysisResult["metrics"];
    assessment: Pick<CurrentAnalysisResult["assessment"], "source" | "updatedAt">;
  };
};

function graphLabel(address: string) {
  return address.length <= 18
    ? address
    : `${address.slice(0, 8)}...${address.slice(-6)}`;
}

// Hash-picked angles clump and leave wedges empty, and a 24-38 band packs every
// node onto one thin ring. A golden-angle spiral spreads any number of nodes
// evenly without knowing the total up front, so an appended page fills the gaps
// left by earlier pages instead of landing on top of them. Callers pass a
// running index, never a per-page one, or each page would restart at the centre.
const GOLDEN_ANGLE = Math.PI * (3 - Math.sqrt(5));

function layoutNode(
  node: SavedTransactionGraphNode,
  group: number,
  index: number,
): TransactionGraphNode {
  if (node.type === "focus") {
    return { ...node, label: graphLabel(node.address), x: 50, y: 50, group };
  }

  // Radius stays under 44 so a laid-out node can never start outside the drag
  // clamp in moveGraphNode.
  const rank = index + 1;
  const angle = rank * GOLDEN_ANGLE;
  const radius = Math.min(44, 20 + 4.6 * Math.sqrt(rank));
  return {
    ...node,
    label: graphLabel(node.address),
    x: Math.round((50 + Math.cos(angle) * radius) * 100) / 100,
    y: Math.round((50 + Math.sin(angle) * radius) * 100) / 100,
    group,
  };
}

function createGraph(
  page: TransactionGraphPageResponse,
  context: TransactionGraphBrowserContext,
): TransactionGraphResponse {
  return {
    datasetId: page.datasetId,
    address: context.address,
    network: context.network,
    nodes: page.nodes.map((node, index) => layoutNode(node, 0, index)),
    edges: page.edges.map((edge) => ({ ...edge, group: 0 })),
    nextCursor: page.nextCursor,
    hasMore: page.hasMore,
    transactionCount: context.result.metrics.transferCount,
    totalFlow: context.result.metrics.totalFlow,
    flowAsset: context.result.metrics.totalFlow.asset,
    source: "Saved Analysis Dataset",
    updatedAt: context.result.assessment.updatedAt,
  };
}

function mergeGraphPage(
  graph: TransactionGraphResponse,
  page: TransactionGraphPageResponse,
  group: number,
  updatePageCursor: boolean,
) {
  const nodeIds = new Set(graph.nodes.map((node) => node.id));
  const edgeIds = new Set(graph.edges.map((edge) => edge.id));
  const nodes = [
    ...graph.nodes,
    ...page.nodes
      .filter((node) => !nodeIds.has(node.id))
      .map((node, index) => layoutNode(node, group, graph.nodes.length + index)),
  ];
  const edges: TransactionGraphEdge[] = [
    ...graph.edges,
    ...page.edges
      .filter((edge) => !edgeIds.has(edge.id))
      .map((edge) => ({ ...edge, group })),
  ];

  return {
    ...graph,
    nodes,
    edges,
    nextCursor: updatePageCursor ? page.nextCursor : graph.nextCursor,
    hasMore: updatePageCursor ? page.hasMore : graph.hasMore,
  };
}

export function createTransactionGraphBrowser(
  fetchImpl: Fetch = fetch,
  pageSize = TRANSACTION_GRAPH_PAGE_SIZE,
) {
  let graph: TransactionGraphResponse | null = null;
  let context: TransactionGraphBrowserContext | null = null;
  let generation = 0;
  const requests = new Set<AbortController>();

  function abortRequests() {
    for (const controller of requests) controller.abort();
    requests.clear();
  }

  async function requestPage(
    request: TransactionGraphPageRequest,
    requestGeneration: number,
  ) {
    if (!context) return null;
    const controller = new AbortController();
    requests.add(controller);
    try {
      const page = await getTransactionGraphPage(
        context.investigationId,
        request,
        fetchImpl,
        controller.signal,
      );
      if (requestGeneration !== generation || !context) {
        return null;
      }
      if (page.datasetId !== context.result.dataset.id) {
        throw new TransactionGraphClientError(
          "分析資料集已更新，請重新載入目前結果。",
          409,
          "stale_dataset",
        );
      }
      return page;
    } catch (error) {
      if (controller.signal.aborted) return null;
      throw error;
    } finally {
      requests.delete(controller);
    }
  }

  async function load(nextContext: TransactionGraphBrowserContext) {
    generation += 1;
    const requestGeneration = generation;
    abortRequests();
    context = nextContext;
    graph = null;
    const datasetId = nextContext.result.dataset.id;
    const page = await requestPage(
      { datasetId, pageSize },
      requestGeneration,
    );
    if (!page || requestGeneration !== generation || context !== nextContext) {
      return graph;
    }
    graph = createGraph(page, nextContext);
    return graph;
  }

  async function loadMore() {
    if (!graph?.hasMore || !graph.nextCursor || !context) return graph;
    const requestGeneration = generation;
    const page = await requestPage(
      {
        datasetId: graph.datasetId,
        cursor: graph.nextCursor,
        pageSize,
      },
      requestGeneration,
    );
    if (!page || !graph || requestGeneration !== generation) return graph;
    graph = mergeGraphPage(graph, page, 0, true);
    return graph;
  }

  async function expand(anchor: string) {
    if (!graph || !context) return graph;
    const requestGeneration = generation;
    const nextGroup = Math.max(0, ...graph.nodes.map((node) => node.group)) + 1;
    const page = await requestPage(
      { datasetId: graph.datasetId, pageSize, anchor },
      requestGeneration,
    );
    if (!page || !graph || requestGeneration !== generation) return graph;
    graph = mergeGraphPage(graph, page, nextGroup, false);
    return graph;
  }

  return {
    load,
    loadMore,
    expand,
    getGraph() {
      return graph;
    },
    clear() {
      generation += 1;
      abortRequests();
      context = null;
      graph = null;
    },
    replaceGraph(nextGraph: TransactionGraphResponse) {
      if (context?.result.dataset.id === nextGraph.datasetId) graph = nextGraph;
    },
  };
}

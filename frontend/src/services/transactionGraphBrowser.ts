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

const EXPAND_BATCH = 10;

const clampPosition = (value: number) => Math.min(94, Math.max(6, value));

// A parent can reveal children over several batches, so slots continue from
// the children it already has: each ring holds RING_SLOTS nodes and later
// rings sit further out instead of on top of earlier batches.
const RING_SLOTS = 12;

function layoutChildren(
  parent: TransactionGraphNode,
  nodes: SavedTransactionGraphNode[],
  group: number,
  hop: number,
  existingChildren = 0,
) {
  const outward = Math.atan2(parent.y - 50, parent.x - 50);
  const baseRadius = Math.max(8, 14 - Math.max(0, hop - 1) * 2);
  return nodes.map((node, index) => {
    const slot = existingChildren + index;
    const ring = Math.floor(slot / RING_SLOTS);
    const position = (slot % RING_SLOTS) + ring * 0.5;
    const angle = parent.hop === 0
      ? (Math.PI * 2 * position) / RING_SLOTS - Math.PI / 2
      : outward - Math.PI / 3 + (Math.PI * 2 / 3) * ((position + 1) / (RING_SLOTS + 1));
    const radius = baseRadius + ring * 5;
    return {
      ...node,
      label: graphLabel(node.address),
      x: Math.round(clampPosition(parent.x + Math.cos(angle) * radius) * 100) / 100,
      y: Math.round(clampPosition(parent.y + Math.sin(angle) * radius) * 100) / 100,
      group,
      hop,
      parentId: parent.id,
    };
  });
}

function createGraph(
  page: TransactionGraphPageResponse,
  context: TransactionGraphBrowserContext,
): TransactionGraphResponse {
  const graph: TransactionGraphResponse = {
    datasetId: page.datasetId,
    address: context.address,
    network: context.network,
    nodes: [],
    edges: page.edges.map((edge) => ({ ...edge, group: 0 })),
    nextCursor: page.nextCursor,
    hasMore: context.result.metrics.transferCount > page.edges.length,
    transactionCount: context.result.metrics.transferCount,
    totalFlow: context.result.metrics.totalFlow,
    flowAsset: context.result.metrics.totalFlow.asset,
    source: "Saved Analysis Dataset",
    updatedAt: context.result.assessment.updatedAt,
    expandRanks: {},
  };
  const focus = page.nodes.find((node) => node.address === context.address) ?? {
    id: context.address,
    address: context.address,
    type: "focus" as const,
    counterparties: 0,
  };
  const focusNode: TransactionGraphNode = {
    ...focus,
    type: "focus",
    label: graphLabel(focus.address),
    x: 50,
    y: 50,
    group: 0,
    hop: 0,
    parentId: null,
  };
  graph.nodes = [focusNode, ...layoutChildren(
    focusNode,
    page.nodes.filter((node) => node.id !== focus.id),
    0,
    1,
  )];
  graph.expandRanks = { [focusNode.id]: EXPAND_BATCH };
  return graph;
}

function mergeGraphPage(
  graph: TransactionGraphResponse,
  page: TransactionGraphPageResponse,
  group: number,
  updatePageCursor: boolean,
  parentId: string,
  hop: number,
) {
  const nodeIds = new Set(graph.nodes.map((node) => node.id));
  const edgeIds = new Set(graph.edges.map((edge) => edge.id));
  const parent = graph.nodes.find((node) => node.id === parentId || node.address === parentId);
  const added = page.nodes.filter((node) => !nodeIds.has(node.id));
  const existingChildren = parent
    ? graph.nodes.filter((node) => node.parentId === parent.id).length
    : 0;
  const laidOut = parent
    ? layoutChildren(parent, added, group, hop, existingChildren)
    : [];
  const nodes = [
    ...graph.nodes,
    ...laidOut,
  ];
  const mergedNodeIds = new Set(nodes.map((node) => node.id));
  const edges: TransactionGraphEdge[] = [
    ...graph.edges,
    ...page.edges
      .filter((edge) => !edgeIds.has(edge.id) && mergedNodeIds.has(edge.from) && mergedNodeIds.has(edge.to))
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
  let focusCursor: string | undefined;
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
    focusCursor = undefined;
    const datasetId = nextContext.result.dataset.id;
    const page = await requestPage(
      { datasetId, pageSize, anchor: nextContext.address, top: EXPAND_BATCH },
      requestGeneration,
    );
    if (!page || requestGeneration !== generation || context !== nextContext) {
      return graph;
    }
    graph = createGraph(page, nextContext);
    return graph;
  }

  async function loadMore() {
    if (!graph?.hasMore || !context) return graph;
    const requestGeneration = generation;
    const page = await requestPage(
      {
        datasetId: graph.datasetId,
        cursor: focusCursor,
        pageSize,
        anchor: context.address,
      },
      requestGeneration,
    );
    if (!page || !graph || requestGeneration !== generation) return graph;
    focusCursor = page.nextCursor ?? undefined;
    graph = mergeGraphPage(graph, page, 0, true, context.address, 1);
    return graph;
  }

  // Each double-click asks for the next EXPAND_BATCH counterparties by rank.
  // A batch the graph already shows (reached through another node) reveals
  // nothing, so keep going until something appears or the ranking runs out.
  async function expand(anchor: string) {
    if (!graph || !context) return null;
    const requestGeneration = generation;
    const nextGroup = Math.max(0, ...graph.nodes.map((node) => node.group)) + 1;
    const parent = graph.nodes.find((node) => node.id === anchor);
    if (!parent) return null;
    const shownEdges = graph.edges.length;
    for (;;) {
      const skip: number = graph.expandRanks[anchor] ?? 0;
      if (skip >= parent.counterparties) {
        return { graph, revealed: graph.edges.length - shownEdges, hasMore: false };
      }
      const page = await requestPage(
        { datasetId: graph.datasetId, pageSize, anchor, top: EXPAND_BATCH, skip },
        requestGeneration,
      );
      if (!page || !graph || requestGeneration !== generation) return null;
      graph = mergeGraphPage(graph, page, nextGroup, false, anchor, parent.hop + 1);
      graph = {
        ...graph,
        expandRanks: { ...graph.expandRanks, [anchor]: skip + EXPAND_BATCH },
      };
      const revealed = graph.edges.length - shownEdges;
      const hasMore = skip + EXPAND_BATCH < parent.counterparties;
      if (revealed > 0 || !hasMore) return { graph, revealed, hasMore };
    }
  }

  async function expandRange(
    anchor: string,
    options: { start: number; end: number; depth: number },
  ) {
    if (!graph || !context) return null;
    const requestGeneration = generation;
    const nextGroup = Math.max(0, ...graph.nodes.map((node) => node.group)) + 1;
    let frontier = [anchor];
    let revealed = 0;
    for (let level = 0; level < options.depth && frontier.length > 0; level += 1) {
      const nextFrontier: string[] = [];
      for (let offset = 0; offset < frontier.length; offset += 4) {
        const anchors = frontier.slice(offset, offset + 4);
        const pages = await Promise.all(anchors.map((current) => requestPage({
          datasetId: graph!.datasetId,
          pageSize,
          anchor: current,
          start: options.start,
          end: options.end,
        }, requestGeneration)));
        if (requestGeneration !== generation || !graph) return null;
        for (let index = 0; index < pages.length; index += 1) {
          const page = pages[index];
          const parentId = anchors[index];
          const parent = graph.nodes.find((node) => node.id === parentId);
          if (!page || !parent) continue;
          const existing = new Set(graph.nodes.map((node) => node.id));
          const shownEdges = graph.edges.length;
          graph = mergeGraphPage(graph, page, nextGroup, false, parentId, parent.hop + 1);
          revealed += graph.edges.length - shownEdges;
          nextFrontier.push(
            ...graph.nodes.filter((node) => !existing.has(node.id)).map((node) => node.id),
          );
        }
      }
      frontier = nextFrontier;
    }
    return { graph, revealed };
  }

  return {
    load,
    loadMore,
    expand,
    expandRange,
    getGraph() {
      return graph;
    },
    clear() {
      generation += 1;
      abortRequests();
      context = null;
      graph = null;
      focusCursor = undefined;
    },
    replaceGraph(nextGraph: TransactionGraphResponse) {
      if (context?.result.dataset.id === nextGraph.datasetId) graph = nextGraph;
    },
  };
}

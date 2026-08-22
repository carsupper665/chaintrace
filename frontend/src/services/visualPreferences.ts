type ReadableStorage = Pick<Storage, "getItem">;

function storedWidth(value: string | null, fallback: number) {
  const width = Number(value);
  return Number.isFinite(width) && width > 0 ? width : fallback;
}

function storedNumber(value: string | null, fallback: number) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

function storedRange(
  value: string | null,
  fallback: number,
  minimum: number,
  maximum: number,
) {
  return Math.min(maximum, Math.max(minimum, storedNumber(value, fallback)));
}

export function loadVisualPreferences(storage: ReadableStorage) {
  const sidebarCollapsed = storage.getItem("chaintrace-sidebar-collapsed");
  const analysisCollapsed = storage.getItem("chaintrace-analysis-collapsed");

  return {
    isLightMode: storage.getItem("chaintrace-theme") === "light",
    isSidebarCollapsed:
      sidebarCollapsed === null ? null : sidebarCollapsed === "true",
    isAnalysisCollapsed:
      analysisCollapsed === null ? null : analysisCollapsed === "true",
    sidebarWidth: storedWidth(
      storage.getItem("chaintrace-sidebar-width"),
      280,
    ),
    analysisWidth: storedWidth(
      storage.getItem("chaintrace-analysis-width"),
      420,
    ),
    graphZoom: storedRange(
      storage.getItem("chaintrace-graph-zoom"),
      1,
      0.6,
      1.8,
    ),
    graphSpread: storedRange(
      storage.getItem("chaintrace-graph-spread"),
      1,
      1,
      6,
    ),
    graphOffset: {
      x: storedNumber(storage.getItem("chaintrace-graph-offset-x"), 0),
      y: storedNumber(storage.getItem("chaintrace-graph-offset-y"), 0),
    },
  };
}

"use client";

import type { CSSProperties } from "react";
import { useChainTrace } from "@/src/hooks/useChainTrace";
import { AgentPanel } from "@/src/ui/AgentPanel";
import { AnalysisPanel } from "@/src/ui/AnalysisPanel";
import { CaseDialogs } from "@/src/ui/CaseDialogs";
import { WorkspacePanel } from "@/src/ui/WorkspacePanel";

export function ChainTraceApp() {
  const ui = useChainTrace();

  return (
    <main
      style={
        {
          "--sidebar-width": `${ui.sidebarWidth}px`,
          "--analysis-width": `${ui.analysisWidth}px`,
        } as CSSProperties
      }
      className={`app-shell ${
        ui.isSidebarCollapsed ? "sidebar-collapsed" : ""
      } ${ui.isAnalysisCollapsed ? "analysis-collapsed" : ""} ${
        ui.isSidebarCollapsed && ui.isSidebarHovered ? "sidebar-peek" : ""
      } ${
        ui.isAnalysisCollapsed && ui.isAnalysisHovered ? "analysis-peek" : ""
      } ${ui.isSidebarClosing ? "sidebar-closing" : ""} ${
        ui.isAnalysisClosing ? "analysis-closing" : ""
      } ${ui.isLightMode ? "theme-light" : "theme-dark"} ${
        ui.resizingPanel ? "panel-resizing" : ""
      }`}
    >
      <WorkspacePanel ui={ui} />
      <AgentPanel ui={ui} />
      <AnalysisPanel ui={ui} />
      <CaseDialogs ui={ui} />
    </main>
  );
}

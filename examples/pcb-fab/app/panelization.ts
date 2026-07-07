export type BoardOutline = {
  widthMm: number;
  heightMm: number;
  railsMm: number;
};

export type PanelPlan = {
  rows: number;
  columns: number;
  boardsPerPanel: number;
  utilizationPct: number;
};

export function planPanelization(outline: BoardOutline, panelWidthMm = 457, panelHeightMm = 610): PanelPlan {
  const usableWidth = panelWidthMm - outline.railsMm * 2;
  const usableHeight = panelHeightMm - outline.railsMm * 2;
  const columns = Math.max(1, Math.floor(usableWidth / outline.widthMm));
  const rows = Math.max(1, Math.floor(usableHeight / outline.heightMm));
  const usedArea = rows * columns * outline.widthMm * outline.heightMm;
  const usableArea = usableWidth * usableHeight;

  return {
    rows,
    columns,
    boardsPerPanel: rows * columns,
    utilizationPct: Math.round((usedArea / usableArea) * 10000) / 100,
  };
}

export function stencilFrameNeeded(plan: PanelPlan): boolean {
  return plan.boardsPerPanel > 24 || plan.utilizationPct > 82;
}

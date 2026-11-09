import React from "react";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { AnswerDisplays, chartPlan, niceTicks, valueAt } from "./AnswerDisplay";

afterEach(cleanup);

const barHint = {
  root: "accounts",
  kind: "bar",
  columns: [
    { name: "id", role: "key" },
    { name: "name", role: "text", type: "text" },
    { name: "mrr", role: "measure", type: "numeric" },
  ],
  rows: [
    { id: 3, name: "Meridian Robotics", mrr: 12000 },
    { id: 9, name: "Northwind", mrr: 9500.5 },
    { id: 4, name: "Contoso", mrr: 4000 },
  ],
  row_count: 3,
};

const lineHint = {
  root: "invoices",
  kind: "line",
  columns: [
    { name: "month", role: "time", type: "date" },
    { name: "sum_amount", role: "measure", aggregate: "sum" },
  ],
  // Out of order on purpose: a line is drawn in time order.
  rows: [
    { month: "2026-03-01", sum_amount: "300" },
    { month: "2026-01-01", sum_amount: "100" },
    { month: "2026-02-01", sum_amount: "250" },
  ],
  row_count: 3,
};

describe("answer displays", () => {
  it("draws a ranked bar list in the query's order with each value", () => {
    render(<AnswerDisplays hints={[barHint]} />);
    const list = screen.getByRole("list", { name: "Mrr by name" });
    const items = within(list).getAllByRole("listitem");
    expect(items.map((item) => item.textContent)).toEqual([
      expect.stringContaining("Meridian Robotics"),
      expect.stringContaining("Northwind"),
      expect.stringContaining("Contoso"),
    ]);
    expect(items[1].textContent).toContain("9,500.5");
  });

  it("switches a chart to its table and hides key columns", async () => {
    const user = userEvent.setup();
    render(<AnswerDisplays hints={[barHint]} />);
    await user.click(screen.getByRole("button", { name: "Table" }));
    const headers = screen.getAllByRole("columnheader").map((header) => header.textContent);
    expect(headers).toEqual(["Name", "Mrr"]);
    expect(screen.getAllByRole("row")).toHaveLength(4);
    await user.click(screen.getByRole("button", { name: "Chart" }));
    expect(screen.getByRole("list", { name: "Mrr by name" })).toBeTruthy();
  });

  it("orders a line by time and reads points from the keyboard", () => {
    const plan = chartPlan(lineHint);
    expect(plan.chart).toBe("line");
    expect(plan.points.map((point) => point.values[0])).toEqual([100, 250, 300]);
    render(<AnswerDisplays hints={[lineHint]} />);
    const chart = screen.getByRole("img", { name: /Sum amount over month, 3 points/ });
    fireEvent.keyDown(chart, { key: "ArrowRight" });
    expect(screen.getByRole("status").textContent).toContain("100");
    fireEvent.keyDown(chart, { key: "ArrowRight" });
    expect(screen.getByRole("status").textContent).toContain("250");
  });

  it("draws nothing for a single value the answer already states", () => {
    const { container } = render(<AnswerDisplays hints={[{ root: "tickets", kind: "value", columns: [{ name: "count_id", role: "measure", aggregate: "count" }], rows: [{ count_id: 7 }], row_count: 1 }]} />);
    expect(container.textContent).toBe("");
  });

  it("keeps a table behind a toggle and pages long tables", async () => {
    const user = userEvent.setup();
    const rows = Array.from({ length: 12 }, (_, index) => ({ subject: `Ticket ${index}`, status: "open" }));
    render(<AnswerDisplays hints={[{ root: "support_tickets", kind: "table", columns: [{ name: "subject", role: "text" }, { name: "status", role: "text" }], rows, row_count: 60, truncated: true }]} />);
    expect(screen.queryByRole("table")).toBeNull();
    await user.click(screen.getByRole("button", { name: /Show table · first 12 of 60 rows/ }));
    expect(screen.getAllByRole("row")).toHaveLength(9);
    await user.click(screen.getByRole("button", { name: "Show all 12 rows" }));
    expect(screen.getAllByRole("row")).toHaveLength(13);
  });

  it("falls back to a table when values don't fit the suggested form", () => {
    expect(chartPlan({ ...barHint, rows: [{ name: "A", mrr: -5 }, { name: "B", mrr: 3 }] }).chart).toBe("");
    expect(chartPlan({ ...lineHint, rows: [{ month: "not a date", sum_amount: 1 }, { month: "later", sum_amount: 2 }] }).chart).toBe("");
  });

  it("captions each display with its question when a lead reply has several", () => {
    render(<AnswerDisplays hints={[{ ...barHint, question: "Top accounts by MRR?" }, { ...lineHint, question: "Invoice totals by month?" }]} />);
    expect(screen.getByText("Top accounts by MRR?")).toBeTruthy();
    expect(screen.getByText("Invoice totals by month?")).toBeTruthy();
  });

  it("reads nested paths and picks clean ticks", () => {
    expect(valueAt({ owner: { name: "Ana" } }, "owner.name")).toBe("Ana");
    expect(niceTicks(0, 12000)).toEqual([0, 5000, 10000, 15000]);
    expect(niceTicks(0, 300)).toEqual([0, 100, 200, 300]);
  });
});

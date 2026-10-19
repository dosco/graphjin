import React from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { EvalReportDetail, reportListResult, reportStatus } from "./EvalReports";

afterEach(cleanup);

const completedReport = {
  run_id: "complete-run",
  run_status: "complete",
  model: "gpt-test",
  generated_at: "2026-08-05T00:00:00Z",
  accepted: true,
  recall: 0.75,
  pass_at_k: 22 / 24,
  pass_power_k: 15 / 24,
  method_recall: 0.875,
  safety_precision: 1,
  friendly_summary: {
    complete: true,
    title: "Evaluation complete",
    question_count: 24,
    questions_passed_reliably: 18,
    questions_solved_at_least_once: 22,
    questions_solved_every_time: 15,
  },
  markdown: "## Evaluation complete\n\nThe agent reliably passed 18 of 24 questions.",
  technical_markdown: "## Headline\n\n| Recall | Pass@3 | Pass^3 |\n| ---: | ---: | ---: |",
};

describe("evaluation report views", () => {
  it("shows friendly counts first and exposes the technical benchmark separately", async () => {
    const user = userEvent.setup();
    render(<EvalReportDetail report={completedReport} />);

    expect(screen.getByText("Questions passed reliably")).toBeInTheDocument();
    expect(screen.getByText("18 of 24")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Plain-language summary" })).toHaveAttribute("aria-selected", "true");

    await user.click(screen.getByRole("tab", { name: "Technical benchmark" }));
    expect(screen.getByRole("heading", { name: "Technical benchmark report" })).toBeInTheDocument();
    expect(screen.getByText("Recall")).toBeInTheDocument();
  });

  it("shows saved attempt progress without invented scores for partial runs", () => {
    const report = {
      ...completedReport,
      run_id: "partial-run",
      run_status: "environment_failed",
      accepted: false,
      friendly_summary: {
        complete: false,
        title: "This evaluation did not finish",
        question_count: 24,
        completed_test_attempts: 36,
        planned_test_attempts: 72,
      },
      markdown: "## This evaluation did not finish\n\nGraphJin completed 36 of 72 test attempts before Google stopped accepting requests because of quota limits. No overall performance score is available yet.",
    };
    render(<EvalReportDetail report={report} />);

    expect(screen.getByText("Test attempts")).toBeInTheDocument();
    expect(screen.getByText("36 of 72")).toBeInTheDocument();
    expect(screen.getByText(/No overall performance score is available yet/)).toBeInTheDocument();
    expect(screen.queryByText("Questions passed reliably")).not.toBeInTheDocument();
  });

  it("uses attempt progress rather than recall in partial run list rows", () => {
    expect(reportListResult({ friendly_summary: { complete: false, completed_test_attempts: 36, planned_test_attempts: 72 } })).toBe("36 of 72 attempts");
    expect(reportStatus({ run_status: "environment_failed", accepted: false })).toBe("stopped");
    expect(reportStatus({ run_status: "complete", accepted: false })).toBe("needs improvement");
  });
});

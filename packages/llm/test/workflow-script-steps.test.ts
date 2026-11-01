import { describe, expect, it } from "vitest";
import { assertNoScriptSteps } from "../src/workflows/script-steps";
import type { WorkflowStep } from "../src/workflows/store";
import { WORKFLOW_SAVE_SCHEMA } from "../src/workflows/fence-schemas";

describe("workflow script steps", () => {
  it("stops a run whose saved workflow still has a script step", () => {
    expect(() => assertNoScriptSteps([{ id: "s1", description: "Read the orders" }])).not.toThrow();
    const saved = [
      { id: "s1", description: "Read the orders" },
      { id: "s2", description: "Build the report", script: { skill: "xlsx", command: ["python3", "build.py"] } },
    ] as unknown as WorkflowStep[];
    expect(() => assertNoScriptSteps(saved)).toThrow(/script steps \(s2\).*no longer runs scripts/);
  });

  it("saves a step without a script", () => {
    const parsed = WORKFLOW_SAVE_SCHEMA.parse({
      name: "Weekly report",
      steps: [{ id: "s1", description: "Build the report", script: { skill: "xlsx", command: ["python3", "build.py"] } }],
    });
    expect(parsed.steps).toEqual([{ id: "s1", description: "Build the report" }]);
  });
});

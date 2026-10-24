import { describe, expect, it } from "vitest";
import { detectSkillUse } from "../src/work/skill-usage";
import type { AgentEvent } from "../src/agent-backend";

describe("detectSkillUse", () => {
  it("reads a SKILL.md path from a file-read tool", () => {
    const event: AgentEvent = {
      type: "tool_start",
      id: "tc-1",
      name: "read",
      input: {
        title: "read: /tmp/org/skills/pdf/SKILL.md",
        locations: [{ path: "/tmp/org/skills/pdf/SKILL.md" }],
      },
    };
    expect(detectSkillUse(event)).toEqual({ name: "pdf", source: "read" });
  });

  it("ignores a non-skill file read", () => {
    const event: AgentEvent = {
      type: "tool_start",
      id: "tc-2",
      name: "read",
      input: { locations: [{ path: "/tmp/org/uploads/notes.md" }] },
    };
    expect(detectSkillUse(event)).toBeNull();
  });

  it("ignores other tool starts", () => {
    const event: AgentEvent = {
      type: "tool_start",
      id: "tc-4",
      name: "bash",
      input: { command: "ls" },
    };
    expect(detectSkillUse(event)).toBeNull();
  });
});

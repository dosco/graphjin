import { describe, expect, it } from "vitest";
import {
  WORKFLOW_MENTION_SENTINEL,
  WORK_MENTION_SENTINEL,
  activeWorkMentions,
  appendWorkMentionBlock,
  appendWorkflowMentionBlock,
  filterWorkMentions,
  inferTextEdit,
  stripWorkMentionBlock,
  stripWorkflowMentionBlock,
  updateDraftWorkMentions,
  workMentionKeyAction,
  type DraftWorkMention,
  type WorkMention,
} from "@/lib/workflow-mention";

const options: WorkMention[] = [
  {
    kind: "workflow",
    id: "workflow-1",
    name: "Revenue review",
    description: "Compare revenue against the approved plan.",
  },
  {
    kind: "skill",
    id: "revenue-review",
    name: "Revenue review",
    description: "Inspect sales trends and explain material movement.",
  },
  {
    kind: "skill",
    id: "pdf",
    name: "PDF",
    description: "Read, create, and inspect PDF files.",
  },
];

describe("typed Work mentions", () => {
  it("serializes colliding skill and workflow names with explicit kinds", () => {
    const message = appendWorkMentionBlock("Use @Revenue review", [
      { kind: "skill", id: "revenue-review", name: "Revenue review" },
      { kind: "workflow", id: "workflow-1", name: "Revenue review" },
    ]);

    expect(message).toContain(WORK_MENTION_SENTINEL);
    expect(message).toContain('"kind":"skill"');
    expect(message).toContain('"kind":"workflow"');
    expect(stripWorkMentionBlock(message)).toBe("Use @Revenue review");
  });

  it("strips legacy workflow metadata while keeping malformed user text", () => {
    const legacy = appendWorkflowMentionBlock("Run @Close", [
      { id: "workflow-2", name: "Close" },
    ]);
    expect(legacy).toContain(WORKFLOW_MENTION_SENTINEL);
    expect(stripWorkflowMentionBlock(legacy)).toBe("Run @Close");
    expect(
      stripWorkMentionBlock(`Explain\n\n${WORK_MENTION_SENTINEL}not-json`),
    ).toContain("not-json");
  });

  it("matches descriptions, ranks names first, and applies a hard bound", () => {
    expect(filterWorkMentions(options, "pdf").map((item) => item.id)).toEqual([
      "pdf",
    ]);
    expect(filterWorkMentions(options, "approved plan")[0]?.id).toBe(
      "workflow-1",
    );
    expect(filterWorkMentions(options, "revenue").map((item) => item.kind)).toEqual([
      "skill",
      "workflow",
    ]);
    expect(filterWorkMentions(options, "", 2)).toHaveLength(2);
  });

  it("preserves exact typed metadata when one colliding token is deleted", () => {
    const before = "Use @Revenue review then @Revenue review";
    const mentions: DraftWorkMention[] = [
      {
        kind: "skill",
        id: "revenue-review",
        name: "Revenue review",
        start: 4,
        end: 19,
      },
      {
        kind: "workflow",
        id: "workflow-1",
        name: "Revenue review",
        start: 25,
        end: 40,
      },
    ];
    const after = "Use @Revenue review";
    const updated = updateDraftWorkMentions(
      mentions,
      inferTextEdit(before, after),
    );

    expect(activeWorkMentions(after, updated)).toEqual([
      { kind: "skill", id: "revenue-review", name: "Revenue review" },
    ]);
  });

  it("maps the complete combobox keyboard contract", () => {
    expect(workMentionKeyAction("ArrowDown", 1, 3)).toEqual({
      type: "move",
      index: 2,
    });
    expect(workMentionKeyAction("ArrowUp", 0, 3)).toEqual({
      type: "move",
      index: 2,
    });
    expect(workMentionKeyAction("Enter", 1, 3)).toEqual({
      type: "select",
      index: 1,
    });
    expect(workMentionKeyAction("Tab", 1, 3)?.type).toBe("select");
    expect(workMentionKeyAction("Escape", 1, 0)).toEqual({ type: "close" });
  });
});

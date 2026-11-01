import { beforeEach, describe, expect, it, vi } from "vitest";

const update = vi.fn(async () => ({ ok: true }));
vi.mock("../src/work/bot", () => ({ BOT_INSTRUCTIONS_MAX: 2000, updateThreadBotInstructions: update }));

const { instructionsKeeper, keepBotInstructionsCurrent } = await import("../src/work/bot-instructions");

const bot = { id: "b1", name: "Stock watcher", instructions: "Watches stock levels." };
const base = { orgId: "o1", threadId: "t1", bot, operatorMessage: "Also flag late orders.", botReply: "Will do." };

describe("keepBotInstructionsCurrent", () => {
  beforeEach(() => update.mockClear());

  it("saves a changed job description", async () => {
    const keep = vi.fn(async () => ({ changed: true, instructions: "Watches stock levels and flags late orders." }));
    expect(await keepBotInstructionsCurrent(base, keep)).toBe(true);
    expect(keep).toHaveBeenCalledWith(expect.objectContaining({ currentInstructions: "Watches stock levels.", operatorMessage: "Also flag late orders." }));
    expect(update).toHaveBeenCalledWith({ orgId: "o1", threadId: "t1", instructions: "Watches stock levels and flags late orders." });
  });

  it("leaves the job alone when nothing changed", async () => {
    const keep = vi.fn(async () => ({ changed: false, instructions: "Something else." }));
    expect(await keepBotInstructionsCurrent(base, keep)).toBe(false);
    expect(update).not.toHaveBeenCalled();
  });

  it("skips an empty message without a model call", async () => {
    const keep = vi.fn();
    expect(await keepBotInstructionsCurrent({ ...base, operatorMessage: "  " }, keep)).toBe(false);
    expect(keep).not.toHaveBeenCalled();
  });

  it("sends only the end of a long reply", async () => {
    const keep = vi.fn(async () => ({ changed: false, instructions: "" }));
    await keepBotInstructionsCurrent({ ...base, botReply: "x".repeat(5000) }, keep);
    expect(keep.mock.calls[0][0].botReply).toHaveLength(1500);
  });

  it("reads earlier messages only while the bot has no description", async () => {
    const keep = vi.fn(async () => ({ changed: false, instructions: "" }));
    const earlierOperatorMessages = ["Check stock.", "Check stock again.", "", "x".repeat(900)];
    await keepBotInstructionsCurrent({ ...base, earlierOperatorMessages }, keep);
    expect(keep.mock.calls[0][0].earlierOperatorMessages).toBe("");
    await keepBotInstructionsCurrent({ ...base, bot: { ...bot, instructions: "" }, earlierOperatorMessages }, keep);
    const sent = keep.mock.calls[1][0].earlierOperatorMessages.split("\n");
    expect(sent).toEqual(["- Check stock.", "- Check stock again.", `- ${"x".repeat(400)}`]);
  });

  it("declares a boolean change flag and the whole description", () => {
    const inputs = instructionsKeeper.getSignature().getInputFields();
    expect(inputs.filter((f) => f.isOptional).map((f) => f.name)).toEqual(["currentInstructions", "earlierOperatorMessages"]);
    const outputs = instructionsKeeper.getSignature().getOutputFields().map((f) => f.name);
    expect(outputs).toEqual(["changed", "instructions"]);
  });
});

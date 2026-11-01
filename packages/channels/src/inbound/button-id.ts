import type { IntentEvent } from "@neko/interaction";

/** Decodes the `verb:rest` button-id convention used by quick-reply channels. */
export const intentFromButtonId = (id: string): IntentEvent | null => {
  const idx = id.indexOf(":");
  if (idx < 0) return null;
  const verb = id.slice(0, idx);
  const rest = id.slice(idx + 1);
  if (verb === "approve") return { kind: "decision", decisionRef: rest, choice: "approve" };
  if (verb === "reject") return { kind: "decision", decisionRef: rest, choice: "reject" };
  if (verb === "select") {
    const sep = rest.indexOf(":");
    if (sep < 0) return null;
    return { kind: "select", ref: rest.slice(0, sep), optionId: rest.slice(sep + 1) };
  }
  return null;
};

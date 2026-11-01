/**
 * Inbound: the mirror of InteractionEvent. A channel normalizes its native
 * input into one of these, and the worker feeds them to the same agent entry
 * points that already exist (utterance → chat turn, decision → action approve).
 */
export type IntentEvent =
  | { kind: "utterance"; threadRef?: string; text: string }
  | { kind: "decision"; decisionRef: string; choice: "approve" | "reject"; reason?: string }
  | { kind: "select"; ref: string; optionId: string }
  | { kind: "invoke"; command: string; args?: Record<string, unknown> };

export type IntentEventKind = IntentEvent["kind"];

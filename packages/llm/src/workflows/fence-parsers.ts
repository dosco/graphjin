import {
  ACTION_REQUEST_SCHEMA,
  FOLLOWUPS_SCHEMA,
  POLICY_SAVE_SCHEMA,
  VALUE_ESTIMATE_SCHEMA,
  VITALS_SCHEMA,
  WORKFLOW_OUTPUT_SCHEMA,
  WORKFLOW_SAVE_SCHEMA,
  type ActionRequestPayload,
  type FollowupsPayload,
  type PolicySavePayload,
  type ValueEstimatePayload,
  type VitalsPayload,
  type WorkflowOutputPayload,
  type WorkflowSavePayload,
} from "./fence-schemas";

const SAVE_FENCE_RE = /```neko_workflow_save\s*([\s\S]*?)```/gi;
const OUTPUT_FENCE_RE = /```neko_workflow_output\s*([\s\S]*?)```/gi;
const ACTION_FENCE_RE = /```neko_action_request\s*([\s\S]*?)```/gi;
const RULE_FENCE_RE = /```neko_rule_save\s*([\s\S]*?)```/gi;
const VALUE_FENCE_RE = /```neko_value\s*([\s\S]*?)```/gi;
const FOLLOWUPS_FENCE_RE = /```neko_followups\s*([\s\S]*?)```/gi;
const VITALS_FENCE_RE = /```neko_vitals\s*([\s\S]*?)```/gi;

export type FenceParseError = {
  raw: string;
  reason: string;
};

export type WorkflowSaveFenceResult = {
  text: string;
  payload: WorkflowSavePayload | null;
  errors: FenceParseError[];
};

export type WorkflowOutputFenceResult = {
  text: string;
  payloads: WorkflowOutputPayload[];
  errors: FenceParseError[];
};

export type ActionRequestFenceResult = {
  text: string;
  payloads: ActionRequestPayload[];
  errors: FenceParseError[];
};

export type PolicySaveFenceResult = {
  text: string;
  payload: PolicySavePayload | null;
  errors: FenceParseError[];
};

export type ValueFenceResult = {
  text: string;
  payload: ValueEstimatePayload | null;
  errors: FenceParseError[];
};

export type FollowupsFenceResult = {
  text: string;
  payload: FollowupsPayload | null;
  errors: FenceParseError[];
};

export type VitalsFenceResult = {
  text: string;
  payload: VitalsPayload | null;
  errors: FenceParseError[];
};

function stripAllFences(raw: string, re: RegExp): string {
  const fresh = new RegExp(re.source, re.flags);
  return raw.replace(fresh, "").replace(/\n{3,}/g, "\n\n").trim();
}

function tryParse(body: string): { ok: true; value: unknown } | { ok: false; reason: string } {
  try {
    return { ok: true, value: JSON.parse(body.trim()) };
  } catch (e) {
    return {
      ok: false,
      reason: e instanceof Error ? e.message : "json parse failed",
    };
  }
}

export function extractWorkflowSaveFence(raw: string): WorkflowSaveFenceResult {
  const matches = [...raw.matchAll(SAVE_FENCE_RE)];
  const errors: FenceParseError[] = [];
  let payload: WorkflowSavePayload | null = null;

  for (const m of matches) {
    if (payload) break;
    const parsed = tryParse(m[1]);
    if (!parsed.ok) {
      errors.push({ raw: m[0], reason: parsed.reason });
      continue;
    }
    const validated = WORKFLOW_SAVE_SCHEMA.safeParse(parsed.value);
    if (!validated.success) {
      errors.push({ raw: m[0], reason: validated.error.message });
      continue;
    }
    payload = validated.data;
  }

  return {
    text: stripAllFences(raw, SAVE_FENCE_RE),
    payload,
    errors,
  };
}

export function extractWorkflowOutputFences(
  raw: string,
): WorkflowOutputFenceResult {
  const matches = [...raw.matchAll(OUTPUT_FENCE_RE)];
  const errors: FenceParseError[] = [];
  const payloads: WorkflowOutputPayload[] = [];

  for (const m of matches) {
    const parsed = tryParse(m[1]);
    if (!parsed.ok) {
      errors.push({ raw: m[0], reason: parsed.reason });
      continue;
    }
    const validated = WORKFLOW_OUTPUT_SCHEMA.safeParse(parsed.value);
    if (!validated.success) {
      errors.push({ raw: m[0], reason: validated.error.message });
      continue;
    }
    payloads.push(validated.data);
  }

  return {
    text: stripAllFences(raw, OUTPUT_FENCE_RE),
    payloads,
    errors,
  };
}

export function extractActionRequestFences(
  raw: string,
): ActionRequestFenceResult {
  const matches = [...raw.matchAll(ACTION_FENCE_RE)];
  const errors: FenceParseError[] = [];
  const payloads: ActionRequestPayload[] = [];

  for (const m of matches) {
    const parsed = tryParse(m[1]);
    if (!parsed.ok) {
      errors.push({ raw: m[0], reason: parsed.reason });
      continue;
    }
    const validated = ACTION_REQUEST_SCHEMA.safeParse(parsed.value);
    if (!validated.success) {
      errors.push({ raw: m[0], reason: validated.error.message });
      continue;
    }
    payloads.push(validated.data);
  }

  return {
    text: stripAllFences(raw, ACTION_FENCE_RE),
    payloads,
    errors,
  };
}

export function extractRuleSaveFence(raw: string): PolicySaveFenceResult {
  const matches = [...raw.matchAll(RULE_FENCE_RE)];
  const errors: FenceParseError[] = [];
  let payload: PolicySavePayload | null = null;

  for (const m of matches) {
    if (payload) break;
    const parsed = tryParse(m[1]);
    if (!parsed.ok) {
      errors.push({ raw: m[0], reason: parsed.reason });
      continue;
    }
    const validated = POLICY_SAVE_SCHEMA.safeParse(parsed.value);
    if (!validated.success) {
      errors.push({ raw: m[0], reason: validated.error.message });
      continue;
    }
    payload = validated.data;
  }

  return {
    text: stripAllFences(raw, RULE_FENCE_RE),
    payload,
    errors,
  };
}

// Per-run analysis value estimate. Last fence wins (the agent emits one at
// the very end of the turn), so iterate and keep the last valid payload.
export function extractValueFence(raw: string): ValueFenceResult {
  const matches = [...raw.matchAll(VALUE_FENCE_RE)];
  const errors: FenceParseError[] = [];
  let payload: ValueEstimatePayload | null = null;

  for (const m of matches) {
    const parsed = tryParse(m[1]);
    if (!parsed.ok) {
      errors.push({ raw: m[0], reason: parsed.reason });
      continue;
    }
    const validated = VALUE_ESTIMATE_SCHEMA.safeParse(parsed.value);
    if (!validated.success) {
      errors.push({ raw: m[0], reason: validated.error.message });
      continue;
    }
    payload = validated.data;
  }

  return {
    text: stripAllFences(raw, VALUE_FENCE_RE),
    payload,
    errors,
  };
}

// Suggested follow-up questions (channel-agnostic content). Last valid fence
// wins.
export function extractFollowupsFence(raw: string): FollowupsFenceResult {
  const matches = [...raw.matchAll(FOLLOWUPS_FENCE_RE)];
  const errors: FenceParseError[] = [];
  let payload: FollowupsPayload | null = null;

  for (const m of matches) {
    const parsed = tryParse(m[1]);
    if (!parsed.ok) {
      errors.push({ raw: m[0], reason: parsed.reason });
      continue;
    }
    const validated = FOLLOWUPS_SCHEMA.safeParse(parsed.value);
    if (!validated.success) {
      errors.push({ raw: m[0], reason: validated.error.message });
      continue;
    }
    payload = validated.data;
  }

  return {
    text: stripAllFences(raw, FOLLOWUPS_FENCE_RE),
    payload,
    errors,
  };
}

// Headline numbers that carry the answer (channel-agnostic content). Last valid
// fence wins.
export function extractVitalsFence(raw: string): VitalsFenceResult {
  const matches = [...raw.matchAll(VITALS_FENCE_RE)];
  const errors: FenceParseError[] = [];
  let payload: VitalsPayload | null = null;

  for (const m of matches) {
    const parsed = tryParse(m[1]);
    if (!parsed.ok) {
      errors.push({ raw: m[0], reason: parsed.reason });
      continue;
    }
    const validated = VITALS_SCHEMA.safeParse(parsed.value);
    if (!validated.success) {
      errors.push({ raw: m[0], reason: validated.error.message });
      continue;
    }
    payload = validated.data;
  }

  return {
    text: stripAllFences(raw, VITALS_FENCE_RE),
    payload,
    errors,
  };
}

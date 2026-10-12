import { z } from "zod";

// A workflow trigger that fires when a row in the operator's data source
// matches a filter (the "do Y when data X changes" half). Lives inside
// `triggers.when`, alongside cron — so a single workflow save defines both
// the response and its data-change trigger. Stored as a subscription row.
export const SOURCE_CHANGE_TRIGGER_SCHEMA = z.object({
  table: z.string().trim().min(1).max(120),
  where: z.record(z.string(), z.unknown()).optional(),
  select: z.array(z.string().trim().min(1).max(120)).optional(),
  primary_key: z.array(z.string().trim().min(1).max(120)).min(1).max(8),
  version_column: z.string().trim().min(1).max(120).optional(),
  enabled: z.boolean().optional(),
  idempotency_key_template: z.string().max(200).optional(),
  acknowledge_mutation_loop: z.boolean().optional(),
});

export type SourceChangeTriggerPayload = z.infer<
  typeof SOURCE_CHANGE_TRIGGER_SCHEMA
>;

export const WORKFLOW_BATCH_DEFINITION_SCHEMA = z.object({
  recordsField: z
    .string()
    .trim()
    .regex(/^[a-zA-Z][a-zA-Z0-9_]{0,63}$/)
    .optional(),
  columns: z
    .array(
      z.object({
        name: z.string().trim().min(1).max(80).refine((value) => !/[\r\n]/.test(value)),
        path: z.string().trim().min(1).max(256),
        default: z.union([z.string(), z.number(), z.boolean(), z.null()]).optional(),
      }),
    )
    .min(1)
    .max(64),
});

export const WORKFLOW_SCRIPT_SCHEMA = z
  .object({
    skill: z
      .string()
      .trim()
      .regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/)
      .max(64),
    command: z.array(z.string().max(2000)).min(1).max(64),
    timeoutSeconds: z.number().int().min(1).max(3600).optional(),
  })
  .describe(
    "Makes this a deterministic script step. It runs before the agent turn, from the run directory, " +
      "without a shell. Arguments may use ${SKILL_DIR}, ${RUN_DIR}, ${ARTIFACT_DIR}, and " +
      "${input.<field>} or ${input.<field>:-default} from the trigger payload. The script " +
      "queries data with neko_data or neko-query.",
  );

export const WORKFLOW_SAVE_SCHEMA = z.object({
  name: z.string().trim().min(1).max(120),
  description: z.string().max(2000).optional(),
  goal: z.string().max(2000).optional(),
  systemPromptOverlay: z.string().max(16000).optional(),
  steps: z
    .array(
      z.object({
        id: z.string().trim().min(1).max(60),
        description: z.string().trim().min(1).max(2000),
        script: WORKFLOW_SCRIPT_SCHEMA.optional(),
      }),
    )
    .min(1)
    .max(40),
  // Workflow-native batch projection. A workflow can define this whether it
  // uses zero, one, or many skills; the trusted save boundary compiles it.
  batch: WORKFLOW_BATCH_DEFINITION_SCHEMA.nullable().optional(),
  triggers: z
    .object({
      cron: z.string().trim().min(1).max(120).optional(),
      timezone: z.string().trim().min(1).max(80).optional(),
      enabled: z.boolean().optional(),
      when: SOURCE_CHANGE_TRIGGER_SCHEMA.optional(),
      // OL4 condition trigger: run `query`, read `value_path` from the
      // result, fire the workflow when the condition holds.
      watch: z
        .object({
          query: z
            .string()
            .trim()
            .min(3)
            .max(8000)
            .describe(
              "The exact GraphQL query already executed successfully through GraphJin. Copy it without rewriting it.",
            ),
          value_path: z
            .string()
            .trim()
            .min(1)
            .max(400)
            .describe(
              "Dot-separated path under the successful GraphJin response data. Include numeric array segments, for example orders.0.count; bracket notation is not supported.",
            ),
          op: z.enum(["gt", "gte", "lt", "lte", "eq", "ne", "changed"]),
          threshold: z.union([z.number(), z.string()]).optional(),
          cadence_seconds: z.number().int().min(60).max(86_400).optional(),
          debounce_seconds: z
            .number()
            .int()
            .min(0)
            .max(604_800)
            .optional()
            .describe(
              "Quiet period in seconds after the watch fires; further matches inside it stay silent. Default 3600. Use 0 when the operator wants to hear about every change.",
            ),
          severity: z.enum(["low", "medium", "high", "critical"]).optional(),
        })
        .optional(),
    })
    .optional(),
});

export type WorkflowSavePayload = z.infer<typeof WORKFLOW_SAVE_SCHEMA>;

export const OUTPUT_KINDS = [
  "report",
  "summary",
  "briefing_card_proposal",
  "chart",
  "table",
  "file",
  "message_draft",
  "finding",
  "observation",
  "recommendation",
] as const;

export const MOODS = ["good", "watch", "act"] as const;

export const WORKFLOW_OUTPUT_SCHEMA = z.object({
  kind: z.enum(OUTPUT_KINDS),
  title: z.string().max(240).optional(),
  body: z.string().max(64_000).optional(),
  payload: z.record(z.string(), z.unknown()).optional(),
  artifactPath: z.string().max(1024).optional(),
  scope: z.string().max(120).optional(),
  topic: z.string().max(120).optional(),
  mood: z.enum(MOODS).optional(),
  timeWindowStart: z.string().datetime().optional(),
  timeWindowEnd: z.string().datetime().optional(),
  freshnessTtlSeconds: z.number().int().positive().max(31_536_000).optional(),
});

export type WorkflowOutputPayload = z.infer<typeof WORKFLOW_OUTPUT_SCHEMA>;

export const RISK_LEVELS = ["low", "medium", "high", "critical"] as const;
export const ACTION_SCOPES = ["internal", "external"] as const;

// Policy CRUD via Ask. Mirrors the workflow-save fence pattern. The agent
// describes a policy in plain English with the user, then commits via this
// fence. createActionPolicy seeds the row; if a policy with the same name
// exists, the upsert path updates it in place.
export const POLICY_MODES = [
  "observe_only",
  "draft_only",
  "auto_approve",
  "approval_required",
  "never",
] as const;

export const POLICY_SAVE_SCHEMA = z.object({
  name: z.string().trim().min(1).max(120),
  description: z.string().max(2000).optional(),
  applies_to_kinds: z.array(z.string().trim().min(1).max(120)).min(0).max(40),
  applies_to_scopes: z
    .array(z.enum(ACTION_SCOPES))
    .min(0)
    .max(8)
    .default(["external"] as ("internal" | "external")[]),
  mode: z.enum(POLICY_MODES),
  risk_threshold_auto_approve: z.enum(RISK_LEVELS).optional(),
  allowed_targets: z
    .record(z.string(), z.unknown())
    .optional()
    .describe(
      '{"patterns": [...], "on_miss"?: "next"}. Every target must match a pattern. A pattern is exact ("acme.com"), a prefix ("https://hooks.acme.com/*"), or subdomains only ("*.acme.com"). Email actions match lowercase recipient domains. Without on_miss, a miss is denied; with "next", a miss falls to the next rule, usually one that asks.',
    ),
  denied_targets: z
    .record(z.string(), z.unknown())
    .optional()
    .describe('{"patterns": [...]}. Denies the action when any target matches.'),
  limits: z.record(z.string(), z.unknown()).default({}),
  approver_role: z.string().trim().max(120).optional(),
  priority: z.number().int().min(0).max(10_000).default(100),
  enabled: z.boolean().default(true),
});

export type PolicySavePayload = z.infer<typeof POLICY_SAVE_SCHEMA>;

export const ACTION_REQUEST_SCHEMA = z.object({
  scope: z.enum(ACTION_SCOPES),
  kind: z.string().trim().min(1).max(120),
  target: z.string().trim().min(1).max(1024).optional(),
  payload: z.record(z.string(), z.unknown()).optional(),
  risk_level: z.enum(RISK_LEVELS).optional(),
  summary: z.string().trim().min(1).max(2000),
  minutes_saved: z
    .number()
    .int()
    .min(0)
    .max(600)
    .optional()
    .describe(
      "Conservative estimate of the minutes a competent human would spend doing this one action by hand (e.g. send email ~8, file refund ~15). Omit if unsure.",
    ),
  basis: z
    .string()
    .trim()
    .max(160)
    .optional()
    .describe("One short line naming why minutes_saved is what it is."),
});

export type ActionRequestPayload = z.infer<typeof ACTION_REQUEST_SCHEMA>;

// Per-run analysis value estimate. Emitted once at the end of every run via
// a `neko_value` fence (excludes per-action work, which carries its own
// estimate). Server-clamped before persisting. See docs/HOURS_SAVED_PLAN.md.
export const VALUE_ESTIMATE_SCHEMA = z.object({
  minutes_saved: z.number().int().min(0).max(600),
  basis: z.string().trim().max(160).optional(),
});

export type ValueEstimatePayload = z.infer<typeof VALUE_ESTIMATE_SCHEMA>;

// Suggested follow-up questions — channel-agnostic CONTENT, not UI. The model
// emits a `neko_followups` fence at the end of a /work run; any channel (the
// Ask rail, Telegram, Slack) can surface these as "ask next" prompts.
export const FOLLOWUPS_SCHEMA = z.object({
  followups: z.array(z.string().trim().min(1).max(120)).max(3),
});

export type FollowupsPayload = z.infer<typeof FOLLOWUPS_SCHEMA>;

// The few headline numbers that carry an answer — channel-agnostic CONTENT,
// not UI. The model emits a `neko_vitals` fence at the end of a /work run; each
// channel renders the figures its own way (the web rail as a tile grid, a chat
// channel as a one-line recap, a voice channel by reading them aloud).
export const VITALS_SCHEMA = z.object({
  vitals: z
    .array(
      z.object({
        label: z.string().trim().min(1).max(28),
        value: z.string().trim().min(1).max(18),
        sub: z.string().trim().max(36).optional(),
        basis: z.enum(["observed", "calculated", "estimated"]).optional(),
        asOf: z.string().trim().min(1).max(48).optional(),
        source: z.string().trim().min(1).max(64).optional(),
      }),
    )
    .max(4),
});

export type VitalsPayload = z.infer<typeof VITALS_SCHEMA>;

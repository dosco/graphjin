import { relations, sql } from "drizzle-orm";
import {
  type AnyPgColumn,
  bigserial,
  boolean,
  check,
  customType,
  date,
  index,
  bigint,
  integer,
  jsonb,
  numeric,
  pgTable,
  primaryKey,
  real,
  smallint,
  text,
  timestamp,
  uniqueIndex,
  uuid,
} from "drizzle-orm/pg-core";

const ts = (name: string) =>
  timestamp(name, { withTimezone: true, mode: "date" });

// pgvector column. Stored as `vector(N)`; we don't read it back into JS
// (similarity search uses raw SQL with the `<=>` operator). Drizzle just
// needs to know the type exists so inserts typecheck.
const vector = (name: string, dim: number) =>
  customType<{ data: string; driverData: string }>({
    dataType() {
      return `vector(${dim})`;
    },
  })(name);

export const organization = pgTable(
  "organization",
  {
    id: text("id").primaryKey(),
    scalekit_org_id: text("scalekit_org_id"),
    name: text("name").notNull(),
    domain: text("domain"),
    status: text("status").notNull().default("active"),
    setup_complete_at: ts("setup_complete_at"),
    solo_admin_user_id: text("solo_admin_user_id").references((): AnyPgColumn => app_user.id, { onDelete: "set null" }),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    domain_unique: uniqueIndex("organization_domain_unique")
      .on(t.domain)
      .where(sql`${t.domain} is not null`),
  }),
);

export const app_user = pgTable(
  "app_user",
  {
    id: text("id").primaryKey(),
    sub: text("sub"),
    email: text("email").notNull(),
    name: text("name"),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    source: text("source").notNull().default("local"),
    // ADM1: a deactivated user can't sign in and their sessions are dead.
    disabled_at: ts("disabled_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    last_login_at: ts("last_login_at"),
  },
  (t) => ({
    org_idx: index("app_user_org_idx").on(t.org_id),
    // One row per SSO subject and per mailbox within an org — the backstop
    // that makes the concurrent first-sign-in race fail loudly instead of
    // leaving duplicate identities behind (migration 0061).
    org_sub_unique: uniqueIndex("app_user_org_sub_unique")
      .on(t.org_id, t.sub)
      .where(sql`${t.sub} is not null`),
    org_email_unique: uniqueIndex("app_user_org_email_unique").on(
      t.org_id,
      sql`lower(${t.email})`,
    ),
  }),
);

export const ADMINISTRATORS_GROUP_SLUG = "administrators";
export const EVERYONE_GROUP_SLUG = "everyone";

// Administrators membership records who administers an install (migration
// 0079 dropped app_user.role). Everyone has no membership rows.
export const user_group = pgTable(
  "user_group",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    slug: text("slug").notNull(),
    name: text("name").notNull(),
    description: text("description"),
    kind: text("kind").notNull().default("custom"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_slug_unique: uniqueIndex("user_group_org_id_slug_key").on(t.org_id, t.slug),
    id_org_unique: uniqueIndex("user_group_id_org_id_key").on(t.id, t.org_id),
  }),
);

export const user_group_membership = pgTable(
  "user_group_membership",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    group_id: uuid("group_id")
      .notNull()
      .references(() => user_group.id, { onDelete: "cascade" }),
    user_id: text("user_id")
      .notNull()
      .references(() => app_user.id, { onDelete: "cascade" }),
    source: text("source").notNull().default("local"),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.group_id, t.user_id, t.source] }),
    user_idx: index("user_group_membership_user_idx").on(t.org_id, t.user_id, t.group_id),
  }),
);

export const data_access_rule = pgTable(
  "data_access_rule",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    group_id: uuid("group_id")
      .notNull()
      .references(() => user_group.id, { onDelete: "cascade" }),
    source: text("source").notNull(),
    table_schema: text("table_schema").notNull().default(""),
    table_name: text("table_name").notNull(),
    columns: text("columns").array().notNull(),
    row_filter: jsonb("row_filter"),
    created_by_user_id: text("created_by_user_id").references(() => app_user.id, { onDelete: "set null" }),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    rule_unique: uniqueIndex("data_access_rule_org_id_group_id_source_table_schema_table_name_key").on(
      t.org_id,
      t.group_id,
      t.source,
      t.table_schema,
      t.table_name,
    ),
    source_idx: index("data_access_rule_source_idx").on(t.org_id, t.source),
  }),
);

export const data_access_settings = pgTable("data_access_settings", {
  org_id: text("org_id")
    .primaryKey()
    .references(() => organization.id, { onDelete: "cascade" }),
  group_grants_enabled: boolean("group_grants_enabled").notNull().default(false),
  previous_read_modes: jsonb("previous_read_modes").notNull().default({}),
  enabled_by_user_id: text("enabled_by_user_id").references(() => app_user.id, { onDelete: "set null" }),
  updated_at: ts("updated_at").notNull().defaultNow(),
});

export const sso_group = pgTable(
  "sso_group",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    provider: text("provider").notNull(),
    tenant_id: text("tenant_id").notNull(),
    external_id: text("external_id").notNull(),
    display_name: text("display_name"),
    active: boolean("active").notNull().default(true),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    identity_unique: uniqueIndex("sso_group_identity_unique").on(
      t.org_id,
      t.provider,
      t.tenant_id,
      t.external_id,
    ),
    id_org_unique: uniqueIndex("sso_group_id_org_unique").on(t.id, t.org_id),
  }),
);

export const idp_group_rule = pgTable(
  "idp_group_rule",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    sso_group_id: uuid("sso_group_id")
      .notNull()
      .references(() => sso_group.id, { onDelete: "cascade" }),
    user_group_id: uuid("user_group_id")
      .notNull()
      .references(() => user_group.id, { onDelete: "cascade" }),
    created_by_user_id: text("created_by_user_id").references(() => app_user.id, { onDelete: "set null" }),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    rule_unique: uniqueIndex("idp_group_rule_org_id_sso_group_id_user_group_id_key").on(
      t.org_id,
      t.sso_group_id,
      t.user_group_id,
    ),
    group_idx: index("idp_group_rule_group_idx").on(t.org_id, t.user_group_id),
  }),
);

export const directory_sync_state = pgTable("directory_sync_state", {
  org_id: text("org_id")
    .primaryKey()
    .references(() => organization.id, { onDelete: "cascade" }),
  provider: text("provider"),
  status: text("status").notNull().default("never"),
  started_at: ts("started_at"),
  finished_at: ts("finished_at"),
  last_error: text("last_error"),
  stats: jsonb("stats").notNull().default({}),
  updated_at: ts("updated_at").notNull().defaultNow(),
});

export const sso_group_membership = pgTable(
  "sso_group_membership",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    group_id: uuid("group_id")
      .notNull()
      .references(() => sso_group.id, { onDelete: "cascade" }),
    user_id: text("user_id")
      .notNull()
      .references(() => app_user.id, { onDelete: "cascade" }),
    synced_at: ts("synced_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.group_id, t.user_id] }),
    user_idx: index("sso_group_membership_user_idx").on(
      t.org_id,
      t.user_id,
      t.group_id,
    ),
  }),
);

// One-time SSO admin-setup state machine (portal link → poll → connected).
export const sso_setup = pgTable(
  "sso_setup",
  {
    org_id: text("org_id")
      .primaryKey()
      .references(() => organization.id, { onDelete: "cascade" }),
    status: text("status").notNull().default("not_started"),
    portal_link: text("portal_link"),
    portal_link_expires_at: ts("portal_link_expires_at"),
    provider: text("provider"),
    last_polled_at: ts("last_polled_at"),
    last_error: text("last_error"),
    setup_completed_at: ts("setup_completed_at"),
    scalekit_environment_id: text("scalekit_environment_id"),
    scalekit_organization_id: text("scalekit_organization_id"),
    scalekit_environment_tier: text("scalekit_environment_tier"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
);


// Availability mirror for the records-engine registry. The records database
// remains authoritative for app definitions; metadata consumers use this
// narrow projection for navigation, orchestration, and degraded-state checks.
export const app_state = pgTable(
  "app_state",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    app_id: text("app_id").notNull(),
    status: text("status").notNull().default("draft"),
    created_by: text("created_by").references(() => app_user.id, {
      onDelete: "set null",
    }),
    created_at: ts("created_at").notNull().defaultNow(),
    config: jsonb("config")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.app_id] }),
    org_status_idx: index("app_state_org_status_idx").on(t.org_id, t.status),
  }),
);

export const records_ops_health = pgTable(
  "records_ops_health",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    check_name: text("check_name").notNull(),
    level: text("level").notNull(),
    sample: jsonb("sample")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    sampled_at: ts("sampled_at").notNull(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.check_name] }),
    org_level_idx: index("records_ops_health_org_level_idx").on(
      t.org_id,
      t.level,
      t.updated_at.desc(),
    ),
  }),
);

export const records_watch_binding = pgTable(
  "records_watch_binding",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    app_id: text("app_id").notNull(),
    watch_key: text("watch_key").notNull(),
    workflow_id: uuid("workflow_id")
      .notNull()
      .references(() => workflow_definition.id, { onDelete: "cascade" }),
    graphjin_watch_id: text("graphjin_watch_id").notNull(),
    definition_hash: text("definition_hash").notNull(),
    enabled: boolean("enabled").notNull().default(true),
    status: text("status").notNull().default("active"),
    last_evaluated_at: ts("last_evaluated_at"),
    last_error: text("last_error"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_app_watch_unique: uniqueIndex("records_watch_binding_org_app_watch_unique").on(
      t.org_id,
      t.app_id,
      t.watch_key,
    ),
    graphjin_watch_unique: uniqueIndex("records_watch_binding_graphjin_watch_unique").on(
      t.graphjin_watch_id,
    ),
    due_idx: index("records_watch_binding_due_idx").on(
      t.org_id,
      t.enabled,
      t.status,
      t.last_evaluated_at,
    ),
  }),
);

export const records_watch_event_receipt = pgTable(
  "records_watch_event_receipt",
  {
    event_id: text("event_id").primaryKey(),
    binding_id: uuid("binding_id")
      .notNull()
      .references(() => records_watch_binding.id, { onDelete: "cascade" }),
    payload_hash: text("payload_hash").notNull(),
    received_at: ts("received_at").notNull().defaultNow(),
    queued_at: ts("queued_at"),
  },
  (t) => ({
    binding_idx: index("records_watch_event_receipt_binding_idx").on(
      t.binding_id,
      t.received_at,
    ),
  }),
);

export const data_source = pgTable(
  "data_source",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    kind: text("kind").notNull(),
    graphql_url: text("graphql_url").notNull(),
    subscription_url: text("subscription_url"),
    label: text("label"),
    // GJ4: 'none' = anonymous legacy; 'jwt' = source mode w/ actor tokens.
    auth_mode: text("auth_mode").notNull().default("none"),
    // ADM2 registry: stable per-org name; agents use the default source
    // unless told otherwise; disabled sources are registry placeholders.
    name: text("name").notNull().default("default"),
    is_default: boolean("is_default").notNull().default(false),
    enabled: boolean("enabled").notNull().default(true),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    mcp_url: text("mcp_url"),
  },
  (t) => ({
    org_idx: index("data_source_org_idx").on(t.org_id),
    org_name_unique: uniqueIndex("data_source_org_name_unique").on(
      t.org_id,
      t.name,
    ),
  }),
);

export const onboarding_wizard = pgTable("onboarding_wizard", {
  org_id: text("org_id")
    .primaryKey()
    .references(() => organization.id, { onDelete: "cascade" }),
  company_note: text("company_note"),
  fiscal_year_start_month: smallint("fiscal_year_start_month"),
  active_seats: text("active_seats")
    .array()
    .notNull()
    .default(sql`'{}'::text[]`),
  priorities: text("priorities")
    .array()
    .notNull()
    .default(sql`'{}'::text[]`),
  step: text("step").notNull().default("company"),
  submitted_at: ts("submitted_at"),
  created_at: ts("created_at").notNull().defaultNow(),
  updated_at: ts("updated_at").notNull().defaultNow(),
});

export const processing_job = pgTable(
  "processing_job",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    kind: text("kind").notNull(),
    status: text("status").notNull().default("queued"),
    trigger: text("trigger").notNull(),
    trigger_payload: jsonb("trigger_payload"),
    progress: jsonb("progress").notNull().default(sql`'{}'::jsonb`),
    result: jsonb("result"),
    error: text("error"),
    started_at: ts("started_at"),
    finished_at: ts("finished_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_status_idx: index("processing_job_org_status_idx").on(
      t.org_id,
      t.kind,
      t.status,
    ),
    org_recent_idx: index("processing_job_org_recent_idx").on(
      t.org_id,
      t.created_at.desc(),
    ),
  }),
);

export const customer_profile = pgTable(
  "customer_profile",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    version: integer("version").notNull(),
    is_current: boolean("is_current").notNull().default(false),
    company_note: text("company_note"),
    fiscal_year_start_month: smallint("fiscal_year_start_month"),
    declared_priorities: text("declared_priorities")
      .array()
      .notNull()
      .default(sql`'{}'::text[]`),
    industry_insights: text("industry_insights"),
    built_at: ts("built_at").notNull().defaultNow(),
    built_by_job: uuid("built_by_job"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    business_profile: text("business_profile"),
    industry_insights_research_task: text("industry_insights_research_task"),
  },
  (t) => ({
    org_version_unique: uniqueIndex("customer_profile_org_version_unique").on(
      t.org_id,
      t.version,
    ),
    one_current_per_org: uniqueIndex("one_current_profile_per_org")
      .on(t.org_id)
      .where(sql`${t.is_current}`),
  }),
);

export const metric = pgTable(
  "metric",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    role: text("role").notNull(),
    // CV3: per-persona cards. NULL = org-shared card.
    user_id: text("user_id"),
    slug: text("slug").notNull(),
    source: text("source").notNull(),
    title: text("title").notNull(),
    description: text("description"),
    why: text("why"),
    chart_hint: text("chart_hint"),
    unit: text("unit"),
    direction_good: text("direction_good"),
    cadence: text("cadence").notNull().default("daily"),
    active: boolean("active").notNull().default(true),
    // Pack metrics execute a reviewed saved query and declarative result
    // mapping. Existing/custom cards retain the agent execution path.
    execution_mode: text("execution_mode").notNull().default("agent"),
    definition_json: jsonb("definition_json").$type<Record<string, unknown>>(),
    definition_version: integer("definition_version"),
    definition_hash: text("definition_hash"),
    created_by_job: uuid("created_by_job").references(() => processing_job.id),
    // Denormalized refresh status — read by /api/briefing so the dashboard
    // can render pending vs failed cards without joining processing_job.
    // Writers: metric-refresh.ts (success/failure) and bootstrap-metrics-build.ts
    // ('pending' on insert).
    last_refresh_status: text("last_refresh_status"),
    last_refresh_error: text("last_refresh_error"),
    last_refresh_job_id: uuid("last_refresh_job_id").references(() => processing_job.id),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_role_slug_unique: uniqueIndex("metric_org_role_slug_unique").on(
      t.org_id,
      t.role,
      t.slug,
    ),
    org_role_active_idx: index("metric_org_role_active_idx")
      .on(t.org_id, t.role)
      .where(sql`${t.active}`),
    org_refresh_status_idx: index("metric_org_refresh_status_idx")
      .on(t.org_id, t.last_refresh_status)
      .where(sql`${t.last_refresh_status} is not null`),
  }),
);

export const metric_snapshot = pgTable(
  "metric_snapshot",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    metric_id: uuid("metric_id")
      .notNull()
      .references(() => metric.id, { onDelete: "cascade" }),
    captured_at: ts("captured_at").notNull().defaultNow(),
    value: numeric("value"),
    value_json: jsonb("value_json"),
    baseline: numeric("baseline"),
    delta_pct: numeric("delta_pct"),
    status: text("status"),
    created_at: ts("created_at").notNull().defaultNow(),
    payload: jsonb("payload"),
    definition_hash: text("definition_hash"),
    source_freshness_at: ts("source_freshness_at"),
    duration_ms: integer("duration_ms"),
    error_class: text("error_class"),
  },
  (t) => ({
    metric_recent_idx: index("metric_snapshot_metric_recent_idx").on(
      t.metric_id,
      t.captured_at.desc(),
    ),
  }),
);

export const dashboard_pin = pgTable(
  "dashboard_pin",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id").notNull(),
    role: text("role").notNull(),
    metric_id: uuid("metric_id")
      .notNull()
      .references(() => metric.id, { onDelete: "cascade" }),
    sort_order: integer("sort_order").notNull().default(0),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    org_role_metric_unique: uniqueIndex(
      "dashboard_pin_org_role_metric_unique",
    ).on(t.org_id, t.role, t.metric_id),
  }),
);

// CH2 — channel workspace → org mapping. First inbound contact
// auto-binds a workspace to the default org; multi-tenant remaps rows.
export const channel_workspace = pgTable(
  "channel_workspace",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    channel_plugin: text("channel_plugin").notNull(),
    workspace_id: text("workspace_id").notNull(),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    plugin_ws_unique: uniqueIndex("channel_workspace_plugin_ws_unique").on(
      t.channel_plugin,
      t.workspace_id,
    ),
    org_idx: index("channel_workspace_org_idx").on(t.org_id),
  }),
);

// CV3 — personas: one profile per (org, user). user_id = '' is the
// org-default persona (solo profile / unlinked channels). The compiled
// brief_md is what the agent reads as <operator-profile>; raw answers
// stay out of prompts.
export const operator_profile = pgTable(
  "operator_profile",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    user_id: text("user_id").notNull().default(""),
    display_name: text("display_name"),
    role_template: text("role_template").notNull().default(""),
    focus_areas: text("focus_areas").array().notNull().default(sql`'{}'::text[]`),
    answers: jsonb("answers").notNull().default(sql`'{}'::jsonb`),
    brief_md: text("brief_md").notNull().default(""),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_user_unique: uniqueIndex("operator_profile_org_user_unique").on(
      t.org_id,
      t.user_id,
    ),
  }),
);

// CV0 — config-vcs ref pointers: the DB knows each org layer's current
// commit; git holds the content. Team layer = (scope='team', user_id='').
export const config_ref = pgTable(
  "config_ref",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    scope: text("scope").notNull().default("team"),
    user_id: text("user_id").notNull().default(""),
    commit_sha: text("commit_sha").notNull(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_scope_user_unique: uniqueIndex("config_ref_org_scope_user_unique").on(
      t.org_id,
      t.scope,
      t.user_id,
    ),
  }),
);

// OL5 — named connection secrets for chat-first source config. The agent
// references a secret by NAME; the worker resolves + decrypts (enc:v1) at
// apply time. Values never enter model context.
export const data_source_secret = pgTable(
  "data_source_secret",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    value_enc: text("value_enc").notNull(),
    description: text("description"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.name] }),
  }),
);

// Managed OpenAPI documents imported by an admin. The original document is
// normalized and kept org-scoped in metadata storage; only the approval worker
// materializes it into the customer GraphJin config volume.
export const openapi_spec_asset = pgTable(
  "openapi_spec_asset",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    created_by_user_id: text("created_by_user_id").references(
      () => app_user.id,
      { onDelete: "set null" },
    ),
    source_type: text("source_type").notNull(),
    original_name: text("original_name").notNull(),
    source_url: text("source_url"),
    content: text("content").notNull(),
    checksum_sha256: text("checksum_sha256").notNull(),
    document_format: text("document_format").notNull().default("yaml"),
    title: text("title").notNull(),
    version: text("version"),
    base_url: text("base_url").notNull(),
    server_urls: text("server_urls")
      .array()
      .notNull()
      .default(sql`'{}'::text[]`),
    auth_schemes: text("auth_schemes")
      .array()
      .notNull()
      .default(sql`'{}'::text[]`),
    warnings: text("warnings")
      .array()
      .notNull()
      .default(sql`'{}'::text[]`),
    operation_count: integer("operation_count").notNull().default(0),
    read_operation_count: integer("read_operation_count").notNull().default(0),
    mutating_operation_count: integer("mutating_operation_count")
      .notNull()
      .default(0),
    status: text("status").notNull().default("staged"),
    source_name: text("source_name"),
    activated_at: ts("activated_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_created_idx: index("openapi_spec_asset_org_created_idx").on(
      t.org_id,
      t.created_at.desc(),
    ),
    org_checksum_idx: index("openapi_spec_asset_org_checksum_idx").on(
      t.org_id,
      t.checksum_sha256,
    ),
  }),
);

// CH3 — channel→app_user mapping. One row per channel-native identity
// an org has seen; linking (SSO email match or admin-map) binds it to
// an app_user. Unlinked identities act as anonymous members.
export const channel_identity = pgTable(
  "channel_identity",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    channel_plugin: text("channel_plugin").notNull(),
    workspace_id: text("workspace_id").notNull().default(""),
    channel_user_id: text("channel_user_id").notNull(),
    app_user_id: text("app_user_id").references(() => app_user.id, {
      onDelete: "cascade",
    }),
    display_name: text("display_name"),
    email: text("email"),
    status: text("status").notNull().default("unverified"),
    first_seen_at: ts("first_seen_at").notNull().defaultNow(),
    verified_at: ts("verified_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    tuple_unique: uniqueIndex("channel_identity_tuple_unique").on(
      t.org_id,
      t.channel_plugin,
      t.workspace_id,
      t.channel_user_id,
    ),
    org_user_idx: index("channel_identity_org_user_idx").on(
      t.org_id,
      t.app_user_id,
    ),
  }),
);

// SEC5 — every authenticated gateway (broker) call a sandboxed agent
// makes, stamped with the dual identity (human principal + agent
// backend). SEC7 reads this for behavioral thresholds.
export const control_plane_audit = pgTable(
  "control_plane_audit",
  {
    id: bigserial("id", { mode: "number" }).primaryKey(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    run_id: uuid("run_id"),
    path: text("path").notNull(),
    actor_user_id: text("actor_user_id"),
    actor_role: text("actor_role"),
    backend: text("backend"),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    org_idx: index("control_plane_audit_org_idx").on(
      t.org_id,
      t.created_at.desc(),
    ),
    run_idx: index("control_plane_audit_run_idx").on(t.run_id),
  }),
);

// SEC10 — append-only per-org hash chain over the governance entities.
// Any retroactive edit/deletion breaks every later link.
export const audit_chain = pgTable(
  "audit_chain",
  {
    id: bigserial("id", { mode: "number" }).primaryKey(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    seq: integer("seq").notNull(),
    entity_kind: text("entity_kind").notNull(),
    entity_id: text("entity_id").notNull(),
    event: text("event").notNull(),
    payload: jsonb("payload").notNull().default(sql`'{}'::jsonb`),
    payload_hash: text("payload_hash").notNull(),
    prev_hash: text("prev_hash").notNull(),
    chain_hash: text("chain_hash").notNull(),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    org_seq_unique: uniqueIndex("audit_chain_org_seq_unique").on(t.org_id, t.seq),
    org_entity_idx: index("audit_chain_org_entity_idx").on(
      t.org_id,
      t.entity_kind,
      t.entity_id,
    ),
  }),
);

// OL4 — watchers: condition monitors over GraphJin queries that fire
// their linked workflow when the condition holds (polling v1).
export const watcher = pgTable(
  "watcher",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    workflow_id: uuid("workflow_id")
      .notNull()
      .references(() => workflow_definition.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    description: text("description").notNull().default(""),
    enabled: boolean("enabled").notNull().default(true),
    query: text("query").notNull(),
    value_path: text("value_path").notNull(),
    op: text("op").notNull(),
    threshold: jsonb("threshold"),
    cadence_seconds: integer("cadence_seconds").notNull().default(300),
    debounce_seconds: integer("debounce_seconds").notNull().default(3600),
    cooldown_seconds: integer("cooldown_seconds").notNull().default(0),
    dedupe_key: text("dedupe_key"),
    activation: text("activation"),
    variables_json: jsonb("variables_json")
      .$type<Record<string, unknown>>()
      .notNull()
      .default({}),
    severity: text("severity").notNull().default("medium"),
    last_checked_at: ts("last_checked_at"),
    last_fired_at: ts("last_fired_at"),
    last_value: jsonb("last_value"),
    last_error: text("last_error"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_name_unique: uniqueIndex("watcher_org_name_unique").on(t.org_id, t.name),
    org_enabled_idx: index("watcher_org_enabled_idx").on(
      t.org_id,
      t.enabled,
      t.last_checked_at,
    ),
  }),
);

// SEC7 — behavioral threshold alerts raised by the worker sweep over
// the SEC5 audit stream and action/memory write rates.
export const behavior_alert = pgTable(
  "behavior_alert",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    kind: text("kind").notNull(),
    subject: text("subject").notNull().default(""),
    observed: integer("observed").notNull(),
    threshold: integer("threshold").notNull(),
    window_seconds: integer("window_seconds").notNull(),
    details: jsonb("details"),
    acknowledged_at: ts("acknowledged_at"),
    acknowledged_by: text("acknowledged_by"),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    org_idx: index("behavior_alert_org_idx").on(t.org_id, t.created_at.desc()),
    org_kind_subject_idx: index("behavior_alert_org_kind_subject_idx").on(
      t.org_id,
      t.kind,
      t.subject,
      t.created_at.desc(),
    ),
  }),
);

// CV4 — per-member fork baseline for 3-way memory pulls.
export const memory_fork = pgTable(
  "memory_fork",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    user_id: text("user_id")
      .notNull()
      .references(() => app_user.id, { onDelete: "cascade" }),
    baseline_sha: text("baseline_sha").notNull().default(""),
    baseline_at: ts("baseline_at").notNull().defaultNow(),
    frozen: boolean("frozen").notNull().default(false),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.user_id] }),
  }),
);

// CV4 — config-artifact audit trail + admin adopt inbox. Attribution
// lives here (DB-deletable), never in git commits (DATA_LIFECYCLE §3).
export const config_change = pgTable(
  "config_change",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    artifact_kind: text("artifact_kind").notNull(),
    artifact_ref: text("artifact_ref").notNull(),
    scope: text("scope").notNull().default("team"),
    user_id: text("user_id").notNull().default(""),
    actor_user_id: text("actor_user_id").references(() => app_user.id, {
      onDelete: "set null",
    }),
    commit_sha: text("commit_sha"),
    summary: text("summary").notNull().default(""),
    semantic_diff: jsonb("semantic_diff"),
    status: text("status").notNull().default("recorded"),
    decided_by: text("decided_by"),
    decided_at: ts("decided_at"),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    org_idx: index("config_change_org_idx").on(t.org_id, t.created_at.desc()),
    org_status_idx: index("config_change_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
  }),
);

// OL7 — a muted scope hides matching workflow_output cards from the
// Briefing tributaries until muted_until passes.
export const muted_scope = pgTable(
  "muted_scope",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    scope: text("scope").notNull(),
    muted_until: ts("muted_until").notNull(),
    muted_by_user_id: text("muted_by_user_id"),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    org_scope_unique: uniqueIndex("muted_scope_org_scope_unique").on(
      t.org_id,
      t.scope,
    ),
    org_until_idx: index("muted_scope_org_until_idx").on(
      t.org_id,
      t.muted_until.desc(),
    ),
  }),
);

// Operator-curated pin: links a workflow_output to the Briefing's pinned
// section. See migration 0013_briefing_finding_pin.sql for the column
// rationale. Forward-declared above workflow_output (which it references)
// using sql"workflow_output" deferred resolution — the actual FK constraint
// is enforced by the migration's REFERENCES clause.
export const briefing_finding_pin = pgTable(
  "briefing_finding_pin",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id").notNull(),
    output_id: uuid("output_id").notNull(),
    sort_order: integer("sort_order").notNull().default(0),
    pinned_by_user_id: text("pinned_by_user_id"),
    pinned_at: ts("pinned_at").notNull().defaultNow(),
  },
  (t) => ({
    org_output_unique: uniqueIndex("briefing_finding_pin_org_output_unique").on(
      t.org_id,
      t.output_id,
    ),
    org_idx: index("briefing_finding_pin_org_idx").on(
      t.org_id,
      t.sort_order,
      t.pinned_at.desc(),
    ),
  }),
);

export const briefing = pgTable(
  "briefing",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    role: text("role").notNull(),
    for_date: date("for_date").notNull(),
    profile_version: integer("profile_version").notNull(),
    summary_md: text("summary_md").notNull(),
    insights: jsonb("insights").notNull().default(sql`'[]'::jsonb`),
    generated_by_job: uuid("generated_by_job").references(
      () => processing_job.id,
    ),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_role_date_unique: uniqueIndex("briefing_org_role_date_unique").on(
      t.org_id,
      t.role,
      t.for_date,
    ),
    org_recent_idx: index("briefing_org_recent_idx").on(
      t.org_id,
      t.for_date.desc(),
    ),
  }),
);

export const source_change_log = pgTable(
  "source_change_log",
  {
    id: bigserial("id", { mode: "number" }).primaryKey(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    source_id: uuid("source_id")
      .notNull()
      .references(() => data_source.id, { onDelete: "cascade" }),
    table_name: text("table_name").notNull(),
    change_kind: text("change_kind").notNull(),
    observed_at: ts("observed_at").notNull().defaultNow(),
    payload: jsonb("payload"),
  },
  (t) => ({
    org_recent_idx: index("source_change_log_org_recent_idx").on(
      t.org_id,
      t.observed_at.desc(),
    ),
  }),
);

export const reprofile_debounce = pgTable("reprofile_debounce", {
  org_id: text("org_id")
    .primaryKey()
    .references(() => organization.id, { onDelete: "cascade" }),
  last_change_at: ts("last_change_at").notNull(),
  last_built_at: ts("last_built_at"),
  pending_job_id: uuid("pending_job_id").references(() => processing_job.id),
  updated_at: ts("updated_at").notNull().defaultNow(),
});

export const metricRelations = relations(metric, ({ many }) => ({
  snapshots: many(metric_snapshot),
  pins: many(dashboard_pin),
}));

export const metric_snapshotRelations = relations(metric_snapshot, ({ one }) => ({
  metric: one(metric, {
    fields: [metric_snapshot.metric_id],
    references: [metric.id],
  }),
}));

export const dashboard_pinRelations = relations(dashboard_pin, ({ one }) => ({
  metric: one(metric, {
    fields: [dashboard_pin.metric_id],
    references: [metric.id],
  }),
}));

export const llm_provider_config = pgTable(
  "llm_provider_config",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    scope: text("scope").notNull(),
    provider: text("provider").notNull(),
    model: text("model"),
    label: text("label"),
    enabled: boolean("enabled").notNull().default(true),
    config: jsonb("config").notNull().default(sql`'{}'::jsonb`),
    secrets: jsonb("secrets").notNull().default(sql`'{}'::jsonb`),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_scope_unique: uniqueIndex("llm_provider_config_org_scope_unique").on(
      t.org_id,
      t.scope,
    ),
    org_idx: index("llm_provider_config_org_idx").on(t.org_id),
  }),
);

export const work_thread = pgTable(
  "work_thread",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    title: text("title").notNull().default(""),
    // Origin channel ("web", "telegram", …). The web Ask UI lists only its own
    // ("web") threads so channels stay isolated. See docs/PER_CHANNEL_RENDERING.md.
    channel: text("channel").notNull().default("web"),
    // K1: who opened this thread (web session user). Null for channel /
    // service threads until CH3 links channel senders.
    created_by_user_id: text("created_by_user_id").references(
      () => app_user.id,
      { onDelete: "set null" },
    ),
    backend_state: jsonb("backend_state").notNull().default(sql`'{}'::jsonb`),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    last_message_at: ts("last_message_at").notNull().defaultNow(),
  },
  (t) => ({
    org_recent_idx: index("work_thread_org_recent_idx").on(
      t.org_id,
      t.last_message_at.desc(),
      t.created_at.desc(),
    ),
  }),
);

export const skill_embedding = pgTable(
  "skill_embedding",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    skill_name: text("skill_name").notNull(),
    document: text("document").notNull().default(""),
    embedding: vector("embedding", 384),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_skill_idx: uniqueIndex("skill_embedding_org_id_skill_name_key").on(t.org_id, t.skill_name),
  }),
);

export const bot = pgTable(
  "bot",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    owner_user_id: text("owner_user_id")
      .notNull()
      .references(() => app_user.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    coat: text("coat").notNull(),
    pattern: text("pattern").notNull(),
    instructions: text("instructions").notNull().default(""),
    is_default: boolean("is_default").notNull().default(false),
    thread_id: uuid("thread_id").references(() => work_thread.id, {
      onDelete: "set null",
    }),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    owner_idx: index("bot_owner_idx").on(t.org_id, t.owner_user_id),
  }),
);

export const work_run = pgTable(
  "work_run",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    thread_id: uuid("thread_id")
      .notNull()
      .references(() => work_thread.id, { onDelete: "cascade" }),
    backend: text("backend").notNull(),
    status: text("status").notNull().default("running"),
    error: text("error"),
    // K1: the acting principal, snapshotted at run start. Web runs carry
    // (app_user.id, role); channel runs (NULL, 'member') until CH3;
    // cron/workflow runs (NULL, 'service').
    actor_user_id: text("actor_user_id").references(() => app_user.id, {
      onDelete: "set null",
    }),
    actor_role: text("actor_role"),
    // Agent-estimated minutes of human effort the run's ANALYSIS saved
    // (excludes per-action work, which is tracked on action_request). Null
    // until the run emits a value estimate; server-clamped before write.
    analysis_minutes_saved: integer("analysis_minutes_saved"),
    analysis_minutes_basis: text("analysis_minutes_basis"),
    estimate_version: integer("estimate_version").notNull().default(1),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    finished_at: ts("finished_at"),
  },
  (t) => ({
    thread_created_idx: index("work_run_thread_created_idx").on(
      t.thread_id,
      t.created_at.asc(),
    ),
    org_finished_idx: index("work_run_org_finished_idx").on(
      t.org_id,
      t.finished_at,
    ),
  }),
);

export const work_message = pgTable(
  "work_message",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    thread_id: uuid("thread_id")
      .notNull()
      .references(() => work_thread.id, { onDelete: "cascade" }),
    run_id: uuid("run_id").references(() => work_run.id, { onDelete: "set null" }),
    role: text("role").notNull(),
    content: text("content").notNull(),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    thread_created_idx: index("work_message_thread_created_idx").on(
      t.thread_id,
      t.created_at.asc(),
    ),
  }),
);

export const work_run_event = pgTable(
  "work_run_event",
  {
    // `id` is a Postgres bigserial — globally monotonic, guaranteed
    // unique. Use it as the canonical ordering key ("ORDER BY id ASC")
    // and as the SSE tail cursor ("WHERE id > $lastId"). The old `seq`
    // column was per-run, computed by app code, and racy when two
    // writers (work-run + action-execute) emitted close in time —
    // surfacing as "duplicate key value violates unique constraint
    // work_run_event_run_seq_unique" errors in the chat. Migration
    // 0022 dropped it.
    id: bigserial("id", { mode: "number" }).primaryKey(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    thread_id: uuid("thread_id")
      .notNull()
      .references(() => work_thread.id, { onDelete: "cascade" }),
    run_id: uuid("run_id")
      .notNull()
      .references(() => work_run.id, { onDelete: "cascade" }),
    kind: text("kind").notNull(),
    payload: jsonb("payload").notNull(),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    run_id_idx: index("work_run_event_run_id_idx").on(t.run_id, t.id),
    thread_id_idx: index("work_run_event_thread_id_idx").on(t.thread_id, t.id),
  }),
);

export const work_memory = pgTable(
  "work_memory",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    kind: text("kind").notNull(),
    scope: text("scope").notNull(),
    scope_id: text("scope_id"),
    text: text("text").notNull(),
    pinned: boolean("pinned").notNull().default(false),
    confidence: real("confidence").notNull().default(0.8),
    metadata: jsonb("metadata").notNull().default(sql`'{}'::jsonb`),
    source_run_id: uuid("source_run_id").references(() => work_run.id, {
      onDelete: "set null",
    }),
    source_thread_id: uuid("source_thread_id").references(() => work_thread.id, {
      onDelete: "set null",
    }),
    use_count: integer("use_count").notNull().default(0),
    last_used_at: ts("last_used_at"),
    // CV2 overlay: NULL user_id = team layer; personal rows shadow
    // (overrides_origin_id) or hide (suppressed) the team row of that
    // origin for their owner. promoted_* = promote lineage.
    user_id: text("user_id").references(() => app_user.id, {
      onDelete: "cascade",
    }),
    origin_id: uuid("origin_id"),
    overrides_origin_id: uuid("overrides_origin_id"),
    suppressed: boolean("suppressed").notNull().default(false),
    promoted_from_id: uuid("promoted_from_id"),
    promoted_by: text("promoted_by"),
    promoted_at: ts("promoted_at"),
    // SEC6: per-org HMAC over identity-bearing fields (NULL = pre-SEC6
    // row); expires_at archives short-lived kinds via the nightly sweep.
    integrity_hmac: text("integrity_hmac"),
    expires_at: ts("expires_at"),
    archived_at: ts("archived_at"),
    embedding: vector("embedding", 384),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_active_idx: index("work_memory_org_active_idx").on(
      t.org_id,
      t.archived_at,
      t.updated_at.desc(),
    ),
    org_scope_idx: index("work_memory_org_scope_idx").on(
      t.org_id,
      t.scope,
      t.scope_id,
      t.archived_at,
    ),
    org_pinned_idx: index("work_memory_org_pinned_idx").on(
      t.org_id,
      t.pinned,
      t.archived_at,
      t.updated_at.desc(),
    ),
  }),
);

export const work_memory_event = pgTable(
  "work_memory_event",
  {
    id: bigserial("id", { mode: "number" }).primaryKey(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    memory_id: uuid("memory_id").references(() => work_memory.id, {
      onDelete: "set null",
    }),
    run_id: uuid("run_id").references(() => work_run.id, { onDelete: "set null" }),
    thread_id: uuid("thread_id").references(() => work_thread.id, {
      onDelete: "set null",
    }),
    action: text("action").notNull(),
    payload: jsonb("payload").notNull().default(sql`'{}'::jsonb`),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    memory_idx: index("work_memory_event_memory_idx").on(t.memory_id, t.id.desc()),
    org_recent_idx: index("work_memory_event_org_recent_idx").on(
      t.org_id,
      t.id.desc(),
    ),
  }),
);

export const work_pending_memory = pgTable(
  "work_pending_memory",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    thread_id: uuid("thread_id").references(() => work_thread.id, {
      onDelete: "cascade",
    }),
    run_id: uuid("run_id").references(() => work_run.id, { onDelete: "set null" }),
    status: text("status").notNull().default("proposed"),
    draft_text: text("draft_text").notNull(),
    draft_kind: text("draft_kind").notNull(),
    draft_scope: text("draft_scope").notNull(),
    draft_scope_id: text("draft_scope_id"),
    confidence: real("confidence").notNull(),
    reasoning: text("reasoning"),
    conflict: jsonb("conflict"),
    decision_text: text("decision_text"),
    decided_at: ts("decided_at"),
    memory_id: uuid("memory_id").references(() => work_memory.id, {
      onDelete: "set null",
    }),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    thread_status_idx: index("work_pending_memory_thread_status_idx").on(
      t.org_id,
      t.thread_id,
      t.status,
      t.created_at.desc(),
    ),
    run_status_idx: index("work_pending_memory_run_status_idx").on(
      t.org_id,
      t.run_id,
      t.status,
      t.created_at.desc(),
    ),
    org_status_idx: index("work_pending_memory_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
  }),
);

// Document library: uploaded files promoted from thread context into a
// per-user library, distilled by the librarian job into OKF concept docs
// (see packages/llm/src/library). Personal by default (user_id set),
// team-visible only via explicit share + approval, mirroring the
// work_memory overlay rules.
export const library_document = pgTable(
  "library_document",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    // NULL user_id = solo/no-auth install (actor has no user id); the
    // document still behaves as the uploader's personal layer.
    user_id: text("user_id").references(() => app_user.id, {
      onDelete: "cascade",
    }),
    source_thread_id: uuid("source_thread_id").references(() => work_thread.id, {
      onDelete: "set null",
    }),
    filename: text("filename").notNull(),
    // Org-workspace-relative path of the raw upload, e.g.
    // "uploads/<threadId>/<name>" — the citation target.
    relative_path: text("relative_path").notNull(),
    content_hash: text("content_hash").notNull(),
    size_bytes: integer("size_bytes").notNull(),
    status: text("status").notNull().default("uploaded"),
    skip_reason: text("skip_reason"),
    error: text("error"),
    // Extraction is a durable async stage. The task/checkpoint is temporary;
    // the small hashes and extractor fingerprint remain as provenance after
    // the normalized Markdown artifact has been removed.
    extract_checkpoint: jsonb("extract_checkpoint"),
    extracted_relative_path: text("extracted_relative_path"),
    extracted_content_hash: text("extracted_content_hash"),
    extractor_fingerprint: text("extractor_fingerprint"),
    extracted_at: ts("extracted_at"),
    // Resumable-distillation checkpoint written by the librarian. It binds the
    // chunk cursor and accumulated ops to source/extraction/plan hashes. NULL
    // when idle or complete; kept on failure so a retry resumes mid-document.
    distill_checkpoint: jsonb("distill_checkpoint"),
    distilled_at: ts("distilled_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_recent_idx: index("library_document_org_recent_idx").on(
      t.org_id,
      t.created_at.desc(),
    ),
    org_user_idx: index("library_document_org_user_idx").on(
      t.org_id,
      t.user_id,
      t.status,
    ),
    org_hash_idx: index("library_document_org_hash_idx").on(
      t.org_id,
      t.content_hash,
    ),
    async_recovery_idx: index("library_document_async_recovery_idx").on(
      t.status,
      t.updated_at,
    ),
  }),
);

export const library_concept = pgTable(
  "library_concept",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    // NULL user_id = team layer (approved, org-visible). Non-NULL =
    // the owner's personal layer, invisible to everyone else. Path is
    // unique per (org, layer) — enforced in code, not by constraint,
    // because NULLs make a plain unique index inert here.
    user_id: text("user_id").references(() => app_user.id, {
      onDelete: "cascade",
    }),
    // Bundle-relative OKF path, e.g. "policies/refund-policy.md".
    path: text("path").notNull(),
    type: text("type").notNull(),
    title: text("title").notNull(),
    description: text("description"),
    tags: jsonb("tags").notNull().default(sql`'[]'::jsonb`),
    body: text("body").notNull(),
    status: text("status").notNull().default("draft"),
    // OKF v0.2 provenance/trust, projected into frontmatter on
    // materialization: sources point at raw uploads, generated is the
    // producing actor, verified accumulates human approvals.
    sources: jsonb("sources").notNull().default(sql`'[]'::jsonb`),
    generated_by: text("generated_by"),
    generated_at: ts("generated_at"),
    verified: jsonb("verified").notNull().default(sql`'[]'::jsonb`),
    stale_after: date("stale_after"),
    source_document_id: uuid("source_document_id").references(
      () => library_document.id,
      { onDelete: "set null" },
    ),
    promoted_from_id: uuid("promoted_from_id"),
    promoted_by: text("promoted_by"),
    promoted_at: ts("promoted_at"),
    archived_at: ts("archived_at"),
    embedding: vector("embedding", 384),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_layer_path_idx: index("library_concept_org_layer_path_idx").on(
      t.org_id,
      t.user_id,
      t.path,
    ),
    org_status_idx: index("library_concept_org_status_idx").on(
      t.org_id,
      t.status,
      t.archived_at,
      t.updated_at.desc(),
    ),
    org_user_idx: index("library_concept_org_user_idx").on(
      t.org_id,
      t.user_id,
      t.archived_at,
    ),
  }),
);

export const library_event = pgTable(
  "library_event",
  {
    id: bigserial("id", { mode: "number" }).primaryKey(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    document_id: uuid("document_id").references(() => library_document.id, {
      onDelete: "set null",
    }),
    concept_id: uuid("concept_id").references(() => library_concept.id, {
      onDelete: "set null",
    }),
    user_id: text("user_id"),
    action: text("action").notNull(),
    payload: jsonb("payload").notNull().default(sql`'{}'::jsonb`),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    concept_idx: index("library_event_concept_idx").on(t.concept_id, t.id.desc()),
    org_recent_idx: index("library_event_org_recent_idx").on(t.org_id, t.id.desc()),
  }),
);

export const workflow_definition = pgTable(
  "workflow_definition",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    description: text("description").notNull().default(""),
    enabled: boolean("enabled").notNull().default(true),
    status: text("status").notNull().default("active"),
    goal: text("goal").notNull().default(""),
    system_prompt_overlay: text("system_prompt_overlay").notNull().default(""),
    steps: jsonb("steps").notNull().default(sql`'[]'::jsonb`),
    cron: text("cron"),
    cron_timezone: text("cron_timezone").notNull().default("UTC"),
    cron_enabled: boolean("cron_enabled").notNull().default(true),
    network_hosts: text("network_hosts").array().notNull().default(sql`'{}'::text[]`),
    daily_run_budget: integer("daily_run_budget"),
    // OL7 "pause for today": enabled=false with a re-enable timer the
    // cron sweep honors.
    paused_until: ts("paused_until"),
    // CV1 ownership: '' = org layer; a member's personal workflow carries
    // their user id. Unique is (org, owner, name). origin_id is the stable
    // identity across copy/promote; parent_id the fork/promote source.
    owner_user_id: text("owner_user_id").notNull().default(""),
    origin_id: uuid("origin_id"),
    parent_id: uuid("parent_id"),
    output_contract: jsonb("output_contract"),
    created_by_thread_id: uuid("created_by_thread_id").references(
      () => work_thread.id,
      { onDelete: "set null" },
    ),
    created_by_run_id: uuid("created_by_run_id").references(() => work_run.id, {
      onDelete: "set null",
    }),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_idx: index("workflow_definition_org_idx").on(
      t.org_id,
      t.enabled,
      t.updated_at.desc(),
    ),
    org_owner_name_unique: uniqueIndex(
      "workflow_definition_org_owner_name_unique",
    ).on(t.org_id, t.owner_user_id, t.name),
    cron_active_idx: index("workflow_definition_cron_active_idx")
      .on(t.org_id, t.cron_enabled)
      .where(sql`${t.cron} is not null and ${t.enabled} = true`),
  }),
);

/**
 * Durable cursor for cron workflows. The cron expression and definition
 * timestamp are snapshotted so schedule edits/re-enables reset the cursor to
 * the first occurrence strictly after the edit instead of backfilling an
 * occurrence that predates the change.
 */
export const workflow_schedule_state = pgTable(
  "workflow_schedule_state",
  {
    workflow_id: uuid("workflow_id")
      .primaryKey()
      .references(() => workflow_definition.id, { onDelete: "cascade" }),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    cron: text("cron").notNull(),
    cron_timezone: text("cron_timezone").notNull(),
    definition_updated_at: ts("definition_updated_at").notNull(),
    next_fire_at: ts("next_fire_at").notNull(),
    catch_up_policy: text("catch_up_policy").notNull().default("coalesce"),
    last_materialized_at: ts("last_materialized_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    due_idx: index("workflow_schedule_state_due_idx").on(t.next_fire_at),
    org_idx: index("workflow_schedule_state_org_idx").on(t.org_id),
  }),
);

export const workflow_run = pgTable(
  "workflow_run",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    workflow_id: uuid("workflow_id")
      .notNull()
      .references(() => workflow_definition.id, { onDelete: "cascade" }),
    thread_id: uuid("thread_id")
      .notNull()
      .references(() => work_thread.id, { onDelete: "cascade" }),
    work_run_id: uuid("work_run_id")
      .notNull()
      .references(() => work_run.id, { onDelete: "cascade" }),
    trigger_kind: text("trigger_kind").notNull(),
    trigger_payload: jsonb("trigger_payload")
      .notNull()
      .default(sql`'{}'::jsonb`),
    // Public API mode is typed run metadata, not something callers can hide
    // inside trigger_payload. Null retains the historical meaning for manual,
    // cron, subscription, and watcher runs.
    execution_mode: text("execution_mode"),
    // A bounded, redacted projection for operator surfaces. The executable
    // request body lives on the short-retention admission row instead.
    trigger_input_preview: jsonb("trigger_input_preview"),
    triggered_by_subscription_id: uuid("triggered_by_subscription_id"),
    triggered_by_output_id: uuid("triggered_by_output_id"),
    triggered_by_observation_id: uuid("triggered_by_observation_id"),
    chain_depth: integer("chain_depth").notNull().default(0),
    status: text("status").notNull().default("queued"),
    started_at: ts("started_at"),
    finished_at: ts("finished_at"),
    summary: text("summary"),
    error: text("error"),
    telemetry_summary: jsonb("telemetry_summary"),
    terminal_result: jsonb("terminal_result"),
    result_artifact_path: text("result_artifact_path"),
    progress: jsonb("progress").notNull().default(sql`'{}'::jsonb`),
    queue_attempts: integer("queue_attempts").notNull().default(0),
    admitted_at: ts("admitted_at"),
    result_expires_at: ts("result_expires_at"),
    source_writes: jsonb("source_writes")
      .notNull()
      .default(sql`'[]'::jsonb`),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    execution_mode_check: check(
      "workflow_run_execution_mode_check",
      sql`(${t.trigger_kind} = 'api' and ${t.execution_mode} in ('single', 'batch')) or (${t.trigger_kind} <> 'api' and ${t.execution_mode} is null)`,
    ),
    work_run_unique: uniqueIndex("workflow_run_work_run_unique").on(
      t.work_run_id,
    ),
    workflow_created_idx: index("workflow_run_workflow_created_idx").on(
      t.workflow_id,
      t.created_at.desc(),
    ),
    org_status_idx: index("workflow_run_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
  }),
);

/**
 * Per-workflow external API gate. The token verifier is a SHA-256 digest of a
 * 256-bit random token; plaintext exists only in the enable/rotate response.
 * Limits are deliberately explicit so an administrator can inspect the exact
 * admission contract without reading an opaque settings blob.
 */
export const workflow_api_access = pgTable(
  "workflow_api_access",
  {
    workflow_id: uuid("workflow_id")
      .primaryKey()
      .references(() => workflow_definition.id, { onDelete: "cascade" }),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    enabled: boolean("enabled").notNull().default(false),
    token_verifier: text("token_verifier"),
    token_prefix: text("token_prefix"),
    token_created_at: ts("token_created_at"),
    token_rotated_at: ts("token_rotated_at"),
    last_used_at: ts("last_used_at"),
    request_limit_per_minute: integer("request_limit_per_minute")
      .notNull()
      .default(30),
    poll_limit_per_minute: integer("poll_limit_per_minute")
      .notNull()
      .default(120),
    queue_cap: integer("queue_cap").notNull().default(25),
    concurrency_cap: integer("concurrency_cap").notNull().default(2),
    batch_max_records: integer("batch_max_records").notNull().default(1000),
    batch_chunk_size: integer("batch_chunk_size").notNull().default(100),
    max_request_bytes: integer("max_request_bytes").notNull().default(262144),
    max_result_bytes: integer("max_result_bytes").notNull().default(262144),
    max_artifact_bytes: integer("max_artifact_bytes")
      .notNull()
      .default(10485760),
    max_runtime_seconds: integer("max_runtime_seconds").notNull().default(600),
    max_model_calls: integer("max_model_calls").notNull().default(8),
    max_tool_calls: integer("max_tool_calls").notNull().default(32),
    max_tokens_per_run: integer("max_tokens_per_run").notNull().default(100000),
    max_cost_micros_per_run: integer("max_cost_micros_per_run")
      .notNull()
      .default(5000000),
    rolling_window_seconds: integer("rolling_window_seconds")
      .notNull()
      .default(86400),
    rolling_token_budget: integer("rolling_token_budget")
      .notNull()
      .default(250000),
    rolling_cost_micros_budget: integer("rolling_cost_micros_budget")
      .notNull()
      .default(10000000),
    retention_hours: integer("retention_hours").notNull().default(168),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    token_state_check: check(
      "workflow_api_access_token_state",
      sql`${t.enabled} = false or (${t.token_verifier} is not null and ${t.token_prefix} is not null and ${t.token_created_at} is not null)`,
    ),
    batch_bounds_check: check(
      "workflow_api_access_batch_bounds",
      sql`${t.batch_chunk_size} <= ${t.batch_max_records}`,
    ),
    verifier_shape_check: check(
      "workflow_api_access_verifier_shape",
      sql`${t.token_verifier} is null or ${t.token_verifier} ~ '^[0-9a-f]{64}$'`,
    ),
    prefix_shape_check: check(
      "workflow_api_access_prefix_shape",
      sql`${t.token_prefix} is null or ${t.token_prefix} ~ '^onk_wf_[0-9a-f]{12}$'`,
    ),
    request_limit_check: check(
      "workflow_api_access_request_limit_per_minute_check",
      sql`${t.request_limit_per_minute} between 1 and 600`,
    ),
    poll_limit_check: check(
      "workflow_api_access_poll_limit_per_minute_check",
      sql`${t.poll_limit_per_minute} between 1 and 1200`,
    ),
    queue_cap_check: check(
      "workflow_api_access_queue_cap_check",
      sql`${t.queue_cap} between 1 and 250`,
    ),
    concurrency_cap_check: check(
      "workflow_api_access_concurrency_cap_check",
      sql`${t.concurrency_cap} between 1 and 20`,
    ),
    batch_max_records_check: check(
      "workflow_api_access_batch_max_records_check",
      sql`${t.batch_max_records} between 1 and 10000`,
    ),
    batch_chunk_size_check: check(
      "workflow_api_access_batch_chunk_size_check",
      sql`${t.batch_chunk_size} between 1 and 500`,
    ),
    max_request_bytes_check: check(
      "workflow_api_access_max_request_bytes_check",
      sql`${t.max_request_bytes} between 1024 and 1048576`,
    ),
    max_result_bytes_check: check(
      "workflow_api_access_max_result_bytes_check",
      sql`${t.max_result_bytes} between 1024 and 1048576`,
    ),
    max_artifact_bytes_check: check(
      "workflow_api_access_max_artifact_bytes_check",
      sql`${t.max_artifact_bytes} between 1024 and 52428800`,
    ),
    max_runtime_seconds_check: check(
      "workflow_api_access_max_runtime_seconds_check",
      sql`${t.max_runtime_seconds} between 30 and 1800`,
    ),
    max_model_calls_check: check(
      "workflow_api_access_max_model_calls_check",
      sql`${t.max_model_calls} between 1 and 32`,
    ),
    max_tool_calls_check: check(
      "workflow_api_access_max_tool_calls_check",
      sql`${t.max_tool_calls} between 1 and 128`,
    ),
    max_tokens_per_run_check: check(
      "workflow_api_access_max_tokens_per_run_check",
      sql`${t.max_tokens_per_run} between 1000 and 3000000`,
    ),
    max_cost_per_run_check: check(
      "workflow_api_access_max_cost_micros_per_run_check",
      sql`${t.max_cost_micros_per_run} between 1000 and 100000000`,
    ),
    rolling_window_check: check(
      "workflow_api_access_rolling_window_seconds_check",
      sql`${t.rolling_window_seconds} between 3600 and 604800`,
    ),
    rolling_token_budget_check: check(
      "workflow_api_access_rolling_token_budget_check",
      sql`${t.rolling_token_budget} between 1000 and 10000000`,
    ),
    rolling_cost_budget_check: check(
      "workflow_api_access_rolling_cost_micros_budget_check",
      sql`${t.rolling_cost_micros_budget} between 1000 and 1000000000`,
    ),
    retention_hours_check: check(
      "workflow_api_access_retention_hours_check",
      sql`${t.retention_hours} between 1 and 720`,
    ),
    org_idx: index("workflow_api_access_org_idx").on(t.org_id, t.enabled),
  }),
);

/**
 * Idempotency record + transactional outbox for external workflow runs.
 * request_payload is capped and expires; batch rows live in input_file_path,
 * keeping their data plane out of Postgres and the worker queue.
 */
export const workflow_api_admission = pgTable(
  "workflow_api_admission",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    workflow_id: uuid("workflow_id")
      .notNull()
      .references(() => workflow_definition.id, { onDelete: "cascade" }),
    workflow_run_id: uuid("workflow_run_id")
      .notNull()
      .references(() => workflow_run.id, { onDelete: "cascade" }),
    idempotency_hash: text("idempotency_hash").notNull(),
    payload_hash: text("payload_hash").notNull(),
    execution_mode: text("execution_mode").notNull(),
    request_payload: jsonb("request_payload"),
    input_file_path: text("input_file_path"),
    batch_contract: jsonb("batch_contract"),
    request_bytes: integer("request_bytes").notNull(),
    accepted_records: integer("accepted_records"),
    status: text("status").notNull().default("pending"),
    attempts: integer("attempts").notNull().default(0),
    reserved_tokens: integer("reserved_tokens").notNull().default(0),
    reserved_cost_micros: integer("reserved_cost_micros").notNull().default(0),
    actual_tokens: integer("actual_tokens"),
    actual_cost_micros: integer("actual_cost_micros"),
    available_at: ts("available_at").notNull().defaultNow(),
    lease_until: ts("lease_until"),
    queue_job_id: text("queue_job_id"),
    last_error_code: text("last_error_code"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    dispatched_at: ts("dispatched_at"),
    started_at: ts("started_at"),
    completed_at: ts("completed_at"),
    expires_at: ts("expires_at").notNull(),
  },
  (t) => ({
    execution_mode_check: check(
      "workflow_api_admission_execution_mode_check",
      sql`${t.execution_mode} in ('single', 'batch')`,
    ),
    status_check: check(
      "workflow_api_admission_status_check",
      sql`${t.status} in ('pending', 'dispatching', 'enqueued', 'running', 'completed', 'failed', 'cancelled', 'expired')`,
    ),
    payload_location_check: check(
      "workflow_api_admission_payload_location",
      sql`(${t.execution_mode} = 'single' and ${t.request_payload} is not null and ${t.input_file_path} is null and ${t.batch_contract} is null and ${t.accepted_records} is null) or (${t.execution_mode} = 'batch' and ${t.request_payload} is null and ${t.input_file_path} is not null and ${t.batch_contract} is not null and ${t.accepted_records} between 1 and 10000)`,
    ),
    request_bounds_check: check(
      "workflow_api_admission_request_bounds",
      sql`${t.request_bytes} between 2 and 1048576`,
    ),
    attempts_check: check(
      "workflow_api_admission_attempts_nonnegative",
      sql`${t.attempts} >= 0`,
    ),
    reservations_check: check(
      "workflow_api_admission_reservations_nonnegative",
      sql`${t.reserved_tokens} >= 0 and ${t.reserved_cost_micros} >= 0 and (${t.actual_tokens} is null or ${t.actual_tokens} >= 0) and (${t.actual_cost_micros} is null or ${t.actual_cost_micros} >= 0)`,
    ),
    hashes_check: check(
      "workflow_api_admission_hashes_shape",
      sql`${t.idempotency_hash} ~ '^[0-9a-f]{64}$' and ${t.payload_hash} ~ '^[0-9a-f]{64}$'`,
    ),
    expiry_check: check(
      "workflow_api_admission_expiry_check",
      sql`${t.expires_at} > ${t.created_at}`,
    ),
    idempotency_unique: uniqueIndex(
      "workflow_api_admission_idempotency_unique",
    ).on(t.org_id, t.workflow_id, t.idempotency_hash),
    workflow_run_unique: uniqueIndex(
      "workflow_api_admission_workflow_run_unique",
    ).on(t.workflow_run_id),
    dispatch_idx: index("workflow_api_admission_dispatch_idx").on(
      t.status,
      t.available_at,
    ),
    workflow_created_idx: index(
      "workflow_api_admission_workflow_created_idx",
    ).on(t.workflow_id, t.created_at.desc()),
  }),
);

/** Durable fixed-window counters shared by every web replica. */
export const workflow_api_rate_bucket = pgTable(
  "workflow_api_rate_bucket",
  {
    scope_kind: text("scope_kind").notNull(),
    scope_id: text("scope_id").notNull(),
    operation: text("operation").notNull(),
    window_start: ts("window_start").notNull(),
    count: integer("count").notNull().default(0),
    expires_at: ts("expires_at").notNull(),
  },
  (t) => ({
    count_check: check(
      "workflow_api_rate_bucket_count_check",
      sql`${t.count} >= 0`,
    ),
    scope_check: check(
      "workflow_api_rate_bucket_scope_check",
      sql`${t.scope_kind} in ('deployment', 'client', 'workflow', 'organization')`,
    ),
    operation_check: check(
      "workflow_api_rate_bucket_operation_check",
      sql`${t.operation} in ('edge', 'invoke', 'poll')`,
    ),
    expiry_check: check(
      "workflow_api_rate_bucket_expiry_check",
      sql`${t.expires_at} > ${t.window_start}`,
    ),
    pk: primaryKey({
      name: "workflow_api_rate_bucket_pk",
      columns: [t.scope_kind, t.scope_id, t.operation, t.window_start],
    }),
    expiry_idx: index("workflow_api_rate_bucket_expiry_idx").on(t.expires_at),
  }),
);

/**
 * Transactional outbox for cron occurrences. A unique
 * (workflow_id, scheduled_for) key makes materialization idempotent; the
 * worker leases pending rows and delivers them to pg-boss at least once.
 */
export const workflow_schedule_firing = pgTable(
  "workflow_schedule_firing",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    workflow_id: uuid("workflow_id")
      .notNull()
      .references(() => workflow_definition.id, { onDelete: "cascade" }),
    scheduled_for: ts("scheduled_for").notNull(),
    status: text("status").notNull().default("pending"),
    attempts: integer("attempts").notNull().default(0),
    available_at: ts("available_at").notNull().defaultNow(),
    lease_until: ts("lease_until"),
    queue_job_id: text("queue_job_id"),
    workflow_run_id: uuid("workflow_run_id").references(() => workflow_run.id, {
      onDelete: "set null",
    }),
    last_error: text("last_error"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    dispatched_at: ts("dispatched_at"),
    completed_at: ts("completed_at"),
  },
  (t) => ({
    workflow_scheduled_unique: uniqueIndex(
      "workflow_schedule_firing_workflow_scheduled_unique",
    ).on(t.workflow_id, t.scheduled_for),
    workflow_run_unique: uniqueIndex(
      "workflow_schedule_firing_workflow_run_unique",
    )
      .on(t.workflow_run_id)
      .where(sql`${t.workflow_run_id} is not null`),
    pending_idx: index("workflow_schedule_firing_pending_idx").on(
      t.status,
      t.available_at,
    ),
    workflow_created_idx: index(
      "workflow_schedule_firing_workflow_created_idx",
    ).on(t.workflow_id, t.created_at.desc()),
  }),
);

/** Persisted heartbeat lets health checks distinguish process liveness from a
 * scheduler that has stopped making progress. */
export const workflow_scheduler_health = pgTable(
  "workflow_scheduler_health",
  {
    id: text("id").primaryKey(),
    last_started_at: ts("last_started_at"),
    last_succeeded_at: ts("last_succeeded_at"),
    last_error_at: ts("last_error_at"),
    last_error: text("last_error"),
    last_materialized_count: integer("last_materialized_count")
      .notNull()
      .default(0),
    last_dispatched_count: integer("last_dispatched_count")
      .notNull()
      .default(0),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
);

export const workflow_output = pgTable(
  "workflow_output",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    workflow_run_id: uuid("workflow_run_id")
      .notNull()
      .references(() => workflow_run.id, { onDelete: "cascade" }),
    work_run_id: uuid("work_run_id")
      .notNull()
      .references(() => work_run.id, { onDelete: "cascade" }),
    kind: text("kind").notNull(),
    title: text("title").notNull().default(""),
    body: text("body").notNull().default(""),
    payload: jsonb("payload").notNull().default(sql`'{}'::jsonb`),
    artifact_path: text("artifact_path"),
    scope: text("scope"),
    topic: text("topic"),
    mood: text("mood"),
    time_window_start: ts("time_window_start"),
    time_window_end: ts("time_window_end"),
    freshness_ttl_seconds: integer("freshness_ttl_seconds"),
    // OL8 card-level dedupe: an identical finding within 24h bumps
    // seen_count on the original card instead of creating a new one.
    seen_count: integer("seen_count").notNull().default(1),
    last_seen_at: ts("last_seen_at").notNull().defaultNow(),
    dedupe_key: text("dedupe_key"),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    run_created_idx: index("workflow_output_run_created_idx").on(
      t.workflow_run_id,
      t.created_at.desc(),
    ),
    org_scope_idx: index("workflow_output_org_scope_idx")
      .on(t.org_id, t.scope, t.created_at.desc())
      .where(sql`${t.scope} is not null`),
    org_mood_idx: index("workflow_output_org_mood_idx")
      .on(t.org_id, t.mood, t.created_at.desc())
      .where(sql`${t.mood} is not null`),
  }),
);

// Routes a workflow output (for an audience) to a channel plugin's deliver RPC.
// The web channel is implicit/always-on; rows here add extra membranes
// (Telegram, Slack, …) per the V2 frontend-as-capability design. `recipient`
// is the channel-native address minted at config time.
export const delivery_binding = pgTable(
  "delivery_binding",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    audience: text("audience").notNull().default("*"),
    channel_plugin: text("channel_plugin").notNull(),
    recipient: jsonb("recipient").notNull().default(sql`'{}'::jsonb`),
    filter: jsonb("filter"),
    enabled: boolean("enabled").notNull().default(true),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_enabled_idx: index("delivery_binding_org_enabled_idx").on(
      t.org_id,
      t.enabled,
    ),
  }),
);

// Persisted poll offset per (org, channel). Inbound polling keeps an in-memory
// cursor while running; persisting it means a restart resumes from the last
// acknowledged offset instead of re-polling the provider from scratch.
export const channel_poll_cursor = pgTable(
  "channel_poll_cursor",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    channel_plugin: text("channel_plugin").notNull(),
    cursor: text("cursor").notNull(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_plugin_unique: uniqueIndex("channel_poll_cursor_org_plugin_unique").on(
      t.org_id,
      t.channel_plugin,
    ),
  }),
);

// Per-update ledger for inbound dispatch. A poll restart (or webhook retry) can
// re-deliver an update; claiming a stable per-update key here makes dispatch
// exactly-once. `status` tracks the lifecycle: 'pending' (claimed, retrying),
// 'done' (dispatched — the dedup marker), 'dead' (gave up after MAX attempts —
// payload + last_error retained for inspection). 'done' rows are TTL-pruned;
// 'dead' rows persist as a queryable dead-letter queue.
export const inbound_dedup = pgTable(
  "inbound_dedup",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    channel_plugin: text("channel_plugin").notNull(),
    update_key: text("update_key").notNull(),
    status: text("status").notNull().default("done"),
    attempts: integer("attempts").notNull().default(0),
    last_error: text("last_error"),
    payload: jsonb("payload"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_plugin_key_unique: uniqueIndex("inbound_dedup_org_plugin_key_unique").on(
      t.org_id,
      t.channel_plugin,
      t.update_key,
    ),
    created_idx: index("inbound_dedup_created_idx").on(t.created_at),
    status_idx: index("inbound_dedup_status_idx").on(t.status),
  }),
);

export const subscription = pgTable(
  "subscription",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    workflow_id: uuid("workflow_id")
      .notNull()
      .references(() => workflow_definition.id, { onDelete: "cascade" }),
    source_kind: text("source_kind").notNull(),
    filter: jsonb("filter").notNull().default(sql`'{}'::jsonb`),
    enabled: boolean("enabled").notNull().default(true),
    debounce_ms: integer("debounce_ms").notNull().default(0),
    max_concurrent_runs: integer("max_concurrent_runs").notNull().default(5),
    max_chain_depth_override: integer("max_chain_depth_override"),
    idempotency_key_template: text("idempotency_key_template"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    workflow_idx: index("subscription_workflow_idx").on(t.workflow_id),
    org_enabled_idx: index("subscription_org_enabled_idx")
      .on(t.org_id, t.enabled)
      .where(sql`${t.enabled} = true`),
    source_kind_idx: index("subscription_source_kind_idx")
      .on(t.source_kind)
      .where(sql`${t.enabled} = true`),
  }),
);

export const observation = pgTable(
  "observation",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    source_output_id: uuid("source_output_id").references(
      () => workflow_output.id,
      { onDelete: "set null" },
    ),
    consumer_kind: text("consumer_kind").notNull(),
    consumer_workflow_id: uuid("consumer_workflow_id").references(
      () => workflow_definition.id,
      { onDelete: "set null" },
    ),
    consumer_run_id: uuid("consumer_run_id").references(() => workflow_run.id, {
      onDelete: "set null",
    }),
    consumer_user_id: text("consumer_user_id"),
    subscription_id: uuid("subscription_id").references(() => subscription.id, {
      onDelete: "set null",
    }),
    title: text("title"),
    body: text("body"),
    mood: text("mood"),
    status: text("status").notNull().default("active"),
    first_seen_at: ts("first_seen_at").notNull().defaultNow(),
    last_seen_at: ts("last_seen_at").notNull().defaultNow(),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    source_output_idx: index("observation_source_output_idx")
      .on(t.source_output_id)
      .where(sql`${t.source_output_id} is not null`),
    consumer_workflow_idx: index("observation_consumer_workflow_idx")
      .on(t.consumer_workflow_id, t.created_at.desc())
      .where(sql`${t.consumer_workflow_id} is not null`),
    subscription_idx: index("observation_subscription_idx")
      .on(t.subscription_id, t.created_at.desc())
      .where(sql`${t.subscription_id} is not null`),
    org_status_idx: index("observation_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
  }),
);

export const workflow_output_source_observation = pgTable(
  "workflow_output_source_observation",
  {
    workflow_output_id: uuid("workflow_output_id")
      .notNull()
      .references(() => workflow_output.id, { onDelete: "cascade" }),
    observation_id: uuid("observation_id")
      .notNull()
      .references(() => observation.id, { onDelete: "cascade" }),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: uniqueIndex("workflow_output_source_obs_pk").on(
      t.workflow_output_id,
      t.observation_id,
    ),
    obs_idx: index("workflow_output_source_obs_obs_idx").on(t.observation_id),
  }),
);

// OL2 — observation-elevation: promote a consumer-side observation onto
// the Briefing as a first-class card (it may have no producing output,
// e.g. an external_event or source_change observation).
export const briefing_card = pgTable(
  "briefing_card",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    source_observation_id: uuid("source_observation_id")
      .notNull()
      .references(() => observation.id, { onDelete: "cascade" }),
    title: text("title"),
    body: text("body"),
    mood: text("mood"),
    status: text("status").notNull().default("active"),
    elevated_by: text("elevated_by").notNull().default("system"),
    elevated_by_user_id: text("elevated_by_user_id"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_observation_unique: uniqueIndex(
      "briefing_card_org_observation_unique",
    ).on(t.org_id, t.source_observation_id),
    org_status_idx: index("briefing_card_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
  }),
);

export const action_policy = pgTable(
  "action_policy",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    description: text("description").notNull().default(""),
    applies_to_kinds: text("applies_to_kinds")
      .array()
      .notNull()
      .default(sql`'{}'::text[]`),
    applies_to_scopes: text("applies_to_scopes")
      .array()
      .notNull()
      .default(sql`'{}'::text[]`),
    mode: text("mode").notNull(),
    risk_threshold_auto_approve: text("risk_threshold_auto_approve"),
    allowed_targets: jsonb("allowed_targets"),
    denied_targets: jsonb("denied_targets"),
    limits: jsonb("limits").notNull().default(sql`'{}'::jsonb`),
    approver_group_id: uuid("approver_group_id"),
    priority: integer("priority").notNull().default(100),
    enabled: boolean("enabled").notNull().default(true),
    created_by_thread_id: uuid("created_by_thread_id").references(
      () => work_thread.id,
      { onDelete: "set null" },
    ),
    created_by_run_id: uuid("created_by_run_id").references(() => work_run.id, {
      onDelete: "set null",
    }),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_enabled_priority_idx: index("action_policy_org_enabled_priority_idx").on(
      t.org_id,
      t.enabled,
      t.priority,
    ),
  }),
);

export const action_target_spec = pgTable(
  "action_target_spec",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    kind: text("kind").notNull(),
    plugin_name: text("plugin_name").notNull(),
    spec: jsonb("spec").notNull(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.kind] }),
  }),
);

export const action_request = pgTable(
  "action_request",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    workflow_run_id: uuid("workflow_run_id").references(
      () => workflow_run.id,
      { onDelete: "cascade" },
    ),
    triggered_by_observation_id: uuid("triggered_by_observation_id").references(
      () => observation.id,
      { onDelete: "set null" },
    ),
    policy_id: uuid("policy_id").references(() => action_policy.id, {
      onDelete: "set null",
    }),
    scope: text("scope").notNull(),
    kind: text("kind").notNull(),
    target: text("target"),
    payload: jsonb("payload").notNull().default(sql`'{}'::jsonb`),
    risk_level: text("risk_level"),
    status: text("status").notNull().default("pending_approval"),
    summary: text("summary"),
    intent: text("intent"),
    // Agent-estimated minutes of human effort this action saved, counted
    // toward rollups only once status reaches "executed". Server-clamped.
    minutes_saved: integer("minutes_saved"),
    minutes_saved_basis: text("minutes_saved_basis"),
    estimate_source: text("estimate_source").notNull().default("agent"),
    estimate_version: integer("estimate_version").notNull().default(1),
    // SEC5: dual identity snapshotted at creation — the human principal
    // (K1) and the agent backend that proposed the action.
    actor_user_id: text("actor_user_id"),
    actor_role: text("actor_role"),
    actor_backend: text("actor_backend"),
    work_run_id: uuid("work_run_id").references(() => work_run.id, {
      onDelete: "set null",
    }),
    requested_by_run_id: uuid("requested_by_run_id").references(
      () => workflow_run.id,
      { onDelete: "set null" },
    ),
    approved_by_user_id: text("approved_by_user_id").references(
      () => app_user.id,
      { onDelete: "set null" },
    ),
    approved_at: ts("approved_at"),
    rejection_reason: text("rejection_reason"),
    failure_reason: text("failure_reason"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_status_idx: index("action_request_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
    workflow_run_idx: index("action_request_workflow_run_idx").on(
      t.workflow_run_id,
      t.created_at.desc(),
    ),
    pending_idx: index("action_request_pending_idx").on(
      t.org_id,
      t.created_at.desc(),
    ),
  }),
);

export const action_changeset = pgTable(
  "action_changeset",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    action_request_id: uuid("action_request_id").references(
      () => action_request.id,
      { onDelete: "set null" },
    ),
    // The SQL migration owns the self-reference; omitting it here avoids a
    // circular initializer while retaining the database-level foreign key.
    inverse_of_id: uuid("inverse_of_id"),
    domain: text("domain").notNull(),
    scope: jsonb("scope")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    operation_id: text("operation_id").notNull(),
    risk_class: smallint("risk_class").notNull(),
    status: text("status").notNull().default("draft"),
    summary: text("summary").notNull().default(""),
    idempotency_key: text("idempotency_key").notNull(),
    bulk_uuid: text("bulk_uuid"),
    cap_snapshot: jsonb("cap_snapshot")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    projected_exposure: numeric("projected_exposure", {
      precision: 18,
      scale: 4,
    }),
    previewed_at: ts("previewed_at"),
    approved_by_user_id: text("approved_by_user_id").references(
      () => app_user.id,
      { onDelete: "set null" },
    ),
    approved_at: ts("approved_at"),
    executed_at: ts("executed_at"),
    reconciled_at: ts("reconciled_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_idempotency_unique: uniqueIndex(
      "action_changeset_org_idempotency_unique",
    ).on(t.org_id, t.idempotency_key),
    org_status_idx: index("action_changeset_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
    request_idx: index("action_changeset_request_idx").on(
      t.action_request_id,
      t.created_at.desc(),
    ),
  }),
);

export const action_changeset_row = pgTable(
  "action_changeset_row",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    changeset_id: uuid("changeset_id")
      .notNull()
      .references(() => action_changeset.id, { onDelete: "cascade" }),
    row_index: integer("row_index").notNull(),
    entity_ref: text("entity_ref").notNull(),
    operation_id: text("operation_id").notNull(),
    path: jsonb("path")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    query: jsonb("query")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    before_image: jsonb("before_image")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    after_image: jsonb("after_image")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    expected_current: jsonb("expected_current")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    status: text("status").notNull().default("draft"),
    external_ref: text("external_ref"),
    reconciled_image: jsonb("reconciled_image").$type<Record<string, unknown>>(),
    error: text("error"),
    started_at: ts("started_at"),
    finished_at: ts("finished_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    position_unique: uniqueIndex("action_changeset_row_position_unique").on(
      t.changeset_id,
      t.row_index,
    ),
    status_idx: index("action_changeset_row_status_idx").on(
      t.changeset_id,
      t.status,
      t.row_index,
    ),
  }),
);

export const action_execution = pgTable(
  "action_execution",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    action_request_id: uuid("action_request_id")
      .notNull()
      .references(() => action_request.id, { onDelete: "cascade" }),
    changeset_id: uuid("changeset_id").references(() => action_changeset.id, {
      onDelete: "set null",
    }),
    executor: text("executor").notNull(),
    command_or_operation: text("command_or_operation"),
    payload: jsonb("payload"),
    result: jsonb("result"),
    external_ref: text("external_ref"),
    status: text("status").notNull().default("pending"),
    error: text("error"),
    started_at: ts("started_at"),
    finished_at: ts("finished_at"),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    request_idx: index("action_execution_request_idx").on(
      t.action_request_id,
      t.created_at.desc(),
    ),
    org_status_idx: index("action_execution_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
    changeset_idx: index("action_execution_changeset_idx").on(
      t.changeset_id,
      t.created_at.desc(),
    ),
  }),
);

export const magento_store_control = pgTable(
  "magento_store_control",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    domain: text("domain").notNull(),
    risk_class: smallint("risk_class").notNull(),
    enabled: boolean("enabled").notNull().default(true),
    auto_execute: boolean("auto_execute").notNull().default(false),
    caps: jsonb("caps")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    scope: jsonb("scope")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    updated_by_user_id: text("updated_by_user_id").references(
      () => app_user.id,
      { onDelete: "set null" },
    ),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.domain] }),
  }),
);

export const magento_attribute_classification = pgTable(
  "magento_attribute_classification",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    domain: text("domain").notNull(),
    entity_type: text("entity_type").notNull(),
    attribute: text("attribute").notNull(),
    risk_class: smallint("risk_class").notNull(),
    category: text("category").notNull(),
    rationale: text("rationale").notNull().default(""),
    reviewed: boolean("reviewed").notNull().default(true),
    pack_default: boolean("pack_default").notNull().default(true),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    identity_unique: uniqueIndex(
      "magento_attribute_classification_identity_unique",
    ).on(t.org_id, t.domain, t.entity_type, t.attribute),
    lookup_idx: index("magento_attribute_classification_lookup_idx").on(
      t.org_id,
      t.domain,
      t.entity_type,
    ),
  }),
);

export const magento_auto_rule = pgTable(
  "magento_auto_rule",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    instruction: text("instruction").notNull(),
    domain: text("domain").notNull(),
    action_kind: text("action_kind").notNull(),
    compiled_policy: jsonb("compiled_policy")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    daily_cap: integer("daily_cap").notNull(),
    cooldown_seconds: integer("cooldown_seconds").notNull().default(0),
    enabled: boolean("enabled").notNull().default(false),
    suspended_reason: text("suspended_reason"),
    last_fired_at: ts("last_fired_at"),
    created_by_user_id: text("created_by_user_id").references(
      () => app_user.id,
      { onDelete: "set null" },
    ),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_name_unique: uniqueIndex("magento_auto_rule_org_name_unique").on(
      t.org_id,
      t.name,
    ),
    active_idx: index("magento_auto_rule_active_idx").on(
      t.org_id,
      t.enabled,
      t.domain,
    ),
  }),
);

export const magento_financial_handoff = pgTable(
  "magento_financial_handoff",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    action_request_id: uuid("action_request_id").references(
      () => action_request.id,
      { onDelete: "set null" },
    ),
    kind: text("kind").notNull(),
    entity_ref: text("entity_ref").notNull(),
    status: text("status").notNull().default("draft"),
    draft: jsonb("draft")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    evidence: jsonb("evidence")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    completed_by_user_id: text("completed_by_user_id").references(
      () => app_user.id,
      { onDelete: "set null" },
    ),
    completed_at: ts("completed_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_status_idx: index("magento_financial_handoff_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
  }),
);

// Solution packs are organization-scoped, versioned bundles. The provenance
// rows make install/upgrade/uninstall drift-aware without claiming a database
// transaction can also cover GraphJin config files.
export const pack_install = pgTable(
  "pack_install",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    pack_id: text("pack_id").notNull(),
    version: text("version").notNull(),
    status: text("status").notNull().default("planning"),
    manifest_hash: text("manifest_hash").notNull(),
    config: jsonb("config")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    source: text("source").notNull().default("embedded"),
    installed_by_user_id: text("installed_by_user_id").references(
      () => app_user.id,
      { onDelete: "set null" },
    ),
    operation_id: uuid("operation_id"),
    last_error: text("last_error"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    installed_at: ts("installed_at"),
    removed_at: ts("removed_at"),
  },
  (t) => ({
    org_pack_active_unique: uniqueIndex("pack_install_org_pack_active_unique")
      .on(t.org_id, t.pack_id)
      .where(sql`${t.status} <> 'removed'`),
    org_status_idx: index("pack_install_org_status_idx").on(
      t.org_id,
      t.status,
      t.updated_at.desc(),
    ),
  }),
);

export const pack_user_connection = pgTable("pack_user_connection", {
  org_id: text("org_id").notNull().references(() => organization.id, { onDelete: "cascade" }),
  user_id: text("user_id").notNull().references(() => app_user.id, { onDelete: "cascade" }),
  pack_install_id: uuid("pack_install_id").notNull().references(() => pack_install.id, { onDelete: "cascade" }),
  connection_key: text("connection_key").notNull(),
  revision: uuid("revision").notNull().defaultRandom(),
  account_id: text("account_id"),
  account_label: text("account_label"),
  credentials: text("credentials"),
  pending_state_hash: text("pending_state_hash"),
  pending_expires_at: ts("pending_expires_at"),
  updated_at: ts("updated_at").notNull().defaultNow(),
}, t => ({ pk: primaryKey({ columns: [t.org_id, t.user_id, t.pack_install_id, t.connection_key] }) }));

export const pack_artifact = pgTable(
  "pack_artifact",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    pack_install_id: uuid("pack_install_id")
      .notNull()
      .references(() => pack_install.id, { onDelete: "cascade" }),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    artifact_kind: text("artifact_kind").notNull(),
    artifact_key: text("artifact_key").notNull(),
    target_ref: text("target_ref").notNull(),
    desired_hash: text("desired_hash").notNull(),
    last_applied_hash: text("last_applied_hash").notNull(),
    ownership: text("ownership").notNull().default("managed"),
    readiness: text("readiness").notNull().default("not_applicable"),
    readiness_reason: text("readiness_reason"),
    previous_snapshot: jsonb("previous_snapshot").$type<Record<string, unknown>>(),
    metadata: jsonb("metadata")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
    detached_at: ts("detached_at"),
  },
  (t) => ({
    org_kind_target_unique: uniqueIndex("pack_artifact_org_kind_target_unique").on(
      t.org_id,
      t.artifact_kind,
      t.target_ref,
    ),
    install_kind_key_unique: uniqueIndex("pack_artifact_install_kind_key_unique").on(
      t.pack_install_id,
      t.artifact_kind,
      t.artifact_key,
    ),
    install_idx: index("pack_artifact_install_idx").on(
      t.pack_install_id,
      t.ownership,
      t.artifact_kind,
    ),
    org_readiness_idx: index("pack_artifact_org_readiness_idx").on(
      t.org_id,
      t.readiness,
      t.updated_at.desc(),
    ),
  }),
);

export const pack_action_definition = pgTable(
  "pack_action_definition",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    kind: text("kind").notNull(),
    description: text("description").notNull().default(""),
    definition: jsonb("definition")
      .$type<Record<string, unknown>>()
      .notNull(),
    definition_hash: text("definition_hash").notNull(),
    readiness: text("readiness").notNull().default("blocked"),
    readiness_reason: text("readiness_reason"),
    enabled: boolean("enabled").notNull().default(true),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_kind_unique: uniqueIndex("pack_action_definition_org_kind_unique").on(
      t.org_id,
      t.kind,
    ),
    discovery_idx: index("pack_action_definition_discovery_idx").on(
      t.org_id,
      t.readiness,
      t.enabled,
      t.updated_at.desc(),
    ),
  }),
);

export const pack_operation = pgTable(
  "pack_operation",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    pack_install_id: uuid("pack_install_id")
      .notNull()
      .references(() => pack_install.id, { onDelete: "cascade" }),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    operation_type: text("operation_type").notNull(),
    actor_user_id: text("actor_user_id").references(() => app_user.id, {
      onDelete: "set null",
    }),
    status: text("status").notNull().default("planned"),
    requested_version: text("requested_version"),
    idempotency_key: text("idempotency_key").notNull(),
    plan: jsonb("plan")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    plan_hash: text("plan_hash").notNull(),
    phase: text("phase").notNull().default("planned"),
    failure_phase: text("failure_phase"),
    error: text("error"),
    compensation_status: text("compensation_status"),
    started_at: ts("started_at"),
    completed_at: ts("completed_at"),
    created_at: ts("created_at").notNull().defaultNow(),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_idempotency_unique: uniqueIndex("pack_operation_org_idempotency_unique").on(
      t.org_id,
      t.idempotency_key,
    ),
    install_recent_idx: index("pack_operation_install_recent_idx").on(
      t.pack_install_id,
      t.created_at.desc(),
    ),
    org_status_idx: index("pack_operation_org_status_idx").on(
      t.org_id,
      t.status,
      t.created_at.desc(),
    ),
  }),
);

export const skill_usage = pgTable(
  "skill_usage",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    run_id: uuid("run_id")
      .notNull()
      .references(() => work_run.id, { onDelete: "cascade" }),
    skill_name: text("skill_name").notNull(),
    content_hash: text("content_hash").notNull(),
    origin: text("origin").notNull(),
    pack_id: text("pack_id"),
    pack_version: text("pack_version"),
    config_commit_sha: text("config_commit_sha"),
    source: text("source").notNull(),
    first_event_id: bigint("first_event_id", { mode: "number" }).notNull(),
    attempt: integer("attempt").notNull().default(1),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    run_skill_unique: uniqueIndex("skill_usage_run_skill_unique").on(
      t.run_id,
      t.skill_name,
    ),
    org_skill_recent_idx: index("skill_usage_org_skill_recent_idx").on(
      t.org_id,
      t.skill_name,
      t.created_at.desc(),
    ),
  }),
);

export const skill_learn_org = pgTable("skill_learn_org", {
  org_id: text("org_id")
    .primaryKey()
    .references(() => organization.id, { onDelete: "cascade" }),
  enabled: boolean("enabled").notNull().default(false),
  updated_at: ts("updated_at").notNull().defaultNow(),
});

export const skill_learn_state = pgTable(
  "skill_learn_state",
  {
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    skill_name: text("skill_name").notNull(),
    enabled: boolean("enabled").notNull().default(false),
    high_water_event_id: bigint("high_water_event_id", {
      mode: "number",
    })
      .notNull()
      .default(0),
    pending_settled_count: integer("pending_settled_count").notNull().default(0),
    next_due_at: ts("next_due_at"),
    current_base_hash: text("current_base_hash"),
    current_learned_hash: text("current_learned_hash"),
    lease_owner: text("lease_owner"),
    lease_until: ts("lease_until"),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    pk: primaryKey({ columns: [t.org_id, t.skill_name] }),
  }),
);

export const skill_learn_event = pgTable(
  "skill_learn_event",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    skill_name: text("skill_name").notNull(),
    base_hash: text("base_hash"),
    content_hash: text("content_hash"),
    run_ids: jsonb("run_ids").$type<string[]>().notNull().default(sql`'[]'::jsonb`),
    lesson: text("lesson"),
    rationale: text("rationale"),
    diff: text("diff"),
    model_trace: jsonb("model_trace")
      .$type<Record<string, unknown>>()
      .notNull()
      .default(sql`'{}'::jsonb`),
    decision: text("decision").notNull(),
    reason: text("reason").notNull().default(""),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    org_skill_recent_idx: index("skill_learn_event_org_skill_recent_idx").on(
      t.org_id,
      t.skill_name,
      t.created_at.desc(),
    ),
  }),
);

export const context_remote = pgTable("context_remote", {
  org_id: text("org_id")
    .primaryKey()
    .references(() => organization.id, { onDelete: "cascade" }),
  url: text("url").notNull(),
  mode: text("mode").notNull().default("push"),
  branch: text("branch").notNull().default("main"),
  kinds: jsonb("kinds").$type<string[]>().notNull().default(sql`'["skills", "skill-overlays", "workflows"]'::jsonb`),
  username: text("username"),
  token: text("token"),
  ssh_private_key: text("ssh_private_key"),
  ssh_public_key: text("ssh_public_key"),
  ssh_known_hosts: text("ssh_known_hosts"),
  ssh_host_confirmed: boolean("ssh_host_confirmed").notNull().default(false),
  skill_bases: jsonb("skill_bases").$type<Record<string, string | { remote: string; local?: string }>>().notNull().default(sql`'{}'::jsonb`),
  last_published_at: timestamp("last_published_at", { withTimezone: true }),
  last_publish_status: text("last_publish_status"),
  last_publish_detail: text("last_publish_detail"),
  last_publish_link: text("last_publish_link"),
  updated_by_user_id: text("updated_by_user_id").references(() => app_user.id, { onDelete: "set null" }),
  updated_at: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
});

export const spend_limit = pgTable(
  "spend_limit",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    workflow_id: uuid("workflow_id").references(() => workflow_definition.id, {
      onDelete: "cascade",
    }),
    run_cap_micros: bigint("run_cap_micros", { mode: "number" }),
    org_hourly_micros: bigint("org_hourly_micros", { mode: "number" }),
    org_daily_micros: bigint("org_daily_micros", { mode: "number" }),
    workflow_hourly_micros: bigint("workflow_hourly_micros", { mode: "number" }),
    workflow_daily_micros: bigint("workflow_daily_micros", { mode: "number" }),
    warn_percent: integer("warn_percent"),
    metric_refresh: text("metric_refresh"),
    updated_by_user_id: text("updated_by_user_id").references(() => app_user.id, {
      onDelete: "set null",
    }),
    updated_at: ts("updated_at").notNull().defaultNow(),
  },
  (t) => ({
    org_unique: uniqueIndex("spend_limit_org_unique")
      .on(t.org_id)
      .where(sql`workflow_id is null`),
    workflow_unique: uniqueIndex("spend_limit_workflow_unique")
      .on(t.org_id, t.workflow_id)
      .where(sql`workflow_id is not null`),
  }),
);

export const spend_reservation = pgTable(
  "spend_reservation",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    work_run_id: uuid("work_run_id")
      .unique()
      .references(() => work_run.id, { onDelete: "cascade" }),
    workflow_id: uuid("workflow_id").references(() => workflow_definition.id, {
      onDelete: "set null",
    }),
    source: text("source").notNull(),
    reserved_micros: bigint("reserved_micros", { mode: "number" }).notNull(),
    created_at: ts("created_at").notNull().defaultNow(),
    released_at: ts("released_at"),
  },
  (t) => ({
    open_idx: index("spend_reservation_open_idx")
      .on(t.org_id)
      .where(sql`released_at is null`),
  }),
);

export const spend_ledger = pgTable(
  "spend_ledger",
  {
    id: bigint("id", { mode: "number" }).primaryKey().generatedAlwaysAsIdentity(),
    org_id: text("org_id")
      .notNull()
      .references(() => organization.id, { onDelete: "cascade" }),
    reservation_id: uuid("reservation_id").references(() => spend_reservation.id, {
      onDelete: "set null",
    }),
    work_run_id: uuid("work_run_id").references(() => work_run.id, { onDelete: "set null" }),
    workflow_id: uuid("workflow_id").references(() => workflow_definition.id, {
      onDelete: "set null",
    }),
    source: text("source").notNull(),
    provider: text("provider"),
    model: text("model"),
    cost_micros: bigint("cost_micros", { mode: "number" }).notNull(),
    tokens: bigint("tokens", { mode: "number" }),
    priced: text("priced").notNull(),
    cost_source: text("cost_source"),
    pricing_version: text("pricing_version"),
    created_at: ts("created_at").notNull().defaultNow(),
  },
  (t) => ({
    org_created_idx: index("spend_ledger_org_created_idx").on(t.org_id, t.created_at),
  }),
);

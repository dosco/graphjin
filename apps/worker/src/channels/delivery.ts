// Channel delivery + inbound dispatch — the worker glue that turns the V2
// channel contract into a live capability. Outbound: a newly-emitted workflow
// output fans out to every enabled delivery_binding through the built-in
// channel's deliver. Inbound: an IntentEvent routes to the SAME agent entry
// points the web uses — approve/reject an action_request, or start a chat run.
import { and, db, delivery_binding, eq, processing_job } from "@neko/db";
import { audienceReceivesOutput } from "./audience.js";
import { enqueue, QUEUE, type ChannelDeliverPayload } from "@neko/db/jobs";
import { resolveAgentBackend } from "@neko/llm";
import { SpendBudgetExceeded } from "@neko/llm/spend";
import { outputRowToInteractionEvent, type OutputRow } from "@neko/llm/interaction";
import type { InteractionEvent, SurfaceMessage } from "@neko/interaction";
import {
  approveActionRequest,
  getActionRequest,
  rejectActionRequest,
  setWorkflowOutputDeliveryHook,
  type WorkflowOutputDeliveryHook,
} from "@neko/llm/workflows";
import {
  channelThreadId,
  createWorkMessage,
  createWorkRun,
  createWorkThread,
  getOrCreateChannelThread,
  getWorkThread,
  type RunChannel,
} from "@neko/llm/work";
import { builtInChannel } from "@neko/channels";
import {
  deliverOnChannel,
  parseChannelInbound,
  verifyChannelInbound,
} from "./runtime.js";
import {
  beginInboundUpdate,
  inboundUpdateKey,
  markInboundDone,
  recordInboundFailure,
} from "./inbound-store.js";

/** Channel ids are run channels ("telegram", "slack", …). Channel-inbound runs
 *  are never "web", so they get no a2ui rendering. See docs/PER_CHANNEL_RENDERING.md. */
function channelFromPlugin(channelId: string): RunChannel {
  return channelId as RunChannel;
}

// Stable per-conversation key from the channel-native recipient (+ thread),
// so the same DM / channel thread maps to one work_thread across turns.
export function conversationKeyFor(
  recipient: Record<string, unknown>,
  threadRef?: string,
): string {
  const base =
    (typeof recipient.channel === "string" && recipient.channel) ||
    (recipient.chatId != null && String(recipient.chatId)) ||
    (typeof recipient.to === "string" && recipient.to) ||
    "";
  if (!base) return "";
  return threadRef ? `${base}:${threadRef}` : base;
}

type IntentEvent =
  | { kind: "utterance"; threadRef?: string; text: string }
  | { kind: "decision"; decisionRef: string; choice: "approve" | "reject"; reason?: string }
  | { kind: "select"; ref: string; optionId: string }
  | { kind: "invoke"; command: string; args?: Record<string, unknown> };

const MOODS = ["good", "watch", "act"] as const;

// Outbound delivery retries: resilient to Telegram/Slack network blips and
// worker restarts. The job is durable in pg-boss, so it survives a restart.
const DELIVER_OPTS = { retryLimit: 8, retryDelay: 15, retryBackoff: true } as const;

/**
 * Enqueue a durable, retryable outbound delivery instead of sending inline.
 * `idempotencyKey` is a pg-boss singletonKey — concurrent re-enqueues (e.g. a
 * retried run job) collapse to one in-flight delivery.
 */
async function enqueueChannelDelivery(
  orgId: string,
  channelPlugin: string,
  recipient: Record<string, unknown>,
  events: unknown[],
  idempotencyKey: string,
): Promise<void> {
  const payload: ChannelDeliverPayload = { orgId, channelPlugin, recipient, events };
  await enqueue(QUEUE.CHANNEL_DELIVER, payload, {
    ...DELIVER_OPTS,
    singletonKey: idempotencyKey,
  });
}

/**
 * The CHANNEL_DELIVER job body: perform the actual send. Throwing makes
 * pg-boss retry with backoff (network blips) and survives restarts.
 */
export async function runChannelDelivery(payload: ChannelDeliverPayload): Promise<void> {
  const res = await deliverOnChannel(
    payload.orgId,
    payload.channelPlugin,
    payload.recipient,
    payload.events,
  );
  if (!res.delivered) {
    throw new Error(`channel-delivery: ${payload.channelPlugin} reported delivered=false`);
  }
  console.log(
    `[channel-delivery] ${payload.channelPlugin} delivered=true${res.ref ? ` ref=${res.ref}` : ""}`,
  );
}

const onOutput: WorkflowOutputDeliveryHook = async (orgId, output) => {
  const bindings = await db()
    .select()
    .from(delivery_binding)
    .where(
      and(eq(delivery_binding.org_id, orgId), eq(delivery_binding.enabled, true)),
    );
  if (bindings.length === 0) return;
  const mood = MOODS.find((m) => m === output.mood);
  const row: OutputRow = {
    id: output.id,
    title: output.title,
    body: output.body,
    ...(mood ? { mood } : {}),
  };
  const event = outputRowToInteractionEvent(row);
  for (const b of bindings) {
    if (!(await audienceReceivesOutput(orgId, b.audience, output.workflowRunId))) continue;
    await enqueueChannelDelivery(
      orgId,
      b.channel_plugin,
      (b.recipient ?? {}) as Record<string, unknown>,
      [event],
      `output-${output.id}-${b.channel_plugin}`,
    );
  }
};

/** Register the output→channel fan-out hook. Called once at worker startup. */
export function registerChannelOutputDelivery(): void {
  setWorkflowOutputDeliveryHook(onOutput);
}

/**
 * Send a chat run's reply back to the channel that asked. Channel-initiated
 * runs (Telegram, …) have no other return path — the web UI streams its runs
 * over SSE, but a Telegram sender only hears back if we deliver here. Enqueued
 * as a durable job so the reply survives network blips and restarts.
 */
export async function deliverChatReply(
  orgId: string,
  channelPlugin: string,
  recipient: Record<string, unknown>,
  runId: string,
  body: string,
  surfaces?: unknown[],
  vitals?: { label: string; value: string; sub?: string }[],
): Promise<void> {
  const agentSurfaces =
    Array.isArray(surfaces) && surfaces.length > 0 ? (surfaces as SurfaceMessage[]) : undefined;
  // When the agent emitted no a2ui surface (common on channel runs — it answers
  // in prose plus the mandatory vitals fence), synthesize one from the answer +
  // vitals so a rendering channel shows the metric *cards* web shows, not prose.
  const enrichmentSurfaces =
    agentSurfaces ??
    (vitals && vitals.length > 0 ? buildReplySurface(body, vitals) : undefined);
  // A rendering run (web/telegram) puts its answer in the a2ui surface, so the
  // text body can be thin or empty — deliver as long as either is present.
  if (!body.trim() && !enrichmentSurfaces) return;
  // A chat reply is the assistant's message, not a workflow-output card — use
  // `converse` so channels render the full text, not a summarized headline.
  // The a2ui surface rides as enrichment; a channel that can render it (Telegram)
  // shows the rich answer, a thin one falls back to the text.
  const event: InteractionEvent = {
    kind: "converse",
    id: runId,
    role: "assistant",
    text: body,
    ...(enrichmentSurfaces ? { enrichment: { surfaces: enrichmentSurfaces } } : {}),
  };
  await enqueueChannelDelivery(orgId, channelPlugin, recipient, [event], `reply-${runId}`);
}

/**
 * Synthesize a minimal a2ui surface — the answer prose as Markdown plus one
 * MetricCard per vital — so a channel that renders surfaces shows the vitals as
 * cards even when the agent produced no surface of its own. Web is unaffected
 * (it renders from the event stream, not this channel-reply path).
 */
function buildReplySurface(
  body: string,
  vitals: { label: string; value: string; sub?: string }[],
): SurfaceMessage[] {
  const cards = vitals.map((v, i) => ({
    id: `vital-${i}`,
    component: "MetricCard",
    metric: v.value,
    label: v.label,
    ...(v.sub ? { detail: v.sub } : {}),
  }));
  const hasBody = body.trim().length > 0;
  const components: Record<string, unknown>[] = [
    {
      id: "root",
      component: "Answer",
      children: [...(hasBody ? ["answer-body"] : []), ...cards.map((c) => c.id)],
    },
    ...(hasBody ? [{ id: "answer-body", component: "Markdown", text: body }] : []),
    ...cards,
  ];
  return [
    { version: "v1.0", createSurface: { surfaceId: "reply", catalogId: "urn:app:catalog:answer:v1" } },
    { version: "v1.0", updateComponents: { surfaceId: "reply", components } },
  ];
}

/**
 * Auto-bind delivery to a sender on first inbound contact: the operator DMs the
 * bot once and starts receiving outputs there, never hand-writing a binding.
 * Idempotent — only writes when no binding for (org, channel) exists yet.
 */
export async function ensureInboundBinding(
  orgId: string,
  channelPlugin: string,
  recipient: Record<string, unknown>,
): Promise<void> {
  const existing = await db()
    .select({ id: delivery_binding.id })
    .from(delivery_binding)
    .where(
      and(
        eq(delivery_binding.org_id, orgId),
        eq(delivery_binding.channel_plugin, channelPlugin),
      ),
    )
    .limit(1);
  if (existing.length > 0) return;
  await db()
    .insert(delivery_binding)
    .values({ org_id: orgId, channel_plugin: channelPlugin, recipient, enabled: true });
  console.log(
    `[channel-inbound] auto-bound ${channelPlugin} → ${JSON.stringify(recipient)} (first inbound from this sender)`,
  );
}

/**
 * CH2: resolve the org an inbound update belongs to from the sender's
 * workspace scope. Unknown workspace = first contact: auto-bind to the
 * default org (single-tenant behavior preserved); a multi-tenant install
 * remaps channel_workspace rows explicitly.
 */
export async function resolveInboundOrg(
  channelPlugin: string,
  workspaceId: string | undefined,
  defaultOrgId: string,
): Promise<string> {
  if (!workspaceId) return defaultOrgId;
  const { channel_workspace } = await import("@neko/db");
  const [row] = await db()
    .select({ orgId: channel_workspace.org_id })
    .from(channel_workspace)
    .where(
      and(
        eq(channel_workspace.channel_plugin, channelPlugin),
        eq(channel_workspace.workspace_id, workspaceId),
      ),
    )
    .limit(1);
  if (row) return row.orgId;
  await db()
    .insert(channel_workspace)
    .values({
      org_id: defaultOrgId,
      channel_plugin: channelPlugin,
      workspace_id: workspaceId,
    })
    .onConflictDoNothing();
  console.log(
    `[channel-inbound] auto-bound workspace ${channelPlugin}/${workspaceId} → org ${defaultOrgId}`,
  );
  return defaultOrgId;
}

/** Route one inbound IntentEvent to the same agent entry points the web uses. */
export async function dispatchInboundIntent(
  orgId: string,
  intent: IntentEvent,
  channelPlugin: string,
  recipient?: Record<string, unknown>,
  sender?: { id: string; displayName?: string; workspaceId?: string; email?: string },
): Promise<void> {
  // CH3: a linked sender acts as their app_user; unlinked stays
  // anonymous member-grade; blocked identities are dropped outright.
  const { resolveChannelActor } = await import("./identity.js");
  const actor = await resolveChannelActor(orgId, channelPlugin, sender);
  if (actor.blocked) {
    console.warn(
      `[channel-inbound] dropped inbound from blocked identity ${channelPlugin}/${sender?.id}`,
    );
    return;
  }
  if (intent.kind === "decision") {
    const req = await getActionRequest(orgId, intent.decisionRef);
    if (!req) {
      console.warn(
        `[channel-inbound] decision for unknown action_request ${intent.decisionRef}`,
      );
      return;
    }
    const pending = req.status === "pending_approval" || req.status === "draft";
    // Idempotent: only the call that finds the request still pending acts on it,
    // so a duplicate tap / webhook retry / poller overlap can't double-execute.
    if (!pending) {
      console.log(
        `[channel-inbound] action_request ${req.id} already ${req.status}; ignoring duplicate ${intent.choice}`,
      );
      return;
    }
    if (intent.choice === "approve") {
      await approveActionRequest({
        id: req.id,
        orgId,
        approverUserId: actor.userId,
        // K2: the resolved CH3 actor decides the approval grade — an
        // unlinked sender stays member-grade, so admin-gated policies
        // still block approval from an anonymous chat surface.
        approver: { userId: actor.userId, role: actor.role },
      });
      await enqueue(QUEUE.ACTION_EXECUTE, { orgId, actionRequestId: req.id });
      console.log(`[channel-inbound] approved + queued action_request ${req.id}`);
    } else {
      await rejectActionRequest({
        id: req.id,
        orgId,
        approverUserId: actor.userId,
        reason: intent.reason,
        approver: { userId: actor.userId, role: actor.role },
      });
      console.log(`[channel-inbound] rejected action_request ${req.id}`);
    }
    return;
  }
  if (intent.kind === "utterance" || intent.kind === "invoke") {
    const message =
      intent.kind === "utterance"
        ? intent.text
        : `/${intent.command}${intent.args?.text ? ` ${String(intent.args.text)}` : ""}`;
    await startChatRun(
      orgId,
      message,
      channelFromPlugin(channelPlugin),
      channelPlugin,
      recipient,
      intent.kind === "utterance" ? intent.threadRef : undefined,
      sender,
      actor,
    );
    return;
  }
  // Other channels may still emit a raw select; continue the conversation with
  // the chosen option instead of dropping it (Slack sends the label as text).
  await startChatRun(
    orgId,
    intent.optionId,
    channelFromPlugin(channelPlugin),
    channelPlugin,
    recipient,
    undefined,
    sender,
    actor,
  );
}

async function startChatRun(
  orgId: string,
  message: string,
  channel: RunChannel,
  channelPlugin: string,
  recipient: Record<string, unknown> | undefined,
  threadRef?: string,
  sender?: { id: string; displayName?: string; workspaceId?: string; email?: string },
  actor: { userId: string | null; role: "admin" | "member" } = {
    userId: null,
    role: "member",
  },
): Promise<void> {
  const channelLabel = channel.charAt(0).toUpperCase() + channel.slice(1);
  const title = threadRef ? `${channelLabel} ${threadRef}` : channelLabel;
  const conversationKey = recipient ? conversationKeyFor(recipient, threadRef) : "";
  // W3.2: a bare in-thread channel reply only continues a thread the bot already
  // owns — drop it when no matching work_thread exists (DMs and @-mentions still
  // create one).
  if (recipient?.requireThread) {
    const existing = conversationKey
      ? await getWorkThread(orgId, channelThreadId(orgId, channel, conversationKey))
      : null;
    if (!existing) return;
  }
  const thread = conversationKey
    ? await getOrCreateChannelThread({
        orgId,
        channel,
        conversationKey,
        title,
        createdByUserId: actor.userId ?? undefined,
      })
    : await createWorkThread(orgId, title, channel, actor.userId ?? undefined);
  const backend = await resolveAgentBackend(orgId);
  // K1: the CH3-resolved actor is the run's acting principal — a linked
  // sender gets their personal layer and role, unlinked stays anonymous.
  let run: Awaited<ReturnType<typeof createWorkRun>>;
  try {
    run = await createWorkRun(orgId, thread.id, backend.id, actor, { source: "channel" });
  } catch (error) {
    if (!(error instanceof SpendBudgetExceeded) || !recipient) throw error;
    await deliverChatReply(orgId, channelPlugin, recipient, `spend-${thread.id}-${Date.now()}`, error.message);
    return;
  }
  // Record the user turn so reused threads build real conversation history.
  await createWorkMessage({
    orgId,
    threadId: thread.id,
    runId: run.id,
    role: "user",
    content: message,
  });
  const inserted = await db()
    .insert(processing_job)
    .values({
      org_id: orgId,
      kind: "work_run",
      status: "queued",
      trigger: "channel_inbound",
      // CH1: persist who sent it; K1 resolves to an actor, CH3 links it.
      trigger_payload: sender ? { message, sender } : { message },
    })
    .returning({ id: processing_job.id });
  const processingJobId = inserted[0]?.id;
  if (!processingJobId) return;
  await enqueue(QUEUE.WORK_RUN, {
    processingJobId,
    orgId,
    runId: run.id,
    threadId: thread.id,
    message,
    channel,
    channelPlugin,
    recipient,
    ...(sender ? { sender } : {}),
  });
  console.log(
    `[channel-inbound] started chat run ${run.id} for "${message.slice(0, 40)}"`,
  );
}

// A persistently-failing update is retried up to this many times (with poll
// backoff between attempts) before it's dead-lettered and the cursor advances
// past it. The count is persisted, so the budget survives restarts.
const MAX_INBOUND_ATTEMPTS = 30;

/**
 * Parse + dispatch one raw inbound update, exactly once. The update is claimed
 * in the ledger first: a duplicate (restart re-poll, webhook retry) or a
 * dead-lettered update is skipped. A transient failure is recorded and retried;
 * after MAX_INBOUND_ATTEMPTS it's dead-lettered and consumed. Returns false
 * ONLY while a failure is still retrying (caller holds the cursor); true once
 * the update is done, duplicate, or dead-lettered. Shared by poll + webhook.
 */
export async function processInboundUpdate(
  orgId: string,
  pluginName: string,
  raw: unknown,
): Promise<boolean> {
  const key = inboundUpdateKey(raw);
  const begin = await beginInboundUpdate(orgId, pluginName, key);
  if (!begin.proceed) {
    console.log(
      `[channel-inbound] ${pluginName}: skipping ${begin.dead ? "dead-lettered" : "duplicate"} inbound update`,
    );
    return true;
  }
  try {
    const { intents, recipient, sender } = await parseChannelInbound(pluginName, raw);
    // CH2: the sender's workspace decides the org; first contact binds
    // to the caller's default org.
    const resolvedOrgId = await resolveInboundOrg(
      pluginName,
      sender?.workspaceId,
      orgId,
    );
    if (recipient) {
      await ensureInboundBinding(resolvedOrgId, pluginName, recipient);
    }
    for (const intent of intents) {
      await dispatchInboundIntent(
        resolvedOrgId,
        intent as IntentEvent,
        pluginName,
        recipient as Record<string, unknown> | undefined,
        sender,
      );
    }
    await markInboundDone(orgId, pluginName, key);
    return true;
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    const { dead, attempts } = await recordInboundFailure(
      orgId,
      pluginName,
      key,
      MAX_INBOUND_ATTEMPTS,
      raw,
      errMsg,
    );
    if (dead) {
      console.error(
        `[channel-inbound] ${pluginName}: dead-lettered update after ${attempts} attempt(s): ${errMsg}`,
      );
      return true; // give up — advance past the poison update
    }
    console.warn(
      `[channel-inbound] ${pluginName} dispatch error (attempt ${attempts}/${MAX_INBOUND_ATTEMPTS}): ${errMsg}`,
    );
    return false; // hold the cursor; retry next poll with backoff
  }
}

/** Webhook ingest: verify → parse → dispatch, deduped. */
export async function ingestInboundWebhook(
  orgId: string,
  pluginName: string,
  headers: Record<string, string>,
  rawBody: string,
): Promise<{ ok: boolean; dispatched: number }> {
  // Webhooks registered while the channel was a plugin still post to its package name.
  const channelId = builtInChannel(pluginName)?.id;
  if (!channelId) return { ok: false, dispatched: 0 };
  const verified = await verifyChannelInbound(orgId, channelId, headers, rawBody);
  if (!verified) return { ok: false, dispatched: 0 };
  let raw: unknown;
  try {
    raw = JSON.parse(rawBody);
  } catch {
    return { ok: false, dispatched: 0 };
  }
  const ok = await processInboundUpdate(orgId, channelId, raw);
  return { ok, dispatched: ok ? 1 : 0 };
}

import type { ReactNode } from "react";
import Link from "next/link";
import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { graphjinAdminQuery, parseGraphjinJson, type GraphjinAdminResult } from "@/lib/graphjin-admin";
import { AdminDenied } from "../../AdminShell";
import { Badge } from "@/components/ui/badge";
import { Disclosure } from "@/components/ui/disclosure";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { StatCard, StatGrid } from "@/components/ui/stat-card";
import { GraphjinShell, GraphjinUnavailable, JsonBlock, SettingsCard, StatusBadge, firstParam } from "../GraphjinAdmin";
import ActivityActionButton from "./ActivityActionButton";

// Tasks, watches, and annotations are owner-scoped in GraphJin: these reads
// show what the signed-in admin owns, as the GraphJin console did.
const TASKS_QUERY = `query AdminGraphjinTasks {
  gj_task(order_by: { updated_at: desc }, limit: 100) {
    id goal status outcome verify_status verify_attempts last_entry_at updated_at closed_at
  }
}`;

const TASK_ENTRIES_QUERY = `query AdminGraphjinTaskEntries($task_id: String!) {
  gj_task_entry(where: { task_id: { eq: $task_id } }, order_by: { created_at: desc }, limit: 100) {
    id origin body detail_json status trace_id watch_id created_at
  }
}`;

const WATCHES_QUERY = `query AdminGraphjinWatches {
  gj_watch(order_by: { updated_at: desc }, limit: 100) {
    id name task_id description lifecycle status enabled last_fired_at last_error failure_count
  }
  gj_watch_event(where: { seen: { eq: false } }, order_by: { created_at: desc }, limit: 100) {
    id watch_id data_json data_truncated evidence_json delivery_status receipt_json enrichment_json created_at
  }
}`;

const ANNOTATIONS_QUERY = `query AdminGraphjinAnnotations {
  gj_artifacts(where: { kind: { eq: "annotation" } }, order_by: { updated_at: desc }, limit: 100) {
    id target_ref content tier task_id author_ref approved_ref approved_at updated_at
  }
}`;

type Task = {
  id: string;
  goal: string | null;
  status: string | null;
  outcome: string | null;
  verify_status: string | null;
  verify_attempts: number | null;
  last_entry_at: string | null;
  updated_at: string | null;
  closed_at: string | null;
};
type TaskEntry = {
  id: string;
  origin: string | null;
  body: string | null;
  detail_json: unknown;
  status: string | null;
  trace_id: string | null;
  watch_id: string | null;
  created_at: string | null;
};
type Watch = {
  id: string;
  name: string | null;
  task_id: string | null;
  description: string | null;
  lifecycle: string | null;
  status: string | null;
  enabled: boolean | null;
  last_fired_at: string | null;
  last_error: string | null;
  failure_count: number | null;
};
type WatchEvent = {
  id: string;
  watch_id: string;
  data_json: unknown;
  data_truncated: boolean | null;
  evidence_json: unknown;
  delivery_status: string | null;
  receipt_json: unknown;
  enrichment_json: unknown;
  created_at: string | null;
};
type Annotation = {
  id: string;
  target_ref: string | null;
  content: string | null;
  tier: string | null;
  task_id: string | null;
  author_ref: string | null;
  approved_ref: string | null;
  approved_at: string | null;
  updated_at: string | null;
};

export default async function AdminGraphjinActivityPage({
  searchParams,
}: {
  searchParams: Promise<{ task?: string | string[] }>;
}) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const caller = { orgId: await getOrgId(), userId: actor.userId };
  const selectedTask = firstParam((await searchParams).task);
  const [tasks, watches, annotations, entries] = await Promise.all([
    graphjinAdminQuery<{ gj_task: Task[] }>(caller, TASKS_QUERY),
    graphjinAdminQuery<{ gj_watch: Watch[]; gj_watch_event: WatchEvent[] }>(caller, WATCHES_QUERY),
    graphjinAdminQuery<{ gj_artifacts: Annotation[] }>(caller, ANNOTATIONS_QUERY),
    selectedTask
      ? graphjinAdminQuery<{ gj_task_entry: TaskEntry[] }>(caller, TASK_ENTRIES_QUERY, { task_id: selectedTask })
      : null,
  ]);

  const taskRows = tasks.ok ? tasks.data.gj_task ?? [] : [];
  const watchRows = watches.ok ? watches.data.gj_watch ?? [] : [];
  const events = watches.ok ? watches.data.gj_watch_event ?? [] : [];
  const annotationRows = annotations.ok ? annotations.data.gj_artifacts ?? [] : [];
  const drafts = annotationRows.filter((row) => row.tier !== "approved");
  const approved = annotationRows.filter((row) => row.tier === "approved");
  const watchNames = new Map(watchRows.map((watch) => [watch.id, watch.name ?? watch.id]));
  const openTasks = taskRows.filter((task) => !task.closed_at).length;

  return (
    <GraphjinShell
      current="/admin/graphjin/activity"
      title="Activity"
      subtitle="Declared tasks, the watch inbox, and annotations waiting for review."
    >
      <StatGrid label="Activity summary">
        <StatCard label="Open tasks" value={tasks.ok ? openTasks : "—"} detail={tasks.ok ? `${taskRows.length} total` : undefined} />
        <StatCard label="Unseen events" value={watches.ok ? events.length : "—"} tone={events.length ? "warn" : "neutral"} />
        <StatCard label="Watches" value={watches.ok ? watchRows.length : "—"} />
        <StatCard label="Drafts to review" value={annotations.ok ? drafts.length : "—"} tone={drafts.length ? "warn" : "neutral"} />
      </StatGrid>

      {!watches.ok ? (
        <GraphjinUnavailable title="Watch inbox" error={watches.error} />
      ) : (
        <SettingsCard title="Watch inbox" copy={`${events.length} unseen event${events.length === 1 ? "" : "s"}`}>
          {events.length === 0 ? (
            <p className="text-ui-body-sm text-text2">No unseen watch events.</p>
          ) : (
            <ul className="grid gap-2">
              {events.map((event) => (
                <li key={event.id}>
                  <Disclosure
                    title={watchNames.get(event.watch_id) ?? event.watch_id}
                    meta={event.created_at ? <LocalDateTime value={event.created_at} relativeDay /> : undefined}
                  >
                    <div className="mb-3 flex flex-wrap items-center gap-2">
                      <StatusBadge status={event.delivery_status ?? "pending"} />
                      {event.data_truncated ? <Badge variant="watch">truncated</Badge> : null}
                      <ActivityActionButton action="mark_seen" id={event.id} label="Mark seen" success="Event marked seen." />
                    </div>
                    <JsonBlock
                      value={{
                        data: parseGraphjinJson(event.data_json, null),
                        evidence: parseGraphjinJson(event.evidence_json, null),
                        receipt: parseGraphjinJson(event.receipt_json, null),
                        enrichment: parseGraphjinJson(event.enrichment_json, null),
                      }}
                    />
                  </Disclosure>
                </li>
              ))}
            </ul>
          )}
          {watchRows.length > 0 ? (
            <div className="mt-4">
              <h3 className="mb-2 text-ui-body-sm font-semibold text-text">Watches</h3>
              <ul className="divide-y divide-border">
                {watchRows.map((watch) => (
                  <li key={watch.id} className="flex items-start justify-between gap-3 py-2.5">
                    <div className="min-w-0">
                      <div className="truncate font-semibold text-text">{watch.name ?? watch.id}</div>
                      {watch.description ? <p className="text-ui-body-sm text-text2">{watch.description}</p> : null}
                      <div className="text-ui-caption text-text3">
                        {[watch.lifecycle, watch.failure_count ? `${watch.failure_count} failures` : null, watch.last_error]
                          .filter(Boolean)
                          .join(" · ")}
                        {watch.last_fired_at ? (
                          <>
                            {" · last fired "}
                            <LocalDateTime value={watch.last_fired_at} relativeDay />
                          </>
                        ) : null}
                      </div>
                    </div>
                    <StatusBadge status={watch.enabled === false ? "disabled" : watch.status} />
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </SettingsCard>
      )}

      {!tasks.ok ? (
        <GraphjinUnavailable title="Tasks" error={tasks.error} />
      ) : (
        <SettingsCard title="Tasks" copy="Declared work and its immutable trail.">
          {taskRows.length === 0 ? (
            <p className="text-ui-body-sm text-text2">No tasks.</p>
          ) : (
            <ul className="divide-y divide-border">
              {taskRows.map((task) => (
                <li key={task.id} className="grid gap-1.5 py-3 first:pt-0 last:pb-0">
                  <div className="flex items-start justify-between gap-3">
                    <Link
                      href={task.id === selectedTask ? "/admin/graphjin/activity" : `/admin/graphjin/activity?task=${encodeURIComponent(task.id)}`}
                      className="min-w-0 font-semibold text-text hover:text-accent"
                    >
                      {task.goal || task.id}
                    </Link>
                    <StatusBadge status={task.status} />
                  </div>
                  <div className="text-ui-caption text-text3">
                    {[task.outcome, task.verify_status ? `verify ${task.verify_status}` : null].filter(Boolean).join(" · ")}
                    {task.updated_at ? (
                      <>
                        {task.outcome || task.verify_status ? " · " : ""}
                        <LocalDateTime value={task.updated_at} relativeDay />
                      </>
                    ) : null}
                  </div>
                  {task.id === selectedTask && entries ? <TaskTrail entries={entries} /> : null}
                </li>
              ))}
            </ul>
          )}
        </SettingsCard>
      )}

      {!annotations.ok ? (
        <GraphjinUnavailable title="Annotations" error={annotations.error} />
      ) : (
        <SettingsCard
          title="Annotations"
          copy="Approving a note publishes it to agents in this account. GraphJin records who approved it."
        >
          <h3 className="mb-2 text-ui-body-sm font-semibold text-text">Drafts</h3>
          {drafts.length === 0 ? (
            <p className="mb-4 text-ui-body-sm text-text2">No drafts to review.</p>
          ) : (
            <ul className="mb-4 divide-y divide-border">
              {drafts.map((annotation) => (
                <AnnotationRow key={annotation.id} annotation={annotation}>
                  <ActivityActionButton
                    action="approve"
                    id={annotation.id}
                    label="Approve"
                    success="Annotation approved."
                    variant="primary"
                  />
                </AnnotationRow>
              ))}
            </ul>
          )}
          <h3 className="mb-2 text-ui-body-sm font-semibold text-text">Approved</h3>
          {approved.length === 0 ? (
            <p className="text-ui-body-sm text-text2">No approved annotations.</p>
          ) : (
            <ul className="divide-y divide-border">
              {approved.map((annotation) => (
                <AnnotationRow key={annotation.id} annotation={annotation}>
                  <ActivityActionButton action="demote" id={annotation.id} label="Demote" success="Annotation demoted." />
                </AnnotationRow>
              ))}
            </ul>
          )}
        </SettingsCard>
      )}
    </GraphjinShell>
  );
}

function TaskTrail({ entries }: { entries: GraphjinAdminResult<{ gj_task_entry: TaskEntry[] }> }) {
  if (!entries.ok) return <p className="text-ui-body-sm text-danger">{entries.error}</p>;
  const rows = entries.data.gj_task_entry ?? [];
  if (rows.length === 0) return <p className="text-ui-body-sm text-text2">This task has no entries yet.</p>;
  return (
    <ol className="mt-2 grid gap-2 border-l border-border pl-3">
      {rows.map((entry) => {
        const detail = parseGraphjinJson<unknown>(entry.detail_json, null);
        return (
          <li key={entry.id} className="grid gap-1">
            <div className="flex flex-wrap items-center gap-2 text-ui-caption text-text3">
              <Badge variant="outline">{entry.origin ?? "entry"}</Badge>
              {entry.status ? <StatusBadge status={entry.status} /> : null}
              {entry.created_at ? <LocalDateTime value={entry.created_at} relativeDay /> : null}
              {entry.trace_id ? <span className="font-mono">{entry.trace_id}</span> : null}
            </div>
            {entry.body ? <p className="whitespace-pre-wrap text-ui-body-sm text-text2">{entry.body}</p> : null}
            {detail ? (
              <Disclosure title="Detail">
                <JsonBlock value={detail} />
              </Disclosure>
            ) : null}
          </li>
        );
      })}
    </ol>
  );
}

function AnnotationRow({ annotation, children }: { annotation: Annotation; children: ReactNode }) {
  return (
    <li className="flex items-start justify-between gap-3 py-3 first:pt-0 last:pb-0">
      <div className="min-w-0">
        <p className="whitespace-pre-wrap text-ui-body-sm text-text">{annotation.content}</p>
        <div className="mt-1 text-ui-caption text-text3">
          {[
            annotation.target_ref,
            annotation.author_ref ? `by ${annotation.author_ref}` : null,
            annotation.approved_ref ? `approved by ${annotation.approved_ref}` : null,
          ]
            .filter(Boolean)
            .join(" · ")}
        </div>
      </div>
      {children}
    </li>
  );
}

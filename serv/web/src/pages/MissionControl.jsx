import React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import {
  approveAnnotation,
  demoteAnnotation,
  fetchMissionAnnotations,
  fetchMissionTaskEntries,
  fetchMissionTasks,
  fetchMissionWatches,
  isFeatureUnavailable,
  markWatchEventSeen,
} from "../services/mission";
import {
  hasOperatorIdentity,
  operatorIdentityKey,
  useOperatorIdentity,
} from "../services/identity";
import { parseJSON, relativeTime } from "../services/graphql";
import { MetricWatchGrid, isMetricWatch, metricEventSummary } from "./mission/MetricWatches";
import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel, StatusPill } from "../components/ui";
import { useConsole } from "@/components/Layout/console-context";
import { signInState } from "../services/session";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

const validTabs = new Set(["tasks", "watches", "annotations"]);
const refreshInterval = 15000;
const sectionMeta = {
  tasks: {
    title: "Tasks",
  },
  watches: {
    title: "Watches",
  },
  annotations: {
    title: "Artifacts",
  },
};

const MissionControl = ({ section }) => {
  const identity = useOperatorIdentity();
  const identityKey = operatorIdentityKey(identity);
  const { bootstrap, isLoading: bootstrapLoading } = useConsole();
  const session = signInState(bootstrap);
  // A console session is an identity too: the server scopes rows to the
  // signed-in user, so an empty list means "nothing yet", not "unknown user".
  const hasIdentity = hasOperatorIdentity(identity) || session.signedIn;
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedTab = searchParams.get("tab");
  const fixedSection = validTabs.has(section) ? section : "";
  const [tab, setTab] = React.useState(fixedSection || (validTabs.has(requestedTab) ? requestedTab : "tasks"));
  const [selectedTask, setSelectedTask] = React.useState(null);
  const [selectedEvent, setSelectedEvent] = React.useState(null);

  const tasksQuery = useQuery({
    queryKey: ["mission", "tasks", identityKey],
    queryFn: fetchMissionTasks,
    refetchInterval: refreshInterval,
  });
  const watchesQuery = useQuery({
    queryKey: ["mission", "watches", identityKey],
    queryFn: fetchMissionWatches,
    refetchInterval: refreshInterval,
    enabled: !fixedSection || fixedSection === "watches",
  });
  const annotationsQuery = useQuery({
    queryKey: ["mission", "annotations", identityKey],
    queryFn: fetchMissionAnnotations,
    refetchInterval: refreshInterval,
    enabled: !fixedSection || fixedSection === "annotations",
  });

  const tasks = tasksQuery.data || [];
  const watches = watchesQuery.data?.watches || [];
  const events = watchesQuery.data?.events || [];
  const annotations = annotationsQuery.data || [];
  const drafts = annotations.filter((annotation) => annotation.tier === "observed");
  const approved = annotations.filter((annotation) => annotation.tier === "approved");
  const hasSessionData = tasks.length > 0 || watches.length > 0 || events.length > 0 || annotations.length > 0;
  const resolvingSessionIdentity = !hasIdentity && !hasSessionData &&
    (bootstrapLoading || tasksQuery.isLoading || watchesQuery.isLoading || annotationsQuery.isLoading);
  const canShowMission = hasIdentity || hasSessionData;

  const unavailable = {
    tasks: isFeatureUnavailable(tasksQuery.error),
    watches: isFeatureUnavailable(watchesQuery.error),
    annotations: isFeatureUnavailable(annotationsQuery.error),
  };
  const visibleTabs = (fixedSection ? [fixedSection] : ["tasks", "watches", "annotations"]).filter((name) => !unavailable[name]);
  const meta = sectionMeta[fixedSection || tab] || sectionMeta.tasks;

  React.useEffect(() => {
    if (fixedSection) {
      setTab(fixedSection);
    }
  }, [fixedSection]);

  React.useEffect(() => {
    const linkedTaskID = searchParams.get("task_id");
    if (!linkedTaskID || tasks.length === 0) {
      return;
    }
    const task = tasks.find((item) => item.id === linkedTaskID);
    if (task) {
      setSelectedTask(task);
      setTab("tasks");
    }
  }, [searchParams, tasks]);

  React.useEffect(() => {
    if (visibleTabs.length > 0 && !visibleTabs.includes(tab)) {
      setTab(visibleTabs[0]);
    }
  }, [tab, visibleTabs]);

  function changeTab(value) {
    setTab(value);
    const next = new URLSearchParams(searchParams);
    next.set("tab", value);
    next.delete("task_id");
    setSearchParams(next, { replace: true });
  }

  function openTask(task) {
    setSelectedTask(task);
    const next = new URLSearchParams(searchParams);
    if (!fixedSection) {
      next.set("tab", "tasks");
    }
    next.set("task_id", task.id);
    setSearchParams(next, { replace: true });
  }

  function closeTask() {
    setSelectedTask(null);
    const next = new URLSearchParams(searchParams);
    next.delete("task_id");
    setSearchParams(next, { replace: true });
  }

  const openTasks = tasks.filter((task) => task.status === "open" || task.status === "verifying").length;
  return (
    <div className="grid w-full min-w-0 max-w-6xl gap-6">
      <PageHeader
        eyebrow={meta.eyebrow}
        title={meta.title}
        description={meta.description}
        actions={
          canShowMission ? (
            <div className="flex flex-wrap items-center gap-2">
              {tab === "tasks" && <Badge variant={openTasks ? "warning" : "muted"}>{openTasks} open</Badge>}
              {tab === "watches" && <Badge variant={events.length ? "warning" : "muted"}>{events.length} unseen</Badge>}
              {tab === "annotations" && <Badge variant={drafts.length ? "warning" : "muted"}>{drafts.length} drafts</Badge>}
            </div>
          ) : null
        }
      />

      {resolvingSessionIdentity ? (
        <LoadingState />
      ) : !canShowMission ? (
        <NoIdentityState />
      ) : (
        <>
          <FeatureUnavailableHints unavailable={unavailable} />

          {visibleTabs.length === 0 ? (
            <EmptyState
              title="Tasks, watches and artifacts are off"
            />
          ) : (
            <div className="grid gap-4">
              {!fixedSection && <Tabs value={tab} onValueChange={changeTab}>
                <TabsList className="h-auto max-w-full flex-wrap justify-start">
                  {!unavailable.tasks && (
                    <TabsTrigger value="tasks">
                      Tasks
                      {openTasks > 0 && <Badge variant="muted">{openTasks}</Badge>}
                    </TabsTrigger>
                  )}
                  {!unavailable.watches && (
                    <TabsTrigger value="watches">
                      Watch inbox
                      {events.length > 0 && <Badge variant="warning">{events.length}</Badge>}
                    </TabsTrigger>
                  )}
                  {!unavailable.annotations && (
                    <TabsTrigger value="annotations">
                      Annotations
                      {drafts.length > 0 && <Badge variant="warning">{drafts.length}</Badge>}
                    </TabsTrigger>
                  )}
                </TabsList>
              </Tabs>}

              {tab === "tasks" && !unavailable.tasks && (
                <TasksPanel query={tasksQuery} tasks={tasks} onOpenTask={openTask} />
              )}
              {tab === "watches" && !unavailable.watches && (
                <WatchInboxPanel
                  query={watchesQuery}
                  watches={watches}
                  events={events}
                  identityKey={identityKey}
                  onOpenEvent={setSelectedEvent}
                  onOpenTask={openTask}
                  tasks={tasks}
                />
              )}
              {tab === "annotations" && !unavailable.annotations && (
                <AnnotationsPanel
                  query={annotationsQuery}
                  drafts={drafts}
                  approved={approved}
                  identity={identity}
                  identityKey={identityKey}
                  onOpenTask={openTask}
                  tasks={tasks}
                />
              )}
            </div>
          )}
        </>
      )}

      <TaskDrawer task={selectedTask} identityKey={identityKey} onClose={closeTask} />
      <WatchEventDrawer event={selectedEvent} watches={watches} identityKey={identityKey} onClose={() => setSelectedEvent(null)} />
    </div>
  );
};

function NoIdentityState() {
  return (
    <p className="py-6 text-sm text-muted-foreground">Set an identity to see your tasks, watches and artifacts.</p>
  );
}

function FeatureUnavailableHints({ unavailable }) {
  const labels = Object.entries(unavailable)
    .filter(([, hidden]) => hidden)
    .map(([name]) => name);
  if (labels.length === 0) {
    return null;
  }
  return (
    <p className="text-sm text-muted-foreground">
      {labels.map(titleCase).join(", ")} {labels.length === 1 ? "is" : "are"} off on this server.
    </p>
  );
}

function TasksPanel({ query, tasks, onOpenTask }) {
  return (
    <Panel>
      {query.isLoading ? (
        <LoadingState />
      ) : query.error ? (
        <DataErrorState
          error={query.error}
          permissionMessage="Your role can't see tasks."
          queryMessage="Couldn't load tasks."
        />
      ) : tasks.length === 0 ? (
        <EmptyState title="No tasks" message="Ask the agent to create one." />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Goal</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Verification</TableHead>
              <TableHead>Attempts</TableHead>
              <TableHead>Last activity</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tasks.map((task) => (
              <TableRow key={task.id}>
                <TableCell className="min-w-64 max-w-xl">
                  <button type="button" className="w-full text-left" onClick={() => onOpenTask(task)}>
                    <span className="block truncate font-medium text-foreground">{task.goal}</span>
                    <span className="mt-1 block truncate font-mono text-xs text-muted-foreground">{task.id}</span>
                  </button>
                </TableCell>
                <TableCell><StatusPill status={task.status} /></TableCell>
                <TableCell><StatusPill status={task.verify_status || "not requested"} /></TableCell>
                <TableCell>{task.verify_attempts || 0}</TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground" title={formatDate(task.last_entry_at || task.updated_at)}>{timeLabel(task.last_entry_at || task.updated_at)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Panel>
  );
}

function TaskDrawer({ task, identityKey, onClose }) {
  const entriesQuery = useQuery({
    queryKey: ["mission", "task-entries", identityKey, task?.id],
    queryFn: () => fetchMissionTaskEntries(task.id),
    enabled: Boolean(task?.id),
    refetchInterval: task?.id ? refreshInterval : false,
  });
  const entries = entriesQuery.data || [];
  return (
    <Sheet open={Boolean(task)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="grid w-[min(96vw,42rem)] grid-rows-[auto_minmax(0,1fr)] sm:w-[42rem]">
        {task && (
          <>
            <SheetHeader className="pr-8">
              <div className="flex flex-wrap items-center gap-2">
                <StatusPill status={task.status} />
                {task.verify_status && <StatusPill status={task.verify_status} />}
              </div>
              <SheetTitle className="text-lg leading-7">{task.goal}</SheetTitle>
              <SheetDescription className="font-mono text-xs">{task.id}</SheetDescription>
            </SheetHeader>
            <div className="grid content-start gap-5 overflow-y-auto py-5">
              <dl className="grid gap-2 text-sm">
                <DetailRow label="Owner" value={task.owner_ref || "current operator"} />
                <DetailRow label="Updated" value={timeLabel(task.updated_at)} />
                <DetailRow label="Verification attempts" value={task.verify_attempts || 0} />
                {task.outcome && <DetailRow label="Outcome" value={task.outcome} />}
                {task.verify_after && <DetailRow label="Verify after" value={formatDate(task.verify_after)} />}
              </dl>
              {task.verify_json && <JSONBlock title="Verification contract" value={task.verify_json} />}
              <Button asChild variant="secondary" size="sm" className="justify-self-start">
                <Link to={`/user/agent?task_id=${encodeURIComponent(task.id)}`}>Continue in Agent</Link>
              </Button>

              <section className="grid gap-3">
                <h3 className="hud-label">History</h3>
                {entriesQuery.isLoading ? (
                  <LoadingState />
                ) : entriesQuery.error ? (
                  <DataErrorState error={entriesQuery.error} queryMessage="Couldn't load this task's history." />
                ) : entries.length === 0 ? (
                  <EmptyState title="No history yet" />
                ) : (
                  entries.map((entry) => <TaskEntry key={entry.id} entry={entry} />)
                )}
              </section>
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

function TaskEntry({ entry }) {
  const detail = parseJSON(entry.detail_json, entry.detail_json) || {};
  const verification = entry.origin === "verification";
  return (
    <article className="grid gap-3 py-1">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={originVariant(entry.origin)}>{entry.origin || "unknown"}</Badge>
          {entry.status && <StatusPill status={entry.status} />}
        </div>
        <time className="text-xs text-muted-foreground" dateTime={entry.created_at} title={formatDate(entry.created_at)}>{timeLabel(entry.created_at)}</time>
      </div>
      {entry.body && <p className="text-sm leading-6 text-foreground">{entry.body}</p>}
      {verification ? (
        <div className="grid gap-2 sm:grid-cols-2">
          <JSONBlock title="Expected" value={detail?.expect ?? "not recorded"} open />
          <JSONBlock title="Observed" value={detail?.observed ?? detail?.error ?? "not recorded"} open />
        </div>
      ) : detail && Object.keys(detail).length > 0 ? (
        <JSONBlock title="Entry detail" value={detail} />
      ) : null}
      <div className="flex flex-wrap gap-3 font-mono text-[11px] text-muted-foreground">
        {entry.trace_id && <span>trace {entry.trace_id}</span>}
        {entry.watch_id && <span>watch {entry.watch_id}</span>}
      </div>
    </article>
  );
}

// WatchStarterHint turns the empty watch list from a dead end into the one
// example a reader needs: a standing question is an ordinary cursor-paginated
// subscription handed to gj_watch.
function WatchStarterHint() {
  return (
    <div className="grid gap-3">
      <p className="leading-6 text-muted-foreground">
        Ask the agent to watch something, or add one with a mutation:
      </p>
      <pre className="overflow-x-auto rounded-md bg-accent p-3 font-mono text-xs leading-5">
{`mutation {
  gj_watch(insert: {
    name: "urgent_tickets"
    query: "subscription urgent_tickets { support_tickets(where: {severity: {eq: \\"urgent\\"}}, first: 25, after: $cursor) { id status } support_tickets_cursor }"
  }) { id name status }
}`}
      </pre>
      <a
        className="text-sm font-medium underline underline-offset-4"
        href="https://graphjin.com/watch/"
        target="_blank"
        rel="noreferrer"
      >
        How standing questions work
      </a>
    </div>
  );
}

const eventPageSize = 20;

function WatchInboxPanel({ query, watches, events, identityKey, onOpenEvent, onOpenTask, tasks }) {
  const queryClient = useQueryClient();
  const seenMutation = useMutation({
    mutationFn: markWatchEventSeen,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["mission", "watches", identityKey] }),
  });
  const watchesByID = React.useMemo(() => new Map(watches.map((watch) => [watch.id, watch])), [watches]);
  const [showAllEvents, setShowAllEvents] = React.useState(false);
  const visibleEvents = showAllEvents ? events : events.slice(0, eventPageSize);
  const otherWatches = watches.filter((watch) => !isMetricWatch(watch));

  return (
    <div className="grid gap-4">
      <Panel title="Unseen events">
        {query.isLoading ? (
          <LoadingState />
        ) : query.error ? (
          <DataErrorState
            error={query.error}
            permissionMessage="Your role can't see watches."
            queryMessage="Couldn't load watches."
          />
        ) : events.length === 0 ? (
          <EmptyState title="All caught up" />
        ) : (
          <ul className="-mx-3 grid gap-0.5">
            {visibleEvents.map((event) => {
              const watch = watchesByID.get(event.watch_id);
              const summary = metricEventSummary(parseJSON(event.data_json), watch);
              const threadID = parseJSON(event.receipt_json)?.thread_id;
              return (
                <li key={event.id} className="flex items-start justify-between gap-3 rounded-md px-3 py-2.5 transition-colors hover:bg-accent/50">
                  <div className="min-w-0">
                    <button type="button" className="block max-w-full text-left" onClick={() => onOpenEvent(event)}>
                      <span className="block truncate text-sm font-medium">{watch?.name || event.watch_id}</span>
                      {summary && <span className="mt-0.5 block truncate text-xs text-muted-foreground">{summary}</span>}
                    </button>
                    <span className="mt-1 block font-mono text-[0.6875rem] text-muted-foreground" title={formatDate(event.created_at)}>
                      {event.delivery_status || "inbox"} · {timeLabel(event.created_at)}
                    </span>
                    {threadID && (
                      <Link className="mt-1 inline-block text-xs text-brand hover:underline" to={`/user/agent?thread=${encodeURIComponent(threadID)}`}>
                        Read the agent&apos;s answer
                      </Link>
                    )}
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    className="shrink-0"
                    disabled={seenMutation.isPending && seenMutation.variables === event.id}
                    onClick={() => seenMutation.mutate(event.id)}
                  >
                    Mark seen
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
        {events.length > visibleEvents.length && (
          <Button type="button" variant="ghost" size="sm" className="mt-2" onClick={() => setShowAllEvents(true)}>
            Show all {events.length}
          </Button>
        )}
        {seenMutation.error && (
          <div className="mt-4">
            <DataErrorState error={seenMutation.error} queryMessage="Couldn't mark this as seen." />
          </div>
        )}
      </Panel>

      {!query.error && !query.isLoading && (
        <Panel>
          <MetricWatchGrid watches={watches} identityKey={identityKey} />
          <div className="mt-6" />
          {otherWatches.length === 0 && watches.length > 0 ? null : otherWatches.length === 0 ? (
            <EmptyState title="No watches">
              <WatchStarterHint />
            </EmptyState>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Failures</TableHead>
                  <TableHead>Last fired</TableHead>
                  <TableHead>Task</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {otherWatches.map((watch) => {
                  const linkedTask = tasks.find((task) => task.id === watch.task_id);
                  return (
                    <TableRow key={watch.id}>
                      <TableCell>
                        <span className="block font-medium">{watch.name}</span>
                        {watch.last_error && <span className="mt-1 block max-w-md truncate text-xs text-destructive">{watch.last_error}</span>}
                      </TableCell>
                      <TableCell><StatusPill status={watch.enabled ? watch.status : "paused"} /></TableCell>
                      <TableCell>{watch.failure_count || 0}</TableCell>
                      <TableCell className="whitespace-nowrap text-muted-foreground">{watch.last_fired_at ? timeLabel(watch.last_fired_at) : "never"}</TableCell>
                      <TableCell>
                        {linkedTask ? (
                          <Button type="button" variant="link" size="sm" className="h-auto p-0" onClick={() => onOpenTask(linkedTask)}>
                            Open
                          </Button>
                        ) : watch.task_id ? (
                          <span className="font-mono text-xs text-muted-foreground">{watch.task_id}</span>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </Panel>
      )}
    </div>
  );
}

function WatchEventDrawer({ event, watches, identityKey, onClose }) {
  const watch = event ? watches.find((item) => item.id === event.watch_id) : null;
  const queryClient = useQueryClient();
  const seen = useMutation({
    mutationFn: markWatchEventSeen,
    onSuccess: () => {
      onClose();
      return queryClient.invalidateQueries({ queryKey: ["mission", "watches", identityKey] });
    },
  });
  return (
    <Sheet open={Boolean(event)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="grid w-[min(96vw,38rem)] grid-rows-[auto_minmax(0,1fr)] sm:w-[38rem]">
        {event && (
          <>
            <SheetHeader className="pr-8">
              <SheetTitle>{watch?.name || event.watch_id}</SheetTitle>
              <SheetDescription className="font-mono text-xs">
                {event.delivery_status || "inbox"} · {timeLabel(event.created_at)}
              </SheetDescription>
            </SheetHeader>
            <div className="grid content-start gap-5 overflow-y-auto py-5">
              <div className="flex items-center gap-3">
                <Button type="button" size="sm" disabled={seen.isPending} onClick={() => seen.mutate(event.id)}>
                  Mark seen
                </Button>
                {seen.error && <span className="text-xs text-destructive">Couldn&apos;t mark this as seen.</span>}
              </div>
              <dl className="grid gap-2 text-sm">
                <DetailRow label="Fired" value={formatDate(event.created_at)} />
                <DetailRow label="Delivery attempts" value={event.delivery_attempts || 0} />
                {event.snoozed_until && <DetailRow label="Snoozed until" value={formatDate(event.snoozed_until)} />}
                <DetailRow label="Event" value={event.id} />
              </dl>
              <JSONBlock title={event.data_truncated ? "Payload · truncated" : "Payload"} value={parseJSON(event.data_json, event.data_json)} open />
              <JSONBlock title="Delivery" value={parseJSON(event.delivery_json, event.delivery_json)} />
              <JSONBlock title="Receipt" value={parseJSON(event.receipt_json, event.receipt_json)} />
              <JSONBlock title="Evidence" value={parseJSON(event.evidence_json, event.evidence_json)} />
              <JSONBlock title="Enrichment" value={parseJSON(event.enrichment_json, event.enrichment_json)} />
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

function AnnotationsPanel({ query, drafts, approved, identity, identityKey, onOpenTask, tasks }) {
  const queryClient = useQueryClient();
  const [confirmingID, setConfirmingID] = React.useState("");
  const approveMutation = useMutation({
    mutationFn: approveAnnotation,
    onSuccess: () => {
      setConfirmingID("");
      return queryClient.invalidateQueries({ queryKey: ["mission", "annotations", identityKey] });
    },
  });
  const demoteMutation = useMutation({
    mutationFn: demoteAnnotation,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["mission", "annotations", identityKey] }),
  });

  return (
    <div className="grid gap-4">
      <Panel
        title="To review"
      >
        {query.isLoading ? (
          <LoadingState />
        ) : query.error ? (
          <DataErrorState
            error={query.error}
            permissionMessage="Your role can't see artifacts."
            queryMessage="Couldn't load artifacts."
          />
        ) : drafts.length === 0 ? (
          <EmptyState title="Nothing to review" />
        ) : (
          <div className="grid gap-3">
            {drafts.map((annotation) => {
              const task = tasks.find((item) => item.id === annotation.task_id);
              const confirming = confirmingID === annotation.id;
              return (
                <article key={annotation.id} className="grid gap-4 py-2">
                  <AnnotationSummary annotation={annotation} task={task} onOpenTask={onOpenTask} />
                  {confirming ? (
                    <div className="grid gap-3">
                      <p className="text-sm font-semibold">Share this note with your account’s agents?</p>
                      <div className="flex gap-2">
                        <Button type="button" variant="ghost" size="sm" onClick={() => setConfirmingID("")}>
                          Cancel
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          disabled={approveMutation.isPending}
                          onClick={() => approveMutation.mutate(annotation.id)}
                        >
                          Approve
                        </Button>
                      </div>
                    </div>
                  ) : (
                    <Button type="button" size="sm" variant="secondary" className="justify-self-start" onClick={() => setConfirmingID(annotation.id)}>
                      Approve
                    </Button>
                  )}
                </article>
              );
            })}
          </div>
        )}
        {(approveMutation.error || demoteMutation.error) && (
          <div className="mt-4">
            <DataErrorState
              error={approveMutation.error || demoteMutation.error}
              queryMessage="Couldn't update this artifact."
            />
          </div>
        )}
      </Panel>

      {!query.error && !query.isLoading && (
        <Panel title="Approved">
          {approved.length === 0 ? (
            <EmptyState title="Nothing approved yet" />
          ) : (
            <div className="grid gap-3">
              {approved.map((annotation) => {
                const task = tasks.find((item) => item.id === annotation.task_id);
                return (
                  <article key={annotation.id} className="grid gap-4 py-2 md:grid-cols-[minmax(0,1fr)_auto]">
                    <AnnotationSummary annotation={annotation} task={task} onOpenTask={onOpenTask} />
                    {identity.role === "admin" && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        disabled={demoteMutation.isPending}
                        onClick={() => demoteMutation.mutate(annotation.id)}
                      >
                        Demote
                      </Button>
                    )}
                  </article>
                );
              })}
            </div>
          )}
        </Panel>
      )}
    </div>
  );
}

function AnnotationSummary({ annotation, task, onOpenTask }) {
  return (
    <div className="min-w-0">
      <div className="flex flex-wrap items-center gap-2">
        <StatusPill status={annotation.tier} />
        <span className="max-w-full truncate font-mono text-xs text-muted-foreground" title={annotation.target_ref}>
          {annotation.target_ref || "unaddressed"}
        </span>
      </div>
      <p className="mt-3 whitespace-pre-wrap text-sm leading-6 text-foreground">{annotation.content}</p>
      <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
        <span>Author {annotation.author_ref || "unknown"}</span>
        {annotation.approved_ref && <span>Approved by {annotation.approved_ref}</span>}
        {annotation.approved_at && <span>{timeLabel(annotation.approved_at)}</span>}
        {task ? (
          <Button type="button" variant="link" size="sm" className="h-auto p-0 text-xs" onClick={() => onOpenTask(task)}>
            Source task
          </Button>
        ) : annotation.task_id ? (
          <span className="font-mono">task {annotation.task_id}</span>
        ) : null}
      </div>
    </div>
  );
}

// JSONBlock folds raw JSON under its title; only what the reader opens takes room.
function JSONBlock({ title, value, open = false }) {
  if (value == null || value === "") {
    return null;
  }
  return (
    <details className="group min-w-0" open={open}>
      <summary className="hud-label cursor-pointer list-none select-none hover:text-foreground [&::-webkit-details-marker]:hidden">
        <span className="mr-1.5 inline-block transition-transform group-open:rotate-90">›</span>{title}
      </summary>
      <pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-md bg-accent p-3 font-mono text-xs leading-5 text-foreground">
        <code>{formatJSON(value)}</code>
      </pre>
    </details>
  );
}

function DetailRow({ label, value }) {
  return (
    <div className="grid grid-cols-[9rem_minmax(0,1fr)] gap-3">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words font-medium">{String(value)}</dd>
    </div>
  );
}

function originVariant(origin) {
  switch (origin) {
    case "verification":
      return "success";
    case "agent_run":
      return "outline";
    case "watch_created":
      return "warning";
    default:
      return "muted";
  }
}

function formatJSON(value) {
  if (typeof value === "string") {
    const parsed = parseJSON(value, undefined);
    if (parsed === undefined) {
      return value;
    }
    value = parsed;
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function formatDate(value) {
  if (!value) {
    return "not recorded";
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
}

function timeLabel(value) {
  if (!value) {
    return "not recorded";
  }
  return relativeTime(value);
}

function titleCase(value) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export default MissionControl;

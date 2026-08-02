import React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import {
  Bell,
  BookOpenCheck,
  Bot,
  Check,
  CircleAlert,
  Clock3,
  Eye,
  FileCheck2,
  Link2,
  Radar,
  ShieldCheck,
  UserRound,
  X,
} from "lucide-react";

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
import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel, StatusPill } from "../components/ui";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

const validTabs = new Set(["tasks", "watches", "annotations"]);
const refreshInterval = 15000;
const sectionMeta = {
  tasks: {
    eyebrow: "User workspace",
    title: "Tasks",
    description: "Declared goals, verification state, and the durable trail behind agent work.",
  },
  watches: {
    eyebrow: "User workspace",
    title: "Watch inbox",
    description: "Events that need attention across your governed GraphJin watches.",
  },
  annotations: {
    eyebrow: "User workspace",
    title: "Artifacts",
    description: "Observed and approved annotations retained as reviewed graph memory.",
  },
};

const MissionControl = ({ section }) => {
  const identity = useOperatorIdentity();
  const identityKey = operatorIdentityKey(identity);
  const hasIdentity = hasOperatorIdentity(identity);
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
    (tasksQuery.isLoading || watchesQuery.isLoading || annotationsQuery.isLoading);
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
    <div className="grid w-full min-w-0 gap-6">
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
              <Badge variant={identity.role === "admin" ? "warning" : "outline"}>
                <UserRound aria-hidden="true" />
                {identity.userId || "Session identity"}
              </Badge>
            </div>
          ) : null
        }
      />

      {resolvingSessionIdentity ? (
        <LoadingState label="Resolving operator scope" />
      ) : !canShowMission ? (
        <NoIdentityState />
      ) : (
        <>
          <FeatureUnavailableHints unavailable={unavailable} />

          {visibleTabs.length === 0 ? (
            <EmptyState
              title="Mission features are not enabled"
              message="Enable tasks, watches, or artifacts in this GraphJin environment to populate Mission Control."
            />
          ) : (
            <div className="grid gap-4">
              {!fixedSection && <Tabs value={tab} onValueChange={changeTab}>
                <TabsList className="h-auto max-w-full flex-wrap justify-start">
                  {!unavailable.tasks && (
                    <TabsTrigger value="tasks">
                      <FileCheck2 aria-hidden="true" />
                      Tasks
                      {openTasks > 0 && <Badge variant="muted">{openTasks}</Badge>}
                    </TabsTrigger>
                  )}
                  {!unavailable.watches && (
                    <TabsTrigger value="watches">
                      <Bell aria-hidden="true" />
                      Watch inbox
                      {events.length > 0 && <Badge variant="warning">{events.length}</Badge>}
                    </TabsTrigger>
                  )}
                  {!unavailable.annotations && (
                    <TabsTrigger value="annotations">
                      <BookOpenCheck aria-hidden="true" />
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
      <WatchEventDrawer event={selectedEvent} watches={watches} onClose={() => setSelectedEvent(null)} />
    </div>
  );
};

function NoIdentityState() {
  return (
    <div className="grid min-h-80 place-items-center rounded-lg border border-dashed bg-card p-8 text-center shadow-[0_12px_36px_rgba(28,35,48,0.04)]">
      <div className="grid max-w-lg justify-items-center gap-4">
        <div className="flex size-12 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
          <UserRound aria-hidden="true" />
        </div>
        <div>
          <h2 className="text-lg font-semibold">No operator identity</h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            Set a development identity in the header to see owner-scoped tasks, watches, and annotation drafts. Without it GraphJin correctly returns an empty owner scope.
          </p>
        </div>
        <Badge variant="outline">Same-origin auth cookies continue to flow</Badge>
      </div>
    </div>
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
    <Alert>
      <Radar aria-hidden="true" className="size-4" />
      <AlertTitle>Some mission features are not enabled</AlertTitle>
      <AlertDescription>
        {labels.map(titleCase).join(", ")} {labels.length === 1 ? "is" : "are"} hidden because the current GraphJin service does not expose those roots.
      </AlertDescription>
    </Alert>
  );
}

function TasksPanel({ query, tasks, onOpenTask }) {
  return (
    <Panel title="Declared tasks" description="Explicit goals and their durable outcome state. Task changes remain in Agent chat.">
      {query.isLoading ? (
        <LoadingState label="Reading declared tasks" />
      ) : query.error ? (
        <DataErrorState
          error={query.error}
          permissionMessage="gj_task is owner-scoped. Set the matching operator identity or grant this role task access."
          queryMessage="The task surface could not be read from this GraphJin service."
        />
      ) : tasks.length === 0 ? (
        <EmptyState title="No declared tasks" message="Create a task explicitly in Agent chat when work needs a durable goal and trail." />
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
                <TableCell className="whitespace-nowrap text-muted-foreground">{timeLabel(task.last_entry_at || task.updated_at)}</TableCell>
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
              <div className="grid gap-3 rounded-lg border bg-card p-4 text-sm">
                <DetailRow label="Owner" value={task.owner_ref || "current operator"} />
                <DetailRow label="Updated" value={timeLabel(task.updated_at)} />
                <DetailRow label="Verification attempts" value={task.verify_attempts || 0} />
                {task.outcome && <DetailRow label="Outcome" value={task.outcome} />}
                {task.verify_after && <DetailRow label="Verify after" value={formatDate(task.verify_after)} />}
                {task.verify_json && <JSONBlock title="Verification contract" value={task.verify_json} />}
                <Button asChild variant="outline" size="sm" className="justify-self-start">
                  <Link to={`/user/agent?task_id=${encodeURIComponent(task.id)}`}>
                    <Bot aria-hidden="true" />
                    Continue in Agent
                  </Link>
                </Button>
              </div>

              <section className="grid gap-3">
                <div>
                  <h3 className="text-sm font-semibold">Trail</h3>
                  <p className="mt-1 text-xs text-muted-foreground">Newest first · provenance is assigned by GraphJin.</p>
                </div>
                {entriesQuery.isLoading ? (
                  <LoadingState label="Reading task trail" />
                ) : entriesQuery.error ? (
                  <DataErrorState error={entriesQuery.error} queryMessage="The trail for this task could not be loaded." />
                ) : entries.length === 0 ? (
                  <EmptyState title="No trail entries" message="The task exists but has no caller, agent, watch, or verification entries yet." />
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
    <article className={cn("grid gap-3 rounded-lg border bg-card p-4", verification && entry.status === "failed" && "border-red-300")}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={originVariant(entry.origin)}>{entry.origin || "unknown"}</Badge>
          {entry.status && <StatusPill status={entry.status} />}
        </div>
        <time className="text-xs text-muted-foreground" dateTime={entry.created_at}>{timeLabel(entry.created_at)}</time>
      </div>
      {entry.body && <p className="text-sm leading-6 text-foreground">{entry.body}</p>}
      {verification ? (
        <div className="grid gap-2 sm:grid-cols-2">
          <JSONBlock title="Expected" value={detail?.expect ?? "not recorded"} />
          <JSONBlock title="Observed" value={detail?.observed ?? detail?.error ?? "not recorded"} />
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

function WatchInboxPanel({ query, watches, events, identityKey, onOpenEvent, onOpenTask, tasks }) {
  const queryClient = useQueryClient();
  const seenMutation = useMutation({
    mutationFn: markWatchEventSeen,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["mission", "watches", identityKey] }),
  });
  const watchNames = React.useMemo(() => new Map(watches.map((watch) => [watch.id, watch.name])), [watches]);

  return (
    <div className="grid gap-4">
      <Panel title="Unseen watch events" description="The durable inbox for fired standing questions.">
        {query.isLoading ? (
          <LoadingState label="Reading the watch inbox" />
        ) : query.error ? (
          <DataErrorState
            error={query.error}
            permissionMessage="gj_watch and gj_watch_event are owner-scoped. Set the matching operator identity or grant watch access."
            queryMessage="The watch surface could not be read from this GraphJin service."
          />
        ) : events.length === 0 ? (
          <EmptyState title="Inbox reviewed" message="No unseen watch events are waiting for this operator." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Watch</TableHead>
                <TableHead>Delivery</TableHead>
                <TableHead>Fired</TableHead>
                <TableHead className="text-right">Action</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {events.map((event) => (
                <TableRow key={event.id}>
                  <TableCell className="min-w-56">
                    <button type="button" className="w-full text-left" onClick={() => onOpenEvent(event)}>
                      <span className="block truncate font-medium">{watchNames.get(event.watch_id) || event.watch_id}</span>
                      <span className="mt-1 block truncate font-mono text-xs text-muted-foreground">{event.id}</span>
                    </button>
                  </TableCell>
                  <TableCell><StatusPill status={event.delivery_status || "inbox"} /></TableCell>
                  <TableCell className="whitespace-nowrap text-muted-foreground">{timeLabel(event.created_at)}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={seenMutation.isPending && seenMutation.variables === event.id}
                      onClick={() => seenMutation.mutate(event.id)}
                    >
                      <Check aria-hidden="true" />
                      Mark seen
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {seenMutation.error && (
          <div className="mt-4">
            <DataErrorState error={seenMutation.error} queryMessage="GraphJin did not mark this watch event as seen." />
          </div>
        )}
      </Panel>

      {!query.error && !query.isLoading && (
        <Panel title="Watches" description="Definition health and recent runner state.">
          {watches.length === 0 ? (
            <EmptyState title="No watches" message="No standing questions are visible for this operator." />
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
                {watches.map((watch) => {
                  const linkedTask = tasks.find((task) => task.id === watch.task_id);
                  return (
                    <TableRow key={watch.id}>
                      <TableCell>
                        <span className="block font-medium">{watch.name}</span>
                        {watch.last_error && <span className="mt-1 block max-w-md truncate text-xs text-red-700">{watch.last_error}</span>}
                      </TableCell>
                      <TableCell><StatusPill status={watch.enabled ? watch.status : "paused"} /></TableCell>
                      <TableCell>{watch.failure_count || 0}</TableCell>
                      <TableCell className="whitespace-nowrap text-muted-foreground">{watch.last_fired_at ? timeLabel(watch.last_fired_at) : "never"}</TableCell>
                      <TableCell>
                        {linkedTask ? (
                          <Button type="button" variant="link" size="sm" onClick={() => onOpenTask(linkedTask)}>
                            <Link2 aria-hidden="true" />
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

function WatchEventDrawer({ event, watches, onClose }) {
  const watch = event ? watches.find((item) => item.id === event.watch_id) : null;
  return (
    <Sheet open={Boolean(event)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="grid w-[min(96vw,38rem)] grid-rows-[auto_minmax(0,1fr)] sm:w-[38rem]">
        {event && (
          <>
            <SheetHeader className="pr-8">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="warning"><Eye aria-hidden="true" />unseen</Badge>
                <StatusPill status={event.delivery_status || "inbox"} />
              </div>
              <SheetTitle>{watch?.name || event.watch_id}</SheetTitle>
              <SheetDescription className="font-mono text-xs">{event.id}</SheetDescription>
            </SheetHeader>
            <div className="grid content-start gap-4 overflow-y-auto py-5">
              <div className="grid gap-2 rounded-lg border bg-card p-4 text-sm">
                <DetailRow label="Created" value={formatDate(event.created_at)} />
                <DetailRow label="Delivery attempts" value={event.delivery_attempts || 0} />
                <DetailRow label="Data hash" value={event.data_hash || "not recorded"} />
                {event.snoozed_until && <DetailRow label="Snoozed until" value={formatDate(event.snoozed_until)} />}
              </div>
              <JSONBlock title="Event payload" value={parseJSON(event.data_json, event.data_json)} />
              {event.data_truncated && (
                <Alert variant="warning">
                  <CircleAlert aria-hidden="true" className="size-4" />
                  <AlertTitle>Payload truncated</AlertTitle>
                  <AlertDescription>The stored inbox projection contains a bounded snapshot.</AlertDescription>
                </Alert>
              )}
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
        title="Review queue"
        description="Observed notes remain owner-only until a human deliberately publishes them to the account."
      >
        {query.isLoading ? (
          <LoadingState label="Reading annotation drafts" />
        ) : query.error ? (
          <DataErrorState
            error={query.error}
            permissionMessage="Annotation drafts are owner-scoped. Set the matching operator identity or grant artifact access."
            queryMessage="The annotation surface could not be read from this GraphJin service."
          />
        ) : drafts.length === 0 ? (
          <EmptyState title="No drafts to review" message="Observed annotations authored for this operator will appear here." />
        ) : (
          <div className="grid gap-3">
            {drafts.map((annotation) => {
              const task = tasks.find((item) => item.id === annotation.task_id);
              const confirming = confirmingID === annotation.id;
              return (
                <article key={annotation.id} className="grid gap-4 rounded-lg border bg-card p-4">
                  <AnnotationSummary annotation={annotation} task={task} onOpenTask={onOpenTask} />
                  {confirming ? (
                    <div className="grid gap-3 rounded-lg border border-amber-300 bg-amber-50/40 p-4">
                      <div className="flex items-start gap-3">
                        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-amber-800" aria-hidden="true" />
                        <div>
                          <p className="text-sm font-semibold">Publish this note to your account’s agents?</p>
                          <blockquote className="mt-2 border-l-2 border-amber-400 pl-3 text-sm leading-6 text-foreground">
                            “{annotation.content}”
                          </blockquote>
                          <p className="mt-2 text-xs leading-5 text-muted-foreground">
                            Approval is attributed to the current operator. The note remains organizational data, never an instruction or permission grant.
                          </p>
                        </div>
                      </div>
                      <div className="flex justify-end gap-2">
                        <Button type="button" variant="ghost" size="sm" onClick={() => setConfirmingID("")}>
                          <X aria-hidden="true" />Cancel
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          disabled={approveMutation.isPending}
                          onClick={() => approveMutation.mutate(annotation.id)}
                        >
                          <Check aria-hidden="true" />Approve and publish
                        </Button>
                      </div>
                    </div>
                  ) : (
                    <Button type="button" size="sm" className="justify-self-start" onClick={() => setConfirmingID(annotation.id)}>
                      <ShieldCheck aria-hidden="true" />Review approval
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
              queryMessage="GraphJin did not apply this annotation moderation action."
            />
          </div>
        )}
      </Panel>

      {!query.error && !query.isLoading && (
        <Panel title="Approved annotations" description="Account-visible notes with server-stamped attribution.">
          {approved.length === 0 ? (
            <EmptyState title="No approved annotations" message="Reviewed notes will appear here with their approver and timestamp." />
          ) : (
            <div className="grid gap-3">
              {approved.map((annotation) => {
                const task = tasks.find((item) => item.id === annotation.task_id);
                return (
                  <article key={annotation.id} className="grid gap-4 rounded-lg border bg-card p-4 md:grid-cols-[minmax(0,1fr)_auto]">
                    <AnnotationSummary annotation={annotation} task={task} onOpenTask={onOpenTask} />
                    {identity.role === "admin" && (
                      <Button
                        type="button"
                        variant="outline"
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
        <Badge variant="outline" className="max-w-full truncate font-mono" title={annotation.target_ref}>
          {annotation.target_ref || "unaddressed"}
        </Badge>
      </div>
      <p className="mt-3 whitespace-pre-wrap text-sm leading-6 text-foreground">{annotation.content}</p>
      <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
        <span>Author {annotation.author_ref || "unknown"}</span>
        {annotation.approved_ref && <span>Approved by {annotation.approved_ref}</span>}
        {annotation.approved_at && <span>{timeLabel(annotation.approved_at)}</span>}
        {task ? (
          <Button type="button" variant="link" size="sm" className="h-auto p-0 text-xs" onClick={() => onOpenTask(task)}>
            <Link2 aria-hidden="true" />
            Source task
          </Button>
        ) : annotation.task_id ? (
          <span className="font-mono">task {annotation.task_id}</span>
        ) : null}
      </div>
    </div>
  );
}

function JSONBlock({ title, value }) {
  if (value == null || value === "") {
    return null;
  }
  return (
    <div className="min-w-0 rounded-lg border bg-muted/20 p-3">
      <p className="mb-2 text-xs font-semibold uppercase text-muted-foreground">{title}</p>
      <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words font-mono text-xs leading-5 text-foreground">
        <code>{formatJSON(value)}</code>
      </pre>
    </div>
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
  return `${relativeTime(value)} · ${formatDate(value)}`;
}

function titleCase(value) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export default MissionControl;

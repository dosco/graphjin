import {
  graphjinSubscribe,
  type GraphjinSubscriptionHandle,
} from "../graphjin/client";
import {
  listEnabledSubscriptions,
  type SubscriptionRecord,
} from "./store";
import {
  buildSubscriptionQuery,
  parseSourceChangeFilter,
  parseSourceChangeMatch,
  parseWorkflowOutputMatch,
  type SourceChangeMatch,
  type WorkflowOutputMatch,
} from "./subscription-query";

export type SubscriptionMatchEvent =
  | {
      kind: "workflow_output";
      subscription: SubscriptionRecord;
      output: WorkflowOutputMatch;
    }
  | {
      kind: "source_change";
      subscription: SubscriptionRecord;
      match: SourceChangeMatch;
    };

export type SubscriptionTransport = {
  baseUrl: string;
};

export type ResolveTransport = (
  sub: SubscriptionRecord,
) => Promise<SubscriptionTransport>;

export type SubscriptionManagerOptions = {
  resolveTransport: ResolveTransport;
  onMatch: (event: SubscriptionMatchEvent) => void | Promise<void>;
  refreshIntervalMs?: number;
  onError?: (err: Error, sub?: SubscriptionRecord) => void;
};

export type SubscriptionManagerHandle = {
  /** Resolves once the initial set of subscriptions is connected. */
  ready: Promise<void>;
  /** Stop all subscriptions and the refresh loop. */
  stop: () => Promise<void>;
  /** Current subscription ids the manager is tracking (test helper). */
  activeSubscriptionIds: () => string[];
  /** Force a refresh now (test helper). */
  refresh: () => Promise<void>;
};

const DEFAULT_REFRESH_INTERVAL_MS = 60_000;

export function startSubscriptionManager(
  opts: SubscriptionManagerOptions,
): SubscriptionManagerHandle {
  const handles = new Map<string, GraphjinSubscriptionHandle>();
  let stopping = false;
  let refreshTimer: NodeJS.Timeout | null = null;
  let resolveReady: () => void;
  let rejectReady: (err: Error) => void;
  const ready = new Promise<void>((res, rej) => {
    resolveReady = res;
    rejectReady = rej;
  });

  const openOne = async (sub: SubscriptionRecord): Promise<void> => {
    const payload = buildSubscriptionQuery({
      sourceKind: sub.sourceKind,
      filter: sub.filter,
      orgId: sub.orgId,
    });
    if (!payload) {
      console.warn(
        `[subscription-manager] skipping subscription ${sub.id} — source_kind="${sub.sourceKind}" not wired or filter invalid`,
      );
      return;
    }

    let transport: SubscriptionTransport;
    try {
      transport = await opts.resolveTransport(sub);
    } catch (err) {
      opts.onError?.(
        err instanceof Error ? err : new Error(String(err)),
        sub,
      );
      return;
    }

    const handle = graphjinSubscribe<{ data?: unknown } & Record<string, unknown>>({
      baseUrl: transport.baseUrl,
      query: payload.query,
      variables: payload.variables,
      onNext: async (msg) => {
        try {
          if (sub.sourceKind === "workflow_output") {
            const match = parseWorkflowOutputMatch(msg);
            if (!match) return;
            await opts.onMatch({
              kind: "workflow_output",
              subscription: sub,
              output: match,
            });
            return;
          }
          if (sub.sourceKind === "source_change") {
            const filter = parseSourceChangeFilter(sub.filter);
            if (!filter) return;
            const match = parseSourceChangeMatch(msg, filter);
            if (!match) return;
            await opts.onMatch({
              kind: "source_change",
              subscription: sub,
              match,
            });
            return;
          }
        } catch (err) {
          opts.onError?.(
            err instanceof Error ? err : new Error(String(err)),
            sub,
          );
        }
      },
      onError: (err) => {
        opts.onError?.(err, sub);
      },
    });
    // The handle's `ready` promise rejects when the WS connection fails;
    // the same failure already fires onError above, so absorb the rejection
    // here to keep it from surfacing as an unhandled rejection that crashes
    // the worker. Manager-level callers consume errors via opts.onError.
    handle.ready.catch(() => {});
    handles.set(sub.id, handle);
  };

  const closeOne = (id: string) => {
    const handle = handles.get(id);
    if (!handle) return;
    handle.stop();
    handles.delete(id);
  };

  const refresh = async (): Promise<void> => {
    if (stopping) return;
    const rows = await listEnabledSubscriptions();
    const desired = new Set(rows.map((r) => r.id));
    for (const id of handles.keys()) {
      if (!desired.has(id)) closeOne(id);
    }
    for (const row of rows) {
      if (handles.has(row.id)) continue;
      await openOne(row);
    }
  };

  const run = async () => {
    try {
      await refresh();
      resolveReady();
    } catch (err) {
      rejectReady(err instanceof Error ? err : new Error(String(err)));
      return;
    }
    const interval = opts.refreshIntervalMs ?? DEFAULT_REFRESH_INTERVAL_MS;
    refreshTimer = setInterval(() => {
      void refresh().catch((err) => {
        opts.onError?.(err instanceof Error ? err : new Error(String(err)));
      });
    }, interval);
    refreshTimer.unref();
  };

  void run();

  return {
    ready,
    activeSubscriptionIds: () => Array.from(handles.keys()),
    refresh,
    stop: async () => {
      stopping = true;
      if (refreshTimer) clearInterval(refreshTimer);
      for (const id of Array.from(handles.keys())) closeOne(id);
    },
  };
}

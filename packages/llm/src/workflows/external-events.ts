import {
  handleExternalEventMatch,
  type ExternalEventMatch,
  type MatchHandlerDecision,
} from "./match-handler";
import {
  listEnabledSubscriptions,
  type SubscriptionRecord,
} from "./store";

export type DispatchExternalEventInput = {
  orgId: string;
  event: ExternalEventMatch;
};

export type DispatchExternalEventResult = {
  matched: number;
  enqueued: number;
  decisions: Array<{ subscriptionId: string } & MatchHandlerDecision>;
};

/**
 * An external_event subscription's filter selects which events fire it:
 *
 *   { "name": "invoice.paid", "source": "@open-neko/plugin-stripe" }
 *
 * Both keys are optional — an absent key matches everything, so `{}`
 * subscribes to every external event in the org.
 */
export function externalEventFilterMatches(
  filter: Record<string, unknown>,
  event: ExternalEventMatch,
): boolean {
  if (typeof filter.name === "string" && filter.name !== event.name) {
    return false;
  }
  if (
    typeof filter.source === "string" &&
    filter.source !== (event.source ?? "")
  ) {
    return false;
  }
  return true;
}

/**
 * Fan an external event out to every enabled external_event subscription
 * in the org whose filter matches. The OL4 watchers and the worker's
 * /admin/events/external ingress both land here.
 */
export async function dispatchExternalEvent(
  input: DispatchExternalEventInput,
  deps: {
    listSubscriptions?: typeof listEnabledSubscriptions;
    handleMatch?: typeof handleExternalEventMatch;
  } = {},
): Promise<DispatchExternalEventResult> {
  const list = deps.listSubscriptions ?? listEnabledSubscriptions;
  const handle = deps.handleMatch ?? handleExternalEventMatch;

  const subs: SubscriptionRecord[] = (
    await list({ sourceKind: "external_event" })
  ).filter(
    (s) =>
      s.orgId === input.orgId &&
      externalEventFilterMatches(s.filter, input.event),
  );

  const decisions: DispatchExternalEventResult["decisions"] = [];
  for (const subscription of subs) {
    const decision = await handle({ subscription, event: input.event });
    decisions.push({ subscriptionId: subscription.id, ...decision });
  }
  return {
    matched: subs.length,
    enqueued: decisions.filter((d) => d.action === "enqueued").length,
    decisions,
  };
}

import type {
  CapabilityProfile,
  InteractionEvent,
  IntentEvent,
  Projection,
} from "@neko/interaction";

export type Direction = "inbound" | "outbound";

/** Opaque to the worker; minted at connect/config time. */
export type ChannelRecipient = { kind: string; [key: string]: unknown };

/** CH1: the human behind an inbound update, scoped to its workspace when the substrate has one. */
export interface ChannelSender {
  id: string;
  displayName?: string;
  workspaceId?: string;
}

/** One raw inbound update, normalized: the intents plus where to reply and who sent it. */
export interface ParsedInbound {
  intents: IntentEvent[];
  recipient?: ChannelRecipient;
  sender?: ChannelSender;
}

/**
 * How inbound reaches the worker when there is no public webhook URL:
 * `poll` loops `pollInbound`, `socket` holds a worker-owned connection,
 * `webhook` needs OPENNEKO_PUBLIC_URL.
 */
export type ChannelIngress = "webhook" | "poll" | "socket";

/** Credentials and settings of one channel, read from the OpenNeko secrets store. */
export type ChannelEnv = Readonly<Record<string, string | undefined>>;

/** A value a channel reads from its secrets-store section. */
export interface ChannelSetting {
  key: string;
  description: string;
  required: boolean;
  secret: boolean;
  /** The host mints the value (an HMAC key nobody types). */
  autogenerate?: boolean;
}

export interface DeliverResult {
  delivered: boolean;
  ref?: string;
  error?: string;
}

export interface ChannelDeliveryOutcome {
  native: unknown;
  result: DeliverResult;
}

/**
 * A membrane, with its Native payload type erased. Every channel builds one
 * in-process via `defineChannel`; the worker never handles a typed
 * intermediate payload.
 */
export interface ChannelAdapter {
  pluginName: string;
  providerLabel: string;
  profile: CapabilityProfile;
  directions: Direction[];
  /** Built-in channels deliver to every audience without an explicit binding. */
  builtIn?: boolean;
  deliver: (recipient: ChannelRecipient, events: InteractionEvent[]) => Promise<ChannelDeliveryOutcome>;
  parseInbound?: (raw: unknown) => IntentEvent[];
}

export interface ChannelProviderInfo {
  pluginName: string;
  providerLabel: string;
  profile: CapabilityProfile;
  directions: Direction[];
  builtIn: boolean;
}

/** Authoring shape for an in-process channel: a pure projection + a send. */
export interface ChannelAdapterSpec<Native> {
  pluginName: string;
  providerLabel: string;
  profile: CapabilityProfile;
  directions: Direction[];
  builtIn?: boolean;
  project: Projection<Native>;
  send: (recipient: ChannelRecipient, native: Native) => Promise<DeliverResult>;
  parseInbound?: (raw: unknown) => IntentEvent[];
}

/** Closes project+send over the substrate's Native type, yielding an erased adapter. */
export const defineChannel = <Native>(spec: ChannelAdapterSpec<Native>): ChannelAdapter => ({
  pluginName: spec.pluginName,
  providerLabel: spec.providerLabel,
  profile: spec.profile,
  directions: spec.directions,
  builtIn: spec.builtIn,
  parseInbound: spec.parseInbound,
  deliver: async (recipient, events) => {
    const native = spec.project(events, spec.profile);
    const result = await spec.send(recipient, native);
    return { native, result };
  },
});

/**
 * The full substrate contract of a built-in channel. The worker drives
 * delivery and every inbound transport through it in-process; credentials
 * arrive as `env` on each call, so nothing here reads process state.
 */
export interface ChannelSubstrate {
  id: string;
  providerLabel: string;
  profile: CapabilityProfile;
  directions: Direction[];
  ingress: ChannelIngress;
  settings: ChannelSetting[];
  deliver(input: {
    recipient: ChannelRecipient;
    events: InteractionEvent[];
    profile?: CapabilityProfile;
    env: ChannelEnv;
  }): Promise<DeliverResult>;
  parseInbound(raw: unknown, env: ChannelEnv): ParsedInbound;
  /** Verifies a webhook request; absent means the channel takes no webhooks. */
  verifyInbound?(headers: Record<string, string>, body: string, env: ChannelEnv): boolean;
  /** Pulls the next batch of raw updates for hosts without a public webhook URL. */
  pollInbound?(cursor: string | undefined, env: ChannelEnv): Promise<{ updates: unknown[]; cursor?: string }>;
}

/** Wraps a substrate as a registry adapter, reading credentials on every delivery. */
export const substrateAdapter = (substrate: ChannelSubstrate, env: () => ChannelEnv): ChannelAdapter => ({
  pluginName: substrate.id,
  providerLabel: substrate.providerLabel,
  profile: substrate.profile,
  directions: substrate.directions,
  parseInbound: (raw) => substrate.parseInbound(raw, env()).intents,
  deliver: async (recipient, events) => {
    const result = await substrate.deliver({ recipient, events, env: env() });
    return { native: null, result };
  },
});

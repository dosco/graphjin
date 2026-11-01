import type { IntentEvent, InteractionEvent } from "@neko/interaction";
import type {
  ChannelAdapter,
  ChannelProviderInfo,
  ChannelRecipient,
  DeliverResult,
} from "./channel-adapter";

/** audience → channel routing. The web binding is implicit (see `builtIn`). */
export interface DeliveryBinding {
  audience: string;
  channelPlugin: string;
  recipient: ChannelRecipient;
  enabled?: boolean;
  filter?: (event: InteractionEvent) => boolean;
}

export interface ChannelDelivery {
  channelPlugin: string;
  providerLabel: string;
  recipient: ChannelRecipient;
  native: unknown;
  result: DeliverResult;
}

export interface DeliveryReport {
  audience: string;
  deliveries: ChannelDelivery[];
}

interface ResolvedTarget {
  adapter: ChannelAdapter;
  recipient: ChannelRecipient;
  filter?: (event: InteractionEvent) => boolean;
}

export class ChannelRegistry {
  private readonly adapters = new Map<string, ChannelAdapter>();
  private readonly bindings: DeliveryBinding[] = [];

  register(adapter: ChannelAdapter): void {
    this.adapters.set(adapter.pluginName, adapter);
  }

  bind(binding: DeliveryBinding): void {
    this.bindings.push(binding);
  }

  getChannelProviders(): ChannelProviderInfo[] {
    return [...this.adapters.values()].map((a) => ({
      pluginName: a.pluginName,
      providerLabel: a.providerLabel,
      profile: a.profile,
      directions: a.directions,
      builtIn: a.builtIn ?? false,
    }));
  }

  private targetsFor(audience: string): ResolvedTarget[] {
    const targets: ResolvedTarget[] = [];
    for (const adapter of this.adapters.values()) {
      if (adapter.builtIn) targets.push({ adapter, recipient: { kind: "builtin" } });
    }
    for (const binding of this.bindings) {
      if (binding.enabled === false) continue;
      if (binding.audience !== audience) continue;
      const adapter = this.adapters.get(binding.channelPlugin);
      if (!adapter) continue;
      targets.push({ adapter, recipient: binding.recipient, filter: binding.filter });
    }
    return targets;
  }

  async deliver(audience: string, events: InteractionEvent[]): Promise<DeliveryReport> {
    const deliveries: ChannelDelivery[] = [];
    for (const { adapter, recipient, filter } of this.targetsFor(audience)) {
      if (!adapter.directions.includes("outbound")) continue;
      const scoped = filter ? events.filter(filter) : events;
      if (scoped.length === 0) continue;
      const { native, result } = await adapter.deliver(recipient, scoped);
      deliveries.push({
        channelPlugin: adapter.pluginName,
        providerLabel: adapter.providerLabel,
        recipient,
        native,
        result,
      });
    }
    return { audience, deliveries };
  }

  parseInbound(channelPlugin: string, raw: unknown): IntentEvent[] {
    const adapter = this.adapters.get(channelPlugin);
    if (!adapter?.parseInbound) return [];
    return adapter.parseInbound(raw);
  }
}

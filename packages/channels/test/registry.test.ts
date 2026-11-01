import { describe, expect, it } from "vitest";
import type { InteractionEvent } from "@neko/interaction";
import {
  ChannelRegistry,
  createSlackChannel,
  createVoiceChannel,
  createWebChannel,
  type VoiceProjectionResult,
} from "../src/index.js";

const inform: InteractionEvent = {
  kind: "inform",
  id: "o1",
  mood: "good",
  title: "Q3 revenue landed",
  body: "Revenue hit $4.7M.",
  metric: { label: "Revenue MTD", value: "$4.7M" },
};

const converse: InteractionEvent = { kind: "converse", id: "c1", role: "assistant", text: "On it." };

const build = (): ChannelRegistry => {
  const registry = new ChannelRegistry();
  registry.register(createWebChannel());
  registry.register(createSlackChannel());
  registry.register(createVoiceChannel());
  return registry;
};

describe("ChannelRegistry routing", () => {
  it("lists every registered provider and marks web built-in", () => {
    const providers = build().getChannelProviders();
    expect(providers).toHaveLength(3);
    expect(providers.find((p) => p.pluginName === "web")?.builtIn).toBe(true);
    expect(providers.find((p) => p.pluginName === "slack")?.builtIn).toBe(false);
  });

  it("fans one inform out to web (implicit) + every bound channel", async () => {
    const registry = build();
    registry.bind({ audience: "coo", channelPlugin: "slack", recipient: { kind: "slack", channel: "#exec" } });

    const report = await registry.deliver("coo", [inform]);
    const reached = report.deliveries.map((d) => d.channelPlugin).sort();
    expect(reached).toEqual(["slack", "web"]);
    expect(report.deliveries.every((d) => d.result.delivered)).toBe(true);
  });

  it("delivers only to the implicit web channel for an unbound audience", async () => {
    const report = await build().deliver("intern", [inform]);
    expect(report.deliveries.map((d) => d.channelPlugin)).toEqual(["web"]);
  });

  it("applies a binding filter before projecting", async () => {
    const registry = build();
    registry.bind({
      audience: "ops",
      channelPlugin: "voice",
      recipient: { kind: "voice", to: "+15551111" },
      filter: (event) => event.kind === "inform",
    });
    const report = await registry.deliver("ops", [converse, inform]);
    const voice = report.deliveries.find((d) => d.channelPlugin === "voice");
    const native = voice?.native as VoiceProjectionResult;
    expect(native.ssml).toContain("Q3 revenue landed");
    expect(native.ssml).not.toContain("On it.");
  });
});

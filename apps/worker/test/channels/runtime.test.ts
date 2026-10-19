import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

// Built-in channel runtime: which channels installed packs turn on, where their
// credentials live, and how the plugin-era settings move over. The secrets
// store is real (in a temp config dir); the pack_install query is stubbed.
const h = vi.hoisted(() => ({ rows: [] as Array<{ config: Record<string, unknown> }> }));
vi.mock("@neko/db", () => ({
  and: (...args: unknown[]) => args,
  eq: (...args: unknown[]) => args,
  pack_install: { config: "config", org_id: "org_id", status: "status" },
  db: () => ({
    select: () => ({ from: () => ({ where: async () => h.rows }) }),
  }),
}));

import { readSecretsStore, writeSecretsStore } from "@neko/secrets";
import {
  channelEnv,
  channelSettings,
  deliverOnChannel,
  enabledChannelIds,
  getChannelProviders,
  migrateLegacyChannelSecrets,
  prepareChannelsForPack,
  setChannelSettings,
  verifyChannelInbound,
} from "../../src/channels/runtime.js";

let root: string;

beforeAll(async () => {
  root = await mkdtemp(join(tmpdir(), "channel-runtime-"));
  vi.stubEnv("XDG_CONFIG_HOME", root);
});

afterAll(async () => {
  vi.unstubAllEnvs();
  await rm(root, { recursive: true, force: true });
});

beforeEach(async () => {
  h.rows = [];
  await writeSecretsStore({});
});

describe("enabled channels", () => {
  it("turns on the union of channels listed by installed packs", async () => {
    h.rows = [
      { config: { _channels: ["telegram"] } },
      { config: { _channels: ["slack", "telegram", "discord"] } },
      { config: {} },
    ];
    expect(await enabledChannelIds("org-1")).toEqual(["slack", "telegram"]);
    expect((await getChannelProviders("org-1")).map((p) => [p.id, p.ingress])).toEqual([
      ["slack", "socket"],
      ["telegram", "webhook"],
    ]);
  });

  it("refuses delivery and webhooks for a channel no pack turned on", async () => {
    await expect(deliverOnChannel("org-1", "telegram", { chatId: 1 }, [])).rejects.toThrow(/not turned on/);
    expect(await verifyChannelInbound("org-1", "telegram", {}, "{}")).toBe(false);
  });
});

describe("channel settings", () => {
  it("writes declared settings and reports secrets only as set", async () => {
    h.rows = [{ config: { _channels: ["telegram"] } }];
    await setChannelSettings("telegram", { TELEGRAM_BOT_TOKEN: "123:abc" });
    expect(await channelEnv("telegram")).toEqual({ TELEGRAM_BOT_TOKEN: "123:abc" });
    const telegram = (await channelSettings("org-1")).find((c) => c.id === "telegram")!;
    expect(telegram.enabled).toBe(true);
    expect(telegram.missing).toEqual([]);
    expect(telegram.fields.find((f) => f.key === "TELEGRAM_BOT_TOKEN")).toMatchObject({ set: true });
    expect(JSON.stringify(telegram)).not.toContain("123:abc");
  });

  it("refuses undeclared keys and clearing a required one", async () => {
    await expect(setChannelSettings("slack", { NOPE: "x" })).rejects.toThrow(/does not declare/);
    await expect(setChannelSettings("slack", { SLACK_BOT_TOKEN: null })).rejects.toThrow(/required/);
  });

  it("mints the HUD secret and scopes the HUD channel to the installing pack", async () => {
    await prepareChannelsForPack("fujairah-live", ["hud"]);
    const env = await channelEnv("hud");
    expect(env.HUD_CHANNEL_SECRET).toMatch(/^[A-Za-z0-9_-]{64}$/);
    expect(env.HUD_CHANNEL_WORKSPACE).toBe("fujairah-live");
    const secret = env.HUD_CHANNEL_SECRET;
    await prepareChannelsForPack("fujairah-live", ["hud"]);
    expect((await channelEnv("hud")).HUD_CHANNEL_SECRET).toBe(secret);
  });
});

describe("migrateLegacyChannelSecrets", () => {
  it("moves plugin-era sections to channel sections without overwriting newer values", async () => {
    await writeSecretsStore({
      "@open-neko/plugin-slack": { SLACK_BOT_TOKEN: "xoxb-old", SLACK_APP_TOKEN: "xapp-old" },
      "@open-neko/channel-telegram": { TELEGRAM_BOT_TOKEN: "tg-old" },
      "channel.telegram": { TELEGRAM_BOT_TOKEN: "tg-new" },
      "pack.magento": { KEY: "keep" },
    });
    expect(await migrateLegacyChannelSecrets()).toEqual(["@open-neko/plugin-slack", "@open-neko/channel-telegram"]);
    const store = await readSecretsStore();
    expect(store["channel.slack"]).toEqual({ SLACK_BOT_TOKEN: "xoxb-old", SLACK_APP_TOKEN: "xapp-old" });
    expect(store["channel.telegram"]).toEqual({ TELEGRAM_BOT_TOKEN: "tg-new" });
    expect(store["@open-neko/plugin-slack"]).toBeUndefined();
    expect(store["pack.magento"]).toEqual({ KEY: "keep" });
    expect(await migrateLegacyChannelSecrets()).toEqual([]);
  });
});

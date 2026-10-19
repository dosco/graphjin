// Built-in channel runtime. Slack, Telegram and the HUD channel ship in
// @neko/channels and run in-process; a channel is on for an org while an
// installed pack lists it under `channels:`. Credentials live in the secrets
// store under `channel.<id>`, the same store packs use for their secrets.
import { randomBytes } from "node:crypto";
import { and, db, eq, pack_install } from "@neko/db";
import {
  BUILT_IN_CHANNELS,
  BUILT_IN_CHANNEL_IDS,
  LEGACY_CHANNEL_NAMES,
  builtInChannel,
  channelSecretSection,
  isBuiltInChannelId,
  type BuiltInChannelId,
  type ChannelEnv,
  type ChannelRecipient,
  type ChannelSubstrate,
  type DeliverResult,
  type ParsedInbound,
} from "@neko/channels";
import type { InteractionEvent } from "@neko/interaction";
import {
  readSecretsStore,
  writeSecretsStore,
  type SecretsStore,
} from "@neko/secrets";

export interface ChannelProviderInfo {
  id: BuiltInChannelId;
  providerLabel: string;
  directions: string[];
  ingress: string;
}

export interface ChannelSettingsView extends ChannelProviderInfo {
  enabled: boolean;
  fields: Array<{ key: string; required: boolean; secret: boolean; description: string; set: boolean; value?: string }>;
  missing: string[];
}

export class ChannelSettingsError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ChannelSettingsError";
  }
}

function requireChannel(id: string): ChannelSubstrate {
  const channel = builtInChannel(id);
  if (!channel) throw new ChannelSettingsError(`unknown channel ${id}`);
  return channel;
}

const providerInfo = (channel: ChannelSubstrate): ChannelProviderInfo => ({
  id: channel.id as BuiltInChannelId,
  providerLabel: channel.providerLabel,
  directions: [...channel.directions],
  ingress: channel.ingress,
});

/** Channels turned on by the org's installed packs, in declaration order. */
export async function enabledChannelIds(orgId: string): Promise<BuiltInChannelId[]> {
  const rows = await db()
    .select({ config: pack_install.config })
    .from(pack_install)
    .where(and(eq(pack_install.org_id, orgId), eq(pack_install.status, "installed")));
  const enabled = new Set<BuiltInChannelId>();
  for (const row of rows) {
    const channels = (row.config as { _channels?: unknown })._channels;
    if (!Array.isArray(channels)) continue;
    for (const id of channels) {
      if (typeof id === "string" && isBuiltInChannelId(id)) enabled.add(id);
    }
  }
  return BUILT_IN_CHANNEL_IDS.filter((id) => enabled.has(id));
}

export async function getChannelProviders(orgId: string): Promise<ChannelProviderInfo[]> {
  return (await enabledChannelIds(orgId)).map((id) => providerInfo(BUILT_IN_CHANNELS[id]));
}

/** A channel's credentials and settings from the secrets store. */
export async function channelEnv(id: string, store?: SecretsStore): Promise<ChannelEnv> {
  const channel = requireChannel(id);
  const secrets = store ?? (await readSecretsStore());
  return { ...(secrets[channelSecretSection(channel.id as BuiltInChannelId)] ?? {}) };
}

async function requireEnabled(orgId: string, id: string): Promise<ChannelSubstrate> {
  const channel = requireChannel(id);
  if (!(await enabledChannelIds(orgId)).includes(channel.id as BuiltInChannelId)) {
    throw new Error(`channel ${channel.id} is not turned on — install a pack that lists it under channels`);
  }
  return channel;
}

export async function deliverOnChannel(
  orgId: string,
  id: string,
  recipient: Record<string, unknown>,
  events: unknown[],
): Promise<DeliverResult> {
  const channel = await requireEnabled(orgId, id);
  return channel.deliver({
    recipient: recipient as ChannelRecipient,
    events: events as InteractionEvent[],
    env: await channelEnv(channel.id),
  });
}

export async function parseChannelInbound(id: string, raw: unknown): Promise<ParsedInbound> {
  const channel = requireChannel(id);
  return channel.parseInbound(raw, await channelEnv(channel.id));
}

export async function verifyChannelInbound(
  orgId: string,
  id: string,
  headers: Record<string, string>,
  body: string,
): Promise<boolean> {
  const channel = builtInChannel(id);
  if (!channel?.verifyInbound) return false;
  if (!(await enabledChannelIds(orgId)).includes(channel.id as BuiltInChannelId)) return false;
  return channel.verifyInbound(headers, body, await channelEnv(channel.id));
}

export async function pollChannelInbound(
  id: string,
  cursor?: string,
): Promise<{ updates: unknown[]; cursor?: string }> {
  const channel = requireChannel(id);
  if (!channel.pollInbound) throw new Error(`channel ${channel.id} does not implement poll_inbound`);
  return channel.pollInbound(cursor, await channelEnv(channel.id));
}

/** Declared settings of every built-in channel; secrets report only whether they are set. */
export async function channelSettings(orgId: string): Promise<ChannelSettingsView[]> {
  const [enabled, store] = await Promise.all([enabledChannelIds(orgId), readSecretsStore()]);
  return BUILT_IN_CHANNEL_IDS.map((id) => {
    const channel = BUILT_IN_CHANNELS[id];
    const env = store[channelSecretSection(id)] ?? {};
    const fields = channel.settings
      .filter((setting) => !setting.autogenerate)
      .map((setting) => {
        const value = env[setting.key];
        const set = typeof value === "string" && value.length > 0;
        return {
          key: setting.key,
          required: setting.required,
          secret: setting.secret,
          description: setting.description,
          set,
          ...(set && !setting.secret ? { value } : {}),
        };
      });
    return {
      ...providerInfo(channel),
      enabled: enabled.includes(id),
      fields,
      missing: fields.filter((f) => f.required && !f.set).map((f) => f.key),
    };
  });
}

/** Writes or clears declared settings of one channel. Refuses undeclared keys and clearing a required one. */
export async function setChannelSettings(id: string, values: Record<string, string | null>): Promise<void> {
  const channel = requireChannel(id);
  const declared = new Map(channel.settings.filter((s) => !s.autogenerate).map((s) => [s.key, s]));
  for (const [key, value] of Object.entries(values)) {
    const setting = declared.get(key);
    if (!setting) throw new ChannelSettingsError(`${channel.id} does not declare ${key}`);
    if (value === null && setting.required) throw new ChannelSettingsError(`${key} is required and cannot be cleared`);
  }
  const store = await readSecretsStore();
  const section = channelSecretSection(channel.id as BuiltInChannelId);
  const next = { ...(store[section] ?? {}) };
  for (const [key, value] of Object.entries(values)) {
    if (value === null) delete next[key];
    else next[key] = value;
  }
  await writeSecretsStore({ ...store, [section]: next });
}

/**
 * Called when a pack that lists channels is installed: mint any host-generated
 * secret the channel still lacks, and scope the HUD channel to that pack.
 */
export async function prepareChannelsForPack(packId: string, ids: readonly string[]): Promise<void> {
  const store = await readSecretsStore();
  let changed = false;
  const next: SecretsStore = { ...store };
  for (const id of ids) {
    if (!isBuiltInChannelId(id)) continue;
    const section = channelSecretSection(id);
    const env = { ...(next[section] ?? {}) };
    for (const setting of BUILT_IN_CHANNELS[id].settings) {
      if (setting.autogenerate && !env[setting.key]) {
        env[setting.key] = randomBytes(48).toString("base64url");
        changed = true;
      }
    }
    if (id === "hud" && env.HUD_CHANNEL_WORKSPACE !== packId) {
      env.HUD_CHANNEL_WORKSPACE = packId;
      changed = true;
    }
    next[section] = env;
  }
  if (changed) await writeSecretsStore(next);
}

/**
 * One-time move of channel credentials from the sections the channel plugins
 * used (`@open-neko/plugin-slack`, …) to `channel.<id>`. Values already in
 * the new section win; the old section is removed once copied.
 */
export async function migrateLegacyChannelSecrets(): Promise<string[]> {
  const store = await readSecretsStore();
  const moved: string[] = [];
  const next: SecretsStore = { ...store };
  for (const [legacy, id] of Object.entries(LEGACY_CHANNEL_NAMES)) {
    const old = next[legacy];
    if (!old) continue;
    const section = channelSecretSection(id);
    const keys = new Set(BUILT_IN_CHANNELS[id].settings.map((s) => s.key));
    const carried = Object.fromEntries(Object.entries(old).filter(([key]) => keys.has(key)));
    next[section] = { ...carried, ...(next[section] ?? {}) };
    delete next[legacy];
    moved.push(legacy);
  }
  if (moved.length > 0) await writeSecretsStore(next);
  return moved;
}

/** Hook the pack lifecycle calls after it changes which channels are on. */
let channelsChanged: ((orgId: string) => void) | null = null;

export function setChannelsChangedHook(hook: ((orgId: string) => void) | null): void {
  channelsChanged = hook;
}

export function notifyChannelsChanged(orgId: string): void {
  channelsChanged?.(orgId);
}

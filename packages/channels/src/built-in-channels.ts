import {
  VOICE_PROFILE,
  WEB_PROFILE,
} from "@neko/interaction";
import {
  defineChannel,
  substrateAdapter,
  type ChannelAdapter,
  type ChannelEnv,
  type ChannelSubstrate,
  type DeliverResult,
} from "./channel-adapter";
import { HUD_CHANNEL } from "./hud/channel";
import { parseSlackInbound } from "./inbound/slack";
import { slackProjection } from "./projections/slack";
import { voiceProjection } from "./projections/voice";
import { webProjection } from "./projections/web";
import { SLACK_CHANNEL } from "./slack/channel";
import { TELEGRAM_CHANNEL } from "./telegram/channel";

const localSend = async (): Promise<DeliverResult> => ({ delivered: true, ref: "local" });

/** Channels a pack can turn on (manifest `channels:`). Web is always on. */
export const BUILT_IN_CHANNELS = {
  slack: SLACK_CHANNEL,
  telegram: TELEGRAM_CHANNEL,
  hud: HUD_CHANNEL,
} as const satisfies Record<string, ChannelSubstrate>;

export type BuiltInChannelId = keyof typeof BUILT_IN_CHANNELS;

export const BUILT_IN_CHANNEL_IDS = Object.keys(BUILT_IN_CHANNELS) as BuiltInChannelId[];

/** The package names these channels shipped under as plugins, still found in old rows and webhook URLs. */
export const LEGACY_CHANNEL_NAMES: Readonly<Record<string, BuiltInChannelId>> = {
  "@open-neko/plugin-slack": "slack",
  "@open-neko/channel-telegram": "telegram",
  "@open-neko/channel-hud": "hud",
};

export const isBuiltInChannelId = (value: string): value is BuiltInChannelId =>
  Object.prototype.hasOwnProperty.call(BUILT_IN_CHANNELS, value);

/** Resolves a channel id, accepting the legacy package names. */
export const builtInChannel = (idOrLegacyName: string): ChannelSubstrate | null => {
  const id = LEGACY_CHANNEL_NAMES[idOrLegacyName] ?? idOrLegacyName;
  return isBuiltInChannelId(id) ? BUILT_IN_CHANNELS[id] : null;
};

/** Secrets-store section holding a channel's credentials. */
export const channelSecretSection = (id: BuiltInChannelId): string => `channel.${id}`;

export const createWebChannel = (): ChannelAdapter =>
  defineChannel({
    pluginName: "web",
    providerLabel: "Web",
    profile: WEB_PROFILE,
    directions: ["inbound", "outbound"],
    builtIn: true,
    project: webProjection,
    send: localSend,
  });

/**
 * Slack adapter for the registry. With `env`, deliveries reach Slack using the
 * configured bot token; without it they stay in-process (demos and tests).
 */
export const createSlackChannel = (options: { env?: () => ChannelEnv } = {}): ChannelAdapter =>
  options.env
    ? substrateAdapter(SLACK_CHANNEL, options.env)
    : defineChannel({
        pluginName: SLACK_CHANNEL.id,
        providerLabel: SLACK_CHANNEL.providerLabel,
        profile: SLACK_CHANNEL.profile,
        directions: SLACK_CHANNEL.directions,
        project: slackProjection,
        send: localSend,
        parseInbound: parseSlackInbound,
      });

export const createTelegramChannel = (env: () => ChannelEnv): ChannelAdapter =>
  substrateAdapter(TELEGRAM_CHANNEL, env);

export const createVoiceChannel = (): ChannelAdapter =>
  defineChannel({
    pluginName: "voice",
    providerLabel: "Voice",
    profile: VOICE_PROFILE,
    directions: ["inbound", "outbound"],
    project: voiceProjection,
    send: localSend,
  });

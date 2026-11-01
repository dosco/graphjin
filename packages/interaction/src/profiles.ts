import type { CapabilityProfile } from "./capability-profile";

export const WEB_PROFILE: CapabilityProfile = {
  modalities: ["text", "visual"],
  richMedia: { markdown: true, cards: true, charts: true, images: true, interactiveControls: true },
  interaction: { turnTaking: "streaming", canApproveInline: true, quickReplies: true },
  constraints: { latencyClass: "interactive", attentionModel: "pull" },
  fidelity: "full",
};

export const SLACK_PROFILE: CapabilityProfile = {
  modalities: ["text", "visual"],
  richMedia: { markdown: true, cards: true, charts: false, images: true, interactiveControls: true },
  interaction: { turnTaking: "async", canApproveInline: true, quickReplies: true },
  constraints: { latencyClass: "interactive", attentionModel: "push" },
  fidelity: "summary",
};

/** Markdown, 4096 chars, inline buttons; leaner than web (no cards/charts). */
export const TELEGRAM_PROFILE: CapabilityProfile = {
  modalities: ["text"],
  richMedia: { markdown: true, cards: false, charts: false, images: true, interactiveControls: true },
  interaction: { turnTaking: "async", canApproveInline: true, quickReplies: true },
  constraints: { maxOutboundChars: 4096, latencyClass: "interactive", attentionModel: "push" },
  fidelity: "summary",
};

export const VOICE_PROFILE: CapabilityProfile = {
  modalities: ["voice"],
  richMedia: { markdown: false, cards: false, charts: false, images: false, interactiveControls: false },
  interaction: { turnTaking: "realtime", canApproveInline: true, quickReplies: false },
  constraints: { latencyClass: "realtime", attentionModel: "push" },
  fidelity: "summary",
};

/** Read-only digest: proves the `ask` degradation — no inline approval, link back instead. */
export const EMAIL_DIGEST_PROFILE: CapabilityProfile = {
  modalities: ["text", "visual"],
  richMedia: { markdown: true, cards: true, charts: false, images: true, interactiveControls: false },
  interaction: { turnTaking: "async", canApproveInline: false, quickReplies: false },
  constraints: { latencyClass: "batch", attentionModel: "pull" },
  fidelity: "full",
};

/** A substrate that didn't exist when the loop was written — proof the profile vocabulary is additive-only. */
export const CORTEX_PROFILE: CapabilityProfile = {
  modalities: ["neural"],
  richMedia: { markdown: false, cards: false, charts: false, images: false, interactiveControls: true },
  interaction: { turnTaking: "realtime", canApproveInline: true, quickReplies: true },
  constraints: { latencyClass: "realtime", attentionModel: "push" },
  fidelity: "full",
};

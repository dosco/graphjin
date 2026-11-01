import { describe, expect, it } from "vitest";
import {
  CORTEX_PROFILE,
  EMAIL_DIGEST_PROFILE,
  SLACK_PROFILE,
  TELEGRAM_PROFILE,
  VOICE_PROFILE,
  WEB_PROFILE,
  type CapabilityProfile,
} from "../src/index.js";

const ALL: Array<[string, CapabilityProfile]> = [
  ["web", WEB_PROFILE],
  ["slack", SLACK_PROFILE],
  ["telegram", TELEGRAM_PROFILE],
  ["voice", VOICE_PROFILE],
  ["email", EMAIL_DIGEST_PROFILE],
  ["cortex", CORTEX_PROFILE],
];

describe("capability profiles", () => {
  it("web is the richest reference profile", () => {
    expect(WEB_PROFILE.richMedia.charts).toBe(true);
    expect(WEB_PROFILE.richMedia.cards).toBe(true);
    expect(WEB_PROFILE.interaction.canApproveInline).toBe(true);
    expect(WEB_PROFILE.fidelity).toBe("full");
  });

  it("slack carries cards but not charts", () => {
    expect(SLACK_PROFILE.richMedia.cards).toBe(true);
    expect(SLACK_PROFILE.richMedia.charts).toBe(false);
    expect(SLACK_PROFILE.interaction.canApproveInline).toBe(true);
  });

  it("telegram is text + markdown + inline buttons, leaner than web", () => {
    expect(TELEGRAM_PROFILE.modalities).toEqual(["text"]);
    expect(TELEGRAM_PROFILE.richMedia.markdown).toBe(true);
    expect(TELEGRAM_PROFILE.richMedia.cards).toBe(false);
    expect(TELEGRAM_PROFILE.richMedia.interactiveControls).toBe(true);
    expect(TELEGRAM_PROFILE.interaction.canApproveInline).toBe(true);
    expect(TELEGRAM_PROFILE.constraints.maxOutboundChars).toBe(4096);
  });

  it("voice is eyes-free realtime with no visual media", () => {
    expect(VOICE_PROFILE.modalities).toEqual(["voice"]);
    expect(VOICE_PROFILE.interaction.turnTaking).toBe("realtime");
    expect(VOICE_PROFILE.richMedia.images).toBe(false);
  });

  it("email digest is read-only — it cannot approve inline", () => {
    expect(EMAIL_DIGEST_PROFILE.interaction.canApproveInline).toBe(false);
  });

  it("cortex declares the neural modality (the additive stress test)", () => {
    expect(CORTEX_PROFILE.modalities).toEqual(["neural"]);
  });

  it.each(ALL)("%s: visual media implies a visual modality", (_name, profile) => {
    const hasVisual = profile.modalities.includes("visual");
    const claimsVisualMedia = profile.richMedia.cards || profile.richMedia.charts;
    if (claimsVisualMedia) expect(hasVisual).toBe(true);
  });
});

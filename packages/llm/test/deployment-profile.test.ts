// SEC8 — the deployment dial: one env knob resolves the posture; the
// behavior envelopes shrink with it.

import { afterEach, describe, expect, it } from "vitest";
import {
  profileBehaviorThresholds,
  reportDeploymentProfile,
  resolveDeploymentProfile,
} from "../src/work/deployment-profile";

const prevProfile = process.env.OPENNEKO_PROFILE;
const prevHosts = process.env.OPENNEKO_AGENT_MODEL_HOST;

afterEach(() => {
  if (prevProfile === undefined) delete process.env.OPENNEKO_PROFILE;
  else process.env.OPENNEKO_PROFILE = prevProfile;
  if (prevHosts === undefined) delete process.env.OPENNEKO_AGENT_MODEL_HOST;
  else process.env.OPENNEKO_AGENT_MODEL_HOST = prevHosts;
});

describe("SEC8 deployment profiles", () => {
  it("defaults to solo and rejects unknown values", () => {
    delete process.env.OPENNEKO_PROFILE;
    expect(resolveDeploymentProfile()).toBe("solo");
    process.env.OPENNEKO_PROFILE = "fortress";
    expect(resolveDeploymentProfile()).toBe("solo");
    process.env.OPENNEKO_PROFILE = "Hardened";
    expect(resolveDeploymentProfile()).toBe("hardened");
  });

  it("tightens behavior envelopes as the posture hardens", () => {
    const solo = profileBehaviorThresholds("solo");
    const org = profileBehaviorThresholds("org");
    const hardened = profileBehaviorThresholds("hardened");
    expect(org.controlPlaneCallsPerRun).toBe(
      Math.floor(solo.controlPlaneCallsPerRun / 2),
    );
    expect(hardened.actionRequestsPerHour).toBe(
      Math.floor(solo.actionRequestsPerHour / 4),
    );
    expect(hardened.memoryWritesPerHour).toBeGreaterThanOrEqual(1);
  });

  it("hardened warns when the model host is a public cloud API", () => {
    process.env.OPENNEKO_PROFILE = "hardened";
    process.env.OPENNEKO_AGENT_MODEL_HOST =
      "generativelanguage.googleapis.com,llm.internal.corp";
    const warnings: string[] = [];
    const original = console.warn;
    console.warn = (msg: unknown) => warnings.push(String(msg));
    try {
      expect(reportDeploymentProfile()).toBe("hardened");
    } finally {
      console.warn = original;
    }
    expect(warnings.join("\n")).toContain("googleapis.com");
    expect(warnings.join("\n")).not.toContain("llm.internal.corp");
  });
});

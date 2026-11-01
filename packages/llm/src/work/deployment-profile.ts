import {
  behaviorThresholdsFromEnv,
  type BehaviorThresholds,
} from "./behavior-monitor";

/**
 * SEC8 — the deployment dial. One knob (OPENNEKO_PROFILE) sets the
 * posture; everything it influences resolves through here so the
 * mapping is auditable in one place:
 *
 *   solo      — single operator IS the admin; default.
 *   team      — sign-in on, member/admin split, CV2 layers.
 *   org       — + admin-gated chat approvals, tighter behavior envelopes.
 *   hardened  — + strictest envelopes; expects a client-provided
 *               on-prem LLM endpoint (config path only — OpenNeko does
 *               not ship a model; it warns when the model host looks
 *               like a public cloud API).
 */
export const DEPLOYMENT_PROFILES = ["solo", "team", "org", "hardened"] as const;
export type DeploymentProfile = (typeof DEPLOYMENT_PROFILES)[number];

export function resolveDeploymentProfile(): DeploymentProfile {
  const raw = (process.env.OPENNEKO_PROFILE ?? "solo").toLowerCase();
  const requested = (DEPLOYMENT_PROFILES as readonly string[]).includes(raw)
    ? (raw as DeploymentProfile)
    : "solo";
  return requested;
}

/** Behavior envelopes shrink as the posture tightens; explicit env overrides still win. */
const THRESHOLD_SCALE: Record<DeploymentProfile, number> = {
  solo: 1,
  team: 1,
  org: 0.5,
  hardened: 0.25,
};

export function profileBehaviorThresholds(
  profile: DeploymentProfile = resolveDeploymentProfile(),
): BehaviorThresholds {
  const base = behaviorThresholdsFromEnv();
  const scale = THRESHOLD_SCALE[profile];
  const apply = (n: number) => Math.max(1, Math.floor(n * scale));
  return {
    controlPlaneCallsPerRun: apply(base.controlPlaneCallsPerRun),
    actionRequestsPerHour: apply(base.actionRequestsPerHour),
    memoryWritesPerHour: apply(base.memoryWritesPerHour),
  };
}

const PUBLIC_MODEL_HOST_RE =
  /(googleapis\.com|anthropic\.com|openai\.com|openrouter\.ai|x\.ai|mistral\.ai)$/i;

/**
 * Startup posture report: one log line stating the resolved profile,
 * plus the hardened-profile warning when the model host is a public
 * cloud API (the decision: hardened implies a client-provided on-prem
 * LLM; we surface the mismatch, the client owns the model).
 */
export function reportDeploymentProfile(): DeploymentProfile {
  const profile = resolveDeploymentProfile();
  console.log(`[deployment-profile] posture: ${profile}`);
  if (profile === "hardened") {
    const hosts = (process.env.OPENNEKO_AGENT_MODEL_HOST ?? "")
      .split(",")
      .map((h) => h.trim())
      .filter(Boolean);
    const publicHosts = hosts.filter((h) => PUBLIC_MODEL_HOST_RE.test(h));
    if (publicHosts.length > 0) {
      console.warn(
        `[deployment-profile] hardened profile expects a client-provided on-prem LLM, but the model host is public: ${publicHosts.join(", ")}`,
      );
    }
  }
  return profile;
}

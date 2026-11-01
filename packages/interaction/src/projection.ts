import type { CapabilityProfile } from "./capability-profile";
import type { InteractionEvent } from "./interaction-event";

/**
 * Pure: (events, profile) → native payload. Built-in channels register one
 * in-process; plugin channels run theirs inside the VM. A projection may drop
 * what the profile can't carry and summarize to its fidelity, but may never
 * invent meaning the modality-free core didn't carry.
 */
export type Projection<Native> = (
  events: InteractionEvent[],
  profile: CapabilityProfile,
) => Native;

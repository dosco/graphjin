export type Modality = "text" | "visual" | "voice" | "haptic" | "neural";

export type TurnTaking = "async" | "streaming" | "realtime";

export type LatencyClass = "batch" | "interactive" | "realtime";

export type AttentionModel = "pull" | "push";

export type Fidelity = "headline" | "summary" | "full";

export interface RichMediaProfile {
  markdown: boolean;
  cards: boolean;
  charts: boolean;
  images: boolean;
  interactiveControls: boolean;
}

export interface InteractionStyleProfile {
  turnTaking: TurnTaking;
  canApproveInline: boolean;
  quickReplies: boolean;
}

export interface ConstraintProfile {
  maxOutboundChars?: number;
  latencyClass: LatencyClass;
  attentionModel: AttentionModel;
}

/**
 * The only thing a projection may branch on. Additive-only: a new substrate
 * adds enum members or fields; channels that don't set them inherit defaults,
 * so existing channels are never disturbed.
 */
export interface CapabilityProfile {
  modalities: Modality[];
  richMedia: RichMediaProfile;
  interaction: InteractionStyleProfile;
  constraints: ConstraintProfile;
  fidelity: Fidelity;
}

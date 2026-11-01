export type PackErrorPhase =
  | "upload"
  | "inspection"
  | "review"
  | "install"
  | "configure"
  | "upgrade"
  | "uninstall"
  | "oauth"
  | "query_preflight";

export type PackArtifactDiagnostic = {
  kind: string;
  name?: string;
  path?: string;
};

export type PackAuthorDiagnostic = {
  code: string;
  phase: PackErrorPhase;
  artifact?: PackArtifactDiagnostic;
  causes: string[];
  nextStep: string;
};

export class PackAuthorError extends Error {
  readonly diagnostic: PackAuthorDiagnostic;

  constructor(message: string, diagnostic: PackAuthorDiagnostic) {
    super(message);
    this.name = "PackAuthorError";
    this.diagnostic = diagnostic;
  }
}

export function packFailurePayload(
  error: unknown,
  phase: PackErrorPhase,
  nextStep: string,
): { error: string; diagnostic: PackAuthorDiagnostic } {
  if (error instanceof PackAuthorError) return { error: error.message, diagnostic: error.diagnostic };
  const message = error instanceof Error ? error.message : String(error);
  return {
    error: message,
    diagnostic: {
      code: "pack_operation_failed",
      phase,
      causes: [message],
      nextStep,
    },
  };
}

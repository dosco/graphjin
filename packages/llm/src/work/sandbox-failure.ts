/** The agent sandbox failed, not the model: creation, setup, exec timeout, or no result. */
export class SandboxFailure extends Error {
  constructor(
    readonly code: `sandbox.${string}`,
    message: string,
    options?: { cause?: unknown },
  ) {
    super(message, options);
    this.name = "SandboxFailure";
  }
}

// Process-wide CoreSecrets handle. The worker's startup constructs one,
// stashes it here, and job handlers ask for the current scrubber without
// receiving the secrets view through three layers of payload.
import { createScrubber, type Scrubber } from "@neko/llm/work";
import type { CoreSecrets } from "./core-secrets.js";

let instance: CoreSecrets | null = null;

export function setCoreSecretsInstance(secrets: CoreSecrets | null): void {
  instance = secrets;
}

/**
 * The scrubber over every stored secret value, or a no-op before the
 * secrets view is loaded (tests, early boot).
 */
export function getCurrentScrubber(): Scrubber {
  return instance?.scrubber() ?? createScrubber([]);
}

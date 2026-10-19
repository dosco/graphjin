// The worker's in-memory view of the secrets store: deployment sections
// (`signin.scalekit`, `channel.slack`, `pack.<id>`, …) plus per-operator
// connect credentials. Sign-in reads it synchronously on every request, so
// it is loaded at start and reloaded whenever the secrets file changes —
// a CLI `openneko secrets set` or another process's write takes effect
// without a restart. Writes go to the file read-modify-write, so they never
// clobber a section another writer (the pack service) just changed.
import { statSync } from "node:fs";
import {
  allSecretValuesFull,
  defaultSecretsPath,
  getOperatorCredential,
  readFullSecretsFileSoft,
  setOperatorCredential,
  setSecret,
  unsetOperatorCredential,
  unsetSecret,
  writeFullSecretsFile,
  type ConnectorCredential,
  type FullSecretsFile,
} from "@open-neko/plugin-install/secrets";
import { FileSecretsResolver, type SecretsResolver } from "@open-neko/plugin-install";
import { createScrubber, type Scrubber } from "@neko/llm/work";

const POLL_MS = 3000;

export interface CoreSecretsOptions {
  /** Where deployment sections are read from; defaults to the local file. */
  resolver?: SecretsResolver;
  /** Overrides the secrets directory (tests). */
  configDir?: string;
  pollMs?: number;
}

export class CoreSecrets {
  private full: FullSecretsFile = { env: {}, operators: {} };
  private scrubberValue: Scrubber = createScrubber([]);
  private timer: NodeJS.Timeout | null = null;
  private lastMtimeMs = 0;
  private readonly resolver: SecretsResolver;

  constructor(private readonly options: CoreSecretsOptions = {}) {
    this.resolver = options.resolver ?? new FileSecretsResolver(options.configDir);
  }

  async start(): Promise<void> {
    await this.refresh();
    const pollMs = this.options.pollMs ?? POLL_MS;
    if (pollMs <= 0) return;
    this.timer = setInterval(() => {
      const mtime = this.mtime();
      if (mtime === this.lastMtimeMs) return;
      void this.refresh().catch((err) => {
        console.warn(`[secrets] reload failed: ${err instanceof Error ? err.message : err}`);
      });
    }, pollMs);
    this.timer.unref?.();
  }

  stop(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
  }

  async refresh(): Promise<void> {
    this.lastMtimeMs = this.mtime();
    this.full = await this.resolver.resolveFullSecrets((line) => console.warn(`[secrets] ${line}`));
    this.scrubberValue = createScrubber(allSecretValuesFull(this.full));
  }

  /** Redacts every stored secret value from agent output. */
  scrubber(): Scrubber {
    return this.scrubberValue;
  }

  /** One deployment section, e.g. `signin.scalekit`. */
  section(name: string): Record<string, string> {
    return { ...(this.full.env[name] ?? {}) };
  }

  operatorCredential(slot: string, name: string): ConnectorCredential | null {
    return getOperatorCredential(this.full, slot, name);
  }

  /** Every credential stored under one operator slot, keyed by connector. */
  operatorCredentials(slot: string): Record<string, ConnectorCredential> {
    return { ...(this.full.operators[slot] ?? {}) };
  }

  /** Writes (string) or deletes (null) values of one section. */
  async setValues(section: string, values: Record<string, string | null>): Promise<void> {
    await this.mutate((full) => {
      let env = full.env;
      for (const [key, value] of Object.entries(values)) {
        env = value === null ? unsetSecret(env, section, key).store : setSecret(env, section, key, value);
      }
      return { env, operators: full.operators };
    });
  }

  async setOperatorCredential(slot: string, name: string, credential: ConnectorCredential | null): Promise<boolean> {
    let removed = true;
    await this.mutate((full) => {
      if (credential) return setOperatorCredential(full, slot, name, credential);
      const result = unsetOperatorCredential(full, slot, name);
      removed = result.removed;
      return result.store;
    });
    return removed;
  }

  private async mutate(change: (full: FullSecretsFile) => FullSecretsFile): Promise<void> {
    const current = await readFullSecretsFileSoft(this.options.configDir, (line) => console.warn(`[secrets] ${line}`));
    await writeFullSecretsFile(change(current), this.options.configDir);
    await this.refresh();
  }

  private mtime(): number {
    try {
      return statSync(defaultSecretsPath(this.options.configDir)).mtimeMs;
    } catch {
      return 0;
    }
  }
}

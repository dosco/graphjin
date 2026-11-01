import {
  readFullSecretsFileSoft,
  type FullSecretsFile,
} from "./secrets-store";

/**
 * SEC2 — the portability seam over secret residency. The worker consumes
 * a resolver, not the file: today the file impl (deployment env bags +
 * per-operator OAuth blobs from ~/.config/openneko/secrets.json, enc:v1
 * at rest); SEC3 adds an Infisical-backed impl for the deployment-wide
 * env bags (per-operator credentials stay local either way).
 */
export interface SecretsResolver {
  /** Full secrets view: deployment env bags + per-operator credentials. */
  resolveFullSecrets(warn?: (line: string) => void): Promise<FullSecretsFile>;
}

export class FileSecretsResolver implements SecretsResolver {
  constructor(private readonly overrideDir?: string) {}

  resolveFullSecrets(warn?: (line: string) => void): Promise<FullSecretsFile> {
    return readFullSecretsFileSoft(this.overrideDir, warn);
  }
}

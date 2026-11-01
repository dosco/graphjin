import type { SolutionPackBundle } from "@neko/packs";
import {
  readSecretsStore,
  type SecretsStore,
} from "@neko/secrets";

const PACK_SECRET_PREFIX = "pack.";

export type PackSecretRequest = {
  secrets?: Record<string, string>;
  secretRefs?: Record<string, string>;
};

export function secretEnvKey(key: string): string {
  return key.replace(/[^A-Za-z0-9]+/g, "_").toUpperCase();
}

export function packSecretSection(packId: string): string {
  return `${PACK_SECRET_PREFIX}${packId}`;
}

export function resolveInputs(
  bundle: SolutionPackBundle,
  supplied: Record<string, unknown>,
): Record<string, unknown> {
  const declared = new Map(bundle.manifest.inputs.map((input) => [input.key, input]));
  for (const key of Object.keys(supplied)) {
    if (!declared.has(key)) throw new Error(`unknown pack input ${key}`);
  }

  const resolved: Record<string, unknown> = {};
  for (const input of bundle.manifest.inputs) {
    const value = Object.hasOwn(supplied, input.key) ? supplied[input.key] : input.default;
    if (value === undefined && input.required) throw new Error(`required pack input ${input.key} is missing`);
    if (value === undefined) continue;

    switch (input.type) {
      case "string":
      case "timezone":
      case "url": {
        if (typeof value !== "string" || (input.type !== "string" && !value.trim())) {
          throw new Error(`pack input ${input.key} must be ${input.type === "string" ? "a string" : `a non-empty ${input.type}`}`);
        }
        const normalized = value.trim();
        if (input.required && !normalized) throw new Error(`required pack input ${input.key} must not be empty`);
        if (input.type === "url") {
          let parsed: URL;
          try {
            parsed = new URL(normalized);
          } catch {
            throw new Error(`pack input ${input.key} must be an absolute URL`);
          }
          if (!["http:", "https:"].includes(parsed.protocol)) {
            throw new Error(`pack input ${input.key} must use HTTP or HTTPS`);
          }
          resolved[input.key] = normalized.replace(/\/+$/, "");
        } else if (input.type === "timezone") {
          try {
            new Intl.DateTimeFormat("en-US", { timeZone: normalized }).format();
          } catch {
            throw new Error(`pack input ${input.key} must be a valid IANA timezone`);
          }
          resolved[input.key] = normalized;
        } else {
          resolved[input.key] = normalized;
        }
        break;
      }
      case "integer": {
        const number = typeof value === "number" ? value : Number(value);
        if (!Number.isInteger(number)) throw new Error(`pack input ${input.key} must be an integer`);
        resolved[input.key] = number;
        break;
      }
      case "boolean":
        if (typeof value !== "boolean") throw new Error(`pack input ${input.key} must be a boolean`);
        resolved[input.key] = value;
        break;
      case "enum":
        if (!input.values?.some((candidate) => candidate === value)) {
          throw new Error(`pack input ${input.key} must be one of: ${input.values?.join(", ")}`);
        }
        resolved[input.key] = value;
        break;
    }
  }

  const port = resolved["database.port"];
  if (port !== undefined && (typeof port !== "number" || port < 1 || port > 65535)) {
    throw new Error("database.port must be an integer from 1 to 65535");
  }
  return resolved;
}

export async function resolveSecrets(
  bundle: SolutionPackBundle,
  request: PackSecretRequest,
): Promise<{
  values: Record<string, string>;
  cleared: Set<string>;
  store: SecretsStore;
}> {
  const store = await readSecretsStore();
  const section = packSecretSection(bundle.manifest.metadata.id);
  const current = store[section] ?? {};
  const declared = new Set(bundle.manifest.secrets.map((secret) => secret.key));
  for (const key of [...Object.keys(request.secrets ?? {}), ...Object.keys(request.secretRefs ?? {})]) {
    if (!declared.has(key)) throw new Error(`unknown pack secret ${key}`);
  }

  const personalTokens = new Set(bundle.manifest.oauth.filter(c => c.scope === "user").flatMap(c => [c.accessToken, c.refreshToken]));
  const values: Record<string, string> = {};
  const cleared = new Set<string>();
  for (const secret of bundle.manifest.secrets) {
    if (personalTokens.has(secret.key)) {
      if (request.secrets?.[secret.key] || request.secretRefs?.[secret.key]) throw new Error("Personal OAuth tokens must be connected on Integrations");
      cleared.add(secret.key);
      continue;
    }
    const direct = request.secrets?.[secret.key];
    const ref = request.secretRefs?.[secret.key];
    if (direct !== undefined && typeof direct !== "string") {
      throw new Error(`pack secret ${secret.key} must be a string`);
    }
    if (direct !== undefined && !direct.trim() && !secret.required) {
      cleared.add(secret.key);
      continue;
    }
    const stored = ref ? current[ref] : current[secretEnvKey(secret.key)];
    const value = direct !== undefined ? direct : stored;
    if (secret.required && (!value || !value.trim())) {
      throw new Error(`required pack secret ${secret.key} is missing`);
    }
    if (value) values[secret.key] = value;
  }
  return { values, cleared, store };
}

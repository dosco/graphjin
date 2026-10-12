import type { PrimaryProviderId } from "./config";

export type ProviderCredentialSource = "stored-api-key" | "google-adc" | "none";

export type ProviderEndpoint = {
  host: string;
  port?: number;
};

export type HermesProviderRuntime = {
  provider: string;
  model: string;
  baseUrl: string;
  keyEnv?: string;
  apiMode?: "chat_completions";
  credentialSource: ProviderCredentialSource;
  endpoint: ProviderEndpoint;
};

function requiredConfig(config: Record<string, unknown>, key: string, label: string): string {
  const value = typeof config[key] === "string" ? config[key].trim() : "";
  if (!value) throw new Error(`${label} is required.`);
  return value;
}

function endpointFromUrl(baseUrl: string): ProviderEndpoint {
  let url: URL;
  try {
    url = new URL(baseUrl);
  } catch {
    throw new Error(`Invalid provider Base URL: ${baseUrl}`);
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") {
    throw new Error(`Provider Base URL must use http or https: ${baseUrl}`);
  }
  return {
    host: url.hostname,
    ...(url.port
      ? { port: Number(url.port) }
      : url.protocol === "http:"
        ? { port: 80 }
        : {}),
  };
}

function fixedRuntime(
  provider: string,
  model: string,
  baseUrl: string,
  keyEnv: string,
): HermesProviderRuntime {
  return {
    provider,
    model,
    baseUrl,
    keyEnv,
    credentialSource: "stored-api-key",
    endpoint: endpointFromUrl(baseUrl),
  };
}

function azureBaseUrl(config: Record<string, unknown>): string {
  const resourceName = requiredConfig(config, "resourceName", "Azure resource name");
  if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(resourceName)) {
    throw new Error("Azure resource name contains invalid characters.");
  }
  return `https://${resourceName}.openai.azure.com/openai/v1`;
}

export function vertexOpenAiBaseUrl(config: Record<string, unknown>): string {
  const projectId = requiredConfig(config, "projectId", "GCP project ID");
  const region = requiredConfig(config, "region", "Vertex region");
  if (!/^[a-z0-9][a-z0-9-.:]{0,126}$/i.test(projectId)) {
    throw new Error("GCP project ID contains invalid characters.");
  }
  if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(region)) {
    throw new Error("Vertex region contains invalid characters.");
  }
  const host = region === "global" ? "aiplatform.googleapis.com" : `${region}-aiplatform.googleapis.com`;
  return `https://${host}/v1beta1/projects/${encodeURIComponent(projectId)}/locations/${encodeURIComponent(region)}/endpoints/openapi`;
}

function ollamaBaseUrl(config: Record<string, unknown>): string {
  const configured = requiredConfig(config, "url", "Ollama Base URL");
  let url: URL;
  try {
    url = new URL(configured);
  } catch {
    throw new Error(`Invalid Ollama Base URL: ${configured}`);
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("Ollama Base URL must use http or https.");
  }

  // The model runs in an OpenShell sandbox, so loopback points at the
  // sandbox itself. Route host-local Ollama through Docker's host gateway.
  if (["localhost", "127.0.0.1", "[::1]", "::1"].includes(url.hostname)) {
    url.hostname = "host.docker.internal";
  }
  if (url.pathname === "/" || url.pathname === "") url.pathname = "/v1";
  return url.toString().replace(/\/$/, "");
}

/**
 * OpenNeko's complete Admin-provider -> Hermes 0.21 runtime contract.
 *
 * Keep this exhaustive. A provider must not appear in Admin unless it has an
 * explicit Hermes identity, credential source, base URL and sandbox endpoint.
 */
export function resolveHermesProviderRuntime(args: {
  provider: PrimaryProviderId;
  model: string;
  config?: Record<string, unknown> | null;
}): HermesProviderRuntime {
  const config = args.config ?? {};

  switch (args.provider) {
    case "openai":
      // In Hermes 0.21, bare `openai` is intentionally an OpenRouter alias.
      return fixedRuntime("openai-api", args.model, "https://api.openai.com/v1", "OPENAI_API_KEY");
    case "anthropic":
      return fixedRuntime("anthropic", args.model, "https://api.anthropic.com", "ANTHROPIC_API_KEY");
    case "google-gemini":
      return fixedRuntime("gemini", args.model, "https://generativelanguage.googleapis.com/v1beta", "GEMINI_API_KEY");
    case "azure-openai": {
      const baseUrl = azureBaseUrl(config);
      return {
        ...fixedRuntime(
          "azure-foundry",
          requiredConfig(config, "deploymentName", "Azure deployment name"),
          baseUrl,
          "AZURE_FOUNDRY_API_KEY",
        ),
        apiMode: "chat_completions",
      };
    }
    // Hermes 0.21 does not register these four vendor slugs as runtime
    // providers. Their APIs are OpenAI-compatible, so route them through the
    // explicit custom transport instead of letting Hermes fail with
    // "Unknown provider" before making a request.
    case "mistral":
      return {
        ...fixedRuntime("custom", args.model, "https://api.mistral.ai/v1", "MISTRAL_API_KEY"),
        apiMode: "chat_completions",
      };
    case "groq":
      return {
        ...fixedRuntime("custom", args.model, "https://api.groq.com/openai/v1", "GROQ_API_KEY"),
        apiMode: "chat_completions",
      };
    case "cohere":
      return {
        ...fixedRuntime(
          "custom",
          args.model,
          "https://api.cohere.ai/compatibility/v1",
          "COHERE_API_KEY",
        ),
        apiMode: "chat_completions",
      };
    case "together":
      return {
        ...fixedRuntime("custom", args.model, "https://api.together.ai/v1", "TOGETHER_API_KEY"),
        apiMode: "chat_completions",
      };
    case "deepseek":
      return fixedRuntime("deepseek", args.model, "https://api.deepseek.com/v1", "DEEPSEEK_API_KEY");
    case "ollama": {
      const baseUrl = ollamaBaseUrl(config);
      return {
        provider: "custom",
        model: args.model,
        baseUrl,
        apiMode: "chat_completions",
        credentialSource: "none",
        endpoint: endpointFromUrl(baseUrl),
      };
    }
    case "huggingface":
      return fixedRuntime("huggingface", args.model, "https://router.huggingface.co/v1", "HF_TOKEN");
    case "openrouter":
      return fixedRuntime("openrouter", args.model, "https://openrouter.ai/api/v1", "OPENROUTER_API_KEY");
    case "reka": {
      const runtime = fixedRuntime("custom", args.model, "https://api.reka.ai/v1", "REKA_API_KEY");
      return { ...runtime, apiMode: "chat_completions" };
    }
    case "x-grok":
      return fixedRuntime("xai", args.model, "https://api.x.ai/v1", "XAI_API_KEY");
    case "vertex": {
      const baseUrl = vertexOpenAiBaseUrl(config);
      return {
        provider: "custom",
        model: args.model,
        baseUrl,
        keyEnv: "VERTEX_ACCESS_TOKEN",
        apiMode: "chat_completions",
        credentialSource: "google-adc",
        endpoint: endpointFromUrl(baseUrl),
      };
    }
  }
}

/** Non-secret Ax Harness model route. The key reaches the box as an OpenShell placeholder in keyEnv. */
export type AxModelRoute = {
  /** An Ax provider profile, or openai-compatible. */
  provider: string;
  model: string;
  url?: string;
  keyEnv?: string;
  /** Ax provider settings, for example Azure's resource and deployment. */
  options?: Record<string, string>;
};

// Ax has a profile for each of these. A profile keeps the provider's auth
// header, request shape and model rules.
const AX_PROFILES: Partial<Record<PrimaryProviderId, string>> = {
  mistral: "mistral",
  groq: "groq",
  cohere: "cohere",
  together: "together",
  deepseek: "deepseek",
  huggingface: "huggingface-router",
  openrouter: "openrouter",
  reka: "reka",
  "x-grok": "grok",
  vertex: "vertex-ai",
};

/** Azure API version Ax sends; structured outputs need 2024-08-01 or later. */
export const AX_AZURE_API_VERSION = "2024-10-21";

/**
 * Maps the admin provider onto an Ax provider profile. Profiles that call an
 * OpenAI-style endpoint keep the Hermes base URL, so model egress reaches the
 * same host for both backends.
 */
export function resolveAxModelRoute(args: {
  provider: PrimaryProviderId;
  model: string;
  config?: Record<string, unknown> | null;
}): AxModelRoute {
  const runtime = resolveHermesProviderRuntime(args);
  const keyEnv = runtime.keyEnv ? { keyEnv: runtime.keyEnv } : {};
  switch (args.provider) {
    case "openai":
    case "anthropic":
    case "google-gemini":
      return { provider: args.provider, model: runtime.model, ...keyEnv };
    case "azure-openai":
      return {
        provider: "azure-openai",
        model: runtime.model,
        ...keyEnv,
        options: {
          resource_name: requiredConfig(args.config ?? {}, "resourceName", "Azure resource name"),
          deployment_name: runtime.model,
          api_version: AX_AZURE_API_VERSION,
        },
      };
    default:
      return { provider: AX_PROFILES[args.provider] ?? "openai-compatible", model: runtime.model, url: runtime.baseUrl, ...keyEnv };
  }
}

// Hermes names a provider its own way; OpenNeko and Ax use the names on the left.
const HERMES_PROVIDER_NAMES: Record<string, string> = {
  gemini: "google-gemini",
  "openai-api": "openai",
  "azure-foundry": "azure-openai",
  anthropic: "anthropic",
  deepseek: "deepseek",
};

/**
 * The OpenNeko provider name for one Hermes reports. Hermes reuses "custom" for
 * several providers, so the configured provider wins when it maps to the
 * reported name.
 */
export function nekoProviderFromHermes(
  hermesProvider: string,
  configured?: { provider: string; model: string },
): string {
  if (configured) {
    try {
      const provider = configured.provider as Parameters<typeof resolveHermesProviderRuntime>[0]["provider"];
      if (resolveHermesProviderRuntime({ provider, model: configured.model }).provider === hermesProvider) {
        return configured.provider;
      }
    } catch {
      // A provider that needs more configuration falls back to the fixed names.
    }
  }
  return HERMES_PROVIDER_NAMES[hermesProvider] ?? hermesProvider;
}

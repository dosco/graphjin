export const PRIMARY_PROVIDER_OPTIONS = [
  { value: "openai", label: "OpenAI", description: "Direct OpenAI API key" },
  { value: "anthropic", label: "Anthropic", description: "Claude via Anthropic API" },
  { value: "google-gemini", label: "Google Gemini", description: "Gemini via Google AI API" },
  { value: "azure-openai", label: "Azure OpenAI", description: "Azure resource + deployment" },
  { value: "mistral", label: "Mistral", description: "Mistral API" },
  { value: "groq", label: "Groq", description: "Groq API" },
  { value: "cohere", label: "Cohere", description: "Cohere API" },
  { value: "together", label: "Together", description: "Together API" },
  { value: "deepseek", label: "DeepSeek", description: "DeepSeek API" },
  { value: "ollama", label: "Ollama", description: "Local or remote Ollama endpoint" },
  { value: "huggingface", label: "Hugging Face", description: "Hugging Face Inference API" },
  { value: "openrouter", label: "OpenRouter", description: "OpenRouter API" },
  { value: "reka", label: "Reka", description: "Reka API" },
  { value: "x-grok", label: "xAI Grok", description: "xAI Grok API" },
  { value: "vertex", label: "Vertex MaaS", description: "Vertex OpenAI-compatible endpoint via Google ADC" },
] as const;

export const RESEARCH_PROVIDER_OPTIONS = [
  { value: "disabled", label: "Disabled", description: "Skip industry research" },
  { value: "perplexity", label: "Perplexity", description: "Perplexity chat completions" },
] as const;

export type PrimaryProviderId = (typeof PRIMARY_PROVIDER_OPTIONS)[number]["value"];
export type ResearchProviderId = (typeof RESEARCH_PROVIDER_OPTIONS)[number]["value"];
export type ProviderScope = "primary" | "research";

export type SettingsField = {
  key: string;
  label: string;
  kind: "text" | "secret" | "url";
  required?: boolean;
  placeholder?: string;
  help?: string;
};

export type StoredProviderConfigRow = {
  id: string;
  org_id: string;
  scope: ProviderScope;
  provider: string;
  model: string | null;
  label: string | null;
  enabled: boolean;
  config: Record<string, unknown> | null;
  secrets: Record<string, unknown> | null;
};

export type EditableProviderConfig =
  | {
      scope: "primary";
      provider: PrimaryProviderId;
      model: string;
      label?: string | null;
      enabled: boolean;
      config: Record<string, unknown>;
      secrets: Record<string, string>;
    }
  | {
      scope: "research";
      provider: ResearchProviderId;
      model: string;
      label?: string | null;
      enabled: boolean;
      config: Record<string, unknown>;
      secrets: Record<string, string>;
    };

export type SecretMaskMap = Record<string, string>;

export function getDefaultPrimaryModel(provider: PrimaryProviderId): string {
  switch (provider) {
    case "openai":
      return "gpt-4.1-mini";
    case "anthropic":
      return "claude-opus-4-7";
    case "google-gemini":
      return "gemini-3.6-flash";
    case "azure-openai":
      return "gpt-4.1";
    case "mistral":
      return "mistral-large-latest";
    case "groq":
      return "llama-3.3-70b-versatile";
    case "cohere":
      return "command-a-03-2025";
    case "together":
      return "meta-llama/Llama-3.3-70B-Instruct-Turbo";
    case "deepseek":
      return "deepseek-chat";
    case "ollama":
      return "llama3.2";
    case "huggingface":
      return "meta-llama/Llama-3.3-70B-Instruct";
    case "openrouter":
      return "openai/gpt-4.1-mini";
    case "reka":
      return "reka-core";
    case "x-grok":
      return "grok-3-mini";
    case "vertex":
      return "zai-org/glm-5-maas";
  }
}

export function getDefaultResearchModel(provider: ResearchProviderId): string {
  switch (provider) {
    case "disabled":
      return "";
    case "perplexity":
      return "sonar-deep-research";
  }
}

export function getPrimaryProviderFields(provider: PrimaryProviderId): SettingsField[] {
  switch (provider) {
    case "ollama":
      return [
        {
          key: "url",
          label: "Base URL",
          kind: "url",
          required: true,
          placeholder: "http://host.docker.internal:11434/v1",
          help: "Use host.docker.internal for Ollama running on the OpenNeko host, or a remote OpenAI-compatible URL.",
        },
      ];
    case "azure-openai":
      return [
        {
          key: "apiKey",
          label: "API key",
          kind: "secret",
          required: true,
          placeholder: "your-azure-key",
        },
        {
          key: "resourceName",
          label: "Resource name",
          kind: "text",
          required: true,
          placeholder: "my-openai-resource",
        },
        {
          key: "deploymentName",
          label: "Deployment name",
          kind: "text",
          required: true,
          placeholder: "gpt-4.1",
        },
      ];
    case "vertex":
      return [
        {
          key: "projectId",
          label: "GCP project ID",
          kind: "text",
          required: true,
          placeholder: "my-gcp-project",
          help: "OpenNeko uses host-side Google Application Default Credentials to mint a short-lived token; the agent sandbox never receives the credential.",
        },
        {
          key: "region",
          label: "Region",
          kind: "text",
          required: true,
          placeholder: "global",
        },
      ];
    default:
      return [
        {
          key: "apiKey",
          label: "API key",
          kind: "secret",
          required: true,
          placeholder: "Paste your provider key",
        },
      ];
  }
}

export function getResearchProviderFields(provider: ResearchProviderId): SettingsField[] {
  switch (provider) {
    case "disabled":
      return [];
    case "perplexity":
      return [
        {
          key: "apiKey",
          label: "API key",
          kind: "secret",
          required: true,
          placeholder: "pplx-...",
        },
      ];
  }
}

export function isPrimaryProvider(value: string): value is PrimaryProviderId {
  return PRIMARY_PROVIDER_OPTIONS.some((provider) => provider.value === value);
}

export function isResearchProvider(value: string): value is ResearchProviderId {
  return RESEARCH_PROVIDER_OPTIONS.some((provider) => provider.value === value);
}

export function maskSecret(value: string | undefined): string {
  if (!value) return "";
  if (value.length <= 8) return "•".repeat(value.length);
  const middle = "•".repeat(Math.min(Math.max(4, value.length - 7), 12));
  return `${value.slice(0, 3)}${middle}${value.slice(-4)}`;
}

// Provider config is read from llm_provider_config in the DB only.
// Env-based fallbacks were removed — admins configure providers via /setup
// and /admin/settings/agent. The functions below remain as no-ops so legacy
// callers compile, but they always return null. Remove on next refactor.
//
// Underscores prevent unused-param TS warnings while keeping the legacy
// signature for any straggling import.
export function readPrimaryProviderConfigFromEnv(): EditableProviderConfig | null {
  return null;
}

export function readResearchProviderConfigFromEnv(): EditableProviderConfig | null {
  return null;
}

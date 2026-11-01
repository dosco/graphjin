"use client";

import { useMemo, useState } from "react";
import EntryShell from "@/components/EntryShell";
import { toast } from "sonner";
import Select from "@/components/Select";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Input } from "@/components/ui/field";

type ProviderOption = { value: string; label: string; description: string };
type ProviderField = {
  key: string;
  label: string;
  kind: "text" | "secret" | "url";
  required?: boolean;
  placeholder?: string;
  help?: string;
};

type ProviderConfig = {
  scope: "primary" | "research";
  source: "org" | "env" | "default";
  provider: string;
  model: string;
  enabled: boolean;
  config: Record<string, unknown>;
  secretStatus: Record<string, string>;
};

type SettingsPayload = {
  primary: ProviderConfig;
  research: ProviderConfig;
  options: {
    primary: readonly ProviderOption[];
    research: readonly ProviderOption[];
  };
  defaults: {
    primary: Record<string, string>;
    research: Record<string, string>;
  };
  fields: {
    primary: Record<string, ProviderField[]>;
    research: Record<string, ProviderField[]>;
  };
};

type AgentSettingsPayload = {
  agent: {
    source: "org" | "default";
    globalCap: number;
  };
  defaults: { globalCap: number };
};

type DataSourcePayload = {
  source: "org" | "unset";
  kind: string;
  graphqlUrl: string;
  mcpUrl: string;
  label: string;
};

type Initial = {
  dataSource: DataSourcePayload;
  providers: SettingsPayload;
  agent: AgentSettingsPayload;
  passwordChanged: boolean;
};

// Step 0 ("Password") is shown only when the admin hasn't picked one yet.
// Once changed, ~/.config/openneko/config.json has pg.password and we skip
// straight to Data on subsequent visits.
const STEPS_WITH_PASSWORD = ["Password", "Data", "Agent", "Research"] as const;
const STEPS_WITHOUT_PASSWORD = ["Data", "Agent", "Research"] as const;

export default function SetupWizard({ initial }: { initial: Initial }) {
  const STEPS = initial.passwordChanged
    ? STEPS_WITHOUT_PASSWORD
    : STEPS_WITH_PASSWORD;

  const [step, setStep] = useState(0);

  // Step 0 (only shown when initial.passwordChanged === false): set DB password.
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [savingPassword, setSavingPassword] = useState(false);

  type StepName = "password" | "data" | "agent" | "research";
  const stepName: StepName = (() => {
    if (initial.passwordChanged) {
      return (["data", "agent", "research"] as const)[step] ?? "data";
    }
    return (
      (["password", "data", "agent", "research"] as const)[step] ?? "password"
    );
  })();

  // Step 1: data source — one root URL, /api/v1/{graphql,mcp} derived on save.
  // Pre-fill a saved data source when one exists. Without a seed row, local dev
  // falls back to localhost and Docker installs can enter the Compose service URL.
  const [data, setData] = useState({
    rootUrl:
      deriveRoot(initial.dataSource.graphqlUrl) || "http://localhost:8080",
    label: initial.dataSource.label || "primary",
  });
  const [savingData, setSavingData] = useState(false);
  const [testingData, setTestingData] = useState(false);

  // Step 2: agent model provider
  const [concurrentJobs, setConcurrentJobs] = useState(
    String(initial.agent.agent.globalCap),
  );
  const [primary, setPrimary] = useState({
    provider: initial.providers.primary.provider,
    model: initial.providers.primary.model,
    config: stringRecord(initial.providers.primary.config),
    secrets: {} as Record<string, string>,
  });
  const [savingPrimary, setSavingPrimary] = useState(false);

  // Step 3: research
  const [researchEnabled, setResearchEnabled] = useState(
    initial.providers.research.enabled &&
      initial.providers.research.provider !== "disabled",
  );
  const initialResearchProvider =
    initial.providers.research.provider === "disabled"
      ? (initial.providers.options.research.find((o) => o.value !== "disabled")
          ?.value ?? "perplexity")
      : initial.providers.research.provider;
  const [research, setResearch] = useState({
    provider: initialResearchProvider,
    model:
      initial.providers.research.provider === "disabled"
        ? (initial.providers.defaults.research[initialResearchProvider] ?? "")
        : initial.providers.research.model,
    config: stringRecord(initial.providers.research.config),
    secrets: {} as Record<string, string>,
  });
  const [savingResearch, setSavingResearch] = useState(false);
  const [finishing, setFinishing] = useState(false);

  // Per-step inline validation errors. Cleared when the user edits the
  // step's inputs or moves to a different step.
  const [dataError, setDataError] = useState<string | null>(null);
  const [primaryError, setPrimaryError] = useState<string | null>(null);
  const [researchError, setResearchError] = useState<string | null>(null);

  const providerOptions = useMemo<ProviderOption[]>(() => {
    return [...initial.providers.options.primary];
  }, [initial.providers.options.primary]);

  const primaryFields: ProviderField[] =
    initial.providers.fields.primary[primary.provider] ?? [];
  const researchFields: ProviderField[] =
    initial.providers.fields.research[research.provider] ?? [];

  const onPrimaryProviderChange = (next: string) => {
    setPrimary({
      provider: next,
      model: initial.providers.defaults.primary[next] ?? "",
      config: {},
      secrets: {},
    });
  };

  // ---------------- Password step actions ----------------

  async function savePasswordAndAdvance() {
    setSavingPassword(true);
    try {
      const res = await fetch("/api/admin/change-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: newPassword }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? "Password change failed");
      toast.success("Database password updated.");
      setNewPassword("");
      setConfirmPassword("");
      setStep(step + 1);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSavingPassword(false);
    }
  }

  // ---------------- Step 1 actions ----------------

  async function testData() {
    setTestingData(true);
    try {
      const { graphqlUrl, mcpUrl } = deriveEndpoints(data.rootUrl);
      const res = await fetch("/api/settings/data-source/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ graphqlUrl, mcpUrl }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? "Test failed");
      toast.success(
        body.mcpOk === false
          ? "GraphQL reachable. MCP unreachable."
          : "Connection looks good.",
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setTestingData(false);
    }
  }

  async function saveDataAndAdvance() {
    setSavingData(true);
    setDataError(null);
    try {
      const { graphqlUrl, mcpUrl } = deriveEndpoints(data.rootUrl);
      // Gate: live connectivity test before save. Reuses the same endpoint
      // the explicit Test button uses; on failure we surface the error
      // inline and DO NOT save — the user shouldn't move past a step with
      // a broken URL only to fail mid-onboarding minutes later.
      const testRes = await fetch("/api/settings/data-source/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ graphqlUrl, mcpUrl }),
      });
      const testBody = await testRes.json().catch(() => ({}));
      if (!testRes.ok) {
        throw new Error(testBody.error ?? "Connection test failed.");
      }
      const res = await fetch("/api/settings/data-source", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ graphqlUrl, mcpUrl, label: data.label }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? "Save failed");
      toast.success(
        testBody.mcpOk === false
          ? "Data source saved. (MCP unreachable, fine for the agent path.)"
          : "Data source saved.",
      );
      setStep(step + 1);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setDataError(msg);
    } finally {
      setSavingData(false);
    }
  }

  // ---------------- Step 2 actions ----------------

  async function savePrimaryAndAdvance() {
    setSavingPrimary(true);
    setPrimaryError(null);
    try {
      // Gate: validate the provider key with a real one-shot LLM call
      // BEFORE saving anything. A bad key would otherwise pass step 2
      // silently and only manifest minutes later as a failed
      // business_profile_build job — surfacing the error here saves
      // the user from a long-tail failure.
      const testRes = await fetch("/api/settings/provider/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          scope: "primary",
          provider: primary.provider,
          model: primary.model,
          enabled: true,
          config: primary.config,
          secrets: primary.secrets,
        }),
      });
      const testBody = await testRes.json().catch(() => ({}));
      if (!testRes.ok) {
        throw new Error(testBody.error ?? "Provider test failed.");
      }
      // Save worker-wide agent concurrency.
      const cap = Number(concurrentJobs) || initial.agent.defaults.globalCap;
      const agentRes = await fetch("/api/settings/agent", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ globalCap: cap }),
      });
      if (!agentRes.ok) {
        const body = await agentRes.json();
        throw new Error(body.error ?? "Agent settings save failed");
      }
      // Save primary provider (with secrets)
      const providerRes = await fetch("/api/settings/provider", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          scope: "primary",
          provider: primary.provider,
          model: primary.model,
          enabled: true,
          config: primary.config,
          secrets: primary.secrets,
        }),
      });
      const providerBody = await providerRes.json();
      if (!providerRes.ok) {
        throw new Error(providerBody.error ?? "Primary provider save failed");
      }
      toast.success("Agent and provider saved.");
      setStep(step + 1);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setPrimaryError(msg);
    } finally {
      setSavingPrimary(false);
    }
  }

  // ---------------- Step 3 actions ----------------

  async function saveResearchOnly() {
    setSavingResearch(true);
    try {
      const body = researchEnabled
        ? {
            scope: "research",
            provider: research.provider,
            model: research.model,
            enabled: true,
            config: research.config,
            secrets: research.secrets,
          }
        : {
            scope: "research",
            provider: "disabled",
            model: "",
            enabled: false,
            config: {},
            secrets: {},
          };
      // Gate: validate the research key with a real one-shot call before
      // saving — only when enabled. Skipping the test for the explicit
      // "disabled" path lets users finish setup without a Perplexity key.
      if (researchEnabled) {
        const testRes = await fetch("/api/settings/provider/test", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        const testBody = await testRes.json().catch(() => ({}));
        if (!testRes.ok) {
          throw new Error(testBody.error ?? "Research provider test failed.");
        }
      }
      const res = await fetch("/api/settings/provider", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const json = await res.json();
      if (!res.ok) throw new Error(json.error ?? "Save failed");
    } finally {
      setSavingResearch(false);
    }
  }

  async function finish(skipResearch: boolean) {
    setFinishing(true);
    setResearchError(null);
    try {
      if (!skipResearch) await saveResearchOnly();
      else {
        // When skipping, persist the explicit "disabled" state so the
        // worker won't try to chain industry research later.
        await fetch("/api/settings/provider", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            scope: "research",
            provider: "disabled",
            model: "",
            enabled: false,
            config: {},
            secrets: {},
          }),
        });
      }

      const res = await fetch("/admin/settings/finish", { method: "POST" });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? "Finish failed");
      toast.success("Setup complete. Now describe your business.");
      // Hard navigation: router.push reuses Next's RSC client cache, which
      // may still have the pre-finish /onboarding response (where it
      // redirected back to /admin/settings because setup_complete_at was null).
      // window.location.assign forces a fresh server fetch.
      window.location.assign("/onboarding");
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setResearchError(msg);
      setFinishing(false);
    }
  }

  // ---------------- Render ----------------

  return (
    <EntryShell
      className="setup-entry-shell"
      title="Connect the operating system."
      description="Configure storage, data access, and the agent runtime. Business onboarding begins when this infrastructure check is complete."
      steps={STEPS}
      currentStep={step}
    >
      {stepName === "password" && (
        <Step
          title="Choose a database password"
          description="OpenNeko's storage ships with a default password. Pick something only you know. You won't need to enter it again."
        >
          <Field
            label="New password (min 8 chars)"
            htmlFor="setup-new-password"
          >
            <Input
              id="setup-new-password"
              type="password"
              value={newPassword}
              autoComplete="new-password"
              onChange={(e) => setNewPassword(e.target.value)}
            />
          </Field>
          <Field
            label="Confirm password"
            htmlFor="setup-confirm-password"
            error={
              confirmPassword.length > 0 && confirmPassword !== newPassword
                ? "Passwords don't match."
                : undefined
            }
          >
            <Input
              id="setup-confirm-password"
              type="password"
              value={confirmPassword}
              autoComplete="new-password"
              onChange={(e) => setConfirmPassword(e.target.value)}
            />
          </Field>
          <div className="flex justify-end gap-2.5 mt-5 max-[720px]:flex-col max-[720px]:items-stretch [&>button]:max-[720px]:w-full">
            <Button
              variant="primary"
              onClick={savePasswordAndAdvance}
              disabled={
                savingPassword ||
                newPassword.length < 8 ||
                newPassword !== confirmPassword
              }
            >
              {savingPassword ? "Saving…" : "Continue"}
            </Button>
          </div>
        </Step>
      )}

      {stepName === "data" && (
        <Step
          title="Connect your data"
          description="Graphjin server endpoint OpenNeko should connect to."
        >
          <Field
            label="GraphJin URL *"
            htmlFor="setup-graphjin-url"
            hint="Just the base URL. OpenNeko handles the GraphQL and MCP endpoints automatically."
          >
            <Input
              id="setup-graphjin-url"
              value={data.rootUrl}
              placeholder="http://localhost:8080"
              onChange={(e) => {
                setDataError(null);
                setData((p) => ({ ...p, rootUrl: e.target.value }));
              }}
            />
          </Field>
          <Field label="Label" htmlFor="setup-data-label">
            <Input
              id="setup-data-label"
              value={data.label}
              placeholder="primary"
              onChange={(e) =>
                setData((p) => ({ ...p, label: e.target.value }))
              }
            />
          </Field>
          <InlineError message={dataError} />
          <div className="flex justify-end gap-2.5 mt-5 max-[720px]:flex-col max-[720px]:items-stretch [&>button]:max-[720px]:w-full">
            <Button
              onClick={testData}
              disabled={testingData || !data.rootUrl.trim()}
            >
              {testingData ? "Testing…" : "Test connection"}
            </Button>
            <Button
              variant="primary"
              onClick={saveDataAndAdvance}
              disabled={savingData || !data.rootUrl.trim()}
            >
              {savingData ? "Saving…" : "Continue"}
            </Button>
          </div>
        </Step>
      )}

      {stepName === "agent" && (
        <Step
          title="Configure the agent"
          description="Choose the model provider the agent uses."
        >
          <div className="settings-grid">
            <Field label="Provider">
              <Select
                value={primary.provider}
                onChange={onPrimaryProviderChange}
                options={providerOptions}
                ariaLabel="Primary provider"
              />
            </Field>
            <Field label="Model" htmlFor="setup-primary-model">
              <Input
                id="setup-primary-model"
                value={primary.model}
                onChange={(e) =>
                  setPrimary((p) => ({ ...p, model: e.target.value }))
                }
              />
            </Field>
          </div>

          {primaryFields.map((field) => (
            <ProviderFieldInput
              key={field.key}
              field={field}
              value={
                field.kind === "secret"
                  ? (primary.secrets[field.key] ?? "")
                  : ((primary.config[field.key] as string) ?? "")
              }
              onChange={(v) => {
                if (field.kind === "secret") {
                  setPrimary((p) => ({
                    ...p,
                    secrets: { ...p.secrets, [field.key]: v },
                  }));
                } else {
                  setPrimary((p) => ({
                    ...p,
                    config: { ...p.config, [field.key]: v },
                  }));
                }
              }}
            />
          ))}

          <Field
            label="Concurrent jobs"
            htmlFor="setup-concurrent-jobs"
            hint="How many metric jobs the worker runs in parallel. Worker restart applies changes."
          >
            <Input
              id="setup-concurrent-jobs"
              type="number"
              min={1}
              max={1000}
              value={concurrentJobs}
              onChange={(e) => setConcurrentJobs(e.target.value)}
            />
          </Field>

          <InlineError message={primaryError} />

          <div className="flex justify-end gap-2.5 mt-5 max-[720px]:flex-col max-[720px]:items-stretch [&>button]:max-[720px]:w-full">
            <Button onClick={() => setStep(step - 1)} disabled={savingPrimary}>
              Back
            </Button>
            <Button
              variant="primary"
              onClick={savePrimaryAndAdvance}
              disabled={savingPrimary}
            >
              {savingPrimary ? "Validating & saving…" : "Continue"}
            </Button>
          </div>
        </Step>
      )}

      {stepName === "research" && (
        <Step
          title="Research (optional)"
          description="Lets the system pull industry context from Perplexity once your business team submits the onboarding profile. Leave the toggle off to set this up later."
        >
          <div className="mt-[18px]">
            <Checkbox
              label="Enable industry research"
              checked={researchEnabled}
              onCheckedChange={(checked) =>
                setResearchEnabled(checked === true)
              }
            />
          </div>

          {researchEnabled && (
            <>
              <div className="settings-grid">
                <Field label="Provider">
                  <Select
                    value={research.provider}
                    onChange={(v) =>
                      setResearch({
                        provider: v,
                        model: initial.providers.defaults.research[v] ?? "",
                        config: {},
                        secrets: {},
                      })
                    }
                    options={initial.providers.options.research.filter(
                      (option) => option.value !== "disabled",
                    )}
                    ariaLabel="Research provider"
                  />
                </Field>
                <Field label="Model" htmlFor="setup-research-model">
                  <Input
                    id="setup-research-model"
                    value={research.model}
                    onChange={(e) =>
                      setResearch((p) => ({ ...p, model: e.target.value }))
                    }
                  />
                </Field>
              </div>

              {researchFields.map((field) => (
                <ProviderFieldInput
                  key={field.key}
                  field={field}
                  value={
                    field.kind === "secret"
                      ? (research.secrets[field.key] ?? "")
                      : ((research.config[field.key] as string) ?? "")
                  }
                  onChange={(v) => {
                    if (field.kind === "secret") {
                      setResearch((p) => ({
                        ...p,
                        secrets: { ...p.secrets, [field.key]: v },
                      }));
                    } else {
                      setResearch((p) => ({
                        ...p,
                        config: { ...p.config, [field.key]: v },
                      }));
                    }
                  }}
                />
              ))}
            </>
          )}

          <InlineError message={researchError} />

          <div className="flex justify-end gap-2.5 mt-5 max-[720px]:flex-col max-[720px]:items-stretch [&>button]:max-[720px]:w-full">
            <Button
              onClick={() => setStep(step - 1)}
              disabled={finishing || savingResearch}
            >
              Back
            </Button>
            <Button
              variant="primary"
              onClick={() => finish(!researchEnabled)}
              disabled={finishing || savingResearch}
            >
              {finishing || savingResearch ? "Saving…" : "Finish setup"}
            </Button>
          </div>
        </Step>
      )}
    </EntryShell>
  );
}

function Step({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">{title}</h2>
          <p className="settings-card-copy">{description}</p>
        </div>
      </div>
      <div className="grid gap-4 mt-4">{children}</div>
    </section>
  );
}

function InlineError({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <div
      role="alert"
      className="rounded-2xl px-4 py-3.5 mt-3.5 text-ui-body leading-[1.5] bg-danger-soft border border-danger/30 text-danger"
    >
      {message}
    </div>
  );
}

function ProviderFieldInput({
  field,
  value,
  onChange,
}: {
  field: ProviderField;
  value: string;
  onChange: (v: string) => void;
}) {
  const id = `setup-provider-${field.key}`;
  return (
    <Field
      label={`${field.label}${field.required ? " *" : ""}`}
      htmlFor={id}
      hint={field.help}
    >
      <Input
        id={id}
        type={field.kind === "secret" ? "password" : "text"}
        value={value}
        placeholder={field.placeholder}
        autoComplete={field.kind === "secret" ? "off" : undefined}
        spellCheck={false}
        onChange={(e) => onChange(e.target.value)}
      />
    </Field>
  );
}

function stringRecord(input: Record<string, unknown>): Record<string, string> {
  return Object.fromEntries(
    Object.entries(input).map(([k, v]) => [k, v == null ? "" : String(v)]),
  );
}

const GRAPHQL_SUFFIX = "/api/v1/graphql";
const MCP_SUFFIX = "/api/v1/mcp";

function deriveEndpoints(rootUrl: string): {
  graphqlUrl: string;
  mcpUrl: string;
} {
  const root = deriveRoot(rootUrl);
  return {
    graphqlUrl: `${root}${GRAPHQL_SUFFIX}`,
    mcpUrl: `${root}${MCP_SUFFIX}`,
  };
}

// Accept whatever the user pastes — bare root, trailing slash, or a full
// GraphJin endpoint URL — and reduce it to a clean root so deriveEndpoints
// can append the canonical suffixes without doubling them.
function deriveRoot(input: string): string {
  let s = input.trim().replace(/\/+$/, "");
  const lower = s.toLowerCase();
  if (lower.endsWith(GRAPHQL_SUFFIX)) s = s.slice(0, -GRAPHQL_SUFFIX.length);
  else if (lower.endsWith(MCP_SUFFIX)) s = s.slice(0, -MCP_SUFFIX.length);
  return s.replace(/\/+$/, "");
}

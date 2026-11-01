"use client";

import { useMemo, useState } from "react";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import { toast } from "sonner";
import Select from "@/components/Select";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/field";
import GraphjinAgentSection, { type GraphjinAgentPayload } from "./GraphjinAgentSection";

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
  options: { primary: readonly ProviderOption[]; research: readonly ProviderOption[] };
  defaults: { primary: Record<string, string>; research: Record<string, string> };
  fields: { primary: Record<string, ProviderField[]>; research: Record<string, ProviderField[]> };
};
type AgentSettingsPayload = {
  agent: {
    source: "org" | "default";
    globalCap: number;
  };
  defaults: { globalCap: number };
};

export default function AgentForm({
  initial,
}: {
  initial: { agent: AgentSettingsPayload; providers: SettingsPayload; graphjinAgent: GraphjinAgentPayload };
}) {
  const [concurrentJobs, setConcurrentJobs] = useState(String(initial.agent.agent.globalCap));
  const [primary, setPrimary] = useState({
    provider: initial.providers.primary.provider,
    model: initial.providers.primary.model,
    config: stringRecord(initial.providers.primary.config),
    secretStatus: initial.providers.primary.secretStatus,
    secretsInput: {} as Record<string, string>,
    clearedSecrets: {} as Record<string, boolean>,
  });
  const [saving, setSaving] = useState(false);

  const providerOptions = useMemo(
    () => initial.providers.options.primary,
    [initial.providers.options.primary],
  );

  const fields: ProviderField[] = initial.providers.fields.primary[primary.provider] ?? [];

  const onPrimaryProviderChange = (next: string) => {
    setPrimary({
      provider: next,
      model: initial.providers.defaults.primary[next] ?? "",
      config: {},
      secretStatus: {},
      secretsInput: {},
      clearedSecrets: {},
    });
  };

  async function save() {
    setSaving(true);
    try {
      const secretsPayload: Record<string, string | null> = {};
      const configPayload: Record<string, string> = {};
      for (const field of fields) {
        if (field.kind === "secret") {
          const input = primary.secretsInput[field.key]?.trim();
          if (input) secretsPayload[field.key] = input;
          else if (primary.clearedSecrets[field.key]) secretsPayload[field.key] = null;
        } else {
          configPayload[field.key] = primary.config[field.key] ?? "";
        }
      }

      const providerRes = await fetch("/api/settings/provider", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          scope: "primary",
          provider: primary.provider,
          model: primary.model,
          enabled: true,
          config: configPayload,
          secrets: secretsPayload,
        }),
      });
      const providerBody = await providerRes.json();
      if (!providerRes.ok) throw new Error(providerBody.error ?? "Primary provider save failed");

      const cap = Number(concurrentJobs) || initial.agent.defaults.globalCap;
      const agentRes = await fetch("/api/settings/agent", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ globalCap: cap }),
      });
      const agentBody = await agentRes.json();
      if (!agentRes.ok) throw new Error(agentBody.error ?? "Agent settings save failed");

      toast.success("Agent settings saved.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="root is-narrow">
      <AppHeader back={{ href: "/admin/settings", label: "Settings" }} />
      <PageHeading
        title="Agent"
        description="Choose the agent's model provider and worker concurrency for the whole organization."
      />

      <section className="settings-card">
        <div className="grid gap-4">
          <div className="settings-grid">
            <Field label="Provider">
              <Select
                id="agent-provider"
                value={primary.provider}
                onChange={onPrimaryProviderChange}
                options={providerOptions}
                ariaLabel="Primary provider"
              />
            </Field>
            <Field label="Model" htmlFor="agent-model">
              <Input
                id="agent-model"
                value={primary.model}
                onChange={(e) => setPrimary((p) => ({ ...p, model: e.target.value }))}
              />
            </Field>
          </div>

          <Field
            label="Concurrent jobs"
            htmlFor="agent-concurrent-jobs"
            hint="How many metric jobs the worker runs in parallel. Worker restart applies changes."
          >
            <Input
              id="agent-concurrent-jobs"
              type="number"
              min={1}
              max={1000}
              value={concurrentJobs}
              onChange={(e) => setConcurrentJobs(e.target.value)}
            />
          </Field>

          {fields.map((field) => {
            const masked = primary.secretStatus[field.key];
            const isSecret = field.kind === "secret";
            const value = isSecret
              ? primary.secretsInput[field.key] ?? ""
              : (primary.config[field.key] as string) ?? "";

            return (
              <Field
                key={field.key}
                label={`${field.label}${field.required ? " *" : ""}`}
                htmlFor={`agent-field-${field.key}`}
                hint={field.help}
              >
                <Input
                  id={`agent-field-${field.key}`}
                  type={field.kind === "secret" ? "password" : "text"}
                  value={value}
                  placeholder={field.placeholder}
                  autoComplete={isSecret ? "off" : undefined}
                  spellCheck={false}
                  onChange={(e) => {
                    if (isSecret) {
                      setPrimary((p) => ({
                        ...p,
                        secretsInput: { ...p.secretsInput, [field.key]: e.target.value },
                        clearedSecrets: { ...p.clearedSecrets, [field.key]: false },
                      }));
                    } else {
                      setPrimary((p) => ({
                        ...p,
                        config: { ...p.config, [field.key]: e.target.value },
                      }));
                    }
                  }}
                />
                {isSecret && masked && !primary.clearedSecrets[field.key] && (
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-text3 text-ui-body-sm">Saved: {masked}</span>
                    <Button
                      type="button"
                      variant="danger"
                      size="sm"
                      onClick={() =>
                        setPrimary((p) => ({
                          ...p,
                          secretsInput: { ...p.secretsInput, [field.key]: "" },
                          clearedSecrets: { ...p.clearedSecrets, [field.key]: true },
                        }))
                      }
                    >
                      Clear saved value
                    </Button>
                  </div>
                )}
              </Field>
            );
          })}
        </div>
        <div className="flex justify-end gap-2.5 mt-5 max-[720px]:flex-col max-[720px]:items-stretch [&>button]:max-[720px]:w-full">
          <Button variant="primary" onClick={save} disabled={saving}>
            {saving ? "Saving…" : "Save settings"}
          </Button>
        </div>
      </section>
      <GraphjinAgentSection initial={initial.graphjinAgent} />
    </div>
  );
}

function stringRecord(input: Record<string, unknown>): Record<string, string> {
  return Object.fromEntries(
    Object.entries(input).map(([k, v]) => [k, v == null ? "" : String(v)]),
  );
}

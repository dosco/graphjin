"use client";

import { useState } from "react";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
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

export default function ResearchForm({
  initial,
}: {
  initial: SettingsPayload;
}) {
  const initialResearch = initial.research;
  const [enabled, setEnabled] = useState(
    initialResearch.enabled && initialResearch.provider !== "disabled",
  );
  const initialProvider =
    initialResearch.provider === "disabled"
      ? (initial.options.research.find((o) => o.value !== "disabled")?.value ??
        "perplexity")
      : initialResearch.provider;
  const [research, setResearch] = useState({
    provider: initialProvider,
    model:
      initialResearch.provider === "disabled"
        ? (initial.defaults.research[initialProvider] ?? "")
        : initialResearch.model,
    config: stringRecord(initialResearch.config),
    secretStatus: initialResearch.secretStatus,
    secretsInput: {} as Record<string, string>,
    clearedSecrets: {} as Record<string, boolean>,
  });
  const [saving, setSaving] = useState(false);

  const fields: ProviderField[] =
    initial.fields.research[research.provider] ?? [];
  const providerOptions = initial.options.research.filter(
    (o) => o.value !== "disabled",
  );

  async function save() {
    setSaving(true);
    try {
      const body = enabled
        ? {
            scope: "research",
            provider: research.provider,
            model: research.model,
            enabled: true,
            config: research.config,
            secrets: secretsPayload(),
          }
        : {
            scope: "research",
            provider: "disabled",
            model: "",
            enabled: false,
            config: {},
            secrets: {},
          };
      const res = await fetch("/api/settings/provider", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const json = await res.json();
      if (!res.ok) throw new Error(json.error ?? "Save failed");
      toast.success(
        enabled ? "Research enabled and saved." : "Research disabled.",
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  function secretsPayload(): Record<string, string | null> {
    const out: Record<string, string | null> = {};
    for (const field of fields) {
      if (field.kind !== "secret") continue;
      const input = research.secretsInput[field.key]?.trim();
      if (input) out[field.key] = input;
      else if (research.clearedSecrets[field.key]) out[field.key] = null;
    }
    return out;
  }

  return (
    <div className="root is-narrow">
      <AppHeader back={{ href: "/admin/settings", label: "Settings" }} />
      <PageHeading
        title="Industry research"
        description="Optionally enrich the business profile with current industry context during onboarding."
      />

      <section className="settings-card">
        <div>
          <Checkbox
            label="Enable industry research"
            checked={enabled}
            onCheckedChange={(checked) => setEnabled(checked === true)}
          />
        </div>

        {enabled && (
          <div className="grid gap-4 mt-4">
            <div className="settings-grid">
              <Field label="Provider">
                <Select
                  id="research-provider"
                  value={research.provider}
                  onChange={(v) =>
                    setResearch({
                      provider: v,
                      model: initial.defaults.research[v] ?? "",
                      config: {},
                      secretStatus: {},
                      secretsInput: {},
                      clearedSecrets: {},
                    })
                  }
                  options={providerOptions}
                  ariaLabel="Research provider"
                />
              </Field>
              <Field label="Model" htmlFor="research-model">
                <Input
                  id="research-model"
                  value={research.model}
                  onChange={(e) =>
                    setResearch((p) => ({ ...p, model: e.target.value }))
                  }
                />
              </Field>
            </div>

            {fields.map((field) => {
              const masked = research.secretStatus[field.key];
              const isSecret = field.kind === "secret";
              const value = isSecret
                ? (research.secretsInput[field.key] ?? "")
                : ((research.config[field.key] as string) ?? "");

              return (
                <Field
                  key={field.key}
                  label={`${field.label}${field.required ? " *" : ""}`}
                  htmlFor={`research-field-${field.key}`}
                  hint={field.help}
                >
                  <Input
                    id={`research-field-${field.key}`}
                    type={field.kind === "secret" ? "password" : "text"}
                    value={value}
                    placeholder={field.placeholder}
                    autoComplete={isSecret ? "off" : undefined}
                    spellCheck={false}
                    onChange={(e) => {
                      if (isSecret) {
                        setResearch((p) => ({
                          ...p,
                          secretsInput: {
                            ...p.secretsInput,
                            [field.key]: e.target.value,
                          },
                          clearedSecrets: {
                            ...p.clearedSecrets,
                            [field.key]: false,
                          },
                        }));
                      } else {
                        setResearch((p) => ({
                          ...p,
                          config: { ...p.config, [field.key]: e.target.value },
                        }));
                      }
                    }}
                  />
                  {isSecret &&
                    masked &&
                    !research.clearedSecrets[field.key] && (
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-text3 text-ui-body-sm">
                          Saved: {masked}
                        </span>
                        <Button
                          type="button"
                          variant="danger"
                          size="sm"
                          onClick={() =>
                            setResearch((p) => ({
                              ...p,
                              secretsInput: {
                                ...p.secretsInput,
                                [field.key]: "",
                              },
                              clearedSecrets: {
                                ...p.clearedSecrets,
                                [field.key]: true,
                              },
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
        )}

        <div className="flex justify-end gap-2.5 mt-5 max-[720px]:flex-col max-[720px]:items-stretch [&>button]:max-[720px]:w-full">
          <Button variant="primary" onClick={save} disabled={saving}>
            {saving ? "Saving…" : "Save settings"}
          </Button>
        </div>
      </section>
    </div>
  );
}

function stringRecord(input: Record<string, unknown>): Record<string, string> {
  return Object.fromEntries(
    Object.entries(input).map(([k, v]) => [k, v == null ? "" : String(v)]),
  );
}

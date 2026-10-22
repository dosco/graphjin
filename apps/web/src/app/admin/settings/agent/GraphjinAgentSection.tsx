"use client";

import { useState } from "react";
import { toast } from "sonner";
import Select from "@/components/Select";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Input } from "@/components/ui/field";

type Option = { value: string; label: string; description: string };
type ProviderField = {
  key: string;
  label: string;
  kind: "text" | "secret" | "url";
  required?: boolean;
  placeholder?: string;
  help?: string;
};
export type GraphjinAgentPayload = {
  settings: {
    enabled: boolean;
    reusePrimary: boolean;
    provider: string;
    model: string;
    config: Record<string, unknown>;
    secretStatus: Record<string, string>;
  };
  options: readonly Option[];
  fields: Record<string, ProviderField[]>;
  defaults: Record<string, string>;
};

const MODEL_CHOICES = [
  { value: "primary", label: "Use the primary provider and model", description: "" },
  { value: "custom", label: "Use a different provider and model", description: "" },
] as const;

export default function GraphjinAgentSection({ initial }: { initial: GraphjinAgentPayload }) {
  const [enabled, setEnabled] = useState(initial.settings.enabled);
  const [reusePrimary, setReusePrimary] = useState(initial.settings.reusePrimary);
  const [provider, setProvider] = useState(initial.settings.provider);
  const [model, setModel] = useState(initial.settings.model);
  const [config, setConfig] = useState<Record<string, string>>(
    Object.fromEntries(Object.entries(initial.settings.config).map(([k, v]) => [k, v == null ? "" : String(v)])),
  );
  const [secretStatus, setSecretStatus] = useState(initial.settings.secretStatus);
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const fields = initial.fields[provider] ?? [];

  const onProviderChange = (next: string) => {
    setProvider(next);
    setModel(initial.defaults[next] ?? "");
    setConfig({});
    setSecrets({});
    setSecretStatus({});
  };

  async function save() {
    setSaving(true);
    try {
      const res = await fetch("/api/settings/graphjin-agent", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(
          reusePrimary
            ? { enabled, reusePrimary }
            : {
                enabled,
                reusePrimary,
                provider,
                model,
                config: Object.fromEntries(fields.filter((f) => f.kind !== "secret").map((f) => [f.key, config[f.key] ?? ""])),
                secrets: Object.fromEntries(Object.entries(secrets).filter(([, v]) => v.trim())),
              },
        ),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? "GraphJin agent settings save failed");
      setSecretStatus(body.secretStatus ?? {});
      setSecrets({});
      toast.success("GraphJin agent settings saved.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="settings-card mt-5" aria-labelledby="graphjin-agent-heading">
      <h2 id="graphjin-agent-heading" className="font-display text-ui-subsection font-bold">
        GraphJin data agent
      </h2>
      <p className="mt-1 text-ui-body-sm text-text3">
        Every agent run gets its data by asking GraphJin&apos;s agent. It applies to the whole organization.
      </p>
      <p className="mt-1 text-ui-body-sm text-text3">
        Query GraphJin directly only when OpenNeko connects to an external GraphJin whose agent isn&apos;t set up. Agents then
        write their own queries with GraphJin&apos;s tools.
      </p>
      <div className="mt-4">
        <Checkbox
          label="Query GraphJin directly instead"
          checked={!enabled}
          onCheckedChange={(checked) => setEnabled(checked !== true)}
        />
      </div>

      {enabled && (
        <div className="grid gap-4 mt-4">
          <Field label="Model for the GraphJin agent">
            <Select
              id="graphjin-agent-model-source"
              value={reusePrimary ? "primary" : "custom"}
              onChange={(v) => setReusePrimary(v === "primary")}
              options={MODEL_CHOICES}
              ariaLabel="Model for the GraphJin agent"
            />
          </Field>

          {!reusePrimary && (
            <>
              <div className="settings-grid">
                <Field label="Provider">
                  <Select
                    id="graphjin-agent-provider"
                    value={provider}
                    onChange={onProviderChange}
                    options={initial.options}
                    ariaLabel="GraphJin agent provider"
                  />
                </Field>
                <Field label="Model" htmlFor="graphjin-agent-model">
                  <Input id="graphjin-agent-model" value={model} onChange={(e) => setModel(e.target.value)} />
                </Field>
              </div>
              {fields.map((field) => {
                const isSecret = field.kind === "secret";
                return (
                  <Field
                    key={field.key}
                    label={`${field.label}${field.required ? " *" : ""}`}
                    htmlFor={`graphjin-agent-field-${field.key}`}
                    hint={isSecret && secretStatus[field.key] ? `Saved: ${secretStatus[field.key]}` : field.help}
                  >
                    <Input
                      id={`graphjin-agent-field-${field.key}`}
                      type={isSecret ? "password" : "text"}
                      value={isSecret ? secrets[field.key] ?? "" : config[field.key] ?? ""}
                      placeholder={field.placeholder}
                      autoComplete={isSecret ? "off" : undefined}
                      spellCheck={false}
                      onChange={(e) =>
                        isSecret
                          ? setSecrets((s) => ({ ...s, [field.key]: e.target.value }))
                          : setConfig((c) => ({ ...c, [field.key]: e.target.value }))
                      }
                    />
                  </Field>
                );
              })}
            </>
          )}
        </div>
      )}

      <div className="flex justify-end gap-2.5 mt-5 max-[720px]:flex-col max-[720px]:items-stretch [&>button]:max-[720px]:w-full">
        <Button variant="primary" onClick={save} disabled={saving}>
          {saving ? "Saving…" : "Save GraphJin agent"}
        </Button>
      </div>
    </section>
  );
}

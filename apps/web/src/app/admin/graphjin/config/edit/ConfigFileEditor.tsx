"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ActionGroup } from "@/components/ui/action-group";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Disclosure } from "@/components/ui/disclosure";
import { Field } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { JsonBlock, SettingsCard } from "../../GraphjinCards";

type Loaded = { path: string; text: string; hash: string; hidden: number };
type Check = {
  valid: boolean;
  reloadMode: string | null;
  changes: unknown;
  findings: unknown;
  errors: string[];
};

async function post(body: Record<string, unknown>) {
  const response = await fetch("/api/admin/graphjin/config-file", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const json = (await response.json().catch(() => null)) as Record<string, unknown> | null;
  return { ok: response.ok, status: response.status, json };
}

function hasEntries(value: unknown): boolean {
  return Array.isArray(value) ? value.length > 0 : Boolean(value && typeof value === "object" && Object.keys(value).length);
}

export default function ConfigFileEditor({ initial }: { initial: Loaded }) {
  const [base, setBase] = useState(initial);
  const [text, setText] = useState(initial.text);
  const [check, setCheck] = useState<{ text: string; result: Check } | null>(null);
  const [busy, setBusy] = useState<"check" | "apply" | null>(null);
  const [error, setError] = useState<{ message: string; stale: boolean } | null>(null);

  const dirty = text !== base.text;
  const checked = check?.text === text ? check.result : null;
  const canApply = dirty && checked?.valid === true && busy === null;

  async function runCheck() {
    setBusy("check");
    setError(null);
    try {
      const { ok, status, json } = await post({ action: "check", text, baseHash: base.hash });
      if (!ok || !json) {
        setError({ message: String(json?.error ?? "GraphJin could not check the config."), stale: status === 409 });
        return;
      }
      setCheck({ text, result: json as unknown as Check });
    } finally {
      setBusy(null);
    }
  }

  async function apply() {
    setBusy("apply");
    setError(null);
    try {
      const { ok, status, json } = await post({ action: "apply", text, baseHash: base.hash });
      if (!ok || !json) {
        setError({ message: String(json?.error ?? "GraphJin could not apply the config."), stale: status === 409 });
        return;
      }
      const saved = json as unknown as Loaded;
      setBase(saved);
      setText(saved.text);
      setCheck(null);
      toast.success("GraphJin is using the new config.");
    } finally {
      setBusy(null);
    }
  }

  async function reload() {
    setError(null);
    const response = await fetch("/api/admin/graphjin/config-file");
    const json = (await response.json().catch(() => null)) as Loaded | null;
    if (response.ok && json) {
      setBase(json);
      setText(json.text);
      setCheck(null);
    } else {
      setError({ message: "Could not load the config file.", stale: false });
    }
  }

  return (
    <>
      <SettingsCard
        title="Config file"
        copy={
          base.hidden > 0
            ? `Secrets show as <hidden>. Leave them as they are to keep them, or type a new value to replace one. ${base.hidden} ${base.hidden === 1 ? "value is" : "values are"} hidden.`
            : "Edit the file, check the changes with GraphJin, then apply them. GraphJin picks them up without a restart."
        }
      >
        <Field label="agentic.yml" htmlFor="graphjin-config-text" hint={base.path}>
          <Textarea
            id="graphjin-config-text"
            value={text}
            onChange={(event) => setText(event.target.value)}
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            rows={24}
            wrap="off"
            className="font-mono text-xs leading-5"
          />
        </Field>
        <ActionGroup>
          <Button variant="ghost" disabled={!dirty || busy !== null} onClick={() => { setText(base.text); setCheck(null); }}>
            Discard edits
          </Button>
          <Button variant="secondary" disabled={!dirty || busy !== null || checked !== null} onClick={runCheck}>
            {busy === "check" ? "Checking…" : "Check changes"}
          </Button>
          <Button variant="primary" disabled={!canApply} onClick={apply}>
            {busy === "apply" ? "Applying…" : "Apply changes"}
          </Button>
        </ActionGroup>
      </SettingsCard>

      {error ? (
        <SettingsCard
          title={error.stale ? "The file changed" : "Not applied"}
          copy={error.message}
          aside={<Badge variant="danger">{error.stale ? "Out of date" : "Failed"}</Badge>}
        >
          {error.stale ? (
            <ActionGroup align="start">
              <Button size="sm" onClick={reload}>
                Load the current file
              </Button>
            </ActionGroup>
          ) : null}
        </SettingsCard>
      ) : null}

      {checked ? (
        checked.valid ? (
          <SettingsCard
            title="GraphJin accepts these changes"
            copy={
              checked.reloadMode === "restart"
                ? "Some of them take effect only the next time GraphJin starts. The rest apply right away."
                : "They apply right away when you apply them."
            }
            aside={<Badge variant="success">Ready to apply</Badge>}
          >
            {hasEntries(checked.changes) || hasEntries(checked.findings) ? (
              <Disclosure title="What changes" meta="Details from GraphJin">
                <JsonBlock value={{ changes: checked.changes, findings: checked.findings }} />
              </Disclosure>
            ) : null}
          </SettingsCard>
        ) : (
          <SettingsCard
            title="GraphJin cannot use this config"
            aside={<Badge variant="danger">Not valid</Badge>}
          >
            <ul className="list-disc pl-5 text-ui-body-sm text-text2">
              {(checked.errors.length ? checked.errors : ["The config is not valid."]).map((message) => (
                <li key={message}>{message}</li>
              ))}
            </ul>
          </SettingsCard>
        )
      ) : null}
    </>
  );
}

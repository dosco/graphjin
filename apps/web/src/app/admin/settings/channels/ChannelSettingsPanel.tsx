"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";
import { adminApi } from "@/components/admin/admin-api";
import { ActionGroup } from "@/components/ui/action-group";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FieldInput } from "@/components/ui/field";
import type { ChannelSettings } from "@/lib/channel-admin";

type Notice = { tone: "ok" | "error"; text: string } | null;

export default function ChannelSettingsPanel() {
  const [channels, setChannels] = useState<ChannelSettings[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = useCallback(async () => {
    const result = await adminApi<{ channels: ChannelSettings[] }>("/api/admin/channels/settings");
    if (result.ok) {
      setChannels(result.body.channels);
      setLoadError(null);
    } else {
      setLoadError(result.error);
    }
  }, []);

  useEffect(() => {
    const initial = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(initial);
  }, [load]);

  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div className="min-w-0">
          <h2 className="settings-card-title">Channel settings</h2>
          <p className="settings-card-copy">
            Enter the tokens each channel needs. OpenNeko stores secrets
            encrypted and never shows them again. A channel delivers and
            listens only while an installed pack turns it on.
          </p>
        </div>
      </div>
      {loadError ? (
        <p className="border-t border-border py-5 text-sm text-danger">Could not load channel settings: {loadError}</p>
      ) : channels === null ? (
        <p className="border-t border-border py-5 text-sm text-text3">Loading channel settings…</p>
      ) : (
        <div className="border-t border-border">
          {channels.map((channel) => (
            <div key={channel.id} className="border-b border-border py-5 last:border-b-0">
              <ChannelSettingsForm key={formKey(channel)} channel={channel} onSaved={load} />
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

/** A new key after a save remounts the form, so it shows the stored state. */
function formKey(channel: ChannelSettings): string {
  return `${channel.id}:${channel.fields.map((f) => `${f.key}=${f.set ? (f.value ?? "*") : ""}`).join("|")}`;
}

function ChannelSettingsForm({ channel, onSaved }: { channel: ChannelSettings; onSaved: () => Promise<void> }) {
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(channel.fields.map((f) => [f.key, f.secret ? "" : (f.value ?? "")])),
  );
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<Notice>(null);

  const changes: Record<string, string | null> = {};
  for (const field of channel.fields) {
    const next = (values[field.key] ?? "").trim();
    if (field.secret) {
      if (next) changes[field.key] = next;
    } else if (next !== (field.value ?? "")) {
      if (next) changes[field.key] = next;
      else if (!field.required && field.set) changes[field.key] = null;
    }
  }
  const changed = Object.keys(changes).length > 0;

  const save = async (next: Record<string, string | null>) => {
    setBusy(true);
    setStatus(null);
    const result = await adminApi("/api/admin/channels/settings", "POST", { channel: channel.id, values: next });
    setBusy(false);
    if (!result.ok) {
      setStatus({ tone: "error", text: result.error });
      return;
    }
    setStatus({ tone: "ok", text: "Saved" });
    await onSaved();
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (changed) void save(changes);
  };

  return (
    <form onSubmit={submit}>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h3 className="font-display text-base font-bold text-text">{channel.providerLabel}</h3>
        {!channel.enabled ? (
          <Badge variant="muted">Off</Badge>
        ) : channel.missing.length > 0 ? (
          <Badge variant="watch">Needs {channel.missing.length} {channel.missing.length === 1 ? "setting" : "settings"}</Badge>
        ) : (
          <Badge variant="success">On</Badge>
        )}
      </div>
      {channel.fields.length === 0 ? (
        <p className="text-sm text-text3">OpenNeko sets this channel up when a pack turns it on.</p>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {channel.fields.map((field) => (
            <div key={field.key} className="grid min-w-0 content-start gap-2">
              <FieldInput
                label={`${field.key}${field.required ? "" : " (optional)"}`}
                hint={field.description}
                type={field.secret ? "password" : "text"}
                autoComplete="off"
                spellCheck={false}
                value={values[field.key] ?? ""}
                placeholder={field.secret ? (field.set ? "Stored. Enter a new value to replace it." : "Not set") : ""}
                onChange={(event) => setValues((current) => ({ ...current, [field.key]: event.target.value }))}
              />
              {field.secret && field.set && !field.required ? (
                <div>
                  <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => void save({ [field.key]: null })}>
                    Remove stored value
                  </Button>
                </div>
              ) : null}
            </div>
          ))}
        </div>
      )}
      {channel.fields.length > 0 ? (
        <ActionGroup className="mt-4">
          <Button type="submit" variant="primary" disabled={busy || !changed}>
            {busy ? "Saving…" : "Save settings"}
          </Button>
          {status ? (
            <span role="status" className={`text-sm ${status.tone === "error" ? "text-danger" : "text-text2"}`}>
              {status.text}
            </span>
          ) : null}
        </ActionGroup>
      ) : null}
    </form>
  );
}

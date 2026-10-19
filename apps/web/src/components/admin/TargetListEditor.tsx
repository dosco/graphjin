"use client";

import { useId, useState, type FormEvent } from "react";
import { ActionGroup } from "@/components/ui/action-group";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Textarea } from "@/components/ui/field";

type SaveResult = { ok: true } | { ok: false; error: string };

/** Edits one target list, typed one pattern per line. Used by rules. */
export function TargetListEditor({
  label,
  hint,
  placeholder,
  patterns,
  onMiss,
  onSave,
}: {
  label: string;
  hint: string;
  placeholder: string;
  patterns: string[];
  /** Shows the "ask for other targets" choice when set. */
  onMiss?: "next" | "deny";
  onSave: (patterns: string, onMiss: "next" | "deny") => Promise<SaveResult>;
}) {
  const id = useId();
  const [saved, setSaved] = useState({ text: patterns.join("\n"), askOthers: onMiss !== "deny" });
  const [text, setText] = useState(saved.text);
  const [askOthers, setAskOthers] = useState(saved.askOthers);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<{ tone: "ok" | "error"; text: string } | null>(null);
  const changed = text.trim() !== saved.text.trim() || (onMiss !== undefined && askOthers !== saved.askOthers);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setStatus(null);
    const result = await onSave(text, askOthers ? "next" : "deny");
    setBusy(false);
    if (result.ok) setSaved({ text, askOthers });
    setStatus(result.ok ? { tone: "ok", text: "Saved" } : { tone: "error", text: result.error });
  };

  return (
    <form onSubmit={submit} className="grid gap-3">
      <Field label={label} hint={hint} htmlFor={id}>
        <Textarea
          id={id}
          rows={4}
          value={text}
          placeholder={placeholder}
          spellCheck={false}
          autoComplete="off"
          onChange={(event) => setText(event.target.value)}
        />
      </Field>
      {onMiss !== undefined ? (
        <Checkbox
          label="Ask for approval when a target is not on the list. When off, OpenNeko denies it."
          checked={askOthers}
          onCheckedChange={(value) => setAskOthers(value === true)}
        />
      ) : null}
      <ActionGroup>
        <Button type="submit" variant="secondary" disabled={busy || !changed}>
          {busy ? "Saving…" : "Save list"}
        </Button>
        {status ? (
          <span role="status" className={`text-sm ${status.tone === "error" ? "text-danger" : "text-text2"}`}>
            {status.text}
          </span>
        ) : null}
      </ActionGroup>
    </form>
  );
}

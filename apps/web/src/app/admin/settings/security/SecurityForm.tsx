"use client";

import { useState, type ReactNode } from "react";
import { toast } from "sonner";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

type InstallPolicy = {
  allowGitUrlInstalls: boolean;
};

type InstallPolicyPayload = {
  policy: InstallPolicy;
  source: "org" | "default";
};

export default function SecurityForm({
  initial,
}: {
  initial: InstallPolicyPayload;
}) {
  const [policy, setPolicy] = useState<InstallPolicy>(initial.policy);
  const [saving, setSaving] = useState(false);

  async function save() {
    setSaving(true);
    try {
      const res = await fetch("/api/settings/install-policy", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(policy),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as {
          error?: string;
        } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      toast.success("Install policy saved.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <div className="root is-narrow">
        <AppHeader back={{ href: "/admin/settings", label: "Settings" }}>
          <SectionNav current="admin" />
        </AppHeader>

        <PageHeading
          title="Security"
          description="Set the trust floor for skill installs. Every exception widens the agent’s install surface."
        />

        <section className="settings-card flex flex-col gap-6">
          <Toggle
            label="Allow community-skill installs from git URLs"
            help={
              <>
                Lets operators run <Command>openneko install &lt;git-url&gt;</Command> to pull a
                skill directly from GitHub, GitLab, or Codeberg. Skills are procedural knowledge
                the agent follows with its own tools. OpenNeko runs no code from a skill.
              </>
            }
            checked={policy.allowGitUrlInstalls}
            onChange={(v) => setPolicy({ allowGitUrlInstalls: v })}
          />

          <div className="flex justify-end mt-2">
            <Button type="button" variant="primary" onClick={save} disabled={saving}>
              {saving ? "Saving…" : "Save settings"}
            </Button>
          </div>
        </section>
      </div>

      <CreatorCredit />
    </>
  );
}

function Toggle({
  label,
  help,
  checked,
  onChange,
}: {
  label: string;
  help: ReactNode;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="grid gap-1.5">
      <Checkbox
        label={label}
        checked={checked}
        onCheckedChange={(checked) => onChange(checked === true)}
      />
      <p className="pl-6 text-ui-caption leading-[var(--leading-compact)] text-text3">
        {help}
      </p>
    </div>
  );
}

function Command({ children }: { children: ReactNode }) {
  return (
    <code className="rounded-[4px] bg-neutral px-1 py-0.5 font-mono text-ui-caption text-text2">
      {children}
    </code>
  );
}

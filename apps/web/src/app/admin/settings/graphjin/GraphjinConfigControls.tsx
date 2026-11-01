"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";

type GraphjinConfigSettings = {
  sourceConfigEnabled: boolean;
};

export default function GraphjinConfigControls({
  initial,
}: {
  initial: GraphjinConfigSettings;
}) {
  const router = useRouter();
  const [enabled, setEnabled] = useState(initial.sourceConfigEnabled);
  const [saving, setSaving] = useState(false);

  async function toggle(next: boolean) {
    const previous = enabled;
    setEnabled(next);
    setSaving(true);
    try {
      const res = await fetch("/api/settings/graphjin-config", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sourceConfigEnabled: next }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as {
          error?: string;
        } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      toast.success(
        next
          ? "GraphJin config tools enabled for admins."
          : "GraphJin config tools disabled.",
      );
      router.refresh();
    } catch (e) {
      setEnabled(previous);
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Ask-based config</h2>
          <p className="settings-card-copy">
            Admin Ask sessions can inspect GraphJin source mode and file
            approved source, role, and access updates.
          </p>
        </div>
        <Badge variant={enabled ? "success" : "muted"}>
          {saving ? "Saving" : enabled ? "Enabled" : "Disabled"}
        </Badge>
      </div>

      <div className="mt-4">
        <Checkbox
          checked={enabled}
          disabled={saving}
          onCheckedChange={(checked) => void toggle(checked === true)}
          label="Enable GraphJin config tools for admin Ask sessions and approved MCP action execution."
        />
      </div>
    </section>
  );
}

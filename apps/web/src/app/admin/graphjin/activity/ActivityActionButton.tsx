"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Button, type ButtonVariant } from "@/components/ui/button";

export default function ActivityActionButton({
  action,
  id,
  label,
  success,
  variant = "secondary",
}: {
  action: "mark_seen" | "approve" | "demote";
  id: string;
  label: string;
  success: string;
  variant?: ButtonVariant;
}) {
  const router = useRouter();
  const [pending, setPending] = useState(false);

  async function run() {
    setPending(true);
    try {
      const res = await fetch("/api/admin/graphjin/activity", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action, id }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      toast.success(success);
      router.refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setPending(false);
    }
  }

  return (
    <Button size="sm" variant={variant} disabled={pending} onClick={() => void run()}>
      {label}
    </Button>
  );
}

"use client";

import { useEffect, useState } from "react";
import type { LucideIcon } from "lucide-react";
import {
  BookOpen,
  Brain,
  CheckCircle2,
  Home,
  LibraryBig,
  MessageCircle,
  MonitorPlay,
  Plug,
  Puzzle,
  Settings,
} from "lucide-react";

export type NavDestination = {
  href: string;
  label: string;
  shortLabel: string;
  description?: string;
  icon: LucideIcon;
  group?: "primary" | "knowledge" | "workspace";
};

export const PRIMARY_NAV: NavDestination[] = [
  {
    href: "/",
    label: "Briefing",
    shortLabel: "Briefing",
    description: "Today, decisions, and signals",
    icon: Home,
    group: "primary",
  },
  {
    href: "/work",
    label: "Ask",
    shortLabel: "Ask",
    description: "Ask OpenNeko anything",
    icon: MessageCircle,
    group: "primary",
  },
  {
    href: "/workflows",
    label: "Workflows",
    shortLabel: "Workflows",
    description: "Watchers and automation",
    icon: MonitorPlay,
    group: "primary",
  },
  {
    href: "/actions",
    label: "Approvals",
    shortLabel: "Approvals",
    description: "Approvals and receipts",
    icon: CheckCircle2,
    group: "primary",
  },
];

export const SECONDARY_NAV: NavDestination[] = [
  {
    href: "/business-profile",
    label: "Business profile",
    shortLabel: "Profile",
    description: "How your business runs",
    icon: BookOpen,
    group: "knowledge",
  },
  {
    href: "/memory",
    label: "Memory",
    shortLabel: "Memory",
    description: "Pinned facts and learned rules",
    icon: Brain,
    group: "knowledge",
  },
  {
    href: "/library",
    label: "Library",
    shortLabel: "Library",
    description: "Documents distilled into knowledge",
    icon: LibraryBig,
    group: "knowledge",
  },
  {
    href: "/skills",
    label: "Skills",
    shortLabel: "Skills",
    description: "What OpenNeko can do",
    icon: Puzzle,
    group: "knowledge",
  },
  {
    href: "/integrations",
    label: "Integrations",
    shortLabel: "Connect",
    description: "Slack, Gmail, Stripe, and more",
    icon: Plug,
    group: "workspace",
  },
  {
    href: "/admin",
    label: "Admin",
    shortLabel: "Admin",
    description: "Users, model, and data sources",
    icon: Settings,
    group: "workspace",
  },
];

export const ALL_NAV = [...PRIMARY_NAV, ...SECONDARY_NAV];

export function isActive(pathname: string, base: string) {
  if (base === "/") return pathname === "/";
  return pathname === base || pathname.startsWith(`${base}/`);
}

export function hideAppChrome(pathname: string) {
  return (
    pathname.startsWith("/signin") ||
    pathname.startsWith("/onboarding") ||
    isActive(pathname, "/chat")
  );
}

export function useApprovalsCount(enabled = true) {
  const visualTest =
    process.env.NODE_ENV !== "production" &&
    process.env.NEXT_PUBLIC_RECORDS_VISUAL_TEST === "true";
  const [pending, setPending] = useState(visualTest ? 3 : 0);

  useEffect(() => {
    if (!enabled || visualTest) return;
    let cancelled = false;
    const tick = async () => {
      try {
        const res = await fetch("/api/approvals?countOnly=true", {
          cache: "no-store",
        });
        if (cancelled || !res.ok) return;
        const data = (await res.json()) as { count?: number };
        setPending(data.count ?? 0);
      } catch {
        // Best effort; the badge simply stays at its last known value.
      }
    };

    void tick();
    const id = setInterval(tick, 30_000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [enabled, visualTest]);

  return pending;
}

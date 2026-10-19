import type { ReactNode } from "react";
import Link from "next/link";
import { GRAPHJIN_NO_SOURCE_ERROR } from "@/lib/graphjin-admin";
import { AdminShell } from "../AdminShell";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { buttonClassName } from "@/components/ui/button";

export const GRAPHJIN_SECTIONS = [
  { href: "/admin/graphjin", label: "Status" },
  { href: "/admin/graphjin/workbench", label: "Workbench" },
  { href: "/admin/graphjin/catalog", label: "Catalog" },
  { href: "/admin/graphjin/security", label: "Security" },
  { href: "/admin/graphjin/code", label: "Code" },
  { href: "/admin/graphjin/api", label: "API" },
  { href: "/admin/graphjin/evals", label: "Evals" },
  { href: "/admin/graphjin/activity", label: "Activity" },
  { href: "/admin/graphjin/config", label: "Config" },
] as const;

export type GraphjinSection = (typeof GRAPHJIN_SECTIONS)[number]["href"];

/** AdminShell plus the GraphJin section links shared by every console page. */
export function GraphjinShell({
  current,
  title,
  subtitle,
  children,
}: {
  current: GraphjinSection;
  title: string;
  subtitle: string;
  children: ReactNode;
}) {
  return (
    <AdminShell
      title={title}
      subtitle={subtitle}
      back={
        current === "/admin/graphjin"
          ? { href: "/admin", label: "Admin" }
          : { href: "/admin/graphjin", label: "GraphJin" }
      }
    >
      <nav aria-label="GraphJin sections" className="mb-5 flex flex-wrap gap-2">
        {GRAPHJIN_SECTIONS.map((section) => (
          <Link
            key={section.href}
            href={section.href}
            aria-current={section.href === current ? "page" : undefined}
            className={buttonClassName({
              size: "sm",
              variant: section.href === current ? "primary" : "secondary",
            })}
          >
            {section.label}
          </Link>
        ))}
      </nav>
      {children}
    </AdminShell>
  );
}

export function SettingsCard({
  title,
  copy,
  aside,
  children,
}: {
  title: string;
  copy?: ReactNode;
  aside?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div className="min-w-0">
          <h2 className="settings-card-title">{title}</h2>
          {copy ? <p className="settings-card-copy">{copy}</p> : null}
        </div>
        {aside}
      </div>
      {children}
    </section>
  );
}

/** A GraphJin read that failed: no source, no permission, or unreachable. */
export function GraphjinUnavailable({ title, error }: { title: string; error: string }) {
  return (
    <SettingsCard title={title} copy={error} aside={<Badge variant="danger">Unavailable</Badge>}>
      {error === GRAPHJIN_NO_SOURCE_ERROR ? (
        <Link href="/admin/settings/data" className={buttonClassName({ size: "sm" })}>
          Add a data source
        </Link>
      ) : null}
    </SettingsCard>
  );
}

export function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="max-h-[520px] overflow-auto whitespace-pre-wrap break-words rounded-inner bg-neutral-soft p-3 font-mono text-xs leading-5 text-text">
      <code>{typeof value === "string" ? value : JSON.stringify(value, null, 2)}</code>
    </pre>
  );
}

const STATUS_VARIANTS: Record<string, BadgeVariant> = {
  ready: "success",
  ok: "success",
  passed: "success",
  enabled: "success",
  allow: "success",
  approved: "success",
  verified: "success",
  closed: "muted",
  low: "muted",
  info: "muted",
  medium: "watch",
  warn: "watch",
  warning: "watch",
  degraded: "watch",
  pending: "watch",
  open: "watch",
  verifying: "watch",
  observed: "watch",
  paused: "watch",
  high: "danger",
  critical: "danger",
  error: "danger",
  failed: "danger",
  deny: "danger",
  disabled: "danger",
  stopped: "danger",
};

export function StatusBadge({ status }: { status: string | null | undefined }) {
  const label = status?.trim() || "unknown";
  return <Badge variant={STATUS_VARIANTS[label.toLowerCase()] ?? "muted"}>{label}</Badge>;
}

export function formatKind(value: string): string {
  return value
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

/** Read one string search param; Next passes repeated keys as arrays. */
export function firstParam(value: string | string[] | undefined): string {
  return (Array.isArray(value) ? value[0] : value)?.trim() ?? "";
}

"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { useApprovalsCount } from "@/lib/nav";

export default function SectionNav({
  current,
  children,
}: {
  current:
    | "dashboard"
    | "workflows"
    | "work"
    | "actions"
    | "admin"
    | "business-profile";
  children?: ReactNode;
}) {
  // The shared hook owns polling and deterministic visual-test state.
  const pendingApprovals = useApprovalsCount();

  return (
    <nav className="topbar-nav">
      <Link
        href="/"
        className={`topbar-nav-link${current === "dashboard" ? " is-active" : ""}`}
      >
        Briefing
      </Link>
      <Link
        href="/work"
        className={`topbar-nav-link${current === "work" ? " is-active" : ""}`}
      >
        Ask
      </Link>
      <Link
        href="/workflows"
        className={`topbar-nav-link${current === "workflows" ? " is-active" : ""}`}
      >
        Workflows
      </Link>
      <Link
        href="/actions"
        className={`topbar-nav-link${current === "actions" ? " is-active" : ""}`}
      >
        Actions
        {pendingApprovals > 0 && (
          <span className="nav-link-badge tabular-nums">{pendingApprovals}</span>
        )}
      </Link>
      <Link
        href="/business-profile"
        className={`topbar-nav-link${current === "business-profile" ? " is-active" : ""}`}
      >
        Business profile
      </Link>
      <Link
        href="/admin"
        className={`topbar-nav-link${current === "admin" ? " is-active" : ""}`}
      >
        Admin
      </Link>
      {children}
    </nav>
  );
}

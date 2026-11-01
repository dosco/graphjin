"use client";

import Link from "next/link";
import Image from "next/image";
import { useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";
import DensityToggle from "@/components/DensityToggle";
import { Button } from "@/components/ui/button";

const MARKETING_URL = "https://openneko.app";
const APP_VERSION = process.env.NEXT_PUBLIC_APP_VERSION ?? "0.0.0";
const VERSION_POLL_MS = 60_000;

export type AppHeaderProps = {
  back?: { href: string; label: string };
  children?: React.ReactNode;
};

// Single top bar matching the dense mockups: brand + version pill (left),
// section nav, then the density toggle (right). The "update available" state
// folds into the version pill so the bar stays mockup-faithful.
export default function AppHeader({ back, children }: AppHeaderProps) {
  const [latestVersion, setLatestVersion] = useState<string | null>(null);
  const [user, setUser] = useState<{ email: string } | null>(null);

  useEffect(() => {
    let cancelled = false;
    const check = async () => {
      try {
        const res = await fetch("/api/version", { cache: "no-store" });
        if (!res.ok) return;
        const data = (await res.json()) as { version?: string };
        if (cancelled) return;
        if (typeof data.version === "string") setLatestVersion(data.version);
      } catch {
        // best-effort
      }
    };
    void check();
    const id = setInterval(check, VERSION_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/auth/session", { cache: "no-store" });
        if (cancelled || !res.ok) return;
        const data = (await res.json()) as {
          user: { id: string; email: string; name: string | null } | null;
          authEnabled?: boolean;
        };
        if (data.user && data.authEnabled !== false) setUser({ email: data.user.email });
      } catch {
        // best-effort
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  async function handleSignOut() {
    try {
      await fetch("/api/auth/logout", { method: "POST" });
    } finally {
      window.location.href = "/signin";
    }
  }

  const updateAvailable =
    latestVersion !== null && latestVersion !== APP_VERSION;
  const isNavOnly = !back;
  const hasContext = Boolean(back);

  return (
    <header
      className={`app-header${isNavOnly ? " is-nav-only" : ""}${hasContext ? " has-context" : ""}`}
    >
      <div className="topbar-inner">
        <a
          className="topbar-brand"
          href={MARKETING_URL}
          target="_blank"
          rel="noreferrer"
          aria-label="OpenNeko — open marketing site in a new tab"
        >
          <Image
            className="topbar-logo"
            src="/cat.png"
            alt=""
            width={22}
            height={23}
          />
          <span className="topbar-name">OpenNeko</span>
        </a>

        {updateAvailable ? (
          <Button
            variant="ghost"
            type="button"
            className="topbar-ver is-update"
            onClick={() => window.location.reload()}
            aria-label={`Update available: v${latestVersion}. Reload to apply.`}
            title={`v${latestVersion} available — reload`}
          >
            <span className="topbar-ver-dot" aria-hidden="true" />v
            {latestVersion} · reload
          </Button>
        ) : (
          <span className="topbar-ver" title="OpenNeko version">
            {APP_VERSION}
          </span>
        )}

        {back && (
          <Link className="settings-backlink" href={back.href}>
            <ArrowLeft
              size={14}
              strokeWidth={2.25}
              aria-hidden="true"
              className="settings-backlink-arrow"
            />
            <span>{back.label}</span>
          </Link>
        )}

        {children}

        <span className="topbar-spacer" />

        <DensityToggle />

        <a
          className="topbar-credit"
          href="https://openneko.app/#about"
          target="_blank"
          rel="noreferrer"
        >
          Built by Amit Deshmukh ↗
        </a>

        {user && (
          <Button
            variant="ghost"
            type="button"
            onClick={handleSignOut}
            aria-label={`Sign out ${user.email}`}
            title={user.email}
            className="topbar-signout"
          >
            Sign out
          </Button>
        )}
      </div>
    </header>
  );
}

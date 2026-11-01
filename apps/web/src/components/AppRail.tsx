"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { LogIn, LogOut, MessageCircle, Search } from "lucide-react";
import { openCommandBar } from "@/components/CommandBar";
import { Button } from "@/components/ui/button";
import {
  ALL_NAV,
  hideAppChrome,
  isActive,
  useApprovalsCount,
  type NavDestination,
} from "@/lib/nav";

const APP_VERSION = process.env.NEXT_PUBLIC_APP_VERSION ?? "0.0.0";
const VERSION_POLL_MS = 60_000;
const VISUAL_TEST =
  process.env.NODE_ENV !== "production" &&
  process.env.NEXT_PUBLIC_VISUAL_TEST === "true";

type SessionUser = {
  email: string;
  name: string | null;
};

export type RailIdentity = {
  user: SessionUser | null;
  mode: "solo" | "admin" | "member";
  signedIn: boolean;
};

function initials(user: SessionUser | null) {
  const source = user?.name || user?.email || "A";
  return (
    source
      .split(/[\s@._-]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase())
      .join("") || "A"
  );
}

function RailLink({
  item,
  active,
  pending,
}: {
  item: NavDestination;
  active: boolean;
  pending: number;
}) {
  const Icon = item.icon;
  const isActions = item.href === "/actions";

  return (
    <div className="app-rail-link-wrap">
      <Link
        href={item.href}
        className={`app-rail-link${active ? " is-active" : ""}`}
        aria-current={active ? "page" : undefined}
        title={item.label}
      >
        <Icon aria-hidden="true" strokeWidth={2} />
        <span className="app-rail-label">{item.label}</span>
        <span className="app-rail-short">{item.shortLabel}</span>
        {isActions && pending > 0 && (
          <span
            className="app-rail-badge tabular-nums"
            aria-label={`${pending} pending`}
          >
            {pending}
          </span>
        )}
      </Link>
    </div>
  );
}

/**
 * `initial` comes from the server layout, so the name and Sign out paint
 * with the first frame. The client fetch only refreshes them.
 */
export default function AppRail({ initial }: { initial?: RailIdentity }) {
  const pathname = usePathname() ?? "/";
  const hidden = hideAppChrome(pathname);
  const pending = useApprovalsCount();
  const navRef = useRef<HTMLElement>(null);
  const [user, setUser] = useState<SessionUser | null>(
    VISUAL_TEST
      ? { email: "kavya@example.com", name: "Kavya M." }
      : (initial?.user ?? null),
  );
  const [sessionMode, setSessionMode] = useState<
    "loading" | "solo" | "admin" | "member"
  >(VISUAL_TEST ? "member" : (initial?.mode ?? "loading"));
  // A visitor holding a session cookie can always sign out, whatever
  // the worker reports about the sign-in provider.
  const [signedIn, setSignedIn] = useState(Boolean(initial?.signedIn));
  const [latestVersion, setLatestVersion] = useState<string | null>(null);

  const grouped = useMemo(() => {
    const visible = ALL_NAV.filter(
      (item) =>
        item.href !== "/admin" ||
        (sessionMode !== "loading" && sessionMode !== "member"),
    );
    return {
      primary: visible.filter((n) => n.group === "primary"),
      knowledge: visible.filter((n) => n.group === "knowledge"),
      workspace: visible.filter((n) => n.group === "workspace"),
    };
  }, [sessionMode]);

  useEffect(() => {
    if (hidden || VISUAL_TEST) return;
    let cancelled = false;
    const load = async () => {
      try {
        const res = await fetch("/api/auth/session", { cache: "no-store" });
        if (cancelled || !res.ok) return;
        const data = (await res.json()) as {
          user: { id: string; email: string; name: string | null } | null;
          authEnabled?: boolean;
          signedIn?: boolean;
          role: "admin" | "member" | null;
        };
        setSignedIn(Boolean(data.signedIn));
        if (data.user) {
          setUser({ email: data.user.email, name: data.user.name });
          setSessionMode(data.authEnabled === false ? "solo" : data.role === "member" ? "member" : "admin");
        } else {
          setSessionMode("solo");
        }
      } catch {
        // Keep admin-only navigation hidden when identity cannot be resolved.
      }
    };
    void load();
    // Re-check periodically: the identity fetch is one-shot otherwise, and a
    // worker blip at page load would leave the profile/signout block missing
    // until a full reload.
    const id = setInterval(load, 30_000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [hidden]);

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      navRef.current
        ?.querySelector<HTMLElement>('[aria-current="page"]')
        ?.scrollIntoView({ block: "nearest", inline: "nearest" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [pathname, sessionMode]);

  useEffect(() => {
    if (hidden || VISUAL_TEST) return;
    let cancelled = false;
    const check = async () => {
      try {
        const res = await fetch("/api/version", { cache: "no-store" });
        if (!res.ok) return;
        const data = (await res.json()) as { version?: string };
        if (!cancelled && typeof data.version === "string") {
          setLatestVersion(data.version);
        }
      } catch {
        // Best effort.
      }
    };
    void check();
    const id = setInterval(check, VERSION_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [hidden]);

  async function handleSignOut() {
    try {
      await fetch("/api/auth/logout", { method: "POST" });
    } finally {
      window.location.href = "/signin";
    }
  }

  if (hidden) return null;

  const updateAvailable =
    latestVersion !== null && latestVersion !== APP_VERSION;

  return (
    <aside className="app-rail-wrap" aria-label="Primary navigation">
      <div className="app-rail-brand">
        <a
          className="app-rail-brand-link"
          href="https://openneko.app"
          target="_blank"
          rel="noopener noreferrer"
          aria-label={`OpenNeko ${APP_VERSION}: open the website in a new tab`}
          title={`OpenNeko ${APP_VERSION}`}
        >
          <Image
            className="app-rail-logo"
            src="/cat.png"
            alt=""
            width={26}
            height={27}
          />
          <span className="app-rail-name">OpenNeko</span>
        </a>
        {!VISUAL_TEST && updateAvailable ? (
          <Button
            variant="ghost"
            type="button"
            className="app-rail-version is-update"
            onClick={() => window.location.reload()}
            title={`Version ${latestVersion} is ready. Reload to update.`}
          >
            Update
          </Button>
        ) : null}
      </div>

      {!VISUAL_TEST && (
        <Button
          variant="ghost"
          type="button"
          className="app-rail-search"
          onClick={openCommandBar}
          title="Search or jump to a page (⌘K)"
        >
          <Search aria-hidden="true" strokeWidth={2} />
          <span className="app-rail-label">Search</span>
          <kbd className="app-rail-kbd">⌘K</kbd>
        </Button>
      )}

      <nav ref={navRef} className="app-rail-nav">
        <div className="app-rail-group">
          <div className="app-rail-link-wrap">
            <Link href="/chat" className="app-rail-link" title="Back to chats">
              <MessageCircle aria-hidden="true" strokeWidth={2} />
              <span className="app-rail-label">Back to chats</span>
              <span className="app-rail-short">Chats</span>
            </Link>
          </div>
          {grouped.primary.map((item) => (
            <RailLink
              key={item.href}
              item={item}
              active={isActive(pathname, item.href)}
              pending={pending}
            />
          ))}
        </div>

        {!VISUAL_TEST && (
          <>
            <div className="app-rail-heading">Knowledge</div>
            <div className="app-rail-group">
              {grouped.knowledge.map((item) => (
                <RailLink
                  key={item.href}
                  item={item}
                  active={isActive(pathname, item.href)}
                  pending={pending}
                />
              ))}
            </div>

            <div className="app-rail-group app-rail-group-quiet">
              {grouped.workspace.map((item) => (
                <RailLink
                  key={item.href}
                  item={item}
                  active={isActive(pathname, item.href)}
                  pending={pending}
                />
              ))}
            </div>
          </>
        )}
      </nav>

      <div className="app-rail-foot">
        {user ? (
          <div className="app-rail-user">
            {/* Two visible controls. Signing out never hides behind a menu. */}
            <Link
              href="/profile"
              className="app-rail-user-profile"
              aria-label={`Your profile: ${user.email}`}
            >
              <span className="app-rail-avatar" aria-hidden="true">
                {initials(user)}
              </span>
              <span className="app-rail-user-copy">
                <span className="app-rail-user-name">
                  {user.name || user.email}
                </span>
                <span className="app-rail-user-email">{user.email}</span>
              </span>
            </Link>
            {signedIn && (
              <Button
                variant="ghost"
                type="button"
                className="app-rail-signout"
                onClick={() => void handleSignOut()}
                aria-label="Sign out"
                title="Sign out"
              >
                <LogOut aria-hidden="true" strokeWidth={2} />
              </Button>
            )}
          </div>
        ) : signedIn === false && sessionMode !== "solo" ? (
          <div className="app-rail-user">
            {/* The session ended. Offer the way back in, never a blank row. */}
            <Link href="/signin" className="app-rail-user-profile">
              <span className="app-rail-avatar" aria-hidden="true">
                <LogIn aria-hidden="true" strokeWidth={2} />
              </span>
              <span className="app-rail-user-copy">
                <span className="app-rail-user-name">Sign in</span>
              </span>
            </Link>
          </div>
        ) : null}
      </div>
    </aside>
  );
}

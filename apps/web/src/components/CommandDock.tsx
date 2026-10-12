"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { ChevronRight, Ellipsis, LogOut, MessageCircle, UserRound, X } from "lucide-react";
import DensityToggle from "@/components/DensityToggle";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  PRIMARY_NAV,
  SECONDARY_NAV,
  hideAppChrome,
  isActive,
  useApprovalsCount,
  type NavDestination,
} from "@/lib/nav";

type OpenSheet = "more" | null;

const dockDestination = (href: string) => {
  const destination = PRIMARY_NAV.find((item) => item.href === href);
  if (!destination) throw new Error(`Missing dock destination: ${href}`);
  return destination;
};

const CHATS: NavDestination = { href: "/chat", label: "Back to chats", shortLabel: "Chats", icon: MessageCircle };
const DASHBOARD = dockDestination("/");
const WORKFLOWS = dockDestination("/workflows");
const ACTIONS = dockDestination("/actions");

function DockLink({
  item,
  pathname,
  pending = 0,
  onNavigate,
}: {
  item: NavDestination;
  pathname: string;
  pending?: number;
  onNavigate: () => void;
}) {
  const active = isActive(pathname, item.href);
  const Icon = item.icon;

  return (
    <Link
      href={item.href}
      className={`cdock-item${active ? " is-active" : ""}`}
      aria-current={active ? "page" : undefined}
      onClick={onNavigate}
    >
      <Icon aria-hidden="true" strokeWidth={1.9} />
      <span className="cdock-lbl">{item.shortLabel}</span>
      {item.href === "/actions" && pending > 0 ? (
        <span
          className="cdock-badge tabular-nums"
          aria-label={`${pending} pending`}
        >
          {pending > 99 ? "99+" : pending}
        </span>
      ) : null}
    </Link>
  );
}

export default function CommandDock() {
  const pathname = usePathname() ?? "/";
  const hidden = hideAppChrome(pathname);
  const pending = useApprovalsCount(!hidden);
  const [openSheet, setOpenSheet] = useState<OpenSheet>(null);
  const [session, setSession] = useState<{
    resolved: boolean;
    signedIn: boolean;
    role: "admin" | "member" | null;
  }>({ resolved: false, signedIn: false, role: null });
  useEffect(() => {
    if (hidden) return;
    let cancelled = false;
    (async () => {
      try {
        const response = await fetch("/api/auth/session", {
          cache: "no-store",
        });
        if (!response.ok || cancelled) return;
        const data = (await response.json()) as {
          user: { id: string } | null;
          role: "admin" | "member" | null;
        };
        setSession({
          resolved: true,
          signedIn: Boolean(data.user),
          role: data.role,
        });
      } catch {
        // Keep member-only controls absent until identity is known.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [hidden]);

  async function handleSignOut() {
    try {
      await fetch("/api/auth/logout", { method: "POST" });
    } finally {
      window.location.href = "/signin";
    }
  }

  function closeSheet() {
    setOpenSheet(null);
  }

  if (hidden) return null;

  const secondaryNav = SECONDARY_NAV.filter(
    (item) =>
      item.href !== "/admin" || (session.resolved && session.role !== "member"),
  );
  const moreActive = secondaryNav.some((item) => isActive(pathname, item.href));
  return (
    <Sheet
      open={openSheet === "more"}
      onOpenChange={(open) => setOpenSheet(open ? "more" : null)}
    >
      <div className="cdock-wrap">
        <SheetContent
          id={`cdock-${openSheet}-sheet`}
          className={`cdock-sheet is-${openSheet}`}
          side="bottom"
          showCloseButton={false}
          overlayClassName="cdock-scrim"
        >
          <div className="cdock-sheet-grab" aria-hidden="true" />
          <SheetHeader className="cdock-sheet-head">
            <div>
              <SheetTitle>More</SheetTitle>
              <SheetDescription>
                Knowledge, connections, and workspace settings.
              </SheetDescription>
            </div>
            <SheetClose asChild>
              <Button
                variant="ghost"
                type="button"
                className="cdock-sheet-close"
                aria-label="Close"
              >
                <X aria-hidden="true" strokeWidth={2} />
              </Button>
            </SheetClose>
          </SheetHeader>

          <>
            <nav className="cdock-sheet-list" aria-label="More destinations">
              {secondaryNav.map((item) => {
                const active = isActive(pathname, item.href);
                const Icon = item.icon;
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    onClick={closeSheet}
                    aria-current={active ? "page" : undefined}
                    className={`cdock-sheet-row${active ? " is-active" : ""}`}
                  >
                    <span className="cdock-sheet-icon">
                      <Icon aria-hidden="true" strokeWidth={1.9} />
                    </span>
                    <span className="cdock-sheet-copy">
                      <span className="cdock-sheet-label">{item.label}</span>
                      <span className="cdock-sheet-desc">
                        {item.description}
                      </span>
                    </span>
                    <ChevronRight
                      className="cdock-sheet-chevron"
                      aria-hidden="true"
                    />
                  </Link>
                );
              })}
            </nav>
            {session.signedIn ? (
              <Link
                href="/profile"
                onClick={closeSheet}
                className="cdock-persona-link"
              >
                <UserRound aria-hidden="true" strokeWidth={1.9} />
                <span>
                  <strong>Your profile</strong>
                  <small>Role and priorities</small>
                </span>
                <ChevronRight aria-hidden="true" />
              </Link>
            ) : null}
            <div className="cdock-sheet-settings">
              <span>Layout density</span>
              <DensityToggle />
            </div>
            {session.signedIn ? (
              <Button
                variant="ghost"
                type="button"
                className="cdock-sheet-out"
                onClick={handleSignOut}
              >
                <LogOut aria-hidden="true" strokeWidth={2} />
                <span>Sign out</span>
              </Button>
            ) : null}
          </>
        </SheetContent>

        <nav className="cdock" aria-label="Primary navigation">
          <DockLink item={CHATS} pathname={pathname} onNavigate={closeSheet} />
          <DockLink
            item={DASHBOARD}
            pathname={pathname}
            onNavigate={closeSheet}
          />
          <DockLink
            item={WORKFLOWS}
            pathname={pathname}
            onNavigate={closeSheet}
          />

          <DockLink
            item={ACTIONS}
            pathname={pathname}
            pending={pending}
            onNavigate={closeSheet}
          />

          <SheetTrigger asChild>
            <Button
              variant="ghost"
              type="button"
              className={`cdock-item${moreActive || openSheet === "more" ? " is-active" : ""}`}
            >
              <Ellipsis aria-hidden="true" strokeWidth={2.2} />
              <span className="cdock-lbl">More</span>
            </Button>
          </SheetTrigger>
        </nav>
      </div>
    </Sheet>
  );
}

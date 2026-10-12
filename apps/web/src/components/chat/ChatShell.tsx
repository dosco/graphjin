"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { ChevronLeft, LogOut, Plug, ShieldCheck } from "lucide-react";
import { NekoAvatar, type NekoStyle } from "@neko/avatars/react";
import type { RailIdentity } from "@/components/AppRail";
import { Button, ButtonLink } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { BotAvatar } from "./BotAvatar";
import { ChatSidebar } from "./ChatSidebar";
import { NekoStyleProvider, useNekoStyle } from "./neko-style";
import { BotsProvider, useBots } from "./useBots";

const STYLE_LABELS: Record<NekoStyle, string> = {
  robot: "Robot",
  flat: "Flat",
  ink: "Ink",
};

function initials(name: string) {
  return (
    name
      .split(/[\s@._-]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase())
      .join("") || "A"
  );
}

function BotChip({ threadId }: { threadId: string }) {
  const { bots } = useBots();
  const current = bots?.find((b) => b.threadId === threadId);
  if (!current) return <span className="chat-bot-chip" />;
  return (
    <span className="chat-bot-chip">
      <BotAvatar
        coat={current.coat}
        pattern={current.pattern}
        size={32}
        aria-hidden
      />
      {current.name}
    </span>
  );
}

function ProfileMenu({ identity }: { identity?: RailIdentity }) {
  const router = useRouter();
  const { nekoStyle, setNekoStyle } = useNekoStyle();
  const user = identity?.user;
  const label = user?.name || user?.email || "You";
  const isAdmin = identity?.mode === "solo" || identity?.mode === "admin";

  async function signOut() {
    try {
      await fetch("/api/auth/logout", { method: "POST" });
    } finally {
      window.location.href = "/signin";
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="chat-profile-trigger"
          aria-label={`Profile menu: ${label}`}
        >
          <span className="chat-profile-avatar" aria-hidden="true">
            {initials(label)}
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="chat-profile-menu">
        <DropdownMenuLabel className="chat-profile-who">
          <span className="chat-profile-name">{label}</span>
          {user?.name && user.email ? (
            <span className="chat-profile-email">{user.email}</span>
          ) : null}
        </DropdownMenuLabel>
        {isAdmin ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => router.push("/")}>
              <ShieldCheck aria-hidden="true" strokeWidth={2} />
              Admin
            </DropdownMenuItem>
          </>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => router.push("/chat/connectors")}>
          <Plug aria-hidden="true" strokeWidth={2} />
          Connectors
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>Neko style</DropdownMenuLabel>
        <DropdownMenuRadioGroup
          value={nekoStyle}
          onValueChange={(value) => setNekoStyle(value as NekoStyle)}
        >
          {(Object.keys(STYLE_LABELS) as NekoStyle[]).map((style) => (
            <DropdownMenuRadioItem key={style} value={style}>
              <NekoAvatar
                seed="neko"
                nekoStyle={style}
                size={22}
                animated={false}
                aria-hidden
              />
              {STYLE_LABELS[style]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        {identity?.signedIn ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              onSelect={() => void signOut()}
            >
              <LogOut aria-hidden="true" strokeWidth={2} />
              Sign out
            </DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export default function ChatShell({
  identity,
  children,
}: {
  identity?: RailIdentity;
  children: React.ReactNode;
}) {
  const pathname = usePathname() ?? "/chat";
  const inThread = pathname.startsWith("/chat/t");
  const inSubpage = inThread || pathname.startsWith("/chat/bots") || pathname.startsWith("/chat/connectors");
  const threadId = pathname.match(/^\/chat\/t\/([^/]+)/)?.[1] ?? null;

  return (
    <NekoStyleProvider>
      <BotsProvider>
        <div className="chat-frame">
          <ChatSidebar activeThreadId={threadId} />
          <div className="chat-shell">
            <header className="chat-topbar">
              {inSubpage ? (
                <ButtonLink
                  href="/chat"
                  variant="ghost"
                  size="icon"
                  className={inThread ? "chat-back-phone" : undefined}
                  aria-label="Back to chats"
                  title="Back to chats"
                >
                  <ChevronLeft aria-hidden="true" strokeWidth={2} />
                </ButtonLink>
              ) : (
                <Link href="/chat" className="chat-brand chat-brand-phone">
                  <Image src="/cat.png" alt="" width={24} height={25} />
                  <span>OpenNeko</span>
                </Link>
              )}
              {threadId ? <BotChip threadId={threadId} /> : null}
              <ProfileMenu identity={identity} />
            </header>
            {children}
          </div>
        </div>
      </BotsProvider>
    </NekoStyleProvider>
  );
}

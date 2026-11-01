"use client";

import Image from "next/image";
import Link from "next/link";
import { Plus } from "lucide-react";
import { ButtonLink } from "@/components/ui/button";
import { SkeletonList } from "@/components/ui/skeleton";
import { BotList } from "./BotList";
import { useBots } from "./useBots";

export const CHAT_DESKTOP_QUERY = "(min-width: 1025px)";

export function ChatSidebar({ activeThreadId }: { activeThreadId: string | null }) {
  const { bots } = useBots();

  return (
    <aside className="chat-sidebar" aria-label="Chats">
      <div className="chat-sidebar-head">
        <Link href="/chat" className="chat-brand">
          <Image src="/cat.png" alt="" width={24} height={25} />
          <span>OpenNeko</span>
        </Link>
        <ButtonLink href="/chat/bots/new" variant="ghost" size="icon" aria-label="New bot" title="New bot">
          <Plus aria-hidden="true" strokeWidth={2} />
        </ButtonLink>
      </div>
      {bots === null ? (
        <SkeletonList rows={3} variant="row" label="Loading bots" />
      ) : (
        <BotList bots={bots} activeThreadId={activeThreadId} compact />
      )}
    </aside>
  );
}

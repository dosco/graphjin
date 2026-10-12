"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { Plus } from "lucide-react";
import { ButtonLink } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty";
import { SkeletonList } from "@/components/ui/skeleton";
import { BotList } from "./BotList";
import { CHAT_DESKTOP_QUERY } from "./ChatSidebar";
import { useBots } from "./useBots";

export default function ChatHome() {
  const router = useRouter();
  const { bots, failed } = useBots();
  const latest = bots?.find((bot) => bot.threadId)?.threadId;

  useEffect(() => {
    if (latest && window.matchMedia(CHAT_DESKTOP_QUERY).matches) {
      router.replace(`/chat/t/${latest}`);
    }
  }, [latest, router]);

  return (
    <main className="chat-home">
      <div className="chat-home-head">
        <h1 className="chat-home-title">Bots</h1>
        <ButtonLink href="/chat/bots/new" variant="primary">
          <Plus aria-hidden="true" strokeWidth={2} />
          New bot
        </ButtonLink>
      </div>

      {failed ? (
        <EmptyState title="Bots did not load" body="Reload the page to try again." />
      ) : bots === null ? (
        <SkeletonList rows={3} variant="row" label="Loading bots" />
      ) : (
        <BotList bots={bots} />
      )}
    </main>
  );
}

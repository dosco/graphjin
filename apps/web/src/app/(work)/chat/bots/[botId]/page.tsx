import type { Metadata } from "next";
import BotInfo from "@/components/chat/BotInfo";
import { botShareMetadata } from "@/lib/share-metadata";

export async function generateMetadata({ params }: { params: Promise<{ botId: string }> }): Promise<Metadata> {
  return botShareMetadata((await params).botId);
}

export default async function BotInfoPage({ params }: { params: Promise<{ botId: string }> }) {
  const { botId } = await params;
  return <BotInfo botId={botId} />;
}

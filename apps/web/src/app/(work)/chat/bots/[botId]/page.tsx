import BotInfo from "@/components/chat/BotInfo";

export default async function BotInfoPage({ params }: { params: Promise<{ botId: string }> }) {
  const { botId } = await params;
  return <BotInfo botId={botId} />;
}

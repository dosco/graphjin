import ChatShell from "@/components/chat/ChatShell";
import { railIdentity } from "@/lib/rail-identity";

export default async function ChatLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const identity = await railIdentity();
  return <ChatShell identity={identity}>{children}</ChatShell>;
}

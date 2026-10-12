import type { Metadata } from "next";
import { chatShareMetadata } from "@/lib/share-metadata";

export async function generateMetadata({ params }: { params: Promise<{ threadId: string }> }): Promise<Metadata> {
  return chatShareMetadata((await params).threadId);
}

export default function ChatThreadPage() {
  return null;
}

import { notFound } from "next/navigation";
import AnswerPreview from "./preview-client";

export default async function AnswerPreviewPage({ searchParams }: { searchParams: Promise<{ f?: string }> }) {
  if (process.env.NODE_ENV === "production") notFound();
  const { f } = await searchParams;
  return <AnswerPreview fixture={f ?? "report"} />;
}

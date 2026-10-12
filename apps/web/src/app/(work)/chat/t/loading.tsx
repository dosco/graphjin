import { SkeletonList } from "@/components/ui/skeleton";

export default function ChatThreadLoading() {
  return (
    <div className="work-transcript">
      <SkeletonList rows={3} variant="row" label="Opening the chat" />
    </div>
  );
}

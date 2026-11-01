import { Suspense } from "react";
import WorkScreen from "./work-screen";

// WorkScreen lives in the layout (not the page) so navigating between
// threads — which only changes the [threadId] segment — doesn't unmount
// the transcript, EventSource, or composer state. The page files are
// intentionally near-empty; WorkScreen reads the active thread from
// `useParams()` and reacts in-place. Suspense wraps it because
// useSearchParams (read on mount to honor ?seed=) needs a boundary to
// avoid forcing client-side bailout on the static build.
export default function WorkLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <>
      <Suspense fallback={null}>
        <WorkScreen />
      </Suspense>
      {children}
    </>
  );
}

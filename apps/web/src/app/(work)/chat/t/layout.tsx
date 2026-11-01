import { Suspense } from "react";
import WorkScreen from "../../work/work-screen";

export default function ChatThreadLayout({
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

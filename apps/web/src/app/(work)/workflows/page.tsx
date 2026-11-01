import { Suspense } from "react";
import WorkflowsPage from "./WorkflowsPage";

export default function Page() {
  return (
    <Suspense fallback={null}>
      <WorkflowsPage />
    </Suspense>
  );
}

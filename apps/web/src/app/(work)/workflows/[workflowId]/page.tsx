import { redirect } from "next/navigation";

type RouteContext = {
  params: Promise<{ workflowId: string }>;
};

export default async function WorkflowDetailRedirect(context: RouteContext) {
  const { workflowId } = await context.params;
  redirect(`/workflows?id=${workflowId}`);
}

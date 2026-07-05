import { use } from "react";
import { WorkflowsPage } from "@/features/workflows/workflows-page";

export default function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = use(params);
  return <WorkflowsPage projectId={projectId} />;
}

import { WorkflowsPage } from "@/components/studio-pages";

export default async function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <WorkflowsPage projectId={projectId} />;
}

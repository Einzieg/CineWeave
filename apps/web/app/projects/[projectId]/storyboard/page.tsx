import { StoryboardPage } from "@/components/studio-pages";

export default async function Page({ params, searchParams }: { params: Promise<{ projectId: string }>; searchParams: Promise<{ requirementId?: string; shotId?: string }> }) {
  const [{ projectId }, query] = await Promise.all([params, searchParams]);
  return <StoryboardPage initialRequirementId={query.requirementId ?? ""} initialShotId={query.shotId ?? ""} projectId={projectId} />;
}

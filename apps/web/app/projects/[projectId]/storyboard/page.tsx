import { StoryboardPage } from "@/features/storyboard/storyboard-page";

export default async function Page({ params, searchParams }: { params: Promise<{ projectId: string }>; searchParams: Promise<{ shotId?: string; requirementId?: string }> }) {
  const [{ projectId }, query] = await Promise.all([params, searchParams]);
  return <StoryboardPage initialRequirementId={query.requirementId ?? ""} initialShotId={query.shotId ?? ""} projectId={projectId} />;
}

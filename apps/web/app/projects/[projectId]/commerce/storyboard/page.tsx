import { CommerceStoryboardPage } from "@/features/commerce/commerce-storyboard-page";

export default async function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <CommerceStoryboardPage projectId={projectId} />;
}

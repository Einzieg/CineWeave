import { CommerceVideoPage } from "@/features/commerce/commerce-video-page";

export default async function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <CommerceVideoPage projectId={projectId} />;
}

import { VideoPage } from "@/features/video/video-page";

export default async function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <VideoPage projectId={projectId} />;
}

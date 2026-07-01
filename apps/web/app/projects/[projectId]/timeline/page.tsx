import { ProjectTimelinePage } from "@/components/studio-pages";

export default async function Page({ params, searchParams }: { params: Promise<{ projectId: string }>; searchParams: Promise<{ clipId?: string; finalVideoId?: string }> }) {
  const [{ projectId }, query] = await Promise.all([params, searchParams]);
  return <ProjectTimelinePage initialClipId={query.clipId ?? ""} initialFinalVideoId={query.finalVideoId ?? ""} projectId={projectId} />;
}

import { TimelinePage } from "@/features/timeline/timeline-page";

export default async function Page({ params, searchParams }: { params: Promise<{ projectId: string }>; searchParams: Promise<{ clipId?: string; finalVideoId?: string }> }) {
  const [{ projectId }, query] = await Promise.all([params, searchParams]);
  return <TimelinePage initialClipId={query.clipId ?? ""} initialFinalVideoId={query.finalVideoId ?? ""} projectId={projectId} />;
}

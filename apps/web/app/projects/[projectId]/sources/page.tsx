import { SourcesPage } from "@/components/studio-pages";

export default async function Page({ params, searchParams }: { params: Promise<{ projectId: string }>; searchParams: Promise<{ sceneId?: string }> }) {
  const [{ projectId }, query] = await Promise.all([params, searchParams]);
  return <SourcesPage initialSceneId={query.sceneId ?? ""} projectId={projectId} />;
}

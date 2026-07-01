import { ProjectReviewPage } from "@/components/studio-pages";

export default async function Page({ params, searchParams }: { params: Promise<{ projectId: string }>; searchParams: Promise<{ category?: string }> }) {
  const [{ projectId }, query] = await Promise.all([params, searchParams]);
  return <ProjectReviewPage initialCategory={query.category ?? "all"} projectId={projectId} />;
}

import { ReviewPage } from "@/features/review/review-page";

export default async function Page({ params, searchParams }: { params: Promise<{ projectId: string }>; searchParams: Promise<{ category?: string }> }) {
  const [{ projectId }] = await Promise.all([params, searchParams]);
  return <ReviewPage projectId={projectId} />;
}

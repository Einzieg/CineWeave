import { AssetsPage } from "@/components/studio-pages";

export default async function Page({ params, searchParams }: { params: Promise<{ projectId: string }>; searchParams: Promise<{ assetId?: string }> }) {
  const [{ projectId }, query] = await Promise.all([params, searchParams]);
  return <AssetsPage initialAssetId={query.assetId ?? ""} projectId={projectId} />;
}

import { CommerceMaterialsPage } from "@/features/commerce/commerce-materials-page";

export default async function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <CommerceMaterialsPage projectId={projectId} />;
}

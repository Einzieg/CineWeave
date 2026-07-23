import { CommerceFinalPage } from "@/features/commerce/commerce-final-page";

export default async function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <CommerceFinalPage projectId={projectId} />;
}

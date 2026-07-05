import { use } from "react";
import { ProductionPage } from "@/features/production/production-page";

export default function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = use(params);
  return <ProductionPage projectId={projectId} />;
}

import { use } from "react";
import { ExportPage } from "@/features/export/export-page";

export default function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = use(params);
  return <ExportPage projectId={projectId} />;
}

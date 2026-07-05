import { use } from "react";
import { ProjectSettingsPage } from "@/features/project-settings/settings-page";

export default function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = use(params);
  return <ProjectSettingsPage projectId={projectId} />;
}

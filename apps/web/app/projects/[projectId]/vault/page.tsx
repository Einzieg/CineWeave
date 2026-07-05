import { use } from "react";
import { VaultPage } from "@/features/vault/vault-page";

export default function Page({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = use(params);
  return <VaultPage projectId={projectId} />;
}

import { useApiQuery } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";

export function useAgentChat(projectId: string, sessionId: string | null, enabled = true, live = false) {
  const { data: messages = [], isLoading } = useApiQuery({
    key: qk.agentMessages(projectId, sessionId || ""),
    queryFn: (session) => studioApi.listAgentMessages(session, projectId, sessionId!).then((r) => r.items || []),
    enabled: enabled && !!sessionId,
    refetchInterval: enabled && live ? 3000 : false,
  });

  return {
    messages,
    isLoading,
  };
}

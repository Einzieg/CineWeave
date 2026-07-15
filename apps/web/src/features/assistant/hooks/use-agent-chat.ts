import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useApiQuery, useApiMutation, useInvalidateKeys, orgScopedKey } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { toast } from "sonner";
import { useStudioSession } from "@/lib/session";
import type { AgentMessage } from "@/lib/types";

export function useAgentChat(projectId: string, sessionId: string | null, enabled = true, live = false) {
  const invalidate = useInvalidateKeys();
  const queryClient = useQueryClient();
  const { session } = useStudioSession();

  // 获取消息列表
  const { data: messages = [], isLoading } = useApiQuery({
    key: qk.agentMessages(projectId, sessionId || ""),
    queryFn: (session) => studioApi.listAgentMessages(session, projectId, sessionId!).then((r) => r.items || []),
    enabled: enabled && !!sessionId,
    refetchInterval: enabled && live ? 3000 : false,
  });

  const sendMutation = useApiMutation({
    mutationFn: (session, content: string) =>
      studioApi.createAgentMessage(session, projectId, sessionId!, content),
    onMutate: (content) => {
      if (!sessionId) {
        return;
      }
      const key = orgScopedKey(session.organizationId, qk.agentMessages(projectId, sessionId));
      const optimisticMessage: AgentMessage = {
        id: `pending:${Date.now()}`,
        sessionId,
        role: "user",
        content,
        createdAt: new Date().toISOString(),
      };
      queryClient.setQueryData<AgentMessage[]>(key, (current = []) => [...current, optimisticMessage]);
    },
    onSettled: () => {
      if (sessionId) {
        invalidate([
          qk.agentMessages(projectId, sessionId),
          qk.workflowRuns(projectId),
          qk.artifacts(projectId),
        ]);
        void queryClient.invalidateQueries({ queryKey: orgScopedKey(session.organizationId, qk.project(projectId)) });
      }
    },
    onError: (error) => {
      toast.error("发送消息失败：" + error.message);
    },
  });

  const sendMessage = useCallback((content: string) => {
    if (!sessionId) {
      toast.error("请先选择或创建会话");
      return;
    }
    if (!content.trim()) return;

    sendMutation.mutate(content);
  }, [sessionId, sendMutation]);

  return {
    messages,
    isLoading,
    isSending: sendMutation.isPending,
    sendMessage,
  };
}

import { useCallback } from "react";
import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { toast } from "sonner";

export function useAgentChat(projectId: string, sessionId: string | null) {
  const invalidate = useInvalidateKeys();

  // 获取消息列表
  const { data: messages = [], isLoading } = useApiQuery({
    key: qk.agentMessages(projectId, sessionId || ""),
    queryFn: (session) => studioApi.listAgentMessages(session, projectId, sessionId!).then(r => r.items || []),
    enabled: !!sessionId,
    refetchInterval: 3000, // 3秒轮询检查新消息
  });

  // 发送消息
  const sendMutation = useApiMutation({
    mutationFn: (session, content: string) =>
      studioApi.createAgentMessage(session, projectId, sessionId!, content),
    onSuccess: () => {
      invalidate([qk.agentMessages(projectId, sessionId!)]);
    },
    onError: (error) => {
      toast.error("发送消息失败：" + error.message);
    }
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

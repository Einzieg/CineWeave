"use client";

import { useEffect, useRef } from "react";
import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Plus } from "lucide-react";
import { toast } from "sonner";

interface SessionSelectorProps {
  projectId: string;
  currentSessionId: string | null;
  onSessionChange: (sessionId: string) => void;
}

export function SessionSelector({
  projectId,
  currentSessionId,
  onSessionChange,
}: SessionSelectorProps) {
  const invalidate = useInvalidateKeys();
  const autoHandledRef = useRef("");
  const agentLabel = "项目控制助手";

  const { data: sessions = [], isLoading } = useApiQuery({
    key: qk.agentSessions(projectId),
    queryFn: (session) => studioApi.listAgentSessions(session, projectId).then((r) => r.items || []),
    enabled: !!projectId,
  });

  const { mutate: createSession, isPending: isCreatingSession } = useApiMutation({
    mutationFn: (session, input: { title: string; silent?: boolean }) =>
      studioApi.createAgentSession(session, projectId, input.title || "新对话"),
    onSuccess: (result, input) => {
      invalidate([qk.agentSessions(projectId)]);
      onSessionChange(result.id);
      if (!input.silent) {
        toast.success("创建会话成功");
      }
    },
    onError: (error) => {
      toast.error("创建会话失败：" + error.message);
    },
  });

  const handleCreateSession = () => {
    createSession({ title: `${agentLabel} - ${new Date().toLocaleString()}` });
  };

  useEffect(() => {
    if (!projectId || currentSessionId || isLoading || isCreatingSession) {
      return;
    }
    if (sessions.length > 0) {
      autoHandledRef.current = projectId;
      onSessionChange(sessions[0].id);
      return;
    }
    if (autoHandledRef.current === projectId) {
      return;
    }
    autoHandledRef.current = projectId;
    createSession({ title: `${agentLabel} - ${new Date().toLocaleString()}`, silent: true });
  }, [agentLabel, createSession, currentSessionId, isCreatingSession, isLoading, onSessionChange, projectId, sessions]);

  return (
    <div className="flex gap-2">
      <Select value={currentSessionId || ""} onValueChange={onSessionChange}>
        <SelectTrigger className="flex-1">
          <SelectValue placeholder="选择或创建会话" />
        </SelectTrigger>
        <SelectContent>
          {sessions.length === 0 && (
            <div className="px-2 py-4 text-sm text-muted-foreground text-center">
              暂无会话
            </div>
          )}
          {sessions.map((session) => (
            <SelectItem key={session.id} value={session.id}>
              {session.title || `会话 ${session.id.slice(0, 8)}`}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        size="icon"
        variant="outline"
        onClick={handleCreateSession}
        disabled={isCreatingSession}
        aria-label="新建会话"
      >
        <Plus className="h-4 w-4" />
      </Button>
    </div>
  );
}

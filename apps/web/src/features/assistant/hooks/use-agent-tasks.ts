import { useMemo } from "react";
import { toast } from "sonner";
import { studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type { AgentPermissionMode, AgentTask, JsonRecord } from "@/lib/types";

type CreateTaskInput = {
  goal: string;
  mode?: "plan_only" | "supervised" | "auto_low_risk";
  permissionMode?: AgentPermissionMode;
  constraints?: JsonRecord;
};

type StepDecisionInput = {
  taskId: string;
  stepId: string;
  note?: string;
  decision?: JsonRecord;
};

export function useAgentTasks(projectId: string, sessionId?: string | null) {
  const invalidate = useInvalidateKeys();
  const scopedSessionId = sessionId || "";

  const tasksQuery = useApiQuery({
    key: qk.agentTasks(projectId, scopedSessionId),
    queryFn: (session) =>
      studioApi
        .listAgentTasks(session, projectId, { "filter[sessionId]": scopedSessionId })
        .then((response) => response.items || []),
    enabled: Boolean(scopedSessionId),
    refetchInterval: 3000,
  });

  const tasks = useMemo(() => (scopedSessionId ? tasksQuery.data || [] : []), [scopedSessionId, tasksQuery.data]);
  const activeTask = useMemo(() => firstActiveTask(tasks), [tasks]);

  const taskDetailQuery = useApiQuery({
    key: qk.agentTask(projectId, activeTask?.id || ""),
    queryFn: (session) => studioApi.getAgentTask(session, projectId, activeTask!.id),
    enabled: Boolean(activeTask?.id),
    refetchInterval: activeTask && !isTerminalTask(activeTask.status) ? 3000 : false,
  });

  const createTaskMutation = useApiMutation({
    mutationFn: (session, input: CreateTaskInput) => {
      const constraints: JsonRecord = { ...(input.constraints || {}) };
      if (input.permissionMode) {
        constraints.permissionMode = input.permissionMode;
      }
      const body: JsonRecord = {
        goal: input.goal,
        mode: input.mode || "supervised",
        constraints,
      };
      if (sessionId) {
        body.sessionId = sessionId;
      }
      return studioApi.createAgentTask(session, projectId, body);
    },
    onSuccess: (task) => {
      toast.success(task.status === "waiting_approval" ? "任务计划已生成，等待确认" : "任务已创建，正在规划");
      invalidate(projectAgentInvalidationKeys(projectId, task.id, task.sessionId || scopedSessionId));
    },
    onError: (error) => toast.error("创建任务失败：" + error.message),
  });

  const cancelTaskMutation = useApiMutation({
    mutationFn: (session, taskId: string) => studioApi.cancelAgentTask(session, projectId, taskId, { reason: "用户在助手面板取消" }),
    onSuccess: (task) => {
      toast.success("任务取消请求已提交");
      invalidate(projectAgentInvalidationKeys(projectId, task.id, task.sessionId || scopedSessionId));
    },
    onError: (error) => toast.error("取消失败：" + error.message),
  });

  const resumeTaskMutation = useApiMutation({
    mutationFn: (session, taskId: string) => studioApi.resumeAgentTask(session, projectId, taskId),
    onSuccess: (task) => {
      toast.success("任务已恢复");
      invalidate(projectAgentInvalidationKeys(projectId, task.id, task.sessionId || scopedSessionId));
    },
    onError: (error) => toast.error("恢复失败：" + error.message),
  });

  const approveStepMutation = useApiMutation({
    mutationFn: (session, payload: StepDecisionInput) =>
      studioApi.approveAgentStep(session, projectId, payload.taskId, payload.stepId, agentStepDecisionBody(payload)),
    onSuccess: (approval) => {
      toast.success("已批准步骤");
      invalidate(projectAgentInvalidationKeys(projectId, approval.taskId, scopedSessionId));
    },
    onError: (error) => toast.error("批准失败：" + error.message),
  });

  const rejectStepMutation = useApiMutation({
    mutationFn: (session, payload: StepDecisionInput) =>
      studioApi.rejectAgentStep(session, projectId, payload.taskId, payload.stepId, agentStepDecisionBody(payload)),
    onSuccess: (approval) => {
      toast.success("已拒绝步骤");
      invalidate(projectAgentInvalidationKeys(projectId, approval.taskId, scopedSessionId));
    },
    onError: (error) => toast.error("拒绝失败：" + error.message),
  });

  return {
    tasks,
    task: taskDetailQuery.data || activeTask || null,
    isLoading: tasksQuery.isLoading || taskDetailQuery.isLoading,
    createTask: createTaskMutation.mutate,
    isCreatingTask: createTaskMutation.isPending,
    cancelTask: cancelTaskMutation.mutate,
    isCancellingTask: cancelTaskMutation.isPending,
    resumeTask: resumeTaskMutation.mutate,
    isResumingTask: resumeTaskMutation.isPending,
    approveStep: approveStepMutation.mutate,
    isApprovingStep: approveStepMutation.isPending,
    rejectStep: rejectStepMutation.mutate,
    isRejectingStep: rejectStepMutation.isPending,
  };
}

function agentStepDecisionBody(payload: StepDecisionInput) {
  const body: JsonRecord = { decision: payload.decision || {} };
  if (payload.note) {
    body.note = payload.note;
  }
  return body;
}

function firstActiveTask(tasks: AgentTask[]) {
  return tasks.find((task) => !isTerminalTask(task.status)) || tasks[0] || null;
}

function isTerminalTask(status: string) {
  return ["succeeded", "failed", "cancelled"].includes(status);
}

function projectAgentInvalidationKeys(projectId: string, taskId: string, sessionId?: string | null) {
  return [
    qk.agentTasks(projectId),
    ...(sessionId ? [qk.agentTasks(projectId, sessionId)] : []),
    qk.agentTask(projectId, taskId),
    qk.workflowRuns(projectId),
    qk.productionStatus(projectId),
    qk.shotProduction(projectId),
    qk.artifacts(projectId),
  ];
}

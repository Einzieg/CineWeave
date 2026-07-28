import { useEffect, useMemo, useRef } from "react";
import { toast } from "sonner";
import { studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { useUiStore } from "@/lib/stores/ui-store";
import { useActivityStore } from "@/lib/stores/activity-store";
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

export function useAgentTasks(projectId: string, sessionId?: string | null, enabled = true) {
  const invalidate = useInvalidateKeys();
  const markGeneratedScript = useUiStore((state) => state.markGeneratedScript);
  const scopedSessionId = sessionId || "";
  const workflowEventRevision = useActivityStore((state) => {
    const events = state.eventsByProject[projectId] ?? [];
    for (let index = events.length - 1; index >= 0; index -= 1) {
      if (
        events[index].eventType.startsWith("workflow.")
        || events[index].eventType.startsWith("storyboard.")
        || events[index].eventType.startsWith("video.production.")
        || events[index].eventType.startsWith("commerce.direct_video.")
        || events[index].eventType.startsWith("commerce.script_derivation.")
      ) {
        return events[index].id;
      }
    }
    return "";
  });

  const tasksQuery = useApiQuery({
    key: qk.agentTasks(projectId, scopedSessionId),
    queryFn: (session) =>
      studioApi
        .listAgentTasks(session, projectId, { "filter[sessionId]": scopedSessionId })
        .then((response) => response.items || []),
    enabled: enabled && Boolean(scopedSessionId),
    refetchInterval: (query) =>
      enabled && query.state.data?.some((task) => !isTerminalTask(task.status)) ? 2000 : false,
  });

  const tasks = useMemo(() => (scopedSessionId ? tasksQuery.data || [] : []), [scopedSessionId, tasksQuery.data]);
  const activeTask = useMemo(() => firstActiveTask(tasks), [tasks]);

  const taskDetailQuery = useApiQuery({
    key: qk.agentTask(projectId, activeTask?.id || ""),
    queryFn: (session) => studioApi.getAgentTask(session, projectId, activeTask!.id),
    enabled: enabled && Boolean(activeTask?.id),
    refetchInterval: (query) => {
      const detailStatus = query.state.data?.status || activeTask?.status || "";
      return enabled && Boolean(activeTask?.id) && !isTerminalTask(detailStatus) ? 1000 : false;
    },
  });
  const task = useMemo(
    () => reconcileAgentTask(activeTask, taskDetailQuery.data),
    [activeTask, taskDetailQuery.data],
  );
  const refreshSignature = useMemo(() => (task ? agentTaskRefreshSignature(task) : ""), [task]);
  const productionRefreshSignature = useMemo(() => (task ? agentTaskProductionRefreshSignature(task) : ""), [task]);
  const generatedScript = useMemo(() => (task ? agentTaskGeneratedScript(task) : null), [task]);
  const lastRefreshSignatureRef = useRef("");
  const lastProductionRefreshSignatureRef = useRef("");
  const lastGeneratedScriptRef = useRef("");

  useEffect(() => {
    if (!refreshSignature || refreshSignature === lastRefreshSignatureRef.current) {
      return;
    }
    lastRefreshSignatureRef.current = refreshSignature;
    if (scopedSessionId) invalidate([qk.agentMessages(projectId, scopedSessionId)]);
  }, [invalidate, projectId, refreshSignature, scopedSessionId]);

  useEffect(() => {
    if (!productionRefreshSignature || productionRefreshSignature === lastProductionRefreshSignatureRef.current) {
      return;
    }
    lastProductionRefreshSignatureRef.current = productionRefreshSignature;
    invalidate(projectAgentProductionInvalidationKeys(projectId));
  }, [invalidate, productionRefreshSignature, projectId]);

  useEffect(() => {
    if (!workflowEventRevision || !activeTask?.id) {
      return;
    }
    invalidate([
      qk.agentTask(projectId, activeTask.id),
      qk.agentTasks(projectId, scopedSessionId),
    ]);
  }, [activeTask?.id, invalidate, projectId, scopedSessionId, workflowEventRevision]);

  useEffect(() => {
    if (!generatedScript?.scriptId) {
      return;
    }
    const signature = `${generatedScript.scriptId}:${generatedScript.versionId || ""}`;
    if (signature === lastGeneratedScriptRef.current) {
      return;
    }
    lastGeneratedScriptRef.current = signature;
    markGeneratedScript(projectId, generatedScript.scriptId, generatedScript.versionId);
  }, [generatedScript, markGeneratedScript, projectId]);

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
      toast.success(approval.approvalType === "question" ? "已提交选择" : "已批准步骤");
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
    task,
    isActive: Boolean(activeTask && !isTerminalTask(activeTask.status)),
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

function reconcileAgentTask(listTask: AgentTask | null, detailTask?: AgentTask) {
  if (!listTask) {
    return detailTask || null;
  }
  if (!detailTask || detailTask.id !== listTask.id) {
    return listTask;
  }
  if (!isTerminalTask(listTask.status) || isTerminalTask(detailTask.status)) {
    return detailTask;
  }

  // The list can observe workflow reconciliation before the detail poll. Keep
  // the detailed steps, but never let that older snapshot mask a terminal task.
  return {
    ...detailTask,
    ...listTask,
    steps: detailTask.steps,
    approvals: detailTask.approvals,
  };
}

function projectAgentInvalidationKeys(projectId: string, taskId: string, sessionId?: string | null) {
  return [
    qk.agentTasks(projectId),
    ...(sessionId ? [qk.agentTasks(projectId, sessionId)] : []),
    qk.agentTask(projectId, taskId),
    ...projectAgentProductionInvalidationKeys(projectId),
  ];
}

function projectAgentProductionInvalidationKeys(projectId: string) {
  return [
    qk.workflowRuns(projectId),
    qk.productionStatus(projectId),
    qk.project(projectId),
    qk.sources(projectId),
    qk.adaptationPlans(projectId),
    qk.scripts(projectId),
    qk.scriptDetailsPrefix(projectId),
    qk.scriptVersionsPrefix(projectId),
    qk.scriptEpisodesPrefix(projectId),
    qk.scriptScenesPrefix(projectId),
    qk.shotProductionPrefix(projectId),
    qk.assetsRoot(projectId),
    qk.requirements(projectId),
    qk.artifacts(projectId),
    qk.commerceProduct(projectId),
    qk.commerceProductReferencesRoot(projectId),
    qk.commerceScriptUnitsRoot(projectId),
    qk.commerceScriptDerivationsRoot(projectId),
    qk.commerceDirectVideoOptions(projectId),
    qk.commerceDirectVideosRoot(projectId),
  ];
}

function agentTaskRefreshSignature(task: AgentTask) {
  const stepSignature = (task.steps || [])
    .map((step) => `${step.id}:${step.status}:${step.updatedAt || ""}:${step.completedAt || ""}`)
    .join("|");
  return [
    task.id,
    task.status,
    task.updatedAt,
    task.completedAt || "",
    stepSignature,
  ].join(":");
}

function agentTaskProductionRefreshSignature(task: AgentTask) {
  const stepSignature = (task.steps || [])
    .map((step) => `${step.id}:${step.status}:${step.completedAt || ""}`)
    .join("|");
  return [task.id, task.status, task.completedAt || "", stepSignature].join(":");
}

function agentTaskGeneratedScript(task: AgentTask) {
  const steps = [...(task.steps || [])].reverse();
  for (const step of steps) {
    if (step.status !== "succeeded" || step.toolName !== "script.generate_from_source") {
      continue;
    }
    const output = isJsonRecord(step.output) ? step.output : {};
    const data = isJsonRecord(output.data) ? output.data : output;
    const scriptId = stringValue(data.scriptId);
    if (!scriptId) {
      continue;
    }
    return {
      scriptId,
      versionId: stringValue(data.versionId),
    };
  }
  return null;
}

function isJsonRecord(value: unknown): value is JsonRecord {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

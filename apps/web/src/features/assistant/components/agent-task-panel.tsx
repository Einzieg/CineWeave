"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import { studioApi } from "@/lib/api-client";
import { localizePlatformError } from "@/lib/error-localization";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import type { AgentApproval, AgentStep, AgentTask, JsonRecord, JsonValue } from "@/lib/types";
import { projectHref } from "@/lib/routes";
import { workflowEpisodeProgressLabel } from "@/lib/workflow-progress-label";
import Link from "next/link";
import type { Route } from "next";
import {
  AlertTriangle,
  Bot,
  ChevronDown,
  ChevronUp,
  CheckCircle2,
  Clock3,
  FileText,
  FileDiff,
  GitFork,
  ListChecks,
  Loader2,
  Play,
  RotateCcw,
  ShieldCheck,
  SquareTerminal,
  X,
  XCircle,
} from "lucide-react";

type StepDecisionPayload = {
  taskId: string;
  stepId: string;
  note?: string;
  decision?: JsonRecord;
};

type AgentTaskPanelProps = {
  projectId: string;
  task: AgentTask | null;
  isLoading?: boolean;
  onApproveStep: (payload: StepDecisionPayload) => void;
  onRejectStep: (payload: StepDecisionPayload) => void;
  onCancelTask: (taskId: string) => void;
  onResumeTask: (taskId: string) => void;
  busy?: boolean;
};

type PendingDecision = {
  kind: "approve" | "reject";
  step: AgentStep;
};

type AgentQuestionOption = {
  id: string;
  label: string;
  description?: string;
  nextGoal?: string;
  value?: JsonValue;
};

type AgentQuestionPrompt = {
  question: string;
  options: AgentQuestionOption[];
  allowCustom: boolean;
  defaultOptionId?: string;
};

export function AgentTaskPanel({ projectId, task, isLoading, onApproveStep, onRejectStep, onCancelTask, onResumeTask, busy }: AgentTaskPanelProps) {
  const [pendingDecision, setPendingDecision] = useState<PendingDecision | null>(null);
  const [note, setNote] = useState("");
  const [constraintText, setConstraintText] = useState("");

  const approvalsByStep = useMemo(() => {
    const items = new Map<string, AgentApproval>();
    for (const approval of task?.approvals || []) {
      if (approval.stepId) {
        items.set(approval.stepId, approval);
      }
    }
    return items;
  }, [task?.approvals]);
  const visibleTaskSteps = useMemo(() => sequentialVisibleAgentSteps(task?.steps || []), [task?.steps]);
  const taskAttachments = useMemo(() => agentTaskImageAttachmentLabels(task), [task]);

  if (!task && !isLoading) {
    return null;
  }

  const closeDecision = () => {
    setPendingDecision(null);
    setNote("");
    setConstraintText("");
  };

  const submitDecision = () => {
    if (!pendingDecision || busy) {
      return;
    }
    const payload: StepDecisionPayload = {
      taskId: pendingDecision.step.taskId,
      stepId: pendingDecision.step.id,
      note: note.trim() || undefined,
      decision: constraintText.trim() ? { constraintText: constraintText.trim() } : undefined,
    };
    if (pendingDecision.kind === "approve") {
      onApproveStep(payload);
    } else {
      onRejectStep(payload);
    }
    closeDecision();
  };

  return (
    <section className="w-full min-w-0 max-w-full overflow-hidden rounded-lg border bg-background p-3 shadow-sm">
      <div className="mb-2 flex min-w-0 items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="text-sm font-medium">任务活动</div>
          <div className="truncate text-xs text-muted-foreground">{task?.userGoal || "正在读取任务"}</div>
        </div>
        {task ? <TaskStatusBadge status={task.status} /> : null}
      </div>
      {taskAttachments.length > 0 ? (
        <div className="mb-3 flex min-w-0 flex-wrap gap-1.5">
          {taskAttachments.map((attachment) => (
            <Badge key={attachment.id} variant="outline" className="max-w-full">
              <span className="truncate">图片：{attachment.fileName}</span>
            </Badge>
          ))}
        </div>
      ) : null}

      {task ? (
        <div className="w-full min-w-0 max-w-full space-y-3 overflow-hidden">
          <ResultSummary task={task} />
          <div className="max-h-[min(30rem,52dvh)] w-full max-w-full overflow-y-auto overflow-x-hidden pr-2">
            <div className="w-full min-w-0 max-w-full space-y-2">
              {visibleTaskSteps.map((step) => (
                <ToolCallCard
                  key={step.id}
                  projectId={projectId}
                  step={step}
                  taskStatus={task.status}
                  approval={approvalsByStep.get(step.id)}
                  onApprove={() => setPendingDecision({ kind: "approve", step })}
                  onReject={() => setPendingDecision({ kind: "reject", step })}
                  onResumeTask={() => onResumeTask(task.id)}
                  onAnswerQuestion={(payload) =>
                    onApproveStep({
                      taskId: step.taskId,
                      stepId: step.id,
                      ...payload,
                    })
                  }
                  busy={busy}
                />
              ))}
            </div>
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            {canResume(task.status) ? (
              <Button size="sm" variant="outline" onClick={() => onResumeTask(task.id)} disabled={busy}>
                <RotateCcw className="mr-1 h-3.5 w-3.5" />
                恢复
              </Button>
            ) : null}
            {canCancel(task.status) ? (
              <Button size="sm" variant="outline" onClick={() => onCancelTask(task.id)} disabled={busy}>
                <X className="mr-1 h-3.5 w-3.5" />
                取消
              </Button>
            ) : null}
          </div>
        </div>
      ) : (
        <div className="rounded-md border bg-background px-3 py-2 text-xs text-muted-foreground">正在读取任务</div>
      )}

      <Dialog open={Boolean(pendingDecision)} onOpenChange={(open) => !open && closeDecision()}>
        <DialogContent className="sm:max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{pendingDecision?.kind === "approve" ? "批准步骤" : "拒绝步骤"}</DialogTitle>
            <DialogDescription>{pendingDecision ? approvalDialogSummary(pendingDecision.step) : ""}</DialogDescription>
          </DialogHeader>
          {pendingDecision ? (
            <div className="space-y-3">
              <ToolImpact step={pendingDecision.step} />
              <div className="space-y-1.5">
                <label className="text-xs font-medium" htmlFor="agent-decision-note">
                  备注
                </label>
                <Textarea
                  id="agent-decision-note"
                  value={note}
                  onChange={(event) => setNote(event.target.value)}
                  placeholder="填写给执行器和审计记录的备注"
                  className="min-h-20"
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium" htmlFor="agent-decision-constraints">
                  约束调整
                </label>
                <Textarea
                  id="agent-decision-constraints"
                  value={constraintText}
                  onChange={(event) => setConstraintText(event.target.value)}
                  placeholder="例如只处理前三个镜头、不要生成视频、最高并发为 1"
                  className="min-h-20"
                />
              </div>
            </div>
          ) : null}
          <DialogFooter>
            <Button variant="outline" onClick={closeDecision} disabled={busy}>
              取消
            </Button>
            <Button
              variant={pendingDecision?.kind === "reject" ? "destructive" : "default"}
              onClick={submitDecision}
              disabled={busy}
            >
              {pendingDecision?.kind === "reject" ? "确认拒绝" : "确认批准"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export function AgentTaskConversationActivity({
  projectId,
  task,
  isLoading,
  onApproveStep,
  onRejectStep,
  onCancelTask,
  onResumeTask,
  busy,
}: AgentTaskPanelProps) {
  const [expanded, setExpanded] = useState(false);
  const approvalsByStep = useMemo(() => {
    const items = new Map<string, AgentApproval>();
    for (const approval of task?.approvals || []) {
      if (approval.stepId) {
        items.set(approval.stepId, approval);
      }
    }
    return items;
  }, [task?.approvals]);

  if (!task && !isLoading) {
    return null;
  }

  const steps = task?.steps || [];
  const sequentialSteps = sequentialVisibleAgentSteps(steps);
  const visibleSteps = expanded ? sequentialSteps : conversationFocusSteps(sequentialSteps);
  const activeStep = conversationActiveStep(sequentialSteps);
  const activeProgress = activeStep ? stepStreamProgress(activeStep) : null;
  const completedCount = steps.filter((step) => step.status === "succeeded").length;
  const waitingCount = steps.filter((step) => step.status === "waiting_approval").length;
  const failedCount = steps.filter((step) => ["failed", "blocked"].includes(step.status)).length;
  const taskError = task?.errorMessage ? localizePlatformError(task.errorMessage, task.errorCode) : "";
  const summary = task
    ? stringValue(task.summary?.summary) ||
      taskError ||
      "任务已创建。"
    : "正在读取任务";

  return (
    <div className="flex min-w-0 items-start gap-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
        {task?.status === "running" || task?.status === "planning" ? <Loader2 className="h-4 w-4 animate-spin" /> : <Bot className="h-4 w-4" />}
      </div>
      <div className="min-w-0 max-w-[88%] flex-1 overflow-hidden rounded-lg border bg-background p-3 shadow-sm">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <ListChecks className="h-4 w-4 shrink-0 text-primary" />
              <div className="truncate text-sm font-medium">任务动态</div>
            </div>
            <div className="mt-1 truncate text-xs text-muted-foreground">{task?.userGoal || "正在读取任务"}</div>
          </div>
          {task ? <TaskStatusBadge status={task.status} /> : null}
        </div>

        <div className="mt-2 flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
          <Badge variant="outline">完成 {completedCount}</Badge>
          <Badge variant={waitingCount > 0 ? "secondary" : "outline"}>待确认 {waitingCount}</Badge>
          <Badge variant={failedCount > 0 ? "destructive" : "outline"}>异常 {failedCount}</Badge>
          {task ? <Badge variant="outline">权限 {agentPermissionModeLabel(stringValue(task.constraints?.permissionMode))}</Badge> : null}
        </div>

        <div className="mt-2 break-words text-xs text-muted-foreground">{summary}</div>
        {taskError && taskError !== summary ? <div className="mt-1 break-words text-xs text-destructive">失败原因：{taskError}</div> : null}

        {activeProgress ? <StreamProgress progress={activeProgress} /> : null}

        {visibleSteps.length > 0 ? (
          <div className="mt-3 grid max-h-[min(34rem,56dvh)] gap-2 overflow-y-auto overflow-x-hidden pr-1">
            {visibleSteps.map((step) => (
              <ToolCallCard
                key={step.id}
                projectId={projectId}
                step={step}
                taskStatus={task?.status || ""}
                approval={approvalsByStep.get(step.id)}
                onApprove={() => onApproveStep({ taskId: step.taskId, stepId: step.id })}
                onReject={() => onRejectStep({ taskId: step.taskId, stepId: step.id })}
                onResumeTask={() => onResumeTask(step.taskId)}
                onAnswerQuestion={(payload) =>
                  onApproveStep({
                    taskId: step.taskId,
                    stepId: step.id,
                    ...payload,
                  })
                }
                busy={busy}
              />
            ))}
          </div>
        ) : null}

        {task ? (
          <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap gap-2">
              {sequentialSteps.length > conversationFocusSteps(sequentialSteps).length ? (
                <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setExpanded((value) => !value)}>
                  {expanded ? <ChevronUp className="mr-1 h-3.5 w-3.5" /> : <ChevronDown className="mr-1 h-3.5 w-3.5" />}
                  {expanded ? "收起步骤" : `展开已开始 ${sequentialSteps.length}`}
                </Button>
              ) : null}
            </div>
            <div className="flex flex-wrap gap-2">
              {canResume(task.status) ? (
                <Button size="sm" variant="outline" className="h-7 px-2 text-xs" onClick={() => onResumeTask(task.id)} disabled={busy}>
                  <RotateCcw className="mr-1 h-3.5 w-3.5" />
                  恢复
                </Button>
              ) : null}
              {canCancel(task.status) ? (
                <Button size="sm" variant="outline" className="h-7 px-2 text-xs" onClick={() => onCancelTask(task.id)} disabled={busy}>
                  <X className="mr-1 h-3.5 w-3.5" />
                  取消
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}

function ResultSummary({ task }: { task: AgentTask }) {
  const steps = task.steps || [];
  const done = steps.filter((step) => step.status === "succeeded").length;
  const failed = steps.filter((step) => ["failed", "blocked"].includes(step.status)).length;
  const waiting = steps.filter((step) => step.status === "waiting_approval").length;
  const summary = stringValue(task.summary?.summary);
  const permissionMode = stringValue(task.constraints?.permissionMode);
  const taskError = task.errorMessage ? localizePlatformError(task.errorMessage, task.errorCode) : "";

  return (
    <div className="w-full min-w-0 max-w-full rounded-md border bg-muted/30 px-3 py-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
        <Badge variant="outline">完成 {done}</Badge>
        <Badge variant={waiting > 0 ? "secondary" : "outline"}>待确认 {waiting}</Badge>
        <Badge variant={failed > 0 ? "destructive" : "outline"}>异常 {failed}</Badge>
        <Badge variant="outline">权限 {agentPermissionModeLabel(permissionMode)}</Badge>
      </div>
      {task.temporalWorkflowId ? <TechnicalDetails items={[["后台任务", shortID(task.temporalWorkflowId)]]} /> : null}
      <div className="mt-2 break-words text-xs text-muted-foreground">
        {summary || taskError || "任务已创建。"}
      </div>
      {taskError && taskError !== summary ? <div className="mt-1 break-words text-xs text-destructive">失败原因：{taskError}</div> : null}
    </div>
  );
}

function conversationFocusSteps(steps: AgentStep[]) {
  if (steps.length <= 4) {
    return steps;
  }
  const important = steps.filter((step) => ["running", "waiting_approval", "blocked", "failed"].includes(step.status));
  const recent = [...steps].filter((step) => step.status === "succeeded").slice(-2);
  const selected = [...important, ...recent];
  if (selected.length === 0) {
    selected.push(...steps.slice(-3));
  }
  const seen = new Set<string>();
  return selected
    .filter((step) => {
      if (seen.has(step.id)) {
        return false;
      }
      seen.add(step.id);
      return true;
    })
    .sort((a, b) => a.stepIndex - b.stepIndex)
    .slice(-4);
}

function sequentialVisibleAgentSteps(steps: AgentStep[]) {
  const visible: AgentStep[] = [];
  for (const step of steps) {
    if (step.status === "planned") {
      break;
    }
    visible.push(step);
    if (!isTerminalAgentStepStatus(step.status)) {
      break;
    }
    const progress = stepWorkflowProgress(step);
    if (progress && !isTerminalWorkflowProgressStatus(progress.status)) {
      break;
    }
  }
  return visible;
}

function isTerminalAgentStepStatus(status: string) {
  return ["succeeded", "failed", "blocked", "skipped", "cancelled"].includes(status);
}

function isTerminalWorkflowProgressStatus(status: string) {
  return ["succeeded", "failed", "cancelled", "skipped"].includes(status);
}

function conversationActiveStep(steps: AgentStep[]) {
  return (
    steps.find((step) => step.status === "running" && stepStreamProgress(step)) ||
    steps.find((step) => step.status === "running") ||
    steps.find((step) => step.status === "waiting_approval") ||
    steps.find((step) => ["failed", "blocked"].includes(step.status)) ||
    [...steps].reverse().find((step) => step.status === "succeeded") ||
    null
  );
}

function ToolCallCard({
  projectId,
  step,
  taskStatus,
  approval,
  onApprove,
  onReject,
  onResumeTask,
  onAnswerQuestion,
  busy,
}: {
  projectId: string;
  step: AgentStep;
  taskStatus: string;
  approval?: AgentApproval;
  onApprove: () => void;
  onReject: () => void;
  onResumeTask: () => void;
  onAnswerQuestion: (payload: { note?: string; decision: JsonRecord }) => void;
  busy?: boolean;
}) {
  const [customAnswer, setCustomAnswer] = useState("");
  const waitingApproval = step.status === "waiting_approval";
  const verifier = asRecord(step.verifierOutput);
  const verifierStatus = stringValue(verifier?.status);
  const supervisor = asRecord(step.supervisorDecision);
  const decision = asRecord(supervisor?.decision);
  const stateGate = asRecord(supervisor?.stateGate);
  const stateGateDetails = asRecord(stateGate?.details);
  const reasons = arrayOfStrings(decision?.reasons);
  const stateGateMessage = stringValue(stateGate?.message);
  const estimatedCostCents = numberValue(stateGateDetails?.estimatedCostCents);
  const output = asRecord(step.output);
  const retryable = Boolean(output?.retryable);
  const nextActions = stepNextActions(step);
  const businessLinks = stepBusinessLinks(projectId, step);
  const questionPrompt = agentQuestionPrompt(approval, step);
  const streamProgress = stepStreamProgress(step);
  const workflowProgress = stepWorkflowProgress(step);
  const derivationPlan = stepScriptDerivationPlan(step);
  const derivationProgress = stepScriptDerivationProgress(step);
  const displayStatus = stepDisplayStatus(step, workflowProgress);

  const submitQuestionOption = (option: AgentQuestionOption) => {
    const decision: JsonRecord = {
      kind: "option",
      selectedOptionId: option.id,
      selectedOptionLabel: option.label,
    };
    if (option.nextGoal) {
      decision.nextGoal = option.nextGoal;
    }
    if (option.value !== undefined) {
      decision.value = option.value;
    }
    onAnswerQuestion({ note: option.label, decision });
  };

  const submitCustomAnswer = () => {
    const value = customAnswer.trim();
    if (!value) {
      return;
    }
    onAnswerQuestion({
      note: value,
      decision: {
        kind: "custom",
        customAnswer: value,
      },
    });
    setCustomAnswer("");
  };

  return (
    <article className="w-full min-w-0 max-w-full overflow-hidden rounded-md border bg-background p-3">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <StepIcon status={displayStatus} />
            <span className="min-w-0 truncate text-sm font-medium">{toolLabel(step.toolName)}</span>
          </div>
          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1.5">
            <Badge variant="outline">{riskLabel(step.risk)}</Badge>
            <Badge variant={stepStatusVariant(displayStatus)}>{stepStatusLabel(displayStatus)}</Badge>
            {step.permission ? <Badge variant="outline">{permissionLabel(step.permission)}</Badge> : null}
            {estimatedCostCents > 0 ? <Badge variant="outline">预计 {formatCents(estimatedCostCents)}</Badge> : null}
            {retryable ? <Badge variant="outline">可重试</Badge> : null}
            {verifierStatus && verifierStatus !== "skipped" ? (
              <Badge variant={verifierStatus === "succeeded" ? "outline" : "destructive"}>校验{verifierStatus === "succeeded" ? "通过" : "失败"}</Badge>
            ) : null}
          </div>
        </div>
        <span className="shrink-0 text-xs text-muted-foreground">#{step.stepIndex}</span>
      </div>

      <ToolImpact step={step} />

      {stepDryRunText(step) ? <div className="mt-2 break-words text-xs text-muted-foreground">{stepDryRunText(step)}</div> : null}

      {reasons.length > 0 ? (
        <div className="mt-2 flex items-start gap-2 rounded bg-amber-500/10 px-2 py-1.5 text-xs text-amber-800">
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span className="min-w-0 break-words">{[...reasons.map(supervisorReasonLabel), stateGateMessage].filter(Boolean).join("、")}</span>
        </div>
      ) : null}

      {stepOutputText(step) ? <div className="mt-2 break-words text-xs text-muted-foreground">{stepOutputText(step)}</div> : null}
      {derivationPlan ? <ScriptDerivationPlanCard plan={derivationPlan} /> : null}
      {derivationProgress ? (
        <ScriptDerivationProgressCard
          projectId={projectId}
          batch={derivationProgress}
          onRetryStarted={canResume(taskStatus) ? onResumeTask : undefined}
        />
      ) : null}
      {streamProgress ? <StreamProgress progress={streamProgress} /> : null}
      {workflowProgress ? <WorkflowProgress progress={workflowProgress} /> : null}
      {nextActions.length > 0 ? (
        <div className="mt-2 min-w-0 overflow-hidden rounded bg-muted px-2 py-1.5 text-xs text-muted-foreground">
          <div className="font-medium text-foreground">下一步</div>
          <ul className="mt-1 space-y-1">
            {nextActions.map((action, index) => (
              <li key={`${action.label}-${index}`} className="break-words">
                {action.label}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {businessLinks.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {businessLinks.map((link) => (
            <Button asChild key={`${link.label}-${link.href}`} size="sm" variant="outline" className="h-7 px-2 text-xs">
              <Link
                href={link.href as Route}
                target={isExternalHref(link.href) ? "_blank" : undefined}
                rel={isExternalHref(link.href) ? "noreferrer" : undefined}
              >
                {link.label}
              </Link>
            </Button>
          ))}
        </div>
      ) : null}
      {stepWorkflowRunId(step) ? <TechnicalDetails items={[["任务编号", shortID(stepWorkflowRunId(step))]]} /> : null}
      {verifierStatus && verifierStatus !== "skipped" ? <VerifierLine verifier={verifier} /> : null}
      {step.errorMessage ? <div className="mt-2 text-xs text-destructive">{localizePlatformError(step.errorMessage, step.errorCode)}</div> : null}

      <details className="mt-2">
        <summary className="cursor-pointer text-xs text-muted-foreground">执行详情</summary>
        <div className="mt-2 space-y-2">
          <DetailBlock icon={<SquareTerminal className="h-3.5 w-3.5" />} label="输入" value={step.input} />
          <DetailBlock icon={<FileDiff className="h-3.5 w-3.5" />} label="预览" value={step.dryRunOutput} />
          <DetailBlock icon={<ShieldCheck className="h-3.5 w-3.5" />} label="监督" value={step.supervisorDecision} />
          <DetailBlock icon={<FileDiff className="h-3.5 w-3.5" />} label="输出" value={step.output} />
          <DetailBlock icon={<CheckCircle2 className="h-3.5 w-3.5" />} label="校验" value={step.verifierOutput} />
        </div>
      </details>

      {waitingApproval && questionPrompt ? (
        <QuestionPrompt
          prompt={questionPrompt}
          customAnswer={customAnswer}
          onCustomAnswerChange={setCustomAnswer}
          onSubmitCustomAnswer={submitCustomAnswer}
          onSelectOption={submitQuestionOption}
          onSkip={onReject}
          busy={busy || approval?.status !== "pending"}
        />
      ) : null}

      {waitingApproval && !questionPrompt ? (
        <div className="mt-3 flex justify-end gap-2">
          <Button size="sm" variant="outline" onClick={onReject} disabled={busy || approval?.status !== "pending"}>
            {step.toolName === "commerce.script.derive.batch" ? "调整方案" : "拒绝"}
          </Button>
          <Button size="sm" onClick={onApprove} disabled={busy || approval?.status !== "pending"}>
            <ShieldCheck className="mr-1 h-3.5 w-3.5" />
            {step.toolName === "commerce.script.derive.batch" ? "批准并创建" : "批准"}
          </Button>
        </div>
      ) : null}
    </article>
  );
}

type StepStreamProgress = {
  episodeIndex: number;
  episodeTotal: number;
  chapterTitle: string;
  text: string;
  textLength: number;
  done: boolean;
  updatedAt: string;
};

type StepWorkflowProgress = {
  workflowRunId: string;
  workflowType: string;
  status: string;
  totalItems: number;
  completedItems: number;
  failedItems: number;
  totalNodes: number;
  completedNodes: number;
  activeNode?: {
    nodeKey: string;
    nodeType: string;
    status: string;
    shotId: string;
    shotNo: number;
    anchorRole: string;
    episodeIndex: number;
    episodeTotal: number;
    batchIndex: number;
    batchTotal: number;
    episodeTitle: string;
    partialText: string;
    receivedChars: number;
    errorCode: string;
    errorMessage: string;
  };
};

type ScriptDerivationPlanView = {
  sourceTitle: string;
  dimension: string;
  instruction: string;
  preserve: string[];
  variations: Array<{ key: string; label: string; brief: string }>;
};

type ScriptDerivationBatchView = {
  id: string;
  retryBatchId: string;
  status: string;
  requestedCount: number;
  queuedCount: number;
  runningCount: number;
  succeededCount: number;
  failedRetryableCount: number;
  failedTerminalCount: number;
  cancelledCount: number;
  items: Array<{
    id: string;
    batchId: string;
    inputOrdinal: number;
    variationLabel: string;
    variationBrief: string;
    status: string;
    outputScriptUnitId: string;
    errorCode: string;
    errorMessage: string;
  }>;
};

function ScriptDerivationPlanCard({ plan }: { plan: ScriptDerivationPlanView }) {
  return (
    <div className="mt-2 min-w-0 overflow-hidden rounded-md border border-primary/20 bg-primary/5 p-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
        <GitFork className="size-3.5 text-primary" />
        <span className="font-medium text-primary">脚本裂变方案</span>
        <Badge variant="outline">{scriptDerivationDimensionLabel(plan.dimension)}</Badge>
        <Badge variant="outline">{plan.variations.length} 个独立脚本</Badge>
        <Badge variant="outline">可能产生文本模型费用</Badge>
      </div>
      {plan.sourceTitle ? (
        <div className="mt-2 text-xs text-muted-foreground">源脚本：{plan.sourceTitle}</div>
      ) : null}
      {plan.instruction ? (
        <div className="mt-1 break-words text-xs text-muted-foreground">要求：{plan.instruction}</div>
      ) : null}
      {plan.preserve.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1">
          {plan.preserve.map((item) => (
            <Badge key={item} variant="secondary">{scriptDerivationPreserveLabel(item)}</Badge>
          ))}
        </div>
      ) : null}
      <div className="mt-2 grid gap-1.5">
        {plan.variations.map((variation, index) => (
          <div key={`${variation.key}-${index}`} className="rounded border bg-background/80 px-2 py-1.5">
            <div className="text-xs font-medium">{index + 1}. {variation.label}</div>
            <div className="mt-0.5 break-words text-xs text-muted-foreground">{variation.brief}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

function ScriptDerivationProgressCard({
  projectId,
  batch,
  onRetryStarted,
}: {
  projectId: string;
  batch: ScriptDerivationBatchView;
  onRetryStarted?: () => void;
}) {
  const invalidate = useInvalidateKeys();
  const retryFailed = useApiMutation({
    mutationFn: (
      session,
      input: { batchId: string; idempotencyKey: string },
    ) => studioApi.retryCommerceScriptDerivation(
      session,
      projectId,
      input.batchId,
      input.idempotencyKey,
    ),
    onSuccess: () => {
      invalidate([
        qk.commerceScriptDerivationsRoot(projectId),
        qk.commerceScriptDerivation(projectId, batch.id),
        qk.commerceScriptUnitsRoot(projectId),
        qk.workflowRuns(projectId),
        qk.projectControlCommands(projectId),
      ]);
      toast.success("失败变体重试命令已提交");
      onRetryStarted?.();
    },
    onError: (error) => toast.error(
      `重试失败：${localizePlatformError(error.message, "COMMERCE_SCRIPT_DERIVATION_STATE_CONFLICT")}`,
    ),
  });
  const active = ["queued", "running", "cancelling"].includes(batch.status);
  const completed = batch.succeededCount + batch.failedRetryableCount
    + batch.failedTerminalCount + batch.cancelledCount;
  return (
    <div className="mt-2 min-w-0 overflow-hidden rounded-md border border-primary/20 bg-primary/5 p-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
        {active ? <Loader2 className="size-3.5 animate-spin text-primary" /> : <CheckCircle2 className="size-3.5 text-emerald-600" />}
        <span className="font-medium text-primary">裂变任务</span>
        <Badge variant={scriptDerivationBatchVariant(batch.status)}>
          {scriptDerivationBatchStatusLabel(batch.status)}
        </Badge>
        <Badge variant="outline">完成 {completed}/{batch.requestedCount}</Badge>
        {batch.failedRetryableCount > 0 ? <Badge variant="secondary">可重试 {batch.failedRetryableCount}</Badge> : null}
        {batch.failedRetryableCount > 0 && !active ? (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="ml-auto h-7 px-2 text-xs"
            disabled={retryFailed.isPending}
            onClick={() => retryFailed.mutate({
              batchId: batch.retryBatchId,
              idempotencyKey: `assistant-derivation-retry-${batch.retryBatchId}-${crypto.randomUUID()}`,
            })}
          >
            {retryFailed.isPending
              ? <Loader2 className="size-3.5 animate-spin" />
              : <RotateCcw className="size-3.5" />}
            {retryFailed.isPending ? "正在重试" : "重试失败项"}
          </Button>
        ) : null}
      </div>
      <div className="mt-2 grid gap-1.5">
        {batch.items.map((item) => {
          const running = ["queued", "running", "reviewing"].includes(item.status);
          return (
            <div key={item.id} className="rounded border bg-background/80 px-2 py-1.5">
              <div className="flex min-w-0 items-center gap-2">
                {running ? (
                  <Loader2 className="size-3.5 shrink-0 animate-spin text-primary" />
                ) : item.status === "succeeded" ? (
                  <CheckCircle2 className="size-3.5 shrink-0 text-emerald-600" />
                ) : (
                  <XCircle className="size-3.5 shrink-0 text-destructive" />
                )}
                <span className="min-w-0 flex-1 truncate text-xs font-medium">
                  {item.inputOrdinal}. {item.variationLabel}
                </span>
                <Badge variant={item.status === "succeeded" ? "outline" : running ? "secondary" : "destructive"}>
                  {scriptDerivationItemStatusLabel(item.status)}
                </Badge>
              </div>
              {item.variationBrief ? (
                <div className="mt-1 break-words text-xs text-muted-foreground">{item.variationBrief}</div>
              ) : null}
              {item.errorMessage ? (
                <div className="mt-1 break-words text-xs text-destructive">
                  {localizePlatformError(item.errorMessage, item.errorCode)}
                </div>
              ) : null}
              {item.status === "succeeded" && item.outputScriptUnitId ? (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  <Button asChild size="sm" variant="outline" className="h-7 px-2 text-xs">
                    <Link
                      href={`${projectHref(projectId, "commerce/video")}?scriptUnitId=${encodeURIComponent(item.outputScriptUnitId)}` as Route}
                    >
                      <FileText className="size-3.5" />
                      查看脚本
                    </Link>
                  </Button>
                  <Button asChild size="sm" variant="outline" className="h-7 px-2 text-xs">
                    <Link
                      href={`${projectHref(projectId, "commerce/video")}?generateScriptUnitId=${encodeURIComponent(item.outputScriptUnitId)}` as Route}
                    >
                      <Play className="size-3.5" />
                      生成视频
                    </Link>
                  </Button>
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function StreamProgress({ progress }: { progress: StepStreamProgress }) {
  const title =
    progress.episodeTotal > 1
      ? `第 ${progress.episodeIndex}/${progress.episodeTotal} 集`
      : "当前输出";
  return (
    <div className="mt-2 min-w-0 overflow-hidden rounded-md border border-blue-500/20 bg-blue-500/5 p-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
        <span className="font-medium text-blue-700">实时输出</span>
        <Badge variant="outline">{title}</Badge>
        {progress.chapterTitle ? <Badge variant="outline" className="max-w-full truncate">{progress.chapterTitle}</Badge> : null}
        {progress.textLength > 0 ? <Badge variant="outline">已输出 {progress.textLength} 字</Badge> : null}
        {progress.done ? <Badge variant="outline">本集完成</Badge> : null}
      </div>
      {progress.text ? (
        <pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap break-words rounded bg-background/80 p-2 text-xs leading-relaxed text-foreground">
          {progress.text}
        </pre>
      ) : (
        <div className="mt-2 text-xs text-muted-foreground">等待模型返回内容</div>
      )}
    </div>
  );
}

function WorkflowProgress({ progress }: { progress: StepWorkflowProgress }) {
  const node = progress.activeNode;
  const active = ["pending", "queued", "running", "cancelling"].includes(progress.status);
  const nodeStage = workflowNodeStageLabel(node?.nodeType || "", node?.nodeKey || "");
  const episodeLabel = node
    ? workflowEpisodeProgressLabel({
        workflowType: progress.workflowType,
        episodeIndex: node.episodeIndex,
        episodeTotal: node.episodeTotal,
        batchIndex: node.batchIndex,
        batchTotal: node.batchTotal,
        totalItems: progress.totalItems,
      })
    : "";
  return (
    <div className="mt-2 min-w-0 overflow-hidden rounded-md border border-primary/20 bg-primary/5 p-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
        {active ? <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" /> : <CheckCircle2 className="h-3.5 w-3.5 text-primary" />}
        <span className="font-medium text-primary">后台任务</span>
        <Badge variant="outline">{stepStatusLabel(progress.status)}</Badge>
        {progress.workflowType === "source_to_script" && progress.totalItems > 0 ? (
          <Badge variant="outline">本次目标 {progress.totalItems} 集</Badge>
        ) : null}
        {node?.shotNo ? <Badge variant="outline">第 {node.shotNo} 个分镜</Badge> : null}
        {!node?.shotNo && episodeLabel ? <Badge variant="outline">{episodeLabel}</Badge> : null}
        {node?.episodeTitle ? <Badge variant="outline" className="max-w-full truncate">{node.episodeTitle}</Badge> : null}
        {nodeStage ? <Badge variant="outline">{nodeStage}</Badge> : null}
        <Badge variant="outline">节点 {progress.completedNodes}/{progress.totalNodes}</Badge>
      </div>
      {node?.partialText ? (
        <pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap break-words rounded bg-background/80 p-2 text-xs leading-relaxed text-foreground">
          {node.partialText}
        </pre>
      ) : node?.status === "running" ? (
        <div className="mt-2 text-xs text-muted-foreground">
          {node?.shotNo && nodeStage ? `正在处理第 ${node.shotNo} 个分镜 · ${nodeStage}` : "正在处理当前任务"}
        </div>
      ) : null}
      {node?.errorMessage ? <div className="mt-2 text-xs text-destructive">{localizePlatformError(node.errorMessage, node.errorCode)}</div> : null}
    </div>
  );
}

function QuestionPrompt({
  prompt,
  customAnswer,
  onCustomAnswerChange,
  onSubmitCustomAnswer,
  onSelectOption,
  onSkip,
  busy,
}: {
  prompt: AgentQuestionPrompt;
  customAnswer: string;
  onCustomAnswerChange: (value: string) => void;
  onSubmitCustomAnswer: () => void;
  onSelectOption: (option: AgentQuestionOption) => void;
  onSkip: () => void;
  busy?: boolean;
}) {
  return (
    <div className="mt-3 space-y-3 rounded-md border border-primary/20 bg-primary/5 p-3">
      <div>
        <div className="text-xs font-medium text-primary">助手需要确认</div>
        <div className="mt-1 break-words text-sm font-medium">{prompt.question}</div>
      </div>
      {prompt.options.length > 0 ? (
        <div className="grid gap-2">
          {prompt.options.map((option) => (
            <Button
              key={option.id}
              type="button"
              variant={option.id === prompt.defaultOptionId ? "default" : "outline"}
              className="h-auto justify-start whitespace-normal px-3 py-2 text-left"
              onClick={() => onSelectOption(option)}
              disabled={busy}
            >
              <span className="min-w-0">
                <span className="block text-sm">{option.label}</span>
                {option.description ? <span className="mt-0.5 block text-xs opacity-75">{option.description}</span> : null}
              </span>
            </Button>
          ))}
        </div>
      ) : null}
      {prompt.allowCustom ? (
        <div className="space-y-2">
          <Textarea
            value={customAnswer}
            onChange={(event) => onCustomAnswerChange(event.target.value)}
            placeholder="输入自定义下一步"
            className="min-h-20 bg-background"
          />
          <div className="flex flex-wrap justify-end gap-2">
            <Button size="sm" variant="outline" onClick={onSkip} disabled={busy}>
              跳过
            </Button>
            <Button size="sm" onClick={onSubmitCustomAnswer} disabled={busy || !customAnswer.trim()}>
              发送
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex justify-end">
          <Button size="sm" variant="outline" onClick={onSkip} disabled={busy}>
            跳过
          </Button>
        </div>
      )}
    </div>
  );
}

function ToolImpact({ step }: { step: AgentStep }) {
  const expected = stringValue(asRecord(step.supervisorDecision)?.expectedResult);
  const input = step.input || {};
  const chips = toolImpactChips(step.toolName, input);
  return (
    <div className="mt-2 min-w-0 max-w-full space-y-1.5">
      <div className="flex min-w-0 max-w-full flex-wrap gap-1.5">
        {chips.map((chip) => (
          <Badge key={chip} variant="secondary" className="min-w-0 max-w-full truncate">
            {chip}
          </Badge>
        ))}
      </div>
      {expected ? <div className="break-words text-xs text-muted-foreground">{expected}</div> : null}
    </div>
  );
}

function VerifierLine({ verifier }: { verifier: Record<string, unknown> | null }) {
  if (!verifier) {
    return null;
  }
  const status = stringValue(verifier.status);
  const verifierError = stringValue(verifier.errorMessage);
  const text = stringValue(verifier.summary) || localizePlatformError(verifierError, stringValue(verifier.errorCode), "");
  if (!text) {
    return null;
  }
  return (
    <div className={status === "failed" ? "mt-2 break-words text-xs text-destructive" : "mt-2 break-words text-xs text-emerald-700"}>
      校验：{text}
    </div>
  );
}

function DetailBlock({ icon, label, value }: { icon: React.ReactNode; label: string; value: JsonValue }) {
  return (
    <div className="w-full min-w-0 max-w-full overflow-hidden rounded border bg-muted/40 p-2">
      <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        {icon}
        {label}
      </div>
      <pre className="max-h-28 overflow-auto whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground">
        {prettyJSON(value)}
      </pre>
    </div>
  );
}

function TechnicalDetails({ items }: { items: Array<[string, string]> }) {
  if (items.length === 0) {
    return null;
  }
  return (
    <details className="mt-2">
      <summary className="cursor-pointer text-xs text-muted-foreground">技术信息</summary>
      <div className="mt-1 grid gap-1 rounded bg-muted px-2 py-1 text-xs text-muted-foreground">
        {items.map(([label, value]) => (
          <div key={label} className="truncate">
            {label}：{value}
          </div>
        ))}
      </div>
    </details>
  );
}

function TaskStatusBadge({ status }: { status: string }) {
  return <Badge variant={taskStatusVariant(status)}>{taskStatusLabel(status)}</Badge>;
}

function StepIcon({ status }: { status: string }) {
  if (["succeeded", "approved"].includes(status)) {
    return <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-600" />;
  }
  if (["failed", "blocked", "skipped", "cancelled"].includes(status)) {
    return <XCircle className="h-4 w-4 shrink-0 text-destructive" />;
  }
  if (status === "running") {
    return <Loader2 className="h-4 w-4 shrink-0 animate-spin text-blue-600" />;
  }
  return <Clock3 className="h-4 w-4 shrink-0 text-muted-foreground" />;
}

function canCancel(status: string) {
  return !["succeeded", "failed", "cancelled"].includes(status);
}

function canResume(status: string) {
  return ["blocked", "failed", "waiting_approval"].includes(status);
}

function taskStatusVariant(status: string) {
  if (status === "succeeded") return "default";
  if (["failed", "blocked", "cancelled"].includes(status)) return "destructive";
  if (status === "waiting_approval") return "secondary";
  return "outline";
}

function stepStatusVariant(status: string) {
  if (["succeeded", "approved"].includes(status)) return "default";
  if (["failed", "blocked", "skipped", "cancelled"].includes(status)) return "destructive";
  if (status === "waiting_approval") return "secondary";
  return "outline";
}

function taskStatusLabel(status: string) {
  const labels: Record<string, string> = {
    queued: "排队中",
    planning: "规划中",
    waiting_approval: "待确认",
    running: "执行中",
    succeeded: "已完成",
    failed: "失败",
    blocked: "已阻断",
    cancelled: "已取消",
  };
  return labels[status] || "未知状态";
}

function stepStatusLabel(status: string) {
  const labels: Record<string, string> = {
    planned: "已计划",
    waiting_approval: "待确认",
    approved: "已批准",
    running: "执行中",
    succeeded: "已完成",
    failed: "失败",
    blocked: "已阻断",
    skipped: "已跳过",
    cancelled: "已取消",
  };
  return labels[status] || "未知状态";
}

function riskLabel(risk: string) {
  const labels: Record<string, string> = {
    read: "读取",
    draft: "草稿",
    write: "写入",
    workflow: "工作流",
    costed: "成本",
    destructive: "高风险",
    admin: "管理",
  };
  return labels[risk] || "未知风险";
}

function permissionLabel(permission: string) {
  const labels: Record<string, string> = {
    "project.read": "项目读取",
    "project.write": "项目写入",
    "source.read": "原文读取",
    "script.read": "剧本读取",
    "script.write": "剧本写入",
    "asset.read": "资产读取",
    "asset.write": "资产写入",
    "artifact.read": "成果读取",
    "workflow.read": "工作流读取",
    "workflow.run": "工作流执行",
    "workflow.cancel": "工作流取消",
    "provider.read": "供应商读取",
    "provider.manage": "供应商管理",
    "prompt.read": "提示词读取",
    "prompt.manage": "提示词管理",
  };
  return labels[permission] || permission;
}

function agentPermissionModeLabel(value: string) {
  const labels: Record<string, string> = {
    require_approval: "需批准",
    auto_approve: "自动审批",
    full_access: "完全访问",
  };
  return labels[value] || "需批准";
}

function toolLabel(tool: string) {
  const labels: Record<string, string> = {
    "agent.ask_user": "询问用户",
    "project.read_summary": "读取项目摘要",
    "source.list": "列出原文",
    "source.list_chapters": "列出分集章节",
    "source.update": "覆盖原文",
    "source.delete": "删除原文",
    "script.list": "列出剧本",
    "script.get": "读取剧本",
    "script.update_episode": "覆盖剧本分集",
    "script.generate_from_source": "生成剧本",
    "script.rewrite": "改写剧本",
    "script.rewrite_preview": "剧本改写预览",
    "script.create_version": "创建剧本版本",
    "script.activate_version": "激活剧本版本",
    "script.delete": "删除剧本",
    "asset.list": "列出资产",
    "asset.get": "读取资产卡",
    "asset.update": "更新资产",
    "asset.revise_prompt": "修订资产提示词",
    "asset.batch_generate_prompts": "批量生成资产提示词",
    "asset.batch_generate_images": "批量生成资产图片",
    "asset.delete": "删除资产",
    "storyboard.list": "列出分镜",
    "storyboard.update_shot": "更新分镜",
    "storyboard.reorder": "重排分镜",
    "timeline.update_clip": "更新时间线片段",
    "workflow.read_runs": "列出任务",
    "workflow.read_nodes": "读取任务节点",
    "workflow.read_shots": "读取任务镜头",
    "review.list_items": "读取审阅问题",
    "review.run": "运行审阅",
    "review.generate_fix": "生成修复建议",
    "review.apply_fix": "应用修复",
    "review.dismiss_fix": "忽略修复",
    "artifact.list": "列出成果",
    "artifact.preview_url": "生成预览链接",
    "provider.list_status": "读取供应商状态",
    "provider.test_model": "测试模型",
    "provider.install_catalog_preset": "安装渠道预设",
    "provider.update_account": "更新供应商",
    "provider.update_model": "更新模型",
    "prompt.render_test": "提示词测试",
    "prompt.create_version": "创建提示词版本",
    "prompt.activate_version": "激活提示词版本",
    "workflow.start": "启动工作流",
    "workflow.cancel": "取消工作流",
    "shot.status": "镜头状态",
    "shot.generate_image_prompts": "生成图片提示词",
    "shot.generate_missing_images": "生成缺失图片",
    "shot.generate_missing_videos": "生成缺失视频",
    "shot.cancel_running_videos": "取消镜头视频",
    "timeline.compose": "合成时间线",
    "final_video.activate": "激活成片",
    "commerce.project.read_summary": "读取带货项目摘要",
    "commerce.product.get": "读取商品配置",
    "commerce.product.references.list": "列出商品参考图",
    "commerce.product.update": "修改商品配置",
    "commerce.script.list": "列出广告脚本",
    "commerce.script.get": "读取广告脚本",
    "commerce.script.revise": "按要求改写广告脚本",
    "commerce.script.create": "新增广告脚本",
    "commerce.script.update": "修改广告脚本",
    "commerce.script.archive": "归档广告脚本",
    "commerce.script.derive.preview": "预览脚本裂变",
    "commerce.script.derive.batch": "创建脚本裂变",
    "commerce.script.derivation.get": "查看脚本裂变",
    "commerce.script.derive.retry_failed": "重试失败变体",
    "commerce.script.derive.cancel": "取消脚本裂变",
    "commerce.video.options": "读取视频生成选项",
    "commerce.video.list": "列出视频任务",
    "commerce.video.get": "查看视频任务",
    "commerce.video.generate": "生成带货视频",
    "commerce.video.cancel": "取消视频任务",
  };
  return labels[tool] || tool;
}

function supervisorReasonLabel(reason: string) {
  const labels: Record<string, string> = {
    approval_required: "需要确认",
    user_question: "等待用户选择",
    missing_permission: "权限不足",
    plan_only: "仅规划",
    unknown_risk: "未知风险",
    workflow_already_running: "同类工作流运行中",
    invalid_workflow_request: "工作流参数无效",
    shot_videos_not_ready: "镜头视频未就绪",
    no_target_shots: "没有可处理镜头",
    shot_image_required: "缺少镜头图片",
    video_generation_disabled: "视频生成已禁用",
    agent_runtime_action_limit: "助手已达到本次任务的最大行动数",
    agent_runtime_repeated_action: "助手连续重复了没有推进任务的相同操作",
  };
  return labels[reason] || reason;
}

function toolImpactChips(toolName: string, input: JsonRecord) {
  const chips = [toolLabel(toolName)];
  const workflowType = stringValue(input.workflowType);
  const action = stringValue(input.action);
  const targetType = stringValue(input.targetType);
  const ids = ["sourceId", "scriptId", "versionId", "assetId", "shotId", "clipId", "fixId", "workflowRunId", "finalVideoId"]
    .map((key) => [key, stringValue(input[key])] as const)
    .filter(([, value]) => value);
  if (workflowType) chips.push(workflowType);
  if (action) chips.push(action);
  if (targetType) chips.push(targetType);
  for (const [key, value] of ids.slice(0, 3)) {
    chips.push(`${fieldLabel(key)} ${shortID(value)}`);
  }
  if (input.maxConcurrency) {
    chips.push(`并发 ${String(input.maxConcurrency)}`);
  }
  return chips;
}

function agentQuestionPrompt(approval: AgentApproval | undefined, step: AgentStep): AgentQuestionPrompt | null {
  const requested = asRecord(approval?.requestedPayload);
  const isQuestion =
    approval?.approvalType === "question" ||
    stringValue(requested?.interactionType) === "question" ||
    step.toolName === "agent.ask_user";
  if (!isQuestion) {
    return null;
  }
  const question = firstNonEmptyString(
    stringValue(requested?.question),
    stringValue(step.input?.question),
    stringValue(step.dryRunOutput?.question),
    "请选择下一步。",
  );
  return {
    question,
    options: agentQuestionOptions(requested?.options ?? step.input?.options ?? step.dryRunOutput?.options),
    allowCustom: booleanValue(requested?.allowCustom) || booleanValue(step.input?.allowCustom),
    defaultOptionId: firstNonEmptyString(stringValue(requested?.defaultOptionId), stringValue(step.input?.defaultOptionId)),
  };
}

function agentQuestionOptions(value: unknown): AgentQuestionOption[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .map((item) => asRecord(item))
    .filter((item): item is Record<string, unknown> => Boolean(item))
    .map((item) => {
      const option: AgentQuestionOption = {
        id: stringValue(item.id),
        label: stringValue(item.label),
      };
      const description = stringValue(item.description);
      const nextGoal = stringValue(item.nextGoal);
      const jsonValue = toJsonValue(item.value);
      if (description) option.description = description;
      if (nextGoal) option.nextGoal = nextGoal;
      if (jsonValue !== undefined) option.value = jsonValue;
      return option;
    })
    .filter((option) => option.id && option.label);
}

function fieldLabel(field: string) {
  const labels: Record<string, string> = {
    sourceId: "原文",
    scriptId: "剧本",
    versionId: "版本",
    assetId: "资产",
    shotId: "镜头",
    clipId: "片段",
    fixId: "修复",
    workflowRunId: "工作流",
    finalVideoId: "成片",
  };
  return labels[field] || field;
}

function approvalDialogSummary(step: AgentStep) {
  return `${toolLabel(step.toolName)}，${riskLabel(step.risk)}，${stepStatusLabel(step.status)}`;
}

function stepOutputText(step: AgentStep) {
  const output = step.output || {};
  return stringValue(output.summary) || stringValue(asRecord(output.data)?.summary);
}

function stepStreamProgress(step: AgentStep): StepStreamProgress | null {
  const progress = asRecord(step.output?.progress);
  if (!progress || stringValue(progress.kind) !== "stream_text") {
    return null;
  }
  return {
    episodeIndex: Math.max(1, numberValue(progress.episodeIndex)),
    episodeTotal: Math.max(1, numberValue(progress.episodeTotal)),
    chapterTitle: stringValue(progress.chapterTitle),
    text: stringValue(progress.text),
    textLength: numberValue(progress.textLength),
    done: booleanValue(progress.done),
    updatedAt: stringValue(progress.updatedAt),
  };
}

function stepWorkflowProgress(step: AgentStep): StepWorkflowProgress | null {
  const data = asRecord(step.output?.data);
  const progress = asRecord(data?.workflowProgress);
  if (!progress) {
    return null;
  }
  const activeNode = asRecord(progress.activeNode);
  const nodeInput = asRecord(activeNode?.input);
  const nodeOutput = asRecord(activeNode?.output);
  return {
    workflowRunId: stringValue(progress.workflowRunId),
    workflowType: stringValue(progress.workflowType),
    status: stringValue(progress.status),
    totalItems: numberValue(progress.totalItems),
    completedItems: numberValue(progress.completedItems),
    failedItems: numberValue(progress.failedItems),
    totalNodes: numberValue(progress.totalNodes),
    completedNodes: numberValue(progress.completedNodes),
    ...(activeNode
      ? {
          activeNode: {
            nodeKey: stringValue(activeNode.nodeKey),
            nodeType: stringValue(activeNode.nodeType),
            status: stringValue(activeNode.status),
            shotId: stringValue(nodeInput?.shotId),
            shotNo: numberValue(nodeInput?.shotNo),
            anchorRole: stringValue(nodeInput?.anchorRole),
            episodeIndex: numberValue(nodeInput?.episodeIndex),
            episodeTotal: numberValue(nodeInput?.episodeTotal),
            batchIndex: numberValue(nodeInput?.batchIndex),
            batchTotal: numberValue(nodeInput?.batchTotal),
            episodeTitle: stringValue(nodeInput?.episodeTitle),
            partialText: stringValue(nodeOutput?.partialText),
            receivedChars: numberValue(nodeOutput?.receivedChars),
            errorCode: stringValue(activeNode.errorCode),
            errorMessage: stringValue(activeNode.errorMessage),
          },
        }
      : {}),
  };
}

function stepScriptDerivationPlan(step: AgentStep): ScriptDerivationPlanView | null {
  if (!["commerce.script.derive.preview", "commerce.script.derive.batch"].includes(step.toolName)) {
    return null;
  }
  const input = asRecord(step.input) || {};
  const dryRun = asRecord(step.dryRunOutput) || {};
  const data = asRecord(step.output?.data) || {};
  const confirmation = asRecord(data.confirmation) || {};
  const rawVariations = Array.isArray(confirmation.variations)
    ? confirmation.variations
    : Array.isArray(data.variations)
      ? data.variations
      : Array.isArray(input.variations)
        ? input.variations
        : [];
  const variations = rawVariations
    .map((value) => asRecord(value))
    .filter((value): value is Record<string, unknown> => Boolean(value))
    .map((value, index) => ({
      key: stringValue(value.key) || `variation-${index + 1}`,
      label: stringValue(value.label) || `方案 ${index + 1}`,
      brief: stringValue(value.brief),
    }));
  if (variations.length === 0 && step.toolName === "commerce.script.derive.preview") {
    return null;
  }
  return {
    sourceTitle: firstNonEmptyString(
      stringValue(confirmation.sourceScriptTitle),
      stringValue(data.sourceScriptTitle),
      stringValue(dryRun.scriptUnitTitle),
    ),
    dimension: firstNonEmptyString(
      stringValue(confirmation.dimension),
      stringValue(data.dimension),
      stringValue(input.dimension),
    ),
    instruction: firstNonEmptyString(
      stringValue(data.instruction),
      stringValue(input.instruction),
    ),
    preserve: arrayOfStrings(confirmation.preserve).length > 0
      ? arrayOfStrings(confirmation.preserve)
      : arrayOfStrings(data.preserve).length > 0
        ? arrayOfStrings(data.preserve)
        : arrayOfStrings(input.preserve),
    variations,
  };
}

function stepScriptDerivationProgress(step: AgentStep): ScriptDerivationBatchView | null {
  const data = asRecord(step.output?.data);
  const batch = asRecord(data?.scriptDerivation);
  if (!batch || !stringValue(batch.id)) {
    return null;
  }
  const lineageItems = Array.isArray(batch.lineageResults)
    ? batch.lineageResults
        .map((value) => asRecord(asRecord(value)?.latestResult))
        .filter((value): value is Record<string, unknown> => Boolean(value))
    : [];
  const currentItems = Array.isArray(batch.items)
    ? batch.items
        .map((value) => asRecord(value))
        .filter((value): value is Record<string, unknown> => Boolean(value))
    : [];
  const items = (lineageItems.length > 0 ? lineageItems : currentItems)
    .map((value) => ({
      id: stringValue(value.id),
      batchId: stringValue(value.batchId),
      inputOrdinal: numberValue(value.inputOrdinal),
      variationLabel: stringValue(value.variationLabel),
      variationBrief: stringValue(value.variationBrief),
      status: stringValue(value.status),
      outputScriptUnitId: stringValue(value.outputScriptUnitId),
      errorCode: stringValue(value.errorCode),
      errorMessage: stringValue(value.errorMessage),
    }));
  const succeededCount = items.filter((item) => item.status === "succeeded").length;
  const failedRetryableCount = items.filter((item) => item.status === "failed_retryable").length;
  const failedTerminalCount = items.filter((item) => item.status === "failed_terminal").length;
  const cancelledCount = items.filter((item) => item.status === "cancelled").length;
  const queuedCount = items.filter((item) => item.status === "queued").length;
  const runningCount = items.filter((item) => ["running", "reviewing"].includes(item.status)).length;
  const requestedCount = lineageItems.length > 0
    ? items.length
    : numberValue(batch.requestedCount);
  const effectiveStatus = lineageItems.length > 0
    ? scriptDerivationLineageStatus(items.map((item) => item.status))
    : stringValue(batch.status);
  const retryBatchId = items.find((item) => item.status === "failed_retryable")?.batchId
    || stringValue(batch.id);
  return {
    id: stringValue(batch.id),
    retryBatchId,
    status: effectiveStatus,
    requestedCount,
    queuedCount: lineageItems.length > 0 ? queuedCount : numberValue(batch.queuedCount),
    runningCount: lineageItems.length > 0 ? runningCount : numberValue(batch.runningCount),
    succeededCount: lineageItems.length > 0 ? succeededCount : numberValue(batch.succeededCount),
    failedRetryableCount: lineageItems.length > 0
      ? failedRetryableCount
      : numberValue(batch.failedRetryableCount),
    failedTerminalCount: lineageItems.length > 0
      ? failedTerminalCount
      : numberValue(batch.failedTerminalCount),
    cancelledCount: lineageItems.length > 0 ? cancelledCount : numberValue(batch.cancelledCount),
    items,
  };
}

function scriptDerivationLineageStatus(statuses: string[]) {
  if (statuses.some((status) => ["queued", "running", "reviewing"].includes(status))) {
    return "running";
  }
  const succeeded = statuses.filter((status) => status === "succeeded").length;
  const failed = statuses.filter((status) => ["failed_retryable", "failed_terminal"].includes(status)).length;
  const cancelled = statuses.filter((status) => status === "cancelled").length;
  if (statuses.length > 0 && succeeded === statuses.length) {
    return "succeeded";
  }
  if (succeeded > 0) {
    return "partial_succeeded";
  }
  if (statuses.length > 0 && cancelled === statuses.length) {
    return "cancelled";
  }
  if (failed > 0) {
    return "failed";
  }
  return "queued";
}

function scriptDerivationDimensionLabel(value: string) {
  switch (value) {
    case "scene":
      return "场景";
    case "audience":
      return "受众";
    case "hook":
      return "开场钩子";
    case "tone":
      return "表达语气";
    case "language":
      return "语言";
    case "cta":
      return "行动号召";
    case "custom":
      return "自定义";
    default:
      return value || "脚本裂变";
  }
}

function scriptDerivationPreserveLabel(value: string) {
  switch (value) {
    case "product_facts":
      return "商品事实";
    case "selling_points":
      return "核心卖点";
    case "prohibited_claims":
      return "宣传边界";
    case "cta":
      return "行动号召";
    case "language":
      return "原脚本语言";
    case "approximate_duration":
      return "目标时长";
    default:
      return value;
  }
}

function scriptDerivationBatchVariant(
  status: string,
): "outline" | "secondary" | "destructive" | "default" {
  if (["failed", "failed_terminal"].includes(status)) return "destructive";
  if (["queued", "running", "cancelling", "partial_succeeded"].includes(status)) return "secondary";
  if (["succeeded", "completed"].includes(status)) return "outline";
  return "default";
}

function scriptDerivationBatchStatusLabel(status: string) {
  switch (status) {
    case "queued":
      return "等待执行";
    case "running":
      return "运行中";
    case "cancelling":
      return "取消中";
    case "cancelled":
      return "已取消";
    case "partial_succeeded":
      return "部分完成";
    case "failed":
    case "failed_terminal":
      return "失败";
    case "succeeded":
    case "completed":
      return "已完成";
    default:
      return status || "未知状态";
  }
}

function scriptDerivationItemStatusLabel(status: string) {
  switch (status) {
    case "queued":
      return "等待执行";
    case "running":
      return "生成中";
    case "reviewing":
      return "审核中";
    case "succeeded":
      return "已生成";
    case "failed_retryable":
      return "可重试";
    case "failed_terminal":
    case "failed":
      return "失败";
    case "cancelled":
      return "已取消";
    default:
      return status || "未知状态";
  }
}

function workflowNodeStageLabel(nodeType: string, nodeKey: string) {
  if (nodeType === "agent.image_prompt.generate" || nodeKey.startsWith("generate_shot_image_prompt_")) return "图片提示词生成";
  if (nodeType === "agent.image_prompt.review" || nodeKey.startsWith("review_shot_image_prompt_")) return "图片提示词审核";
  if (nodeType === "agent.video_prompt.generate" || nodeKey.startsWith("generate_shot_video_prompt_")) return "视频提示词生成";
  if (nodeType === "agent.video_prompt.review" || nodeKey.startsWith("review_shot_video_prompt_")) return "视频提示词审核";
  return "";
}

function stepDisplayStatus(step: AgentStep, workflowProgress: StepWorkflowProgress | null) {
  if (!workflowProgress) {
    return step.status;
  }
  if (["pending", "queued", "running", "cancelling"].includes(workflowProgress.status)) {
    return "running";
  }
  if (workflowProgress.status === "failed") {
    return "failed";
  }
  if (workflowProgress.status === "cancelled") {
    return "cancelled";
  }
  return step.status;
}

function stepDryRunText(step: AgentStep) {
  return stringValue(step.dryRunOutput?.summary) || stringValue(step.dryRunOutput?.explanation);
}

function stepWorkflowRunId(step: AgentStep) {
  const data = asRecord(step.output?.data);
  const workflowRun = asRecord(data?.workflowRun);
  return stringValue(data?.workflowRunId) || stringValue(workflowRun?.id);
}

function stepNextActions(step: AgentStep) {
  const output = asRecord(step.output);
  const raw = output?.nextActions;
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw
    .map((item) => asRecord(item))
    .filter((item): item is Record<string, unknown> => Boolean(item))
    .map((item) => ({
      label: stringValue(item.label),
      reason: stringValue(item.reason),
      tool: stringValue(item.tool),
    }))
    .filter((item) => item.label);
}

function stepBusinessLinks(projectId: string, step: AgentStep) {
  const input = asRecord(step.input) || {};
  const output = asRecord(step.output) || {};
  const data = asRecord(output.data) || {};
  const links: Array<{ label: string; href: string }> = [];
  addBusinessLink(links, input.sourceId ?? data.sourceId, "查看内容", projectHref(projectId, "content"));
  addBusinessLink(links, input.scriptId ?? data.scriptId, "查看剧本", projectHref(projectId, "scripts"));
  addBusinessLink(links, input.assetId ?? data.assetId, "查看资产", projectHref(projectId, "assets"));
  addBusinessLink(links, input.shotId ?? data.shotId, "查看分镜", projectHref(projectId, "storyboard"));
  addBusinessLink(links, input.workflowRunId ?? data.workflowRunId ?? asRecord(data.workflowRun)?.id, "查看任务", projectHref(projectId, "video"));
  addBusinessLink(links, input.finalVideoId ?? data.finalVideoId, "查看成片", projectHref(projectId, "final"));
  addBusinessLink(links, input.fixId ?? data.fixId, "查看审阅", projectHref(projectId, "review"));
  addBusinessLink(
    links,
    input.scriptUnitId ?? data.scriptUnitId ?? data.commerceScriptUnitId,
    "查看广告脚本",
    projectHref(projectId, "commerce/video"),
  );
  addBusinessLink(
    links,
    input.jobId ?? data.jobId,
    "查看视频生成",
    projectHref(projectId, "commerce/video"),
  );
  const directVideo = asRecord(data.directVideo);
  const previewUrl = firstNonEmptyString(
    stringValue(directVideo?.outputPreviewUrl),
    stringValue(data.outputPreviewUrl),
  );
  if (previewUrl) {
    links.push({ label: "预览视频", href: previewUrl });
  }
  if (Array.isArray(data.artifacts) || Array.isArray(data.artifactIds) || stringValue(data.artifactId)) {
    links.push({ label: "查看产物", href: projectHref(projectId, "assets") });
  }
  return dedupeBusinessLinks(links);
}

function isExternalHref(href: string) {
  return /^https?:\/\//i.test(href);
}

function addBusinessLink(links: Array<{ label: string; href: string }>, value: unknown, label: string, href: string) {
  if (stringValue(value)) {
    links.push({ label, href });
  }
}

function dedupeBusinessLinks(links: Array<{ label: string; href: string }>) {
  const seen = new Set<string>();
  return links.filter((link) => {
    const key = `${link.label}:${link.href}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function agentTaskImageAttachmentLabels(task: AgentTask | null) {
  const raw = task?.constraints?.attachments;
  if (!Array.isArray(raw)) return [];
  return raw
    .map((value) => asRecord(value))
    .filter((value): value is Record<string, unknown> => Boolean(value))
    .map((value) => ({
      id: stringValue(value.attachmentId),
      fileName: stringValue(value.fileName) || "已附加图片",
    }))
    .filter((value) => value.id);
}

function arrayOfStrings(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function firstNonEmptyString(...values: string[]) {
  return values.find((value) => value.trim())?.trim() || "";
}

function booleanValue(value: unknown) {
  return value === true;
}

function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function toJsonValue(value: unknown): JsonValue | undefined {
  if (value === undefined) {
    return undefined;
  }
  try {
    JSON.stringify(value);
    return value as JsonValue;
  } catch {
    return undefined;
  }
}

function formatCents(value: number) {
  if (value < 1) {
    return `${value.toFixed(2)} 分`;
  }
  return `${value.toFixed(0)} 分`;
}

function shortID(value: string) {
  return value.length > 8 ? value.slice(0, 8) : value;
}

function prettyJSON(value: unknown) {
  try {
    return JSON.stringify(value || {}, null, 2);
  } catch {
    return "{}";
  }
}

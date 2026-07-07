"use client";

import { useMemo, useState } from "react";
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
import type { AgentApproval, AgentStep, AgentTask, JsonRecord, JsonValue } from "@/lib/types";
import { projectHref } from "@/lib/routes";
import Link from "next/link";
import type { Route } from "next";
import {
  AlertTriangle,
  CheckCircle2,
  Clock3,
  FileDiff,
  PlayCircle,
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

      {task ? (
        <div className="w-full min-w-0 max-w-full space-y-3 overflow-hidden">
          <ResultSummary task={task} />
          <div className="max-h-[min(30rem,52dvh)] w-full max-w-full overflow-y-auto overflow-x-hidden pr-2">
            <div className="w-full min-w-0 max-w-full space-y-2">
              {(task.steps || []).map((step) => (
                <ToolCallCard
                  key={step.id}
                  projectId={projectId}
                  step={step}
                  approval={approvalsByStep.get(step.id)}
                  onApprove={() => setPendingDecision({ kind: "approve", step })}
                  onReject={() => setPendingDecision({ kind: "reject", step })}
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

function ResultSummary({ task }: { task: AgentTask }) {
  const steps = task.steps || [];
  const done = steps.filter((step) => step.status === "succeeded").length;
  const failed = steps.filter((step) => ["failed", "blocked"].includes(step.status)).length;
  const waiting = steps.filter((step) => step.status === "waiting_approval").length;
  const summary = stringValue(task.summary?.summary);
  const permissionMode = stringValue(task.constraints?.permissionMode);

  return (
    <div className="w-full min-w-0 max-w-full rounded-md border bg-muted/30 px-3 py-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
        <Badge variant="outline">完成 {done}</Badge>
        <Badge variant={waiting > 0 ? "secondary" : "outline"}>待确认 {waiting}</Badge>
        <Badge variant={failed > 0 ? "destructive" : "outline"}>异常 {failed}</Badge>
        <Badge variant="outline">权限 {agentPermissionModeLabel(permissionMode)}</Badge>
      </div>
      {task.temporalWorkflowId ? <TechnicalDetails items={[["后台任务", shortID(task.temporalWorkflowId)]]} /> : null}
      <div className="mt-2 break-words text-xs text-muted-foreground">{summary || task.errorMessage || "任务已创建。"}</div>
    </div>
  );
}

function ToolCallCard({
  projectId,
  step,
  approval,
  onApprove,
  onReject,
  busy,
}: {
  projectId: string;
  step: AgentStep;
  approval?: AgentApproval;
  onApprove: () => void;
  onReject: () => void;
  busy?: boolean;
}) {
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

  return (
    <article className="w-full min-w-0 max-w-full overflow-hidden rounded-md border bg-background p-3">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <StepIcon status={step.status} />
            <span className="min-w-0 truncate text-sm font-medium">{toolLabel(step.toolName)}</span>
          </div>
          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1.5">
            <Badge variant="outline">{riskLabel(step.risk)}</Badge>
            <Badge variant={stepStatusVariant(step.status)}>{stepStatusLabel(step.status)}</Badge>
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
              <Link href={link.href as Route}>{link.label}</Link>
            </Button>
          ))}
        </div>
      ) : null}
      {stepWorkflowRunId(step) ? <TechnicalDetails items={[["任务编号", shortID(stepWorkflowRunId(step))]]} /> : null}
      {verifierStatus && verifierStatus !== "skipped" ? <VerifierLine verifier={verifier} /> : null}
      {step.errorMessage ? <div className="mt-2 text-xs text-destructive">{step.errorMessage}</div> : null}

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

      {waitingApproval ? (
        <div className="mt-3 flex justify-end gap-2">
          <Button size="sm" variant="outline" onClick={onReject} disabled={busy || approval?.status !== "pending"}>
            拒绝
          </Button>
          <Button size="sm" onClick={onApprove} disabled={busy || approval?.status !== "pending"}>
            <ShieldCheck className="mr-1 h-3.5 w-3.5" />
            批准
          </Button>
        </div>
      ) : null}
    </article>
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
  const text = stringValue(verifier.summary) || stringValue(verifier.errorMessage);
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
    return <PlayCircle className="h-4 w-4 shrink-0 text-blue-600" />;
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
    "project.read_summary": "读取项目摘要",
    "source.list": "列出原文",
    "source.list_chapters": "列出分集章节",
    "script.list": "列出剧本",
    "script.get": "读取剧本",
    "script.generate_from_source": "生成剧本",
    "script.rewrite": "改写剧本",
    "script.rewrite_preview": "剧本改写预览",
    "script.create_version": "创建剧本版本",
    "script.activate_version": "激活剧本版本",
    "asset.list": "列出资产",
    "asset.update": "更新资产",
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
    "shot.generate_missing_images": "生成缺失图片",
    "shot.generate_missing_videos": "生成缺失视频",
    "shot.cancel_running_videos": "取消镜头视频",
    "timeline.compose": "合成时间线",
    "final_video.activate": "激活成片",
  };
  return labels[tool] || tool;
}

function supervisorReasonLabel(reason: string) {
  const labels: Record<string, string> = {
    approval_required: "需要确认",
    missing_permission: "权限不足",
    plan_only: "仅规划",
    unknown_risk: "未知风险",
    workflow_already_running: "同类工作流运行中",
    invalid_workflow_request: "工作流参数无效",
    shot_videos_not_ready: "镜头视频未就绪",
    no_target_shots: "没有可处理镜头",
    shot_image_required: "缺少镜头图片",
    video_generation_disabled: "视频生成已禁用",
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
  if (Array.isArray(data.artifacts) || Array.isArray(data.artifactIds) || stringValue(data.artifactId)) {
    links.push({ label: "查看产物", href: projectHref(projectId, "assets") });
  }
  return dedupeBusinessLinks(links);
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

function arrayOfStrings(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
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

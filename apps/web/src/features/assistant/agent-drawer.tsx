"use client";

import { useEffect, useState } from "react";
import { useAgentDrawerStore } from "@/lib/stores/agent-drawer-store";
import { useAgentChat } from "./hooks/use-agent-chat";
import { useAgentTasks } from "./hooks/use-agent-tasks";
import { Button } from "@/components/ui/button";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { SessionSelector } from "./components/session-selector";
import { MessageList } from "./components/message-list";
import { MessageInput } from "./components/message-input";
import { assistantQuickActionsForProjectKind } from "./components/quick-actions";
import { AgentTaskConversationActivity } from "./components/agent-task-panel";
import { studioApi } from "@/lib/api-client";
import { userFacingErrorMessage } from "@/lib/error-localization";
import { useApiMutation } from "@/lib/query/use-api";
import type {
  AgentImageAttachment,
  AgentPermissionMode,
  AgentTask,
  JsonRecord,
  ProjectKind,
} from "@/lib/types";
import { Bot, PanelRightClose } from "lucide-react";
import { toast } from "sonner";

interface AgentDrawerProps {
  projectId: string;
  projectKind?: ProjectKind;
}

const AGENT_PERMISSION_MODE_KEY = "cineweave.agent.permissionMode";
const AGENT_PERMISSION_MODES: Array<{ value: AgentPermissionMode; label: string }> = [
  { value: "require_approval", label: "需批准" },
  { value: "auto_approve", label: "自动审批" },
  { value: "full_access", label: "完全访问" },
];

export function AgentDrawer({ projectId, projectKind }: AgentDrawerProps) {
  const {
    isOpen,
    currentSessionId,
    close,
    setSessionId,
    setAgentType,
  } = useAgentDrawerStore();

  const agentTasks = useAgentTasks(projectId, currentSessionId, isOpen);
  const { messages, isLoading } = useAgentChat(projectId, currentSessionId, isOpen, agentTasks.isActive);
  const [permissionMode, setPermissionMode] = useState<AgentPermissionMode>(() => readStoredPermissionMode());
  const [imageAttachments, setImageAttachments] = useState<AgentImageAttachment[]>([]);
  const quickActions = assistantQuickActionsForProjectKind(projectKind);
  const taskActivityKey = agentTasks.task ? agentTaskActivityKey(agentTasks.task) : "";
  const taskActivity =
    agentTasks.task || agentTasks.isLoading ? (
      <AgentTaskConversationActivity
        projectId={projectId}
        task={agentTasks.task}
        isLoading={agentTasks.isLoading}
        onApproveStep={agentTasks.approveStep}
        onRejectStep={agentTasks.rejectStep}
        onCancelTask={agentTasks.cancelTask}
        onResumeTask={agentTasks.resumeTask}
        busy={
          agentTasks.isCreatingTask ||
          agentTasks.isApprovingStep ||
          agentTasks.isRejectingStep ||
          agentTasks.isCancellingTask ||
          agentTasks.isResumingTask
        }
      />
    ) : null;

  useEffect(() => {
    setAgentType("project_agent");
  }, [setAgentType]);

  useEffect(() => {
    window.localStorage.setItem(AGENT_PERMISSION_MODE_KEY, permissionMode);
  }, [permissionMode]);

  const uploadImages = useApiMutation({
    mutationFn: async (session, files: File[]) => {
      const results = await mapWithConcurrency(files, 3, async (file) => {
        try {
          const upload = await studioApi.createAgentImageAttachmentUpload(
            session,
            projectId,
            { fileName: file.name, mimeType: file.type || "application/octet-stream" },
            `agent-image-${projectId}-${crypto.randomUUID()}`,
          );
          await studioApi.uploadAgentImageAttachmentFile(upload, file);
          const attachment = await studioApi.completeAgentImageAttachment(
            session,
            projectId,
            upload.attachmentId,
          );
          return { attachment, error: null as Error | null };
        } catch (error) {
          return {
            attachment: null,
            error: error instanceof Error ? error : new Error("图片上传失败"),
          };
        }
      });
      return results;
    },
    onSuccess: (results) => {
      const completed = results.flatMap((result) => result.attachment ? [result.attachment] : []);
      const failed = results.filter((result) => result.error);
      if (completed.length > 0) {
        setImageAttachments((current) => {
          const next = new Map(current.map((item) => [item.id, item]));
          for (const item of completed) next.set(item.id, item);
          return [...next.values()].slice(0, 8);
        });
      }
      if (failed.length > 0) {
        toast.error(
          failed.length === results.length
            ? userFacingErrorMessage(failed[0]?.error, "图片上传失败")
            : `${completed.length} 张已上传，${failed.length} 张失败`,
        );
      }
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "图片上传失败")),
  });

  const handleQuickAction = (action: string) => {
    const quickAction = quickActions.find((item) => item.id === action);
    if (quickAction) {
      agentTasks.createTask({
        goal: quickAction.goal,
        mode: quickAction.mode,
        permissionMode,
        constraints: agentImageAttachmentConstraints(imageAttachments),
      }, {
        onSuccess: () => setImageAttachments([]),
      });
    }
  };

  const handleProjectGoal = (content: string) => {
    agentTasks.createTask({
      goal: content,
      mode: "supervised",
      permissionMode,
      constraints: agentImageAttachmentConstraints(imageAttachments),
    }, {
      onSuccess: () => setImageAttachments([]),
    });
  };

  const handlePermissionModeChange = (value: string) => {
    if (isAgentPermissionMode(value)) {
      setPermissionMode(value);
    }
  };

  if (!isOpen) {
    return null;
  }

  return (
    <aside className="fixed inset-y-0 right-0 z-50 flex w-[min(100vw,440px)] flex-col border-l bg-background shadow-2xl lg:static lg:z-auto lg:h-dvh lg:w-[420px] lg:shrink-0 lg:shadow-none 2xl:w-[460px]">
      <div className="flex min-h-0 flex-1 flex-col">
        <header className="shrink-0 border-b px-5 py-4">
          <div className="flex items-center justify-between gap-3">
            <div className="flex min-w-0 items-center gap-2">
              <Bot className="h-5 w-5" />
              <h2 className="truncate text-base font-semibold">项目控制助手</h2>
            </div>
            <Button variant="ghost" size="icon" onClick={close} aria-label="关闭AI助手">
              <PanelRightClose className="h-4 w-4" />
            </Button>
          </div>
          <div className="pt-4">
            <SessionSelector
              projectId={projectId}
              currentSessionId={currentSessionId}
              onSessionChange={(sessionId) => {
                setImageAttachments([]);
                setSessionId(sessionId);
              }}
            />
            <ToggleGroup
              type="single"
              value={permissionMode}
              onValueChange={handlePermissionModeChange}
              className="mt-3 grid w-full grid-cols-3 rounded-md border bg-muted/40 p-1"
              spacing={0}
              size="sm"
              aria-label="AI助手权限模式"
            >
              {AGENT_PERMISSION_MODES.map((mode) => (
                <ToggleGroupItem key={mode.value} value={mode.value} className="h-8 min-w-0 rounded-sm px-2 text-xs">
                  {mode.label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
        </header>

        <MessageList
          messages={messages}
          isLoading={isLoading || agentTasks.isCreatingTask}
          activityKey={taskActivityKey}
          activity={taskActivity}
        />

        <div className="shrink-0 border-t px-5 py-4">
          <MessageInput
            onSend={handleProjectGoal}
            quickActions={quickActions}
            onQuickAction={handleQuickAction}
            isLoading={agentTasks.isCreatingTask}
            isUploading={uploadImages.isPending}
            placeholder="输入项目控制目标，/ 调用工具"
            attachments={imageAttachments}
            onAttachFiles={(files) => uploadImages.mutate(files)}
            onRemoveAttachment={(attachmentId) =>
              setImageAttachments((current) => current.filter((item) => item.id !== attachmentId))
            }
          />
        </div>
      </div>
    </aside>
  );
}

function readStoredPermissionMode(): AgentPermissionMode {
  if (typeof window === "undefined") {
    return "require_approval";
  }
  const value = window.localStorage.getItem(AGENT_PERMISSION_MODE_KEY);
  return isAgentPermissionMode(value) ? value : "require_approval";
}

function isAgentPermissionMode(value: unknown): value is AgentPermissionMode {
  return value === "require_approval" || value === "auto_approve" || value === "full_access";
}

function agentTaskActivityKey(task: AgentTask) {
  const stepKey = (task.steps || [])
    .map((step) => {
      const progress = step.output?.progress;
      const progressKey = progress && typeof progress === "object" && !Array.isArray(progress)
        ? `${String(progress.updatedAt || "")}:${String(progress.textLength || "")}:${String(progress.done || "")}`
        : "";
      return `${step.id}:${step.status}:${step.updatedAt}:${step.completedAt || ""}:${progressKey}`;
    })
    .join("|");
  return `${task.id}:${task.status}:${task.updatedAt}:${task.completedAt || ""}:${stepKey}`;
}

function agentImageAttachmentConstraints(attachments: AgentImageAttachment[]): JsonRecord {
  if (attachments.length === 0) return {};
  return {
    attachments: attachments.map((attachment) => ({
      attachmentId: attachment.id,
      usage: "unspecified" as const,
    })),
  };
}

async function mapWithConcurrency<TInput, TOutput>(
  values: TInput[],
  concurrency: number,
  run: (value: TInput, index: number) => Promise<TOutput>,
) {
  const results = new Array<TOutput>(values.length);
  let nextIndex = 0;
  const workerCount = Math.min(Math.max(concurrency, 1), values.length);
  await Promise.all(
    Array.from({ length: workerCount }, async () => {
      while (nextIndex < values.length) {
        const index = nextIndex;
        nextIndex += 1;
        results[index] = await run(values[index], index);
      }
    }),
  );
  return results;
}

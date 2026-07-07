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
import { ASSISTANT_QUICK_ACTIONS } from "./components/quick-actions";
import { AgentTaskPanel } from "./components/agent-task-panel";
import type { AgentPermissionMode } from "@/lib/types";
import { Bot, PanelRightClose } from "lucide-react";

interface AgentDrawerProps {
  projectId: string;
}

const AGENT_PERMISSION_MODE_KEY = "cineweave.agent.permissionMode";
const AGENT_PERMISSION_MODES: Array<{ value: AgentPermissionMode; label: string }> = [
  { value: "require_approval", label: "需批准" },
  { value: "auto_approve", label: "自动审批" },
  { value: "full_access", label: "完全访问" },
];

export function AgentDrawer({ projectId }: AgentDrawerProps) {
  const {
    isOpen,
    currentSessionId,
    agentType,
    close,
    setSessionId,
    setAgentType,
  } = useAgentDrawerStore();

  const { messages, isLoading } = useAgentChat(projectId, currentSessionId);
  const agentTasks = useAgentTasks(projectId, currentSessionId);
  const [permissionMode, setPermissionMode] = useState<AgentPermissionMode>(() => readStoredPermissionMode());

  useEffect(() => {
    setAgentType("project_agent");
  }, [setAgentType]);

  useEffect(() => {
    window.localStorage.setItem(AGENT_PERMISSION_MODE_KEY, permissionMode);
  }, [permissionMode]);

  const handleQuickAction = (action: string) => {
    const quickAction = ASSISTANT_QUICK_ACTIONS.find((item) => item.id === action);
    if (quickAction) {
      agentTasks.createTask({ goal: quickAction.goal, mode: quickAction.mode, permissionMode });
    }
  };

  const handleProjectGoal = (content: string) => {
    agentTasks.createTask({ goal: content, mode: "supervised", permissionMode });
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
              agentType={agentType}
              onSessionChange={setSessionId}
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
          activity={
            <AgentTaskPanel
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
          }
        />

        <div className="shrink-0 border-t px-5 py-4">
          <MessageInput
            onSend={handleProjectGoal}
            quickActions={ASSISTANT_QUICK_ACTIONS}
            onQuickAction={handleQuickAction}
            isLoading={agentTasks.isCreatingTask}
            placeholder="输入项目控制目标，/ 调用工具"
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

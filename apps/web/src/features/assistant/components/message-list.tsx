"use client";

import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import type { AgentMessage } from "@/lib/types";
import { Bot, CheckCircle2, CircleAlert, User, Wrench } from "lucide-react";

interface MessageListProps {
  messages: AgentMessage[];
  isLoading?: boolean;
  activity?: ReactNode;
}

export function MessageList({ messages, isLoading, activity }: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const hasActivity = Boolean(activity);

  // 自动滚动到最新消息
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, hasActivity]);

  if (messages.length === 0 && !isLoading && !hasActivity) {
    return (
      <div className="flex-1 flex items-center justify-center text-muted-foreground">
        <div className="text-center">
          <Bot className="h-12 w-12 mx-auto mb-2 opacity-50" />
          <p className="text-sm">开始对话...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden" ref={scrollRef}>
      <div className="w-full min-w-0 max-w-full space-y-4 px-4 py-4">
        {activity}
        {messages.map((message) => (
          <MessageBubble key={message.id} message={message} />
        ))}
        {isLoading && (
          <div className="flex min-w-0 items-start gap-3">
            <div className="w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center shrink-0">
              <Bot className="h-4 w-4" />
            </div>
            <div className="min-w-0 flex-1 rounded-lg bg-muted p-3">
              <div className="flex gap-1">
                <span className="animate-bounce">●</span>
                <span className="animate-bounce delay-100">●</span>
                <span className="animate-bounce delay-200">●</span>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function MessageBubble({ message }: { message: AgentMessage }) {
  const isUser = message.role === "user";
  const isSystem = message.role === "system";
  const isTool = message.role === "tool";

  if (isSystem) {
    return (
      <div className="text-xs text-center text-muted-foreground py-2">
        {message.content}
      </div>
    );
  }

  if (isTool) {
    return <ToolMessage message={message} />;
  }

  return (
    <div
      className={cn(
        "flex min-w-0 items-start gap-3",
        isUser && "flex-row-reverse"
      )}
    >
      <div
        className={cn(
          "w-8 h-8 rounded-full flex items-center justify-center shrink-0",
          isUser ? "bg-primary text-primary-foreground" : "bg-muted"
        )}
      >
        {isUser ? <User className="h-4 w-4" /> : <Bot className="h-4 w-4" />}
      </div>
      <div
        className={cn(
          "min-w-0 flex-1 rounded-lg p-3 max-w-[80%]",
          isUser
            ? "bg-primary text-primary-foreground"
            : "bg-muted"
        )}
      >
        <div className="text-sm whitespace-pre-wrap break-words">
          {message.content}
        </div>
      </div>
    </div>
  );
}

function ToolMessage({ message }: { message: AgentMessage }) {
  const metadata = asRecord(message.metadata);
  const result = asRecord(metadata?.result);
  const status = stringValue(result?.status);
  const succeeded = status === "succeeded";
  const label = stringValue(metadata?.toolLabel) || stringValue(result?.label) || "项目工具";
  const summary = stringValue(result?.summary) || message.content;
  const data = asRecord(result?.data);
  const workflowRunId = stringValue(data?.workflowRunId);

  return (
    <div className="flex min-w-0 items-start gap-3">
      <div
        className={cn(
          "w-8 h-8 rounded-full flex items-center justify-center shrink-0",
          succeeded ? "bg-emerald-500/10 text-emerald-700" : "bg-destructive/10 text-destructive"
        )}
      >
        <Wrench className="h-4 w-4" />
      </div>
      <div className="min-w-0 max-w-[88%] flex-1 rounded-lg border bg-background p-3">
        <div className="flex min-w-0 items-center gap-2">
          {succeeded ? <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-600" /> : <CircleAlert className="h-4 w-4 shrink-0 text-destructive" />}
          <span className="truncate text-sm font-medium">{label}</span>
          <span className={cn("shrink-0 text-xs", succeeded ? "text-emerald-700" : "text-destructive")}>
            {succeeded ? "完成" : "失败"}
          </span>
        </div>
        <div className="mt-1 whitespace-pre-wrap break-words text-sm text-muted-foreground">{summary}</div>
        {workflowRunId ? <div className="mt-2 rounded bg-muted px-2 py-1 text-xs text-muted-foreground">生产任务已创建</div> : null}
      </div>
    </div>
  );
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

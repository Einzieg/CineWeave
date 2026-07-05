"use client";

import { useEffect, useRef } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import type { AgentMessage } from "@/lib/types";
import { Bot, User } from "lucide-react";

interface MessageListProps {
  messages: AgentMessage[];
  isLoading?: boolean;
}

export function MessageList({ messages, isLoading }: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);

  // 自动滚动到最新消息
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages]);

  if (messages.length === 0 && !isLoading) {
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
    <ScrollArea className="flex-1 px-4" ref={scrollRef}>
      <div className="space-y-4 py-4">
        {messages.map((message) => (
          <MessageBubble key={message.id} message={message} />
        ))}
        {isLoading && (
          <div className="flex items-start gap-3">
            <div className="w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center shrink-0">
              <Bot className="h-4 w-4" />
            </div>
            <div className="flex-1 bg-muted rounded-lg p-3">
              <div className="flex gap-1">
                <span className="animate-bounce">●</span>
                <span className="animate-bounce delay-100">●</span>
                <span className="animate-bounce delay-200">●</span>
              </div>
            </div>
          </div>
        )}
      </div>
    </ScrollArea>
  );
}

function MessageBubble({ message }: { message: AgentMessage }) {
  const isUser = message.role === "user";
  const isSystem = message.role === "system";

  if (isSystem) {
    return (
      <div className="text-xs text-center text-muted-foreground py-2">
        {message.content}
      </div>
    );
  }

  return (
    <div
      className={cn(
        "flex items-start gap-3",
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
          "flex-1 rounded-lg p-3 max-w-[80%]",
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

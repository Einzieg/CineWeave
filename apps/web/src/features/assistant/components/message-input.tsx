"use client";

import { KeyboardEvent, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { Send } from "lucide-react";
import type { AssistantQuickAction } from "./quick-actions";

interface MessageInputProps {
  onSend: (content: string) => void;
  quickActions?: AssistantQuickAction[];
  onQuickAction?: (actionId: string) => void;
  isLoading?: boolean;
  placeholder?: string;
}

export function MessageInput({
  onSend,
  quickActions = [],
  onQuickAction,
  isLoading,
  placeholder = "输入消息...",
}: MessageInputProps) {
  const [content, setContent] = useState("");
  const [activeActionIndex, setActiveActionIndex] = useState(0);
  const [dismissedSlashValue, setDismissedSlashValue] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const slashQuery = useMemo(() => {
    const match = content.match(/^\/([^\n]*)$/);
    if (!match) {
      return null;
    }
    return match[1].trim().toLowerCase();
  }, [content]);

  const filteredActions = useMemo(() => {
    if (slashQuery === null) {
      return [];
    }
    if (slashQuery === "") {
      return quickActions;
    }
    return quickActions.filter((action) => {
      const searchable = [
        action.id,
        action.label,
        action.description,
        action.goal,
        ...action.keywords,
      ].join(" ").toLowerCase();
      return searchable.includes(slashQuery);
    });
  }, [quickActions, slashQuery]);

  const slashMenuOpen = slashQuery !== null && dismissedSlashValue !== content && quickActions.length > 0 && !isLoading;
  const safeActiveActionIndex = Math.min(activeActionIndex, Math.max(filteredActions.length - 1, 0));

  const handleSend = () => {
    if (!content.trim() || isLoading || slashMenuOpen) return;
    onSend(content.trim());
    setContent("");
    setDismissedSlashValue(null);
  };

  const handleContentChange = (value: string) => {
    setContent(value);
    setDismissedSlashValue(null);
    setActiveActionIndex(0);
  };

  const handleQuickAction = (action: AssistantQuickAction) => {
    if (isLoading) return;
    if (onQuickAction) {
      onQuickAction(action.id);
    } else {
      onSend(action.goal);
    }
    setContent("");
    setDismissedSlashValue(null);
    requestAnimationFrame(() => textareaRef.current?.focus());
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (slashMenuOpen) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveActionIndex((current) => (filteredActions.length === 0 ? 0 : (current + 1) % filteredActions.length));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveActionIndex((current) => (filteredActions.length === 0 ? 0 : (current - 1 + filteredActions.length) % filteredActions.length));
        return;
      }
      if ((e.key === "Enter" && !e.shiftKey) || e.key === "Tab") {
        e.preventDefault();
        const action = filteredActions[safeActiveActionIndex];
        if (action) {
          handleQuickAction(action);
        }
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setDismissedSlashValue(content);
        return;
      }
    }

    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="flex items-end gap-2">
      <div className="relative min-w-0 flex-1">
        {slashMenuOpen ? (
          <div className="absolute bottom-full left-0 right-0 z-20 mb-2 overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-xl">
            <div className="max-h-72 overflow-y-auto p-1">
              {filteredActions.length > 0 ? (
                filteredActions.map((action, index) => (
                  <button
                    key={action.id}
                    type="button"
                    className={cn(
                      "flex w-full min-w-0 items-start gap-3 rounded-md px-3 py-2 text-left text-sm outline-none",
                      index === safeActiveActionIndex ? "bg-accent text-accent-foreground" : "hover:bg-accent/70"
                    )}
                    onMouseDown={(event) => event.preventDefault()}
                    onMouseEnter={() => setActiveActionIndex(index)}
                    onClick={() => handleQuickAction(action)}
                  >
                    <action.icon className="mt-0.5 h-4 w-4 shrink-0" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-medium">{action.label}</span>
                      <span className="block truncate text-xs text-muted-foreground">{action.description}</span>
                    </span>
                  </button>
                ))
              ) : (
                <div className="px-3 py-3 text-sm text-muted-foreground">没有匹配工具</div>
              )}
            </div>
          </div>
        ) : null}
        <Textarea
          ref={textareaRef}
          value={content}
          onChange={(e) => handleContentChange(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          disabled={isLoading}
          className="min-h-[60px] max-h-[200px] resize-none pr-12"
          rows={2}
        />
      </div>
      <Button
        onClick={handleSend}
        disabled={!content.trim() || isLoading || slashMenuOpen}
        size="icon"
        className="h-[60px] w-[60px] shrink-0"
      >
        <Send className="h-4 w-4" />
      </Button>
    </div>
  );
}

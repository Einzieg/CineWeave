"use client";

import { useState, KeyboardEvent } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Send } from "lucide-react";

interface MessageInputProps {
  onSend: (content: string) => void;
  isLoading?: boolean;
  placeholder?: string;
}

export function MessageInput({ onSend, isLoading, placeholder = "输入消息..." }: MessageInputProps) {
  const [content, setContent] = useState("");

  const handleSend = () => {
    if (!content.trim() || isLoading) return;
    onSend(content.trim());
    setContent("");
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="flex gap-2 items-end">
      <Textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        disabled={isLoading}
        className="min-h-[60px] max-h-[200px] resize-none"
        rows={2}
      />
      <Button
        onClick={handleSend}
        disabled={!content.trim() || isLoading}
        size="icon"
        className="shrink-0 h-[60px] w-[60px]"
      >
        <Send className="h-4 w-4" />
      </Button>
    </div>
  );
}

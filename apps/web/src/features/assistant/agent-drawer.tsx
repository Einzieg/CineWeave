"use client";

import { useEffect, useMemo } from "react";
import { usePathname } from "next/navigation";
import { useAgentDrawerStore } from "@/lib/stores/agent-drawer-store";
import { useAgentChat } from "./hooks/use-agent-chat";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { SessionSelector } from "./components/session-selector";
import { MessageList } from "./components/message-list";
import { MessageInput } from "./components/message-input";
import { QuickActions } from "./components/quick-actions";
import { Bot } from "lucide-react";

interface AgentDrawerProps {
  projectId: string;
}

export function AgentDrawer({ projectId }: AgentDrawerProps) {
  const pathname = usePathname();
  const {
    isOpen,
    currentSessionId,
    agentType,
    context,
    close,
    setSessionId,
    setAgentType,
  } = useAgentDrawerStore();

  const { messages, isLoading, isSending, sendMessage } = useAgentChat(projectId, currentSessionId);

  // 根据当前路由自动设置agentType
  useEffect(() => {
    const type = pathname.includes("/sources")
      ? "script_agent"
      : pathname.includes("/assets")
      ? "asset_agent"
      : pathname.includes("/storyboard")
      ? "storyboard_agent"
      : "script_agent";

    setAgentType(type);
  }, [pathname, setAgentType]);

  const agentLabel = useMemo(() => {
    switch (agentType) {
      case "script_agent":
        return "剧本助手";
      case "asset_agent":
        return "资产助手";
      case "storyboard_agent":
        return "分镜助手";
      case "shot_asset_agent":
        return "镜头资产助手";
      default:
        return "AI助手";
    }
  }, [agentType]);

  const handleQuickAction = (action: string) => {
    const messageMap: Record<string, string> = {
      "generate-script": "请帮我生成一个剧本",
      "rewrite-script": "请帮我改写当前剧本",
      "analyze-assets": "请分析当前剧本中的资产需求",
    };

    const message = messageMap[action];
    if (message) {
      sendMessage(message);
    }
  };

  return (
    <Sheet open={isOpen} onOpenChange={(open) => !open && close()}>
      <SheetContent side="right" className="w-[500px] sm:w-[540px] flex flex-col p-0">
        <SheetHeader className="px-6 pt-6 pb-4 border-b">
          <SheetTitle className="flex items-center gap-2">
            <Bot className="h-5 w-5" />
            {agentLabel}
          </SheetTitle>
          <div className="pt-3">
            <SessionSelector
              projectId={projectId}
              currentSessionId={currentSessionId}
              agentType={agentType}
              onSessionChange={setSessionId}
            />
          </div>
        </SheetHeader>

        <MessageList messages={messages} isLoading={isLoading || isSending} />

        <div className="px-6 py-4 border-t">
          <QuickActions context={context} onAction={handleQuickAction} />
          <MessageInput
            onSend={sendMessage}
            isLoading={isSending}
            placeholder={currentSessionId ? "输入消息..." : "请先选择或创建会话"}
          />
        </div>
      </SheetContent>
    </Sheet>
  );
}

"use client";

import { Button } from "@/components/ui/button";
import { FileText, Wand2, Image } from "lucide-react";

interface QuickActionsProps {
  context: Record<string, unknown>;
  onAction: (action: string) => void;
}

export function QuickActions({ context, onAction }: QuickActionsProps) {
  const { scriptId, assetId } = context;

  const actions = [
    {
      id: "generate-script",
      label: "生成剧本",
      icon: FileText,
      show: !scriptId,
    },
    {
      id: "rewrite-script",
      label: "改写剧本",
      icon: Wand2,
      show: !!scriptId,
    },
    {
      id: "analyze-assets",
      label: "分析资产",
      icon: Image,
      show: !!scriptId,
    },
  ].filter((action) => action.show);

  if (actions.length === 0) return null;

  return (
    <div className="mb-3">
      <div className="text-xs text-muted-foreground mb-2">快捷操作</div>
      <div className="flex flex-wrap gap-2">
        {actions.map((action) => (
          <Button
            key={action.id}
            variant="outline"
            size="sm"
            onClick={() => onAction(action.id)}
            className="text-xs"
          >
            <action.icon className="h-3 w-3 mr-1" />
            {action.label}
          </Button>
        ))}
      </div>
    </div>
  );
}

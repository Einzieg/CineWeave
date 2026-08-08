"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { CodexControlKeySecret } from "@/lib/types";

export const codexControlKeyEnvironmentVariable = "CINEWEAVE_USER_KEY";
export const codexMcpConfiguration = `[mcp_servers.cineweave]\nurl = "https://cineweave-mcp.einzieg.site/mcp"\nbearer_token_env_var = "CINEWEAVE_USER_KEY"`;

export function CodexControlKeySecretDialog({
  secret,
  onClose,
  title = "保存 Codex 项目控制密钥",
}: {
  secret?: CodexControlKeySecret;
  onClose: () => void;
  title?: string;
}) {
  const [copied, setCopied] = useState<"secret" | "environment" | "configuration">();

  function close() {
    setCopied(undefined);
    onClose();
  }

  async function copy(value: string, target: "secret" | "environment" | "configuration") {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(target);
      toast.success("已复制");
    } catch {
      toast.error("复制失败，请手动复制");
    }
  }

  return (
    <Dialog open={Boolean(secret)} onOpenChange={(open) => { if (!open) close(); }}>
      <DialogContent className="max-h-[min(90svh,48rem)] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>密钥明文仅显示这一次。关闭窗口后无法再次查看，只能轮换。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <SecretCopyField
            label="密钥"
            value={secret?.secret ?? ""}
            copied={copied === "secret"}
            onCopy={() => secret && copy(secret.secret, "secret")}
          />
          <SecretCopyField
            label="环境变量"
            value={codexControlKeyEnvironmentVariable}
            copied={copied === "environment"}
            onCopy={() => copy(codexControlKeyEnvironmentVariable, "environment")}
          />
          <SecretCopyField
            label="Codex MCP 配置"
            value={codexMcpConfiguration}
            multiline
            copied={copied === "configuration"}
            onCopy={() => copy(codexMcpConfiguration, "configuration")}
          />
        </div>
        <DialogFooter>
          <Button onClick={close}>我已保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SecretCopyField({
  label,
  value,
  copied,
  multiline = false,
  onCopy,
}: {
  label: string;
  value: string;
  copied: boolean;
  multiline?: boolean;
  onCopy: () => void;
}) {
  return (
    <div className="grid min-w-0 gap-2">
      <Label>{label}</Label>
      <div className="flex min-w-0 items-start gap-2">
        {multiline ? (
          <pre className="min-w-0 flex-1 overflow-x-auto rounded-md border bg-muted/40 px-3 py-2 font-mono text-xs whitespace-pre">{value}</pre>
        ) : (
          <Input readOnly value={value} className="min-w-0 font-mono text-xs" />
        )}
        <Button type="button" size="icon" variant="outline" aria-label={`复制${label}`} onClick={onCopy}>
          {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
        </Button>
      </div>
    </div>
  );
}

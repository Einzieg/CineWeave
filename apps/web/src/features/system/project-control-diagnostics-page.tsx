"use client";

import { Activity, KeyRound, Loader2, RefreshCw, ShieldAlert } from "lucide-react";
import { AppShell } from "@/components/layout/app-shell";
import { ErrorPanel } from "@/components/shared/error-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiQuery } from "@/lib/query/use-api";
import { useStudioSession } from "@/lib/session";
import type { ProjectControlRuntimeCommandCount } from "@/lib/types";

const statusLabels: Record<string, string> = {
  queued: "排队中",
  running: "运行中",
  waiting_user: "等待用户",
  waiting_workflow: "等待工作流",
  succeeded: "已完成",
  partial_succeeded: "部分完成",
  failed: "失败",
  cancelled: "已取消",
};

const controllerLabels: Record<ProjectControlRuntimeCommandCount["controller"], string> = {
  embedded_agent: "项目助手",
  codex_mcp: "Codex",
  manual: "人工操作",
};

export function ProjectControlDiagnosticsPage() {
  const { session } = useStudioSession();
  const systemAdministrator = Boolean(session.user?.systemAdministrator);
  const diagnostics = useApiQuery({
    key: qk.systemProjectControlDiagnostics(),
    enabled: systemAdministrator,
    queryFn: (activeSession) => studioApi.getSystemProjectControlDiagnostics(activeSession),
    refetchInterval: 30_000,
  });

  return (
    <AppShell active="system-project-control" title="项目控制诊断">
      {!systemAdministrator ? (
        <AccessDenied />
      ) : diagnostics.isLoading ? (
        <DiagnosticsSkeleton />
      ) : diagnostics.error ? (
        <ErrorPanel message={errorMessage(diagnostics.error)} />
      ) : diagnostics.data ? (
        <div className="space-y-6">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
            <div className="flex items-center gap-2">
              <Activity className="h-5 w-5 text-primary" />
              <div>
                <div className="text-sm font-semibold">运行状态</div>
                <div className="mt-1 text-xs text-muted-foreground">发布 {diagnostics.data.releaseId || "未标记"}</div>
              </div>
            </div>
            <Button variant="outline" size="sm" disabled={diagnostics.isFetching} onClick={() => diagnostics.refetch()}>
              {diagnostics.isFetching ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RefreshCw className="mr-2 h-4 w-4" />}
              刷新
            </Button>
          </div>

          <section aria-labelledby="project-control-runtime-heading">
            <h2 id="project-control-runtime-heading" className="text-sm font-semibold">命令运行时</h2>
            <div className="mt-3 grid gap-px overflow-hidden rounded-md border bg-border sm:grid-cols-2 xl:grid-cols-6">
              <Metric label="活动命令" value={diagnostics.data.runtime.activeCommands} tone={diagnostics.data.runtime.activeCommands > 0 ? "active" : "normal"} />
              <Metric label="等待命令" value={diagnostics.data.runtime.waitingCommands} tone={diagnostics.data.runtime.waitingCommands > 0 ? "active" : "normal"} />
              <Metric label="过期租约" value={diagnostics.data.runtime.expiredLeases} tone={diagnostics.data.runtime.expiredLeases > 0 ? "danger" : "normal"} />
              <Metric label="逾期对账" value={diagnostics.data.runtime.overdueReconciliations} tone={diagnostics.data.runtime.overdueReconciliations > 0 ? "danger" : "normal"} />
              <Metric label="最大对账延迟" value={`${diagnostics.data.runtime.oldestReconcileLagSeconds}s`} tone={diagnostics.data.runtime.oldestReconcileLagSeconds > 30 ? "danger" : "normal"} />
              <Metric label="未关联工作流" value={diagnostics.data.runtime.unlinkedDeterministicWorkflows} tone={diagnostics.data.runtime.unlinkedDeterministicWorkflows > 0 ? "danger" : "normal"} />
            </div>
          </section>

          <section aria-labelledby="project-control-command-heading">
            <h2 id="project-control-command-heading" className="text-sm font-semibold">命令分布</h2>
            <div className="mt-3 overflow-hidden rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>状态</TableHead>
                    <TableHead>控制端</TableHead>
                    <TableHead className="text-right">数量</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {diagnostics.data.runtime.commandCounts.length ? diagnostics.data.runtime.commandCounts.map((item) => (
                    <TableRow key={`${item.status}:${item.controller}`}>
                      <TableCell>{statusLabels[item.status] ?? item.status}</TableCell>
                      <TableCell>{controllerLabels[item.controller]}</TableCell>
                      <TableCell className="text-right tabular-nums">{item.count}</TableCell>
                    </TableRow>
                  )) : (
                    <TableRow><TableCell colSpan={3} className="h-20 text-center text-muted-foreground">暂无命令</TableCell></TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
          </section>

          <section aria-labelledby="project-control-contract-heading">
            <h2 id="project-control-contract-heading" className="text-sm font-semibold">控制契约</h2>
            <dl className="mt-3 grid gap-x-6 gap-y-4 border-y py-4 lg:grid-cols-2">
              <ContractValue label="MCP 服务" value={diagnostics.data.mcp.enabled ? "已启用" : "未启用"} badge={diagnostics.data.mcp.enabled} />
              <ContractValue label="发布标识" value={diagnostics.data.releaseId || "未标记"} />
              <ContractValue label="工具目录哈希" value={diagnostics.data.mcp.toolCatalogHash} mono />
              <ContractValue label="动作矩阵哈希" value={diagnostics.data.actionMatrixHash} mono />
            </dl>
          </section>

          <section aria-labelledby="project-control-auth-heading">
            <div className="flex items-center gap-2">
              <KeyRound className="h-4 w-4 text-muted-foreground" />
              <h2 id="project-control-auth-heading" className="text-sm font-semibold">近期认证失败</h2>
            </div>
            <div className="mt-3 overflow-hidden rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>原因</TableHead>
                    <TableHead>首次发生</TableHead>
                    <TableHead>最近发生</TableHead>
                    <TableHead className="text-right">次数</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {diagnostics.data.mcp.recentAuthenticationFailures.length ? diagnostics.data.mcp.recentAuthenticationFailures.map((item) => (
                    <TableRow key={`${item.reason}:${item.firstFailureAt}`}>
                      <TableCell>{authenticationFailureLabel(item.reason)}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{formatDateTime(item.firstFailureAt)}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{formatDateTime(item.lastFailureAt)}</TableCell>
                      <TableCell className="text-right tabular-nums">{item.count}</TableCell>
                    </TableRow>
                  )) : (
                    <TableRow><TableCell colSpan={4} className="h-20 text-center text-muted-foreground">近期没有认证失败</TableCell></TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
          </section>
        </div>
      ) : null}
    </AppShell>
  );
}

function Metric({ label, value, tone }: { label: string; value: string | number; tone: "normal" | "active" | "danger" }) {
  return (
    <div className="bg-background px-4 py-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={`mt-2 text-xl font-semibold tabular-nums ${tone === "danger" ? "text-destructive" : tone === "active" ? "text-primary" : ""}`}>{value}</div>
    </div>
  );
}

function ContractValue({ label, value, mono = false, badge = false }: { label: string; value: string; mono?: boolean; badge?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={`mt-1 break-all text-sm ${mono ? "font-mono" : ""}`}>
        {badge ? <Badge variant="outline">{value}</Badge> : value || "-"}
      </dd>
    </div>
  );
}

function AccessDenied() {
  return (
    <div className="grid min-h-80 place-items-center border-y px-6 text-center">
      <div>
        <ShieldAlert className="mx-auto h-9 w-9 text-muted-foreground" />
        <h2 className="mt-4 text-base font-semibold">仅系统管理员可访问</h2>
        <p className="mt-2 text-sm text-muted-foreground">当前账号没有平台运行诊断权限。</p>
      </div>
    </div>
  );
}

function DiagnosticsSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-12 w-full" />
      <div className="grid gap-1 sm:grid-cols-2 xl:grid-cols-6">
        {Array.from({ length: 6 }).map((_, index) => <Skeleton key={index} className="h-24" />)}
      </div>
      <Skeleton className="h-56 w-full" />
    </div>
  );
}

function formatDateTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN", { hour12: false });
}

function authenticationFailureLabel(reason: string) {
  switch (reason) {
    case "missing_authorization": return "缺少认证信息";
    case "invalid_authorization": return "认证格式无效";
    case "invalid_key": return "控制密钥无效";
    case "expired_key": return "控制密钥已过期";
    case "revoked_key": return "控制密钥已撤销";
    case "rate_limited": return "请求频率受限";
    default: return reason;
  }
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "读取项目控制诊断失败";
}

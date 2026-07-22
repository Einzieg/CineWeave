"use client";

import { useState } from "react";
import { ChevronLeft, ChevronRight, ClipboardList } from "lucide-react";
import { ErrorPanel } from "@/components/shared/error-panel";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { auditActionLabel, auditResourceTypeLabel, statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiQuery } from "@/lib/query/use-api";
import type { JsonValue, OrganizationAuditLog } from "@/lib/types";

const pageSize = 25;

export function AuditLogsPanel() {
  const [resourceType, setResourceType] = useState("all");
  const [page, setPage] = useState(1);
  const query = useApiQuery({
    key: qk.organizationAuditLogs("", resourceType, page),
    queryFn: (session) => studioApi.listOrganizationAuditLogs(session, session.organizationId, {
      resourceType: resourceType === "all" ? undefined : resourceType,
      page,
      pageSize,
    }),
  });
  const totalPages = Math.max(1, Math.ceil((query.data?.total ?? 0) / pageSize));

  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <div className="flex flex-col gap-3 border-b px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
        <div><h2 className="text-sm font-semibold">审计记录</h2><p className="mt-1 text-xs text-muted-foreground">关键组织管理变更随组织生命周期保留，不记录密码、会话或邀请明文令牌。</p></div>
        <Select value={resourceType} onValueChange={(value) => { setResourceType(value); setPage(1); }}>
          <SelectTrigger className="w-full sm:w-40" aria-label="审计资源类型"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部资源</SelectItem>
            <SelectItem value="organization">组织</SelectItem>
            <SelectItem value="organization_invitation">组织邀请</SelectItem>
            <SelectItem value="user">用户与成员</SelectItem>
            <SelectItem value="team">团队</SelectItem>
            <SelectItem value="role">角色</SelectItem>
            <SelectItem value="role_binding">角色绑定</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {query.isLoading ? (
        <div className="grid gap-2 p-4">{Array.from({ length: 5 }).map((_, index) => <Skeleton key={index} className="h-14" />)}</div>
      ) : query.error ? (
        <div className="p-4"><ErrorPanel message={errorMessage(query.error)} /></div>
      ) : query.data?.items.length ? (
        <>
          <Table>
            <TableHeader><TableRow><TableHead>操作</TableHead><TableHead>操作者</TableHead><TableHead className="hidden md:table-cell">对象</TableHead><TableHead className="hidden lg:table-cell">变更摘要</TableHead><TableHead className="text-right">时间</TableHead></TableRow></TableHeader>
            <TableBody>{query.data.items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className="font-medium">{auditActionLabel(item.action)}</TableCell>
                <TableCell><span className="block text-sm">{item.actor?.displayName || item.actor?.username || "系统"}</span>{item.actor?.username ? <span className="block text-xs text-muted-foreground">@{item.actor.username}</span> : null}</TableCell>
                <TableCell className="hidden md:table-cell"><span className="text-sm">{auditResourceTypeLabel(item.resourceType)}</span>{item.resourceId ? <span className="ml-2 font-mono text-[11px] text-muted-foreground">{shortId(item.resourceId)}</span> : null}</TableCell>
                <TableCell className="hidden max-w-md text-xs text-muted-foreground lg:table-cell">{auditSummary(item)}</TableCell>
                <TableCell className="whitespace-nowrap text-right text-xs text-muted-foreground">{formatDate(item.createdAt)}</TableCell>
              </TableRow>
            ))}</TableBody>
          </Table>
          <div className="flex items-center justify-between border-t px-4 py-3 text-xs text-muted-foreground">
            <span>共 {query.data.total} 条记录</span>
            <div className="flex items-center gap-2"><Button size="icon-sm" variant="outline" aria-label="上一页" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}><ChevronLeft className="h-4 w-4" /></Button><span>{page} / {totalPages}</span><Button size="icon-sm" variant="outline" aria-label="下一页" disabled={page >= totalPages} onClick={() => setPage((value) => value + 1)}><ChevronRight className="h-4 w-4" /></Button></div>
          </div>
        </>
      ) : (
        <div className="grid place-items-center px-4 py-16 text-center"><ClipboardList className="h-8 w-8 text-muted-foreground/60" /><p className="mt-3 text-sm font-medium">暂无审计记录</p><p className="mt-1 text-xs text-muted-foreground">完成成员、邀请、团队或授权变更后会显示在这里。</p></div>
      )}
    </div>
  );
}

function auditSummary(item: OrganizationAuditLog) {
  const metadata = item.metadata;
  const changedFields = stringArray(metadata.changedFields);
  if (changedFields.length) {
    const labels: Record<string, string> = { displayName: "显示名称", avatarUrl: "头像", name: "名称", description: "说明", status: "状态", permissions: "权限" };
    return `变更：${changedFields.map((field) => labels[field] || "资料").join("、")}`;
  }
  const status = stringValue(metadata.status);
  if (status) return `状态：${statusLabel(status)}`;
  const userId = stringValue(metadata.userId);
  if (userId) return `成员：${shortId(userId)}`;
  const bindingCount = numberValue(metadata.bindingCount);
  if (bindingCount !== null) return `初始资源授权 ${bindingCount} 项`;
  const activeMemberCount = numberValue(metadata.activeMemberCount);
  const activeBindingCount = numberValue(metadata.activeBindingCount);
  if (activeMemberCount !== null || activeBindingCount !== null) return `影响 ${activeMemberCount ?? 0} 名成员、${activeBindingCount ?? 0} 项授权`;
  return "已记录变更";
}

function stringValue(value: JsonValue | undefined) { return typeof value === "string" ? value : ""; }
function numberValue(value: JsonValue | undefined) { return typeof value === "number" ? value : null; }
function stringArray(value: JsonValue | undefined) { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }
function shortId(value: string) { return value.length > 10 ? `${value.slice(0, 8)}…` : value; }
function formatDate(value: string) { return new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "short" }).format(new Date(value)); }
function errorMessage(error: unknown) { return error instanceof StudioApiError ? error.message : error instanceof Error ? error.message : "加载审计记录失败"; }

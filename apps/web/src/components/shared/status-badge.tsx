import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/cn";

type StatusVariant = "running" | "success" | "warning" | "pending" | "danger";

const variantByStatus: Record<string, StatusVariant> = {
  ready: "success",
  enabled: "success",
  scenes_ready: "success",
  imported: "success",
  active: "success",
  approved: "success",
  fresh: "success",
  succeeded: "success",
  completed: "success",
  processed: "success",
  image_succeeded: "success",
  storyboard_ready: "success",
  video_succeeded: "success",
  running: "running",
  queued: "running",
  image_running: "running",
  video_running: "running",
  needs_review: "warning",
  events_pending_extraction: "warning",
  events_pending_review: "warning",
  adaptation_plan_pending: "warning",
  scenes_pending_parse: "warning",
  scenes_pending_review: "warning",
  needs_edit: "warning",
  needs_regeneration: "warning",
  upstream_changed: "warning",
  partial: "warning",
  failed: "danger",
  rejected: "danger",
  cancelled: "danger",
  image_failed: "danger",
  draft: "pending",
  pending: "pending",
  disabled: "pending",
  not_started: "pending",
};

const variantStyles: Record<StatusVariant, string> = {
  running: "border-status-running/30 bg-status-running/10 text-status-running",
  success: "border-status-success/30 bg-status-success/10 text-status-success",
  warning: "border-status-warning/30 bg-status-warning/10 text-status-warning",
  danger: "border-status-danger/30 bg-status-danger/10 text-status-danger",
  pending: "border-border bg-muted/50 text-muted-foreground",
};

export function StatusBadge({ status, className }: { status?: string; className?: string }) {
  const normalized = (status ?? "pending").toLowerCase();
  const variant = variantByStatus[normalized] ?? "pending";
  return (
    <Badge variant="outline" className={cn("text-xs font-medium", variantStyles[variant], className)}>
      {statusLabel(normalized)}
    </Badge>
  );
}

export function statusLabel(status: string) {
  switch (status) {
    case "ready":
      return "就绪";
    case "enabled":
      return "已启用";
    case "disabled":
      return "已禁用";
    case "scenes_ready":
      return "分场就绪";
    case "imported":
      return "已导入";
    case "active":
      return "启用";
    case "running":
      return "运行中";
    case "queued":
      return "排队中";
    case "draft":
      return "草稿";
    case "pending":
      return "等待中";
    case "not_started":
      return "未开始";
    case "needs_review":
      return "待确认";
    case "events_pending_extraction":
      return "待提取事件";
    case "events_pending_review":
      return "事件待确认";
    case "adaptation_plan_pending":
      return "待生成改编计划";
    case "scenes_pending_parse":
      return "待解析分场";
    case "scenes_pending_review":
      return "分场待确认";
    case "needs_edit":
      return "需修改";
    case "needs_regeneration":
      return "需重生成";
    case "upstream_changed":
      return "上游已变更";
    case "approved":
      return "已确认";
    case "fresh":
      return "最新";
    case "rejected":
      return "已拒绝";
    case "partial":
      return "部分完成";
    case "succeeded":
    case "completed":
      return "已完成";
    case "processed":
      return "已处理";
    case "failed":
      return "失败";
    case "cancelled":
      return "已取消";
    case "image_succeeded":
      return "参考图完成";
    case "image_running":
      return "生成图片中";
    case "image_failed":
      return "图片失败";
    case "storyboard_ready":
      return "分镜就绪";
    case "video_succeeded":
      return "视频完成";
    case "video_running":
      return "生成视频中";
    default:
      return status || "未知";
  }
}

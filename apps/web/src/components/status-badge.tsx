import { cn } from "@/lib/cn";

const toneByStatus: Record<string, string> = {
  ready: "border-blue-200 bg-blue-50 text-blue-700",
  scenes_ready: "border-blue-200 bg-blue-50 text-blue-700",
  imported: "border-blue-200 bg-blue-50 text-blue-700",
  active: "border-blue-200 bg-blue-50 text-blue-700",
  running: "border-amber-200 bg-amber-50 text-amber-700",
  needs_review: "border-amber-200 bg-amber-50 text-amber-700",
  scenes_pending_parse: "border-amber-200 bg-amber-50 text-amber-700",
  scenes_pending_review: "border-amber-200 bg-amber-50 text-amber-700",
  needs_edit: "border-amber-200 bg-amber-50 text-amber-700",
  needs_regeneration: "border-amber-200 bg-amber-50 text-amber-700",
  upstream_changed: "border-amber-200 bg-amber-50 text-amber-700",
  partial: "border-amber-200 bg-amber-50 text-amber-700",
  queued: "border-slate-200 bg-slate-50 text-slate-700",
  draft: "border-slate-200 bg-slate-50 text-slate-700",
  pending: "border-slate-200 bg-slate-50 text-slate-700",
  not_started: "border-slate-200 bg-slate-50 text-slate-700",
  approved: "border-emerald-200 bg-emerald-50 text-emerald-700",
  fresh: "border-emerald-200 bg-emerald-50 text-emerald-700",
  succeeded: "border-emerald-200 bg-emerald-50 text-emerald-700",
  completed: "border-emerald-200 bg-emerald-50 text-emerald-700",
  processed: "border-emerald-200 bg-emerald-50 text-emerald-700",
  failed: "border-rose-200 bg-rose-50 text-rose-700",
  rejected: "border-rose-200 bg-rose-50 text-rose-700",
  cancelled: "border-rose-200 bg-rose-50 text-rose-700",
};

export function StatusBadge({ status, className }: { status?: string; className?: string }) {
  const normalized = (status ?? "pending").toLowerCase();
  return (
    <span className={cn("inline-flex h-6 items-center rounded-md border px-2 text-[12px] font-medium", toneByStatus[normalized] ?? toneByStatus.pending, className)}>
      {statusLabel(normalized)}
    </span>
  );
}

export function statusLabel(status: string) {
  switch (status) {
    case "ready":
      return "就绪";
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

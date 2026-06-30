import { cn } from "@/lib/cn";

const toneByStatus: Record<string, string> = {
  ready: "border-cyan-400/30 bg-cyan-400/10 text-cyan-100",
  imported: "border-cyan-400/30 bg-cyan-400/10 text-cyan-100",
  active: "border-cyan-400/30 bg-cyan-400/10 text-cyan-100",
  running: "border-amber-300/30 bg-amber-300/10 text-amber-100",
  needs_review: "border-amber-300/30 bg-amber-300/10 text-amber-100",
  needs_edit: "border-amber-300/30 bg-amber-300/10 text-amber-100",
  partial: "border-amber-300/30 bg-amber-300/10 text-amber-100",
  queued: "border-zinc-400/30 bg-zinc-400/10 text-zinc-100",
  draft: "border-zinc-400/30 bg-zinc-400/10 text-zinc-100",
  pending: "border-zinc-400/30 bg-zinc-400/10 text-zinc-100",
  not_started: "border-zinc-400/30 bg-zinc-400/10 text-zinc-100",
  approved: "border-emerald-300/30 bg-emerald-300/10 text-emerald-100",
  succeeded: "border-emerald-300/30 bg-emerald-300/10 text-emerald-100",
  completed: "border-emerald-300/30 bg-emerald-300/10 text-emerald-100",
  processed: "border-emerald-300/30 bg-emerald-300/10 text-emerald-100",
  failed: "border-rose-300/30 bg-rose-300/10 text-rose-100",
  rejected: "border-rose-300/30 bg-rose-300/10 text-rose-100",
  cancelled: "border-rose-300/30 bg-rose-300/10 text-rose-100",
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
    case "needs_edit":
      return "需修改";
    case "approved":
      return "已确认";
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

import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/cn";
import { statusLabel } from "@/lib/labels";

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
  resolved: "success",
  image_succeeded: "success",
  storyboard_ready: "success",
  video_succeeded: "success",
  running: "running",
  processing: "running",
  queued: "running",
  cancelling: "running",
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
  changes_requested: "warning",
  partial: "warning",
  failed: "danger",
  rejected: "danger",
  cancelled: "danger",
  image_failed: "danger",
  draft: "pending",
  pending: "pending",
  open: "pending",
  ignored: "pending",
  disabled: "pending",
  not_started: "pending",
  video_failed: "danger",
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

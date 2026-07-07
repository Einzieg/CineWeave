"use client";

import { useMemo, useState } from "react";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { AlertCircle, Check, RefreshCw, Wand2, X } from "lucide-react";
import { toast } from "sonner";
import { reviewCategoryLabel, reviewSeverityLabel, statusLabel } from "@/lib/labels";
import type { ReviewFix, ReviewItem } from "@/lib/types";

export function ReviewPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const [selectedItemId, setSelectedItemId] = useState("");
  const [resolutionNote, setResolutionNote] = useState("");

  const { data: runs = [] } = useApiQuery({
    key: qk.reviewRuns(projectId),
    queryFn: (session) => studioApi.listReviewRuns(session, projectId).then((response) => response.items || []),
  });
  const { data: items = [], isLoading } = useApiQuery({
    key: qk.reviewItems(projectId),
    queryFn: (session) => studioApi.listReviewItems(session, projectId).then((response) => response.items || []),
  });

  const selectedItem = useMemo(() => items.find((item) => item.id === selectedItemId) ?? items.find((item) => item.status === "open") ?? items[0] ?? null, [items, selectedItemId]);
  const { data: fixes = [] } = useApiQuery({
    key: qk.reviewFixes(projectId, selectedItem?.id ?? ""),
    queryFn: (session) => studioApi.listReviewFixes(session, projectId, selectedItem!.id).then((response) => response.items || []),
    enabled: !!selectedItem,
  });

  const runReviewMutation = useApiMutation({
    mutationFn: (session) => studioApi.runProjectReview(session, projectId, { reviewType: "project", useAgent: true, includeDeterministicChecks: true }),
    onSuccess: (response) => {
      toast.success(`审阅已完成，发现 ${response.itemCount} 项`);
      invalidate([qk.reviewRuns(projectId), qk.reviewItems(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("审阅失败：" + error.message),
  });

  const itemStatusMutation = useApiMutation({
    mutationFn: (session, payload: { item: ReviewItem; status: "resolved" | "ignored" | "open" }) => {
      if (payload.status === "resolved") {
        return studioApi.resolveReviewItem(session, projectId, payload.item.id, { note: resolutionNote });
      }
      if (payload.status === "ignored") {
        return studioApi.ignoreReviewItem(session, projectId, payload.item.id, { note: resolutionNote });
      }
      return studioApi.reopenReviewItem(session, projectId, payload.item.id, { note: "" });
    },
    onSuccess: () => {
      setResolutionNote("");
      invalidate([qk.reviewItems(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const generateFixMutation = useApiMutation({
    mutationFn: (session, payload: { itemId: string; mode: "agent" | "deterministic" }) => studioApi.generateReviewFix(session, projectId, payload.itemId, { mode: payload.mode }),
    onSuccess: (_fix, payload) => {
      toast.success("修复建议已生成");
      invalidate([qk.reviewFixes(projectId, payload.itemId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const applyFixMutation = useApiMutation({
    mutationFn: (session, payload: { fix: ReviewFix; triggerRegeneration: boolean }) =>
      studioApi.applyReviewFix(session, projectId, payload.fix.id, {
        resolveReviewItem: true,
        triggerRegeneration: payload.triggerRegeneration,
      }),
    onSuccess: (_response, payload) => {
      toast.success(payload.triggerRegeneration ? "修复已应用，重生成已启动" : "修复已应用");
      invalidate([qk.reviewItems(projectId), qk.reviewFixes(projectId, payload.fix.reviewItemId), qk.productionStatus(projectId), qk.workflowRuns(projectId)]);
    },
    onError: (error) => toast.error("应用失败：" + error.message),
  });

  const dismissFixMutation = useApiMutation({
    mutationFn: (session, fix: ReviewFix) => studioApi.dismissReviewFix(session, projectId, fix.id),
    onSuccess: (_response, fix) => {
      invalidate([qk.reviewFixes(projectId, fix.reviewItemId)]);
    },
    onError: (error) => toast.error("忽略失败：" + error.message),
  });

  const openItems = items.filter((item) => item.status === "open");

  return (
    <Surface>
      <SectionTitle title="审阅中心" description="运行审查、查看问题并应用修复" />
      <div className="grid gap-5 p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap gap-2">
            <Badge variant="outline">运行 {runs.length}</Badge>
            <Badge variant={openItems.length ? "secondary" : "outline"}>待处理 {openItems.length}</Badge>
          </div>
          <Button onClick={() => runReviewMutation.mutate()} disabled={runReviewMutation.isPending}>
            <RefreshCw className="mr-2 h-4 w-4" />
            运行审阅
          </Button>
        </div>

        {isLoading && <Skeleton className="h-64" />}
        {!isLoading && items.length === 0 && (
          <div className="rounded-lg border border-dashed p-12 text-center">
            <AlertCircle className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
            <p className="mt-4 text-sm text-muted-foreground">暂无审阅项</p>
          </div>
        )}

        {items.length > 0 && (
          <div className="grid gap-4 lg:grid-cols-[minmax(260px,360px)_1fr]">
            <div className="grid content-start gap-2">
              {items.map((item) => (
                <button
                  key={item.id}
                  className={`rounded-lg border p-3 text-left transition hover:bg-muted/50 ${selectedItem?.id === item.id ? "bg-muted/50 ring-2 ring-primary" : ""}`}
                  onClick={() => setSelectedItemId(item.id)}
                  type="button"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="font-medium">{item.title}</div>
                    <Badge variant={item.severity === "critical" || item.severity === "high" ? "destructive" : "outline"}>{reviewSeverityLabel(item.severity)}</Badge>
                  </div>
                  <p className="mt-2 line-clamp-2 text-sm text-muted-foreground">{item.description}</p>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <Badge variant="secondary">{statusLabel(item.status)}</Badge>
                    <Badge variant="outline">{reviewCategoryLabel(item.category)}</Badge>
                  </div>
                </button>
              ))}
            </div>

            {selectedItem ? (
              <div className="grid content-start gap-4">
                <div className="rounded-lg border p-4">
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div>
                      <h3 className="text-lg font-semibold">{selectedItem.title}</h3>
                      <p className="mt-2 text-sm text-muted-foreground">{selectedItem.description}</p>
                    </div>
                    <Badge>{statusLabel(selectedItem.status)}</Badge>
                  </div>
                  {selectedItem.suggestion ? <p className="mt-3 text-sm">{selectedItem.suggestion}</p> : null}
                  <Textarea className="mt-4" placeholder="处理备注" value={resolutionNote} onChange={(event) => setResolutionNote(event.target.value)} />
                  <div className="mt-3 flex flex-wrap gap-2">
                    <Button size="sm" variant="outline" onClick={() => itemStatusMutation.mutate({ item: selectedItem, status: "resolved" })}>
                      <Check className="mr-1 h-3.5 w-3.5" />
                      解决
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => itemStatusMutation.mutate({ item: selectedItem, status: "ignored" })}>
                      <X className="mr-1 h-3.5 w-3.5" />
                      忽略
                    </Button>
                    {selectedItem.status !== "open" ? (
                      <Button size="sm" variant="outline" onClick={() => itemStatusMutation.mutate({ item: selectedItem, status: "open" })}>
                        重新打开
                      </Button>
                    ) : null}
                    <Button size="sm" onClick={() => generateFixMutation.mutate({ itemId: selectedItem.id, mode: "agent" })} disabled={generateFixMutation.isPending}>
                      <Wand2 className="mr-1 h-3.5 w-3.5" />
                      生成修复
                    </Button>
                  </div>
                </div>

                <div className="grid gap-3">
                  {fixes.length === 0 ? <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">暂无修复建议</div> : null}
                  {fixes.map((fix) => (
                    <div key={fix.id} className="rounded-lg border p-4">
                      <div className="flex flex-wrap items-start justify-between gap-2">
                        <div>
                          <div className="font-medium">{fix.title}</div>
                          <p className="mt-1 text-sm text-muted-foreground">{fix.explanation}</p>
                        </div>
                        <Badge variant="outline">{statusLabel(fix.status)}</Badge>
                      </div>
                      <div className="mt-3 flex flex-wrap gap-2">
                        <Button size="sm" onClick={() => applyFixMutation.mutate({ fix, triggerRegeneration: false })} disabled={applyFixMutation.isPending || fix.status !== "draft"}>
                          应用
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => applyFixMutation.mutate({ fix, triggerRegeneration: true })} disabled={applyFixMutation.isPending || fix.status !== "draft"}>
                          应用并重生成
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => dismissFixMutation.mutate(fix)} disabled={dismissFixMutation.isPending || fix.status !== "draft"}>
                          忽略
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            ) : null}
          </div>
        )}
      </div>
    </Surface>
  );
}

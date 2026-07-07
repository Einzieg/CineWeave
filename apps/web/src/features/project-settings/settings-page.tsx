"use client";

import { useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorPanel } from "@/components/shared/error-panel";
import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi, StudioApiError } from "@/lib/api-client";
import type { Project } from "@/lib/types";

export function ProjectSettingsPage({ projectId }: { projectId: string }) {
  const { data: project, isLoading } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
  });
  const { data: manualTemplates = [] } = useApiQuery({
    key: qk.projectManualTemplates(),
    queryFn: (session) => studioApi.listProjectManualTemplates(session).then((response) => response.items),
  });
  const { data: manualBindings = [] } = useApiQuery({
    key: qk.projectManualBindings(projectId),
    queryFn: (session) => studioApi.listProjectManualBindings(session, projectId).then((response) => response.items),
  });

  const [draft, setDraft] = useState<Partial<Project>>({});
  const [error, setError] = useState("");
  const invalidateKeys = useInvalidateKeys();

  const form = project ? { ...project, ...draft } : null;

  const saveMutation = useApiMutation({
    mutationFn: (session, data: Partial<Project>) =>
      studioApi.updateProject(session, projectId, {
        name: data.name || "",
        description: data.description || null,
        projectType: data.projectType || "",
        contentType: data.contentType || "",
        videoRatio: data.videoRatio || "16:9",
        artStyle: data.artStyle || "",
        directorManual: data.directorManual || null,
        visualManual: data.visualManual || null,
        imageQuality: data.imageQuality || "standard",
        productionMode: data.productionMode || "silent_video",
        imageModelProfileKey: data.imageModelProfileKey || null,
        videoModelProfileKey: data.videoModelProfileKey || null,
        scriptModelProfileKey: data.scriptModelProfileKey || null,
      }),
    onSuccess: () => {
      setDraft({});
      setError("");
      toast.success("项目设置已保存");
      invalidateKeys([qk.project(projectId), qk.projectManualBindings(projectId)]);
    },
    onError: (err) => {
      setError(err instanceof StudioApiError ? err.message : "保存失败");
    },
  });

  const bindManualMutation = useApiMutation({
    mutationFn: (session, data: { kind: "director" | "visual"; promptVersionId: string }) =>
      studioApi.bindProjectManual(session, projectId, data.kind, data.promptVersionId),
    onSuccess: (_, variables) => {
      setDraft({});
      setError("");
      toast.success(variables.kind === "director" ? "导演手册已绑定" : "视觉手册已绑定");
      invalidateKeys([qk.project(projectId), qk.projectManualBindings(projectId)]);
    },
    onError: (err) => {
      setError(err instanceof StudioApiError ? err.message : "绑定失败");
    },
  });

  if (isLoading) {
    return (
      <div className="grid gap-4">
        <Skeleton className="h-64" />
      </div>
    );
  }

  if (!form) {
    return <div>项目不存在</div>;
  }

  const directorTemplates = manualTemplates.filter((item) => item.purpose === "director_manual" && item.activeVersion?.id);
  const visualTemplates = manualTemplates.filter((item) => item.purpose === "visual_manual" && item.activeVersion?.id);
  const directorBinding = manualBindings.find((item) => item.manualKind === "director");
  const visualBinding = manualBindings.find((item) => item.manualKind === "visual");

  return (
    <Surface>
      <SectionTitle title="项目设置" description="这些字段会被后续任务和提示词读取。" />
      <div className="grid gap-4 p-5 md:grid-cols-2">
        <div className="space-y-2">
          <Label>项目名称</Label>
          <Input value={form.name ?? ""} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
        </div>
        <div className="space-y-2">
          <Label>项目类型</Label>
          <Input value={form.projectType ?? ""} onChange={(e) => setDraft({ ...draft, projectType: e.target.value })} />
        </div>
        <div className="space-y-2">
          <Label>内容类型</Label>
          <Input value={form.contentType ?? ""} onChange={(e) => setDraft({ ...draft, contentType: e.target.value })} />
        </div>
        <div className="space-y-2">
          <Label>视频比例</Label>
          <Input value={form.videoRatio ?? ""} onChange={(e) => setDraft({ ...draft, videoRatio: e.target.value })} />
        </div>
        <div className="space-y-2">
          <Label>画风风格</Label>
          <Input value={form.artStyle ?? ""} onChange={(e) => setDraft({ ...draft, artStyle: e.target.value })} />
        </div>
        <div className="space-y-2">
          <Label>图片质量</Label>
          <Input value={form.imageQuality ?? ""} onChange={(e) => setDraft({ ...draft, imageQuality: e.target.value })} />
        </div>
        <div className="space-y-2">
          <Label>生产模式</Label>
          <Input value={form.productionMode ?? ""} onChange={(e) => setDraft({ ...draft, productionMode: e.target.value })} />
        </div>
        <div className="space-y-2 md:col-span-2">
          <Label>项目简介</Label>
          <Textarea rows={3} value={form.description ?? ""} onChange={(e) => setDraft({ ...draft, description: e.target.value })} />
        </div>
        <div className="space-y-2 md:col-span-2">
          <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_260px] md:items-end">
            <div>
              <Label>导演手册</Label>
            </div>
            <Select
              value={directorBinding?.promptVersionId ?? ""}
              onValueChange={(promptVersionId) => bindManualMutation.mutate({ kind: "director", promptVersionId })}
              disabled={bindManualMutation.isPending || directorTemplates.length === 0}
            >
              <SelectTrigger>
                <SelectValue placeholder="绑定导演手册模板" />
              </SelectTrigger>
              <SelectContent>
                {directorTemplates.map((template) => (
                  <SelectItem key={template.id} value={template.activeVersion?.id ?? ""}>
                    {template.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Textarea rows={4} value={form.directorManual ?? ""} onChange={(e) => setDraft({ ...draft, directorManual: e.target.value })} />
        </div>
        <div className="space-y-2 md:col-span-2">
          <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_260px] md:items-end">
            <div>
              <Label>视觉手册</Label>
            </div>
            <Select
              value={visualBinding?.promptVersionId ?? ""}
              onValueChange={(promptVersionId) => bindManualMutation.mutate({ kind: "visual", promptVersionId })}
              disabled={bindManualMutation.isPending || visualTemplates.length === 0}
            >
              <SelectTrigger>
                <SelectValue placeholder="绑定视觉手册模板" />
              </SelectTrigger>
              <SelectContent>
                {visualTemplates.map((template) => (
                  <SelectItem key={template.id} value={template.activeVersion?.id ?? ""}>
                    {template.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Textarea rows={4} value={form.visualManual ?? ""} onChange={(e) => setDraft({ ...draft, visualManual: e.target.value })} />
        </div>
        <div className="md:col-span-2">
          <ErrorPanel message={error} />
          <Button onClick={() => saveMutation.mutate(form)} disabled={saveMutation.isPending} className="mt-4">
            {saveMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
            保存设置
          </Button>
        </div>
      </div>
    </Surface>
  );
}

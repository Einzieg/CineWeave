"use client";

import { useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCcw, Save, Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { SectionTitle, Surface } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ErrorPanel } from "@/components/shared/error-panel";
import { VideoProductionRebuildDialog } from "@/features/project-settings/video-production-rebuild-dialog";
import { ProjectDeletionDialog } from "@/features/projects/project-deletion-dialog";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { orgScopedKey, useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { sessionHasPermission, useStudioSession } from "@/lib/session";
import type {
  ModelProfile,
  Project,
  VideoProductionConfigurationInput,
} from "@/lib/types";

type CommerceProductionDraft = {
  videoRatio: string;
  videoModelProfileKey: string;
};

function productionDraftFromProject(project: Project): CommerceProductionDraft {
  return {
    videoRatio: project.videoRatio ?? project.aspectRatio ?? "9:16",
    videoModelProfileKey: project.videoModelProfileKey ?? "video_generation_default",
  };
}

export function CommerceProjectSettingsPage({ projectId, project }: { projectId: string; project: Project }) {
  const queryClient = useQueryClient();
  const invalidate = useInvalidateKeys();
  const { session } = useStudioSession();
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description ?? "");
  const [productionDraft, setProductionDraft] = useState<CommerceProductionDraft>(() => productionDraftFromProject(project));
  const [productionDirty, setProductionDirty] = useState(false);
  const [rebuildOpen, setRebuildOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [error, setError] = useState("");
  const canDeleteProject = sessionHasPermission(session, "project.delete");

  const basicDirty = name !== project.name || description !== (project.description ?? "");

  const profilesQuery = useApiQuery({
    key: qk.modelProfiles(),
    queryFn: (session) => studioApi.listModelProfiles(session).then((response) => response.items),
  });

  const ratios = ["9:16", "16:9", "1:1"];
  const videoProfiles = modelProfilesForPurpose(profilesQuery.data ?? [], "video", productionDraft.videoModelProfileKey);

  const saveBasic = useApiMutation({
    mutationFn: (session) => studioApi.updateProject(session, projectId, {
      name: name.trim(),
      description: description.trim(),
      expectedRevision: project.revision,
    }, `project-update-${project.id}-${project.revision}-${crypto.randomUUID()}`),
    onSuccess: (updated) => {
      queryClient.setQueryData(orgScopedKey(session.organizationId, qk.project(projectId)), updated);
      setName(updated.name);
      setDescription(updated.description ?? "");
      setError("");
      toast.success("基本信息已保存");
    },
    onError: (cause) => {
      if (cause instanceof StudioApiError && cause.code === "PROJECT_REVISION_CONFLICT") invalidate([qk.project(projectId)]);
      setError(cause.message);
    },
  });

  const targetConfiguration: VideoProductionConfigurationInput = {
    projectType: "commerce_video",
    contentType: "",
    aspectRatio: productionDraft.videoRatio,
    videoRatio: productionDraft.videoRatio,
    artStyle: "",
    imageModelProfileKey: project.imageModelProfileKey ?? "image_generation_default",
    videoModelProfileKey: productionDraft.videoModelProfileKey,
    scriptModelProfileKey: project.scriptModelProfileKey ?? "script_agent_default",
    ttsModelProfileKey: project.ttsModelProfileKey ?? "tts_generation_default",
    asrModelProfileKey: project.asrModelProfileKey ?? "audio_transcription_default",
    audioStrategy: "native_av",
    audioRequirement: "preferred",
    imageQuality: project.imageQuality ?? "standard",
    timelineTimebase: project.timelineTimebase ?? 90000,
    fpsNumerator: project.fpsNumerator ?? 24,
    fpsDenominator: project.fpsDenominator ?? 1,
    settings: project.settings ?? {},
  };

  function updateProduction(patch: Partial<CommerceProductionDraft>) {
    setProductionDraft((current) => ({ ...current, ...patch }));
    setProductionDirty(true);
  }

  return (
    <div className="space-y-4">
      <Surface>
        <SectionTitle title="基本信息" />
        <div className="grid gap-4 p-5 md:grid-cols-2">
          <div className="space-y-2"><Label>项目名称</Label><Input value={name} onChange={(event) => setName(event.target.value)} /></div>
          <div className="space-y-2 md:col-span-2"><Label>项目简介</Label><Textarea rows={3} value={description} onChange={(event) => setDescription(event.target.value)} /></div>
          <div className="md:col-span-2"><Button disabled={!basicDirty || !name.trim() || saveBasic.isPending} onClick={() => saveBasic.mutate()}>{saveBasic.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}保存基本信息</Button></div>
        </div>
      </Surface>

      <Surface>
        <SectionTitle title="视频生产配置" />
        <div className="grid gap-4 p-5 md:grid-cols-2">
          <SettingSelect label="画面比例" value={productionDraft.videoRatio} options={ratios.map((value) => ({ value, label: value }))} onChange={(videoRatio) => updateProduction({ videoRatio })} />
          <SettingSelect label="视频业务模型" value={productionDraft.videoModelProfileKey} options={videoProfiles.map(profileOption)} onChange={(videoModelProfileKey) => updateProduction({ videoModelProfileKey })} />
        </div>
      </Surface>

      <ErrorPanel message={error} />
      <Button disabled={!productionDirty && !project.videoProductionLocked} onClick={() => setRebuildOpen(true)}>
        <RefreshCcw className="size-4" />
        {project.videoProductionLocked ? "查看换代进度" : "分析影响并应用生产配置"}
      </Button>

      <VideoProductionRebuildDialog
        projectId={projectId}
        project={project}
        open={rebuildOpen}
        onOpenChange={setRebuildOpen}
        targetConfiguration={targetConfiguration}
        onConfigurationApplied={() => {
          setProductionDirty(false);
          invalidate([qk.project(projectId), qk.commerceProjectProductionStatus(projectId)]);
        }}
      />

      {canDeleteProject ? (
        <Surface>
          <div className="flex flex-wrap items-center justify-between gap-4 p-5">
            <div>
              <p className="font-semibold text-destructive">删除项目</p>
              <p className="mt-1 text-sm text-muted-foreground">永久删除商品资料、广告脚本、生成视频、媒体文件和项目内任务记录。</p>
            </div>
            <Button type="button" variant="destructive" onClick={() => setDeleteDialogOpen(true)}>
              <Trash2 className="size-4" />
              删除项目
            </Button>
          </div>
        </Surface>
      ) : null}

      <ProjectDeletionDialog
        project={project}
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
      />
    </div>
  );
}

function SettingSelect({ label, value, options, onChange, disabled, placeholder }: {
  label: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
  disabled?: boolean;
  placeholder?: string;
}) {
  return <div className="space-y-2"><Label>{label}</Label><Select value={value || undefined} onValueChange={onChange} disabled={disabled}><SelectTrigger><SelectValue placeholder={placeholder} /></SelectTrigger><SelectContent>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select></div>;
}

function modelProfilesForPurpose(profiles: ModelProfile[], modality: "image" | "video", currentKey: string) {
  const matches = profiles.filter((profile) => profile.status !== "disabled" && (
    (profile.purpose ?? "").toLowerCase().includes(modality)
    || profile.profileKey.toLowerCase().includes(modality)
  ));
  const current = profiles.find((profile) => profile.profileKey === currentKey);
  if (current && !matches.some((profile) => profile.id === current.id)) matches.unshift(current);
  if (!matches.length) matches.push({ id: currentKey, profileKey: currentKey, name: currentKey });
  return matches;
}

function profileOption(profile: ModelProfile) {
  return { value: profile.profileKey, label: profile.name || profile.profileKey };
}

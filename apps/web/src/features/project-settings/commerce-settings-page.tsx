"use client";

import { useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCcw, Save, SlidersHorizontal } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { SectionTitle, Surface } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ErrorPanel } from "@/components/shared/error-panel";
import { VideoProductionRebuildDialog } from "@/features/project-settings/video-production-rebuild-dialog";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { orgScopedKey, useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { useStudioSession } from "@/lib/session";
import type {
  CommerceLanguageMode,
  CommerceProjectLanguageOption,
  CommerceScriptUnitDefaults,
  ModelProfile,
  Project,
  VideoProductionConfigurationInput,
} from "@/lib/types";

const platformOptions = [
  { value: "douyin", label: "抖音" },
  { value: "kuaishou", label: "快手" },
  { value: "xiaohongshu", label: "小红书" },
  { value: "wechat_channels", label: "微信视频号" },
  { value: "tiktok", label: "TikTok" },
  { value: "youtube_shorts", label: "YouTube Shorts" },
  { value: "other", label: "其他平台" },
];

type CommerceProductionDraft = {
  videoRatio: string;
  imageQuality: string;
  fpsNumerator: number;
  audioStrategy: "native_av" | "external_audio";
  audioRequirement: "preferred" | "required" | "disabled";
  imageModelProfileKey: string;
  videoModelProfileKey: string;
};

function defaultsFromProject(project: Project): CommerceScriptUnitDefaults {
  return project.scriptUnitDefaults ?? {
    targetDurationSeconds: 30,
    targetPlatform: "douyin",
    languageMode: "auto",
    targetLanguage: null,
  };
}

function productionDraftFromProject(project: Project): CommerceProductionDraft {
  return {
    videoRatio: project.videoRatio ?? project.aspectRatio ?? "9:16",
    imageQuality: project.imageQuality ?? "standard",
    fpsNumerator: project.fpsNumerator ?? 24,
    audioStrategy: project.audioStrategy === "tts_postdub" ? "external_audio" : "native_av",
    audioRequirement: project.audioRequirement ?? "preferred",
    imageModelProfileKey: project.imageModelProfileKey ?? "image_generation_default",
    videoModelProfileKey: project.videoModelProfileKey ?? "video_generation_default",
  };
}

export function CommerceProjectSettingsPage({ projectId, project }: { projectId: string; project: Project }) {
  const queryClient = useQueryClient();
  const invalidate = useInvalidateKeys();
  const { session } = useStudioSession();
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description ?? "");
  const [defaults, setDefaults] = useState<CommerceScriptUnitDefaults>(() => defaultsFromProject(project));
  const [productionDraft, setProductionDraft] = useState<CommerceProductionDraft>(() => productionDraftFromProject(project));
  const [productionDirty, setProductionDirty] = useState(false);
  const [rebuildOpen, setRebuildOpen] = useState(false);
  const [error, setError] = useState("");

  const projectDefaults = useMemo(() => defaultsFromProject(project), [project]);
  const defaultsDirty = JSON.stringify(defaults) !== JSON.stringify(projectDefaults);
  const basicDirty = name !== project.name || description !== (project.description ?? "");

  const optionsQuery = useApiQuery({
    key: qk.commerceProjectOptions(project.workspaceId ?? ""),
    queryFn: (session) => studioApi.getCommerceProjectOptions(session, project.workspaceId ?? ""),
    enabled: Boolean(project.workspaceId),
  });
  const profilesQuery = useApiQuery({
    key: qk.modelProfiles(),
    queryFn: (session) => studioApi.listModelProfiles(session).then((response) => response.items),
  });

  const executableLanguages = useMemo(() => (optionsQuery.data?.languages ?? []).filter((language) => {
    if (!language.textAvailable || !language.imagePromptAvailable || !language.videoPromptAvailable) return false;
    return !(productionDraft.audioStrategy === "native_av" && productionDraft.audioRequirement === "required") || language.nativeAudioAvailable;
  }), [optionsQuery.data?.languages, productionDraft.audioRequirement, productionDraft.audioStrategy]);
  const durations = optionsQuery.data?.durations?.length ? optionsQuery.data.durations : [15, 30, 60];
  const ratios = optionsQuery.data?.aspectRatios?.length ? optionsQuery.data.aspectRatios : ["9:16", "16:9", "1:1"];
  const qualities = optionsQuery.data?.imageQualities?.length ? optionsQuery.data.imageQualities : ["standard", "hd"];
  const imageProfiles = modelProfilesForPurpose(profilesQuery.data ?? [], "image", productionDraft.imageModelProfileKey);
  const videoProfiles = modelProfilesForPurpose(profilesQuery.data ?? [], "video", productionDraft.videoModelProfileKey);

  const saveBasic = useApiMutation({
    mutationFn: (session) => studioApi.updateProject(session, projectId, {
      name: name.trim(),
      description: description.trim(),
      expectedRevision: project.revision,
    }),
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

  const saveDefaults = useApiMutation({
    mutationFn: (session) => studioApi.updateCommerceScriptUnitDefaults(session, projectId, {
      expectedRevision: project.revision,
      targetDurationSeconds: defaults.targetDurationSeconds,
      targetPlatform: defaults.targetPlatform,
      languageMode: defaults.languageMode,
      targetLanguage: defaults.languageMode === "explicit" ? defaults.targetLanguage : null,
    }),
    onSuccess: (updated) => {
      queryClient.setQueryData(orgScopedKey(session.organizationId, qk.project(projectId)), updated);
      setDefaults(defaultsFromProject(updated));
      setError("");
      toast.success("新脚本默认值已保存");
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
    imageModelProfileKey: productionDraft.imageModelProfileKey,
    videoModelProfileKey: productionDraft.videoModelProfileKey,
    scriptModelProfileKey: project.scriptModelProfileKey ?? "script_agent_default",
    ttsModelProfileKey: project.ttsModelProfileKey ?? "tts_generation_default",
    asrModelProfileKey: project.asrModelProfileKey ?? "audio_transcription_default",
    audioStrategy: productionDraft.audioStrategy === "external_audio" ? "tts_postdub" : "native_av",
    audioRequirement: productionDraft.audioRequirement,
    imageQuality: productionDraft.imageQuality,
    timelineTimebase: project.timelineTimebase ?? 90000,
    fpsNumerator: productionDraft.fpsNumerator,
    fpsDenominator: 1,
    settings: project.settings ?? {},
  };

  function updateProduction(patch: Partial<CommerceProductionDraft>) {
    setProductionDraft((current) => ({ ...current, ...patch }));
    setProductionDirty(true);
  }

  const selectedLanguageSupported = defaults.languageMode === "auto"
    || executableLanguages.some((language) => language.locale === defaults.targetLanguage);

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
        <SectionTitle title="输出设置" />
        <div className="grid gap-4 p-5 md:grid-cols-3">
          <SettingSelect label="画面比例" value={productionDraft.videoRatio} options={ratios.map((value) => ({ value, label: value }))} onChange={(videoRatio) => updateProduction({ videoRatio })} />
          <SettingSelect label="图片质量" value={productionDraft.imageQuality} options={qualities.map((value) => ({ value, label: value === "hd" ? "高清" : "标准" }))} onChange={(imageQuality) => updateProduction({ imageQuality })} />
          <SettingSelect label="帧率" value={String(productionDraft.fpsNumerator)} options={[24, 25, 30].map((value) => ({ value: String(value), label: `${value} FPS` }))} onChange={(value) => updateProduction({ fpsNumerator: Number(value) })} />
        </div>
      </Surface>

      <Surface>
        <SectionTitle title="新脚本默认值" />
        <div className="grid gap-4 p-5 md:grid-cols-2 xl:grid-cols-4">
          <SettingSelect label="目标时长" value={String(defaults.targetDurationSeconds)} options={durations.map((value) => ({ value: String(value), label: `${value} 秒` }))} onChange={(value) => setDefaults((current) => ({ ...current, targetDurationSeconds: Number(value) }))} />
          <SettingSelect label="目标平台" value={defaults.targetPlatform} options={platformOptions} onChange={(targetPlatform) => setDefaults((current) => ({ ...current, targetPlatform }))} />
          <SettingSelect label="语言方式" value={defaults.languageMode} options={[{ value: "auto", label: "自动判断" }, { value: "explicit", label: "明确指定" }]} onChange={(value) => setDefaults((current) => ({ ...current, languageMode: value as CommerceLanguageMode, targetLanguage: value === "auto" ? null : current.targetLanguage }))} />
          <SettingSelect label="目标语言" value={defaults.targetLanguage ?? ""} disabled={defaults.languageMode === "auto"} placeholder="选择可执行语言" options={executableLanguages.map(languageSelectOption)} onChange={(targetLanguage) => setDefaults((current) => ({ ...current, targetLanguage }))} />
          <div className="md:col-span-2 xl:col-span-4"><Button disabled={!defaultsDirty || saveDefaults.isPending || !selectedLanguageSupported} onClick={() => saveDefaults.mutate()}>{saveDefaults.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}保存新脚本默认值</Button></div>
        </div>
      </Surface>

      <Surface>
        <SectionTitle title="音频设置" />
        <div className="grid gap-4 p-5 md:grid-cols-2">
          <SettingSelect label="音频方式" value={productionDraft.audioStrategy} options={[{ value: "native_av", label: "视频模型原生音频" }, { value: "external_audio", label: "独立音轨" }]} onChange={(value) => updateProduction({ audioStrategy: value as CommerceProductionDraft["audioStrategy"] })} />
          <SettingSelect label="原生音频要求" value={productionDraft.audioRequirement} options={[{ value: "preferred", label: "优先使用" }, { value: "required", label: "必须支持" }, { value: "disabled", label: "不使用" }]} onChange={(value) => updateProduction({ audioRequirement: value as CommerceProductionDraft["audioRequirement"] })} />
        </div>
      </Surface>

      <Surface>
        <details>
          <summary className="flex cursor-pointer list-none items-center gap-2 border-b p-5 font-semibold"><SlidersHorizontal className="size-4" />图片与视频模型</summary>
          <div className="grid gap-4 p-5 md:grid-cols-2">
            <SettingSelect label="图片业务模型" value={productionDraft.imageModelProfileKey} options={imageProfiles.map(profileOption)} onChange={(imageModelProfileKey) => updateProduction({ imageModelProfileKey })} />
            <SettingSelect label="视频业务模型" value={productionDraft.videoModelProfileKey} options={videoProfiles.map(profileOption)} onChange={(videoModelProfileKey) => updateProduction({ videoModelProfileKey })} />
          </div>
        </details>
      </Surface>

      <ErrorPanel message={error || optionsQuery.error?.message || ""} />
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

function languageSelectOption(language: CommerceProjectLanguageOption) {
  return { value: language.locale, label: language.label };
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

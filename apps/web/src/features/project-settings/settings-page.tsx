"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Edit2, Loader2, Plus, RefreshCcw, Save, Trash2, Volume2 } from "lucide-react";
import { toast } from "sonner";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { ErrorPanel } from "@/components/shared/error-panel";
import { orgScopedKey, useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { useStudioSession } from "@/lib/session";
import {
  applyProjectBasicSaveFailure,
  applyProjectBasicSaveSuccess,
  beginProjectBasicSubmission,
  editProjectBasicField,
  projectBasicValues,
  synchronizeProjectBasicSnapshot,
  type ProjectBasicFormState,
  type ProjectBasicSubmission,
} from "@/lib/project-basic-form-state";
import type {
  CharacterVoiceProfile,
  NarrativeContentType,
  NarrativeProjectType,
  Project,
  VideoProductionConfigurationInput,
} from "@/lib/types";
import {
  buildManualStyleOptions,
  DEFAULT_DIRECTOR_MANUAL_KEY,
  DEFAULT_VISUAL_MANUAL_KEY,
  ManualStyleSelector,
  withToonflowSetting,
  type ManualStyleOption,
} from "@/features/projects/manual-style-selector";
import { VideoProductionRebuildDialog } from "@/features/project-settings/video-production-rebuild-dialog";
import { CommerceProjectSettingsPage } from "@/features/project-settings/commerce-settings-page";

const defaultArtStyle = "写实电影感";

type ManualDraft = {
  directorTemplateKey?: string;
  directorPromptVersionId?: string;
  visualTemplateKey?: string;
  visualPromptVersionId?: string;
};

type VoiceDraft = {
  id?: string;
  canonicalAssetId: string;
  characterName: string;
  displayName: string;
  language: string;
  modelProfileKey: string;
  voiceKey: string;
  instructions: string;
  isDefault: boolean;
};

const emptyVoiceDraft: VoiceDraft = {
  canonicalAssetId: "",
  characterName: "",
  displayName: "",
  language: "zh-CN",
  modelProfileKey: "tts_generation_default",
  voiceKey: "",
  instructions: "",
  isDefault: false,
};

function basicSnapshotFromProject(project: Project) {
  return {
    name: project.name ?? "",
    description: project.description ?? "",
    revision: project.revision,
  };
}

export function ProjectSettingsPage({ projectId }: { projectId: string }) {
  const { data: project, isLoading, error } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
  });
  if (isLoading) return <Skeleton className="h-64" />;
  if (error) return <ErrorPanel message={error.message} />;
  if (!project) return <div>项目不存在</div>;
  if (project.projectKind === "commerce_video") {
    return <CommerceProjectSettingsPage projectId={projectId} project={project} />;
  }
  return <NarrativeProjectSettingsPage projectId={projectId} />;
}

function NarrativeProjectSettingsPage({ projectId }: { projectId: string }) {
  const queryClient = useQueryClient();
  const { session } = useStudioSession();
  const { data: project, isLoading } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
  });
  const { data: manualTemplates = [], isLoading: manualTemplatesLoading } = useApiQuery({
    key: qk.projectManualTemplates(),
    queryFn: (session) => studioApi.listProjectManualTemplates(session).then((response) => response.items),
  });
  const { data: manualBindings = [] } = useApiQuery({
    key: qk.projectManualBindings(projectId),
    queryFn: (session) => studioApi.listProjectManualBindings(session, projectId).then((response) => response.items),
  });
  const { data: characterAssets = [] } = useApiQuery({
    key: qk.assets(projectId, { status: "active", assetType: "character" }),
    queryFn: (session) => studioApi.listCanonicalAssets(session, projectId, { status: "active", assetType: "character" }).then((response) => response.items),
  });
  const { data: modelProfiles = [] } = useApiQuery({
    key: qk.modelProfiles(),
    queryFn: (session) => studioApi.listModelProfiles(session).then((response) => response.items),
  });
  const { data: characterVoices = [], isLoading: voicesLoading } = useApiQuery({
    key: qk.characterVoices(projectId),
    queryFn: (session) => studioApi.listCharacterVoices(session, projectId).then((response) => response.items),
  });

  const [draft, setDraft] = useState<Partial<Project>>({});
  const [basicFormState, setBasicFormState] = useState<ProjectBasicFormState | null>(null);
  const [manualDraft, setManualDraft] = useState<ManualDraft>({});
  const [error, setError] = useState("");
  const [voiceDialogOpen, setVoiceDialogOpen] = useState(false);
  const [rebuildDialogOpen, setRebuildDialogOpen] = useState(false);
  const [voiceDraft, setVoiceDraft] = useState<VoiceDraft>(emptyVoiceDraft);
  const invalidateKeys = useInvalidateKeys();

  const effectiveBasicFormState = useMemo(
    () => project ? synchronizeProjectBasicSnapshot(basicFormState, basicSnapshotFromProject(project)) : null,
    [basicFormState, project],
  );
  const basicValues = effectiveBasicFormState ? projectBasicValues(effectiveBasicFormState) : null;
  const form = project ? {
    ...project,
    ...draft,
    name: basicValues?.name ?? project.name,
    description: basicValues?.description ?? project.description,
  } : null;
  const hasUnsavedChanges = Boolean(
    effectiveBasicFormState?.dirtyFields.length
      || Object.keys(draft).length
      || Object.keys(manualDraft).length,
  );

  useEffect(() => {
    if (!hasUnsavedChanges) {
      return;
    }
    const warnBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warnBeforeUnload);
    return () => window.removeEventListener("beforeunload", warnBeforeUnload);
  }, [hasUnsavedChanges]);
  const directorTemplates = useMemo(() => buildManualStyleOptions(manualTemplates, "director"), [manualTemplates]);
  const visualTemplates = useMemo(() => buildManualStyleOptions(manualTemplates, "visual"), [manualTemplates]);

  const saveBasicMutation = useApiMutation({
    mutationFn: async (session, submission: ProjectBasicSubmission) => {
      return studioApi.updateProject(session, projectId, {
        name: submission.values.name,
        description: submission.values.description,
        expectedRevision: submission.baseRevision,
      });
    },
    onSuccess: (updatedProject, submission) => {
      queryClient.setQueryData(orgScopedKey(session.organizationId, qk.project(projectId)), updatedProject);
      setBasicFormState((current) => current
        ? applyProjectBasicSaveSuccess(current, submission, basicSnapshotFromProject(updatedProject))
        : synchronizeProjectBasicSnapshot(null, basicSnapshotFromProject(updatedProject)));
      setError("");
      toast.success("基本信息已保存");
    },
    onError: (err, submission) => {
      setBasicFormState((current) => current ? applyProjectBasicSaveFailure(current, submission) : current);
      if (err instanceof StudioApiError && err.code === "PROJECT_REVISION_CONFLICT") {
        invalidateKeys([qk.project(projectId)]);
      }
      setError(err instanceof Error ? err.message : "保存失败");
    },
  });

  function submitBasicSettings() {
    if (!effectiveBasicFormState) {
      return;
    }
    const started = beginProjectBasicSubmission(effectiveBasicFormState, crypto.randomUUID());
    if (!started) {
      return;
    }
    setBasicFormState(started.state);
    saveBasicMutation.mutate(started.submission);
  }

  const saveVoiceMutation = useApiMutation({
    mutationFn: (session, data: VoiceDraft) => {
      const body = {
        canonicalAssetId: data.canonicalAssetId || null,
        characterName: data.characterName,
        displayName: data.displayName,
        language: data.language,
        modelProfileKey: data.modelProfileKey,
        voiceKey: data.voiceKey,
        instructions: data.instructions || null,
        isDefault: data.isDefault,
      };
      return data.id
        ? studioApi.updateCharacterVoice(session, projectId, data.id, body)
        : studioApi.createCharacterVoice(session, projectId, body);
    },
    onSuccess: () => {
      setVoiceDialogOpen(false);
      setVoiceDraft(emptyVoiceDraft);
      invalidateKeys([qk.characterVoices(projectId)]);
      toast.success("角色声音已保存");
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "角色声音保存失败"),
  });

  const deleteVoiceMutation = useApiMutation({
    mutationFn: (session, voiceId: string) => studioApi.deleteCharacterVoice(session, projectId, voiceId),
    onSuccess: () => {
      invalidateKeys([qk.characterVoices(projectId)]);
      toast.success("角色声音已移除");
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "角色声音移除失败"),
  });

  if (isLoading) {
    return (
      <div className="grid gap-4">
        <Skeleton className="h-64" />
      </div>
    );
  }

  if (!form || !project) {
    return <div>项目不存在</div>;
  }

  const directorBinding = manualBindings.find((item) => item.manualKind === "director");
  const visualBinding = manualBindings.find((item) => item.manualKind === "visual");
  const selectedDirectorTemplateKey = manualDraft.directorTemplateKey ?? directorBinding?.templateKey ?? DEFAULT_DIRECTOR_MANUAL_KEY;
  const selectedVisualTemplateKey = manualDraft.visualTemplateKey ?? visualBinding?.templateKey ?? DEFAULT_VISUAL_MANUAL_KEY;
  const ttsProfiles = modelProfiles.filter((profile) => profile.status !== "disabled" && (profile.purpose === "audio_tts" || profile.profileKey.includes("tts")));
  const asrProfiles = modelProfiles.filter((profile) => profile.status !== "disabled" && (profile.purpose === "audio_transcription" || profile.profileKey.includes("transcription") || profile.profileKey.includes("asr")));
  const targetConfiguration: VideoProductionConfigurationInput = {
    projectType: form.projectType ?? "",
    contentType: form.contentType ?? "",
    aspectRatio: form.aspectRatio ?? form.videoRatio ?? "16:9",
    videoRatio: form.videoRatio ?? form.aspectRatio ?? "16:9",
    artStyle: form.artStyle ?? defaultArtStyle,
    directorManualPromptVersionId: manualDraft.directorPromptVersionId ?? directorBinding?.promptVersionId,
    visualManualPromptVersionId: manualDraft.visualPromptVersionId ?? visualBinding?.promptVersionId,
    imageModelProfileKey: form.imageModelProfileKey ?? "image_generation_default",
    videoModelProfileKey: form.videoModelProfileKey ?? "video_generation_default",
    scriptModelProfileKey: form.scriptModelProfileKey ?? "script_agent_default",
    ttsModelProfileKey: form.ttsModelProfileKey ?? "tts_generation_default",
    asrModelProfileKey: form.asrModelProfileKey ?? "audio_transcription_default",
    audioStrategy: form.audioStrategy ?? "native_av",
    audioRequirement: form.audioRequirement ?? "preferred",
    imageQuality: form.imageQuality ?? "standard",
    timelineTimebase: form.timelineTimebase ?? 90000,
    fpsNumerator: form.fpsNumerator ?? 24,
    fpsDenominator: form.fpsDenominator ?? 1,
    settings: form.settings ?? {},
  };

  function selectManual(option: ManualStyleOption) {
    if (option.kind === "director") {
      setManualDraft((current) => ({
        ...current,
        directorTemplateKey: option.templateKey,
        directorPromptVersionId: option.promptVersionId,
      }));
      setDraft((current) => ({
        ...current,
        settings: withToonflowSetting(current.settings ?? project?.settings, "toonflowStoryStyle", option.styleSlug),
      }));
      return;
    }
    setManualDraft((current) => ({
      ...current,
      visualTemplateKey: option.templateKey,
      visualPromptVersionId: option.promptVersionId,
    }));
    setDraft((current) => ({
      ...current,
      artStyle: option.styleSlug ?? defaultArtStyle,
      settings: withToonflowSetting(current.settings ?? project?.settings, "toonflowVisualStyle", option.styleSlug),
    }));
  }

  function editVoice(voice: CharacterVoiceProfile) {
    setVoiceDraft({
      id: voice.id,
      canonicalAssetId: voice.canonicalAssetId ?? "",
      characterName: voice.characterName,
      displayName: voice.displayName,
      language: voice.language,
      modelProfileKey: voice.modelProfileKey,
      voiceKey: voice.voiceKey,
      instructions: voice.instructions ?? "",
      isDefault: voice.isDefault,
    });
    setVoiceDialogOpen(true);
  }

  return (
    <div className="space-y-4">
      <Surface>
        <SectionTitle title="基本信息" />
        <div className="grid gap-4 p-5 md:grid-cols-2">
        <div className="space-y-2">
          <Label>项目名称</Label>
          <Input
            value={form.name ?? ""}
            onChange={(event) => effectiveBasicFormState && setBasicFormState(editProjectBasicField(effectiveBasicFormState, "name", event.target.value))}
          />
        </div>
        <div className="space-y-2 md:col-span-2">
          <Label>项目简介</Label>
          <Textarea
            rows={3}
            value={form.description ?? ""}
            onChange={(event) => effectiveBasicFormState && setBasicFormState(editProjectBasicField(effectiveBasicFormState, "description", event.target.value))}
          />
        </div>
        <div className="md:col-span-2">
          <ErrorPanel message={error} />
          <Button
            onClick={submitBasicSettings}
            disabled={saveBasicMutation.isPending || !effectiveBasicFormState?.dirtyFields.length}
            className="mt-4"
          >
            {saveBasicMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
            保存基本信息
          </Button>
        </div>
        </div>
      </Surface>

      <Surface>
        <SectionTitle title="视频生产配置" />
        <div className="grid gap-4 p-5 md:grid-cols-2">
        <div className="space-y-2">
          <Label>项目类型</Label>
          <Select
            value={form.projectType ?? "short_film"}
            onValueChange={(value: NarrativeProjectType) => setDraft({ ...draft, projectType: value })}
            disabled={project.projectKind === "commerce_video"}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="short_film">短片</SelectItem>
              <SelectItem value="comic_drama">漫剧</SelectItem>
              <SelectItem value="brand_ad">品牌广告</SelectItem>
              <SelectItem value="character_ip">角色 IP</SelectItem>
              <SelectItem value="other">其他</SelectItem>
              {project.projectKind === "commerce_video" ? <SelectItem value="commerce_video">带货视频</SelectItem> : null}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>内容类型</Label>
          <Select
            value={form.contentType ?? "script"}
            onValueChange={(value: NarrativeContentType) => setDraft({ ...draft, contentType: value })}
            disabled={project.projectKind === "commerce_video"}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="novel">小说改编</SelectItem>
              <SelectItem value="script">剧本创作</SelectItem>
              <SelectItem value="storyboard_first">分镜先行</SelectItem>
              <SelectItem value="original">自定义</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>视频比例</Label>
          <Input value={form.videoRatio ?? ""} onChange={(e) => setDraft({ ...draft, videoRatio: e.target.value, aspectRatio: e.target.value })} />
        </div>
        <div className="space-y-2">
          <Label>画风风格</Label>
          <Input value={form.artStyle ?? ""} onChange={(e) => setDraft({ ...draft, artStyle: e.target.value })} />
        </div>
        <div className="space-y-2">
          <Label>图片质量</Label>
          <Select value={form.imageQuality ?? "standard"} onValueChange={(value) => setDraft({ ...draft, imageQuality: value })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="standard">标准</SelectItem>
              <SelectItem value="hd">高清</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2 md:col-span-2">
          <Label>视频生产方案</Label>
          <div className="flex min-h-14 flex-wrap items-center justify-between gap-3 rounded-md border bg-muted/30 px-3 py-2 text-sm">
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{form.videoProductionBinding?.profileName ?? "图生视频模式"}</span>
                <Badge variant="outline">v{form.videoProductionBinding?.profileVersion ?? 1}</Badge>
                <Badge variant="secondary">第 {form.productionGeneration?.generationNo ?? 1} 代</Badge>
                {form.videoProductionLocked ? <Badge variant="destructive">重建中</Badge> : null}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">
                绑定修订 {form.videoProductionBinding?.revision ?? 1}
                {form.productionGeneration?.id ? ` · 生产代 ${form.productionGeneration.id.slice(0, 8)}` : ""}
              </div>
            </div>
            <Button type="button" variant="outline" size="sm" onClick={() => setRebuildDialogOpen(true)}>
              <RefreshCcw className="h-4 w-4" />
              {form.videoProductionLocked ? "查看重建进度" : "重建或切换方案"}
            </Button>
          </div>
        </div>
        <div className="space-y-2">
          <Label>音频生产策略</Label>
          <Select value={form.audioStrategy ?? "native_av"} onValueChange={(value) => setDraft({ ...draft, audioStrategy: value as Project["audioStrategy"] })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="native_av">视频模型原生音视频</SelectItem>
              <SelectItem value="hybrid">原生音轨与后期配音混合</SelectItem>
              <SelectItem value="tts_postdub">TTS 后期配音</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>原生音频要求</Label>
          <Select value={form.audioRequirement ?? "preferred"} onValueChange={(value) => setDraft({ ...draft, audioRequirement: value as Project["audioRequirement"] })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="preferred">优先使用</SelectItem>
              <SelectItem value="required">必须通过审核</SelectItem>
              <SelectItem value="disabled">不使用原生音频</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>TTS 业务模型</Label>
          <Select value={form.ttsModelProfileKey ?? "tts_generation_default"} onValueChange={(value) => setDraft({ ...draft, ttsModelProfileKey: value })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {ttsProfiles.length === 0 ? <SelectItem value="tts_generation_default">角色配音默认模型</SelectItem> : null}
              {ttsProfiles.map((profile) => <SelectItem key={profile.id} value={profile.profileKey}>{profile.name || profile.profileKey}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>音轨审核业务模型</Label>
          <Select value={form.asrModelProfileKey ?? "audio_transcription_default"} onValueChange={(value) => setDraft({ ...draft, asrModelProfileKey: value })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {asrProfiles.length === 0 ? <SelectItem value="audio_transcription_default">音轨识别默认模型</SelectItem> : null}
              {asrProfiles.map((profile) => <SelectItem key={profile.id} value={profile.profileKey}>{profile.name || profile.profileKey}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-4 md:col-span-2 xl:grid-cols-2">
          <ManualStyleSelector
            title="视觉手册"
            options={visualTemplates}
            selectedTemplateKey={selectedVisualTemplateKey}
            loading={manualTemplatesLoading}
            onSelect={selectManual}
          />
          <ManualStyleSelector
            title="导演手册"
            options={directorTemplates}
            selectedTemplateKey={selectedDirectorTemplateKey}
            loading={manualTemplatesLoading}
            onSelect={selectManual}
          />
        </div>
        <div className="md:col-span-2">
          <Button onClick={() => setRebuildDialogOpen(true)}>
            <RefreshCcw className="mr-2 h-4 w-4" />
            {form.videoProductionLocked ? "查看重建进度" : "分析影响并应用"}
          </Button>
        </div>
        </div>
      </Surface>

      <VideoProductionRebuildDialog
        projectId={projectId}
        project={project}
        targetConfiguration={targetConfiguration}
        open={rebuildDialogOpen}
        onOpenChange={setRebuildDialogOpen}
        onConfigurationApplied={() => {
          setDraft({});
          setManualDraft({});
        }}
      />

      <Surface>
        <div className="flex items-center justify-between gap-4 border-b p-5">
          <div>
            <div className="flex items-center gap-2 font-semibold"><Volume2 className="h-4 w-4" />角色声音</div>
            <div className="mt-1 text-sm text-muted-foreground">角色对白按声音配置独立生成并写入分集音轨。</div>
          </div>
          <Button type="button" onClick={() => { setVoiceDraft({ ...emptyVoiceDraft, isDefault: characterVoices.length === 0, modelProfileKey: form.ttsModelProfileKey ?? "tts_generation_default" }); setVoiceDialogOpen(true); }}>
            <Plus className="mr-2 h-4 w-4" />添加声音
          </Button>
        </div>
        <div className="divide-y">
          {voicesLoading ? <div className="p-5"><Skeleton className="h-16" /></div> : null}
          {!voicesLoading && characterVoices.length === 0 ? <div className="p-5 text-sm text-muted-foreground">尚未配置角色声音</div> : null}
          {characterVoices.map((voice) => (
            <div key={voice.id} className="flex items-center justify-between gap-4 p-5">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{voice.characterName}</span>
                  <Badge variant="outline">{voice.displayName}</Badge>
                  <Badge variant="secondary">{voice.language}</Badge>
                  {voice.isDefault ? <Badge>默认旁白</Badge> : null}
                </div>
                <div className="mt-1 truncate text-sm text-muted-foreground">声音 {voice.voiceKey} · {voice.modelProfileKey}</div>
              </div>
              <div className="flex shrink-0 gap-1">
                <Button type="button" size="icon" variant="ghost" title="编辑声音" onClick={() => editVoice(voice)}><Edit2 className="h-4 w-4" /></Button>
                <Button type="button" size="icon" variant="ghost" title="移除声音" disabled={deleteVoiceMutation.isPending} onClick={() => deleteVoiceMutation.mutate(voice.id)}><Trash2 className="h-4 w-4" /></Button>
              </div>
            </div>
          ))}
        </div>
      </Surface>

      <Dialog open={voiceDialogOpen} onOpenChange={(open) => { setVoiceDialogOpen(open); if (!open) setVoiceDraft(emptyVoiceDraft); }}>
        <DialogContent>
          <DialogHeader><DialogTitle>{voiceDraft.id ? "编辑角色声音" : "添加角色声音"}</DialogTitle></DialogHeader>
          <div className="grid gap-4 py-2 md:grid-cols-2">
            <div className="space-y-2 md:col-span-2">
              <Label>关联角色资产</Label>
              <Select value={voiceDraft.canonicalAssetId || "none"} onValueChange={(value) => {
                const asset = characterAssets.find((item) => item.id === value);
                setVoiceDraft((current) => ({ ...current, canonicalAssetId: value === "none" ? "" : value, characterName: asset?.name || current.characterName }));
              }}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">不关联资产</SelectItem>
                  {characterAssets.map((asset) => <SelectItem key={asset.id} value={asset.id}>{asset.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2"><Label>角色名称</Label><Input value={voiceDraft.characterName} onChange={(event) => setVoiceDraft({ ...voiceDraft, characterName: event.target.value })} /></div>
            <div className="space-y-2"><Label>声音名称</Label><Input value={voiceDraft.displayName} onChange={(event) => setVoiceDraft({ ...voiceDraft, displayName: event.target.value })} /></div>
            <div className="space-y-2"><Label>声音标识</Label><Input value={voiceDraft.voiceKey} onChange={(event) => setVoiceDraft({ ...voiceDraft, voiceKey: event.target.value })} /></div>
            <div className="space-y-2"><Label>语言</Label><Input value={voiceDraft.language} onChange={(event) => setVoiceDraft({ ...voiceDraft, language: event.target.value })} /></div>
            <div className="space-y-2 md:col-span-2">
              <Label>TTS 业务模型</Label>
              <Select value={voiceDraft.modelProfileKey} onValueChange={(value) => setVoiceDraft({ ...voiceDraft, modelProfileKey: value })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {ttsProfiles.length === 0 ? <SelectItem value="tts_generation_default">角色配音默认模型</SelectItem> : null}
                  {ttsProfiles.map((profile) => <SelectItem key={profile.id} value={profile.profileKey}>{profile.name || profile.profileKey}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2 md:col-span-2"><Label>声音指令</Label><Textarea rows={3} value={voiceDraft.instructions} onChange={(event) => setVoiceDraft({ ...voiceDraft, instructions: event.target.value })} /></div>
            <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2 md:col-span-2">
              <Label htmlFor="voice-is-default">默认旁白与未匹配角色声音</Label>
              <Switch id="voice-is-default" checked={voiceDraft.isDefault} onCheckedChange={(checked) => setVoiceDraft({ ...voiceDraft, isDefault: checked })} />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setVoiceDialogOpen(false)}>取消</Button>
            <Button type="button" disabled={saveVoiceMutation.isPending || !voiceDraft.characterName.trim() || !voiceDraft.displayName.trim() || !voiceDraft.voiceKey.trim()} onClick={() => saveVoiceMutation.mutate(voiceDraft)}>
              {saveVoiceMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

"use client";

import { useMemo, useState } from "react";
import { Edit2, Loader2, Plus, Save, Trash2, Volume2 } from "lucide-react";
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
import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import type { CharacterVoiceProfile, Project } from "@/lib/types";
import {
  buildManualStyleOptions,
  DEFAULT_DIRECTOR_MANUAL_KEY,
  DEFAULT_VISUAL_MANUAL_KEY,
  ManualStyleSelector,
  withToonflowSetting,
  type ManualStyleOption,
} from "@/features/projects/manual-style-selector";

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

export function ProjectSettingsPage({ projectId }: { projectId: string }) {
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
    key: qk.assets(projectId),
    queryFn: (session) => studioApi.listCanonicalAssets(session, projectId).then((response) => response.items.filter((asset) => asset.assetType === "character")),
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
  const [manualDraft, setManualDraft] = useState<ManualDraft>({});
  const [error, setError] = useState("");
  const [voiceDialogOpen, setVoiceDialogOpen] = useState(false);
  const [voiceDraft, setVoiceDraft] = useState<VoiceDraft>(emptyVoiceDraft);
  const invalidateKeys = useInvalidateKeys();

  const form = project ? { ...project, ...draft } : null;
  const directorTemplates = useMemo(() => buildManualStyleOptions(manualTemplates, "director"), [manualTemplates]);
  const visualTemplates = useMemo(() => buildManualStyleOptions(manualTemplates, "visual"), [manualTemplates]);

  const saveMutation = useApiMutation({
    mutationFn: async (session, data: { project: Partial<Project>; manuals: ManualDraft }) => {
      const expectedRevision = data.project.revision ?? project?.revision;
      if (expectedRevision === undefined) {
        throw new Error("项目版本不可用，请刷新后重试");
      }
      const updated = await studioApi.updateProject(session, projectId, {
        name: data.project.name || "",
        description: data.project.description || null,
        projectType: data.project.projectType || "",
        contentType: data.project.contentType || "",
        videoRatio: data.project.videoRatio || "16:9",
        artStyle: data.project.artStyle || "",
        imageQuality: data.project.imageQuality || "standard",
        productionMode: data.project.productionMode || "silent_video",
        imageModelProfileKey: data.project.imageModelProfileKey || null,
        videoModelProfileKey: data.project.videoModelProfileKey || null,
        scriptModelProfileKey: data.project.scriptModelProfileKey || null,
        ttsModelProfileKey: data.project.ttsModelProfileKey || "tts_generation_default",
        asrModelProfileKey: data.project.asrModelProfileKey || "audio_transcription_default",
        audioStrategy: data.project.audioStrategy || "native_av",
        audioRequirement: data.project.audioRequirement || "preferred",
        directorManualPromptVersionId: data.manuals.directorPromptVersionId || null,
        visualManualPromptVersionId: data.manuals.visualPromptVersionId || null,
        settings: data.project.settings ?? {},
        expectedRevision,
      });
      return updated;
    },
    onSuccess: () => {
      setDraft({});
      setManualDraft({});
      setError("");
      toast.success("项目设置已保存");
      invalidateKeys([qk.project(projectId), qk.projectManualBindings(projectId)]);
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : "保存失败");
    },
  });

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

  if (!form) {
    return <div>项目不存在</div>;
  }

  const directorBinding = manualBindings.find((item) => item.manualKind === "director");
  const visualBinding = manualBindings.find((item) => item.manualKind === "visual");
  const selectedDirectorTemplateKey = manualDraft.directorTemplateKey ?? directorBinding?.templateKey ?? DEFAULT_DIRECTOR_MANUAL_KEY;
  const selectedVisualTemplateKey = manualDraft.visualTemplateKey ?? visualBinding?.templateKey ?? DEFAULT_VISUAL_MANUAL_KEY;
  const ttsProfiles = modelProfiles.filter((profile) => profile.status !== "disabled" && (profile.purpose === "audio_tts" || profile.profileKey.includes("tts")));
  const asrProfiles = modelProfiles.filter((profile) => profile.status !== "disabled" && (profile.purpose === "audio_transcription" || profile.profileKey.includes("transcription") || profile.profileKey.includes("asr")));

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
        <div className="space-y-2 md:col-span-2">
          <Label>项目简介</Label>
          <Textarea rows={3} value={form.description ?? ""} onChange={(e) => setDraft({ ...draft, description: e.target.value })} />
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
          <ErrorPanel message={error} />
          <Button onClick={() => saveMutation.mutate({ project: form, manuals: manualDraft })} disabled={saveMutation.isPending} className="mt-4">
            {saveMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
            保存设置
          </Button>
        </div>
        </div>
      </Surface>

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

"use client";

import { useRouter } from "next/navigation";
import type { Route } from "next";
import { useEffect, useMemo, useState } from "react";
import type { LucideIcon } from "lucide-react";
import {
  Check,
  Clapperboard,
  FileText,
  ImageIcon,
  Layers,
  Loader2,
  Monitor,
  Package,
  RectangleHorizontal,
  RectangleVertical,
  Smartphone,
  Sparkles,
  Square,
} from "lucide-react";
import { AppShell } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ErrorPanel } from "@/components/shared/error-panel";
import { sessionHasPermission, useStudioSession } from "@/lib/session";
import { studioApi, StudioApiError } from "@/lib/api-client";
import { projectHref } from "@/lib/routes";
import { useApiQuery } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { cn } from "@/lib/utils";
import type {
  NarrativeContentType,
  NarrativeProjectType,
  ProjectKind,
  VideoProductionProfileKey,
} from "@/lib/types";
import {
  buildManualStyleOptions,
  DEFAULT_DIRECTOR_MANUAL_KEY,
  DEFAULT_VISUAL_MANUAL_KEY,
  ManualStyleSelector,
  type ManualStyleOption,
} from "./manual-style-selector";

const defaultArtStyle = "写实电影感";

export function NewProjectPage() {
  return (
    <AppShell active="projects" title="新建项目" description="选择业务类型后完成创建所需配置。">
      <NewProjectContent />
    </AppShell>
  );
}

const ratioOptions: Array<{ value: string; label: string; hint: string; icon: LucideIcon }> = [
  { value: "9:16", label: "竖屏", hint: "9:16", icon: Smartphone },
  { value: "16:9", label: "横屏", hint: "16:9", icon: Monitor },
  { value: "21:9", label: "影院宽屏", hint: "21:9", icon: RectangleHorizontal },
  { value: "4:3", label: "经典画幅", hint: "4:3", icon: Square },
  { value: "3:4", label: "竖向画幅", hint: "3:4", icon: RectangleVertical },
  { value: "1:1", label: "方形", hint: "1:1", icon: Square },
];

const projectTypeOptions: Array<{
  value: NarrativeProjectType | "commerce_video";
  kind: ProjectKind;
  title: string;
  description: string;
  icon: LucideIcon;
}> = [
  { value: "short_film", kind: "narrative", title: "短片", description: "完整短片与叙事视频", icon: Clapperboard },
  { value: "comic_drama", kind: "narrative", title: "漫剧", description: "分集漫画与动态漫", icon: Layers },
  { value: "brand_ad", kind: "narrative", title: "品牌广告", description: "品牌叙事与创意广告", icon: Sparkles },
  { value: "character_ip", kind: "narrative", title: "角色 IP", description: "角色驱动的系列内容", icon: ImageIcon },
  { value: "commerce_video", kind: "commerce_video", title: "带货视频", description: "商品图片与多脚本独立成片", icon: Package },
  { value: "other", kind: "narrative", title: "其他", description: "自定义叙事生产流程", icon: FileText },
];

const videoProductionProfileOptions: Array<{
  value: VideoProductionProfileKey;
  title: string;
  description: string;
  icon: LucideIcon;
}> = [
  { value: "single_frame_i2v", title: "图生视频模式", description: "每个镜头使用自己的权威首帧生成视频", icon: Clapperboard },
  { value: "first_last_frame", title: "首尾帧衔接模式", description: "使用同镜头计划首尾帧约束动作过程", icon: Layers },
  { value: "multimodal_reference", title: "多模态参考模式", description: "使用角色、场景、道具和多媒体语义参考", icon: ImageIcon },
  { value: "storyboard_sheet", title: "分镜板模式", description: "使用同一镜头多时间点分镜板生成视频", icon: Monitor },
];

const qualityOptions = [
  { value: "standard", label: "标准" },
  { value: "hd", label: "高清" },
];

type NewNarrativeProjectForm = {
  name: string;
  description: string;
  projectType: NarrativeProjectType;
  contentType: NarrativeContentType;
  videoRatio: string;
  imageQuality: string;
  videoProductionProfileKey: VideoProductionProfileKey;
  artStyle: string;
  directorManualTemplateKey: string;
  directorManualPromptVersionId: string;
  visualManualTemplateKey: string;
  visualManualPromptVersionId: string;
  toonflowVisualStyle: string;
  toonflowStoryStyle: string;
};

type CommerceProjectForm = {
  name: string;
  description: string;
  videoRatio: string;
};

type PersistedCommerceDraft = {
  clientRequestId: string;
  form: CommerceProjectForm;
};

function NewProjectContent() {
  const router = useRouter();
  const { session, ready } = useStudioSession();
  const workspaceId = session.workspaceId?.trim() ?? "";
  const canCreateProject = sessionHasPermission(session, "project.write");
  const [selectedProjectType, setSelectedProjectType] = useState<NarrativeProjectType | "commerce_video">("short_film");
  const projectKind: ProjectKind = selectedProjectType === "commerce_video" ? "commerce_video" : "narrative";
  const [busy, setBusy] = useState(false);
  const [stage, setStage] = useState("");
  const [error, setError] = useState("");
  const [clientRequestId, setClientRequestId] = useState("");
  const [narrativeForm, setNarrativeForm] = useState<NewNarrativeProjectForm>({
    name: "",
    description: "",
    projectType: "short_film",
    contentType: "script",
    videoRatio: "9:16",
    imageQuality: "standard",
    videoProductionProfileKey: "single_frame_i2v",
    artStyle: defaultArtStyle,
    directorManualTemplateKey: DEFAULT_DIRECTOR_MANUAL_KEY,
    directorManualPromptVersionId: "",
    visualManualTemplateKey: DEFAULT_VISUAL_MANUAL_KEY,
    visualManualPromptVersionId: "",
    toonflowVisualStyle: "",
    toonflowStoryStyle: "",
  });
  const [commerceForm, setCommerceForm] = useState<CommerceProjectForm>({
    name: "",
    description: "",
    videoRatio: "9:16",
  });

  const { data: manualTemplates = [], isLoading: manualTemplatesLoading } = useApiQuery({
    key: qk.projectManualTemplates(),
    queryFn: (activeSession) => studioApi.listProjectManualTemplates(activeSession).then((response) => response.items),
    enabled: canCreateProject && projectKind === "narrative",
  });
  const { data: videoProductionProfileVersions = [], isLoading: videoProductionProfilesLoading } = useApiQuery({
    key: qk.videoProductionProfiles(),
    queryFn: (activeSession) => studioApi.listVideoProductionProfiles(activeSession).then((response) => response.items),
    enabled: canCreateProject && projectKind === "narrative",
  });
  const directorManualOptions = useMemo(() => buildManualStyleOptions(manualTemplates, "director"), [manualTemplates]);
  const visualManualOptions = useMemo(() => buildManualStyleOptions(manualTemplates, "visual"), [manualTemplates]);
  const videoProductionProfileCards = useMemo(() => videoProductionProfileOptions.map((option) => {
    const profile = videoProductionProfileVersions
      .filter((candidate) => candidate.profileKey === option.value)
      .sort((left, right) => right.version - left.version)[0];
    return { ...option, available: profile?.available === true, description: profile?.description || option.description, version: profile?.version };
  }), [videoProductionProfileVersions]);
  useEffect(() => {
    if (!workspaceId) return;
    const key = commerceDraftStorageKey(workspaceId);
    const raw = window.localStorage.getItem(key);
    const frame = window.requestAnimationFrame(() => {
      if (raw) {
        try {
          const saved = JSON.parse(raw) as PersistedCommerceDraft;
          if (saved.clientRequestId && saved.form) {
            setClientRequestId(saved.clientRequestId);
            setCommerceForm(saved.form);
            return;
          }
        } catch {
          window.localStorage.removeItem(key);
        }
      }
      setClientRequestId(crypto.randomUUID());
    });
    return () => window.cancelAnimationFrame(frame);
  }, [workspaceId]);

  useEffect(() => {
    if (!workspaceId || !clientRequestId) return;
    const saved: PersistedCommerceDraft = {
      clientRequestId,
      form: commerceForm,
    };
    window.localStorage.setItem(commerceDraftStorageKey(workspaceId), JSON.stringify(saved));
  }, [clientRequestId, commerceForm, workspaceId]);

  async function submit() {
    setError("");
    if (!canCreateProject) {
      setError("当前账号没有创建项目的权限，请联系组织管理员授权。");
      return;
    }
    if (!ready || !workspaceId) {
      setError("当前账号没有可用工作区，请在权限管理中创建或分配工作区。");
      return;
    }
    if (projectKind === "commerce_video") {
      await submitCommerceProject(workspaceId);
      return;
    }
    await submitNarrativeProject(workspaceId);
  }

  async function submitNarrativeProject(activeWorkspaceId: string) {
    if (!narrativeForm.name.trim()) {
      setError("项目名称不能为空。");
      return;
    }
    if (!videoProductionProfileCards.some((option) => option.value === narrativeForm.videoProductionProfileKey && option.available)) {
      setError("所选视频生产方案当前不可用，请刷新后重试。");
      return;
    }
    setBusy(true);
    try {
      const project = await studioApi.createProject(session, {
        workspaceId: activeWorkspaceId,
        name: narrativeForm.name.trim(),
        description: narrativeForm.description.trim() || undefined,
        projectKind: "narrative",
        projectType: narrativeForm.projectType,
        contentType: narrativeForm.contentType,
        videoRatio: narrativeForm.videoRatio,
        artStyle: narrativeForm.artStyle,
        directorManualPromptVersionId: narrativeForm.directorManualPromptVersionId || undefined,
        visualManualPromptVersionId: narrativeForm.visualManualPromptVersionId || undefined,
        imageQuality: narrativeForm.imageQuality,
        videoProductionProfileKey: narrativeForm.videoProductionProfileKey,
        compatibilityPolicy: "strict",
        settings: projectSettingsFromForm(narrativeForm),
      });
      router.push(projectHref(project.id) as Route);
    } catch (cause) {
      setError(cause instanceof StudioApiError ? cause.message : "创建失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  async function submitCommerceProject(activeWorkspaceId: string) {
    if (!commerceForm.name.trim()) {
      setError("请填写项目名称。");
      return;
    }
    if (!clientRequestId) {
      setError("创建请求标识尚未准备完成，请稍后重试。");
      return;
    }
    setBusy(true);
    try {
      setStage("创建并配置项目");
      const project = await studioApi.createProject(session, {
        workspaceId: activeWorkspaceId,
        name: commerceForm.name.trim(),
        description: commerceForm.description.trim() || undefined,
        projectKind: "commerce_video",
        videoRatio: commerceForm.videoRatio,
        imageQuality: "standard",
        audioStrategy: "native_av",
        audioRequirement: "preferred",
        defaultTargetDurationSeconds: 6,
        defaultTargetPlatform: "other",
        defaultLanguageMode: "auto",
      }, clientRequestId);

      window.localStorage.removeItem(commerceDraftStorageKey(activeWorkspaceId));
      setStage("");
      router.push(projectHref(project.id, "commerce/materials") as Route);
    } catch (cause) {
      setError(cause instanceof StudioApiError ? cause.message : "创建失败，请稍后重试。");
    } finally {
      setBusy(false);
      setStage("");
    }
  }

  function selectProjectType(value: NarrativeProjectType | "commerce_video") {
    setSelectedProjectType(value);
    if (value !== "commerce_video") {
      setNarrativeForm((current) => ({ ...current, projectType: value }));
    }
    setError("");
  }

  function selectManual(option: ManualStyleOption) {
    if (option.kind === "director") {
      setNarrativeForm((current) => ({
        ...current,
        directorManualTemplateKey: option.templateKey,
        directorManualPromptVersionId: option.promptVersionId,
        toonflowStoryStyle: option.styleSlug ?? "",
      }));
      return;
    }
    setNarrativeForm((current) => ({
      ...current,
      artStyle: option.styleSlug ?? defaultArtStyle,
      visualManualTemplateKey: option.templateKey,
      visualManualPromptVersionId: option.promptVersionId,
      toonflowVisualStyle: option.styleSlug ?? "",
    }));
  }

  if (!canCreateProject) {
    return (
      <section className="space-y-4 rounded-lg border bg-card p-4 shadow-sm md:p-6">
        <ErrorPanel message="当前账号没有创建项目的权限，请联系组织管理员授权。" />
        <Button variant="outline" onClick={() => router.push("/projects" as Route)}>返回项目列表</Button>
      </section>
    );
  }

  return (
    <section className="space-y-6 rounded-lg border bg-card p-4 shadow-sm md:p-6">
      <ConfigSection title="项目类型">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
          {projectTypeOptions.map((option) => (
            <ProjectTypeCard
              key={option.value}
              selected={selectedProjectType === option.value}
              icon={option.icon}
              title={option.title}
              description={option.description}
              onClick={() => selectProjectType(option.value)}
            />
          ))}
        </div>
      </ConfigSection>

      {projectKind === "commerce_video" ? (
        <CommerceProjectFormView
          form={commerceForm}
          setForm={setCommerceForm}
        />
      ) : (
        <NarrativeProjectFormView
          form={narrativeForm}
          setForm={setNarrativeForm}
          videoProductionProfileCards={videoProductionProfileCards}
          videoProductionProfilesLoading={videoProductionProfilesLoading}
          manualTemplatesLoading={manualTemplatesLoading}
          directorManualOptions={directorManualOptions}
          visualManualOptions={visualManualOptions}
          onSelectManual={selectManual}
        />
      )}

      <div className="flex items-center justify-between gap-4 border-t pt-5">
        <div className="min-w-0 flex-1">
          <ErrorPanel message={error} />
          {busy && stage ? <div className="mt-2 text-sm text-muted-foreground">{stage}</div> : null}
        </div>
        <div className="ml-auto flex shrink-0 gap-2">
          <Button variant="outline" onClick={() => router.back()} disabled={busy}>取消</Button>
          <Button
            onClick={submit}
            disabled={busy}
          >
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Check className="mr-2 h-4 w-4" />}
            创建项目
          </Button>
        </div>
      </div>
    </section>
  );
}

function NarrativeProjectFormView({
  form,
  setForm,
  videoProductionProfileCards,
  videoProductionProfilesLoading,
  manualTemplatesLoading,
  directorManualOptions,
  visualManualOptions,
  onSelectManual,
}: {
  form: NewNarrativeProjectForm;
  setForm: React.Dispatch<React.SetStateAction<NewNarrativeProjectForm>>;
  videoProductionProfileCards: Array<(typeof videoProductionProfileOptions)[number] & { available: boolean; version?: number }>;
  videoProductionProfilesLoading: boolean;
  manualTemplatesLoading: boolean;
  directorManualOptions: ManualStyleOption[];
  visualManualOptions: ManualStyleOption[];
  onSelectManual: (option: ManualStyleOption) => void;
}) {
  return (
    <>
      <div className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr]">
        <div className="space-y-2">
          <Label htmlFor="name">项目名称 *</Label>
          <Input id="name" value={form.name} onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="contentType">内容类型</Label>
          <Select value={form.contentType} onValueChange={(value: NarrativeContentType) => setForm((current) => ({ ...current, contentType: value }))}>
            <SelectTrigger id="contentType"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="novel">小说改编</SelectItem>
              <SelectItem value="script">剧本创作</SelectItem>
              <SelectItem value="storyboard_first">分镜先行</SelectItem>
              <SelectItem value="original">自定义</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="space-y-2">
        <Label htmlFor="description">项目简介</Label>
        <Textarea id="description" rows={4} value={form.description} onChange={(event) => setForm((current) => ({ ...current, description: event.target.value }))} />
      </div>
      <ConfigSection title="画面比例">
        <div className="flex flex-wrap gap-3">
          {ratioOptions.map((option) => (
            <SegmentButton key={option.value} selected={form.videoRatio === option.value} icon={option.icon} label={option.label} hint={option.hint} onClick={() => setForm((current) => ({ ...current, videoRatio: option.value }))} />
          ))}
        </div>
      </ConfigSection>
      <ConfigSection title="生产方式">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          {videoProductionProfileCards.map((option) => (
            <VideoProductionProfileCard key={option.value} selected={form.videoProductionProfileKey === option.value} disabled={!option.available} icon={option.icon} title={option.title} description={option.description} version={option.version} onClick={() => option.available && setForm((current) => ({ ...current, videoProductionProfileKey: option.value }))} />
          ))}
        </div>
        {videoProductionProfilesLoading ? <div className="text-xs text-muted-foreground">正在读取可用生产方案</div> : null}
        <QualitySelector value={form.imageQuality} onChange={(value) => setForm((current) => ({ ...current, imageQuality: value }))} />
      </ConfigSection>
      <ConfigSection title="视觉手册">
        <ManualStyleSelector title="风格库" options={visualManualOptions} selectedTemplateKey={form.visualManualTemplateKey} loading={manualTemplatesLoading} layout="strip" onSelect={onSelectManual} />
      </ConfigSection>
      <ConfigSection title="导演手册">
        <ManualStyleSelector title="叙事库" options={directorManualOptions} selectedTemplateKey={form.directorManualTemplateKey} loading={manualTemplatesLoading} layout="strip" onSelect={onSelectManual} />
      </ConfigSection>
    </>
  );
}

function CommerceProjectFormView({
  form,
  setForm,
}: {
  form: CommerceProjectForm;
  setForm: React.Dispatch<React.SetStateAction<CommerceProjectForm>>;
}) {
  return (
    <>
      <div className="space-y-2">
        <Label htmlFor="commerce-name">项目名称 *</Label>
        <Input
          id="commerce-name"
          value={form.name}
          onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="commerce-description">项目简介</Label>
        <Textarea
          id="commerce-description"
          rows={4}
          value={form.description}
          onChange={(event) => setForm((current) => ({ ...current, description: event.target.value }))}
        />
      </div>

      <ConfigSection title="画面比例">
        <div className="flex flex-wrap gap-3">
          {ratioOptions
            .filter((option) => ["9:16", "16:9", "1:1"].includes(option.value))
            .map((option) => (
              <SegmentButton
                key={option.value}
                selected={form.videoRatio === option.value}
                icon={option.icon}
                label={option.label}
                hint={option.hint}
                onClick={() => setForm((current) => ({ ...current, videoRatio: option.value }))}
              />
            ))}
        </div>
      </ConfigSection>

    </>
  );
}

function ProjectTypeCard({ selected, icon: Icon, title, description, onClick }: { selected: boolean; icon: LucideIcon; title: string; description: string; onClick: () => void }) {
  return (
    <button type="button" className={cn("flex min-h-28 flex-col items-start gap-3 rounded-md border p-4 text-left transition hover:border-primary/60", selected ? "border-primary bg-primary/10 text-primary shadow-sm" : "bg-muted/25")} onClick={onClick}>
      <div className="flex w-full items-center justify-between"><Icon className="size-5" />{selected ? <Check className="size-4" /> : null}</div>
      <div><div className="text-sm font-semibold">{title}</div><div className={cn("mt-1 text-xs", selected ? "text-primary/80" : "text-muted-foreground")}>{description}</div></div>
    </button>
  );
}

function ConfigSection({ title, children }: { title: string; children: React.ReactNode }) {
  return <section className="space-y-3"><h2 className="text-sm font-semibold">{title}</h2>{children}</section>;
}

function SegmentButton({ selected, icon: Icon, label, hint, onClick }: { selected: boolean; icon: LucideIcon; label: string; hint: string; onClick: () => void }) {
  return (
    <button type="button" className={cn("flex min-w-36 items-center justify-center gap-2 rounded-full border px-5 py-3 text-sm font-semibold transition hover:border-primary/60", selected ? "border-primary bg-primary/10 text-primary" : "bg-muted/50 text-muted-foreground")} onClick={onClick}>
      <Icon className="size-4" /><span>{label}</span><span className="font-normal opacity-80">{hint}</span>
    </button>
  );
}

function QualitySelector({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3 pt-1">
      <div className="text-sm font-medium">图片质量</div>
      {qualityOptions.map((option) => <button key={option.value} type="button" className={cn("rounded-full border px-4 py-2 text-sm font-medium transition hover:border-primary/60", value === option.value ? "border-primary bg-primary/10 text-primary" : "bg-muted/40 text-muted-foreground")} onClick={() => onChange(option.value)}>{option.label}</button>)}
    </div>
  );
}

function VideoProductionProfileCard({ selected, icon: Icon, title, description, version, disabled, onClick }: { selected: boolean; icon: LucideIcon; title: string; description: string; version?: number; disabled: boolean; onClick: () => void }) {
  return (
    <button type="button" disabled={disabled} className={cn("flex min-h-24 flex-col items-start gap-2 rounded-lg border bg-muted/40 p-4 text-left transition hover:border-primary/60", selected && "border-primary bg-primary/10 text-primary shadow-sm", disabled && "cursor-not-allowed opacity-55")} onClick={onClick}>
      <div className="flex w-full items-center justify-between gap-3"><span className="flex items-center gap-2 text-sm font-semibold"><Icon className="size-4" />{title}</span>{selected ? <Check className="size-4" /> : disabled ? <span className="text-xs font-normal text-muted-foreground">暂不可用</span> : version ? <span className="text-xs font-normal text-muted-foreground">v{version}</span> : null}</div>
      <span className={cn("text-xs", selected ? "text-primary/80" : "text-muted-foreground")}>{description}</span>
    </button>
  );
}

function projectSettingsFromForm(form: Pick<NewNarrativeProjectForm, "toonflowVisualStyle" | "toonflowStoryStyle">) {
  const settings: Record<string, string> = {};
  if (form.toonflowVisualStyle) settings.toonflowVisualStyle = form.toonflowVisualStyle;
  if (form.toonflowStoryStyle) settings.toonflowStoryStyle = form.toonflowStoryStyle;
  return settings;
}

function commerceDraftStorageKey(workspaceId: string) {
  return `cineweave:commerce-project-draft:${workspaceId}`;
}

"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import { useEffect, useMemo, useRef, useState } from "react";
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
  Trash2,
  Upload,
} from "lucide-react";
import { AppShell } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ErrorPanel } from "@/components/shared/error-panel";
import { useStudioSession } from "@/lib/session";
import { studioApi, StudioApiError } from "@/lib/api-client";
import { projectHref } from "@/lib/routes";
import { useApiQuery } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { cn } from "@/lib/utils";
import type {
  CommerceLanguageMode,
  CommerceProjectLanguageOption,
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
  productName: string;
  brand: string;
  sellingPoints: string;
  scriptTitle: string;
  script: string;
  targetDurationSeconds: number;
  languageMode: CommerceLanguageMode;
  targetLanguage: string;
  videoRatio: string;
  imageQuality: string;
  audioStrategy: "native_av" | "external_audio";
  audioRequirement: "preferred" | "required" | "disabled";
  targetPlatform: string;
};

type ProductImageDraft = {
  id: string;
  file: File;
  previewUrl: string;
  role: string;
  primary: boolean;
};

type PersistedCommerceDraft = {
  clientRequestId: string;
  form: CommerceProjectForm;
  projectId?: string;
  setupSessionId?: string;
};

function NewProjectContent() {
  const router = useRouter();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const productImagesRef = useRef<ProductImageDraft[]>([]);
  const { session, ready } = useStudioSession();
  const workspaceId = session.workspaceId?.trim() ?? "";
  const [selectedProjectType, setSelectedProjectType] = useState<NarrativeProjectType | "commerce_video">("short_film");
  const projectKind: ProjectKind = selectedProjectType === "commerce_video" ? "commerce_video" : "narrative";
  const [busy, setBusy] = useState(false);
  const [stage, setStage] = useState("");
  const [error, setError] = useState("");
  const [clientRequestId, setClientRequestId] = useState("");
  const [draftProjectId, setDraftProjectId] = useState("");
  const [draftSetupSessionId, setDraftSetupSessionId] = useState("");
  const [productImages, setProductImages] = useState<ProductImageDraft[]>([]);
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
    productName: "",
    brand: "",
    sellingPoints: "",
    scriptTitle: "脚本 1",
    script: "",
    targetDurationSeconds: 30,
    languageMode: "auto",
    targetLanguage: "",
    videoRatio: "9:16",
    imageQuality: "standard",
    audioStrategy: "native_av",
    audioRequirement: "preferred",
    targetPlatform: "通用短视频平台",
  });

  const { data: manualTemplates = [], isLoading: manualTemplatesLoading } = useApiQuery({
    key: qk.projectManualTemplates(),
    queryFn: (activeSession) => studioApi.listProjectManualTemplates(activeSession).then((response) => response.items),
    enabled: projectKind === "narrative",
  });
  const { data: videoProductionProfileVersions = [], isLoading: videoProductionProfilesLoading } = useApiQuery({
    key: qk.videoProductionProfiles(),
    queryFn: (activeSession) => studioApi.listVideoProductionProfiles(activeSession).then((response) => response.items),
    enabled: projectKind === "narrative",
  });
  const { data: commerceOptions, isLoading: commerceOptionsLoading } = useApiQuery({
    key: qk.commerceProjectOptions(workspaceId),
    queryFn: (activeSession) => studioApi.getCommerceProjectOptions(activeSession, workspaceId),
    enabled: projectKind === "commerce_video" && Boolean(workspaceId),
    staleTime: 30_000,
  });

  const directorManualOptions = useMemo(() => buildManualStyleOptions(manualTemplates, "director"), [manualTemplates]);
  const visualManualOptions = useMemo(() => buildManualStyleOptions(manualTemplates, "visual"), [manualTemplates]);
  const videoProductionProfileCards = useMemo(() => videoProductionProfileOptions.map((option) => {
    const profile = videoProductionProfileVersions
      .filter((candidate) => candidate.profileKey === option.value)
      .sort((left, right) => right.version - left.version)[0];
    return { ...option, available: profile?.available === true, description: profile?.description || option.description, version: profile?.version };
  }), [videoProductionProfileVersions]);
  const commerceLanguages = useMemo(
    () => (commerceOptions?.languages ?? []).filter((language) => commerceLanguageExecutable(language, commerceForm.audioStrategy)),
    [commerceForm.audioStrategy, commerceOptions?.languages],
  );

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
            setDraftProjectId(saved.projectId ?? "");
            setDraftSetupSessionId(saved.setupSessionId ?? "");
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
      projectId: draftProjectId || undefined,
      setupSessionId: draftSetupSessionId || undefined,
    };
    window.localStorage.setItem(commerceDraftStorageKey(workspaceId), JSON.stringify(saved));
  }, [clientRequestId, commerceForm, draftProjectId, draftSetupSessionId, workspaceId]);

  useEffect(() => {
    productImagesRef.current = productImages;
  }, [productImages]);

  useEffect(() => () => {
    for (const image of productImagesRef.current) URL.revokeObjectURL(image.previewUrl);
  }, []);

  async function submit() {
    setError("");
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
    if (!commerceForm.name.trim() || !commerceForm.productName.trim()) {
      setError("请填写项目名称和产品名称。");
      return;
    }
    if (!commerceForm.script.trim()) {
      setError("请填写第一个广告脚本。");
      return;
    }
    if (productImages.length === 0 && !draftProjectId) {
      setError("请至少上传一张产品图片。");
      return;
    }
    if (!clientRequestId) {
      setError("创建请求标识尚未准备完成，请稍后重试。");
      return;
    }
    if (!commerceOptions?.workflowTemplateVersionId) {
      setError("带货视频工作流模板尚未发布。");
      return;
    }
    if (commerceForm.languageMode === "explicit" && !commerceForm.targetLanguage) {
      setError("请选择目标语言。");
      return;
    }
    if (commerceForm.languageMode === "explicit" && !commerceLanguages.some((item) => item.locale === commerceForm.targetLanguage)) {
      setError("当前模型链路无法执行所选语言。");
      return;
    }

    setBusy(true);
    try {
      setStage("创建项目草稿");
      let projectId = draftProjectId;
      let setupSessionId = draftSetupSessionId;
      if (!projectId) {
        const project = await studioApi.createProject(session, {
          workspaceId: activeWorkspaceId,
          name: commerceForm.name.trim(),
          description: commerceForm.description.trim() || undefined,
          projectKind: "commerce_video",
          videoRatio: commerceForm.videoRatio,
          imageQuality: commerceForm.imageQuality,
          audioStrategy: commerceForm.audioStrategy,
          audioRequirement: commerceForm.audioRequirement,
          defaultTargetDurationSeconds: commerceForm.targetDurationSeconds as 15 | 30 | 60,
          defaultTargetPlatform: commerceForm.targetPlatform,
          defaultLanguageMode: commerceForm.languageMode,
          defaultTargetLanguage: commerceForm.languageMode === "explicit" ? commerceForm.targetLanguage : undefined,
        }, clientRequestId);
        projectId = project.id;
        setupSessionId = project.setupSessionId ?? "";
        setDraftProjectId(projectId);
        setDraftSetupSessionId(setupSessionId);
        persistCommerceDraft(activeWorkspaceId, { clientRequestId, form: commerceForm, projectId, setupSessionId });
      }

      setStage("保存商品资料");
      let product;
      try {
        product = await studioApi.getCommerceProduct(session, projectId);
      } catch (cause) {
        if (!(cause instanceof StudioApiError) || cause.code !== "COMMERCE_PRODUCT_REQUIRED") throw cause;
      }
      const productMutation = await studioApi.createCommerceProductVersion(session, projectId, {
        expectedRevision: product?.revision ?? 0,
        name: commerceForm.productName.trim(),
        brand: commerceForm.brand.trim(),
        sellingPoints: splitLines(commerceForm.sellingPoints),
        immutableFeatures: {},
        prohibitedClaims: [],
        metadata: {},
      });
      product = productMutation.product;

      if (productImages.length > 0) {
        setStage(`上传产品图片 0/${productImages.length}`);
        for (const [index, image] of productImages.entries()) {
          const upload = await studioApi.createCommerceProductReferenceUpload(session, projectId, {
            setupSessionId: setupSessionId || undefined,
            fileName: image.file.name,
            mimeType: image.file.type,
          }, `${clientRequestId}:product-image:${image.file.name}:${image.file.size}:${image.file.lastModified}`);
          await studioApi.uploadCommerceProductReferenceFile(upload, image.file);
          await studioApi.completeCommerceProductReferenceUpload(session, projectId, {
            uploadId: upload.uploadId,
            referenceRole: image.role,
            setPrimary: image.primary,
          });
          setStage(`上传产品图片 ${index + 1}/${productImages.length}`);
        }
      }

      setStage("保存首个广告脚本");
      const units = await studioApi.listCommerceScriptUnits(session, projectId, { limit: 1 });
      if (units.items.length === 0) {
        await studioApi.createCommerceScriptUnit(session, projectId, {
          expectedScriptUnitsRevision: product.scriptUnitsRevision,
          title: commerceForm.scriptTitle.trim() || "脚本 1",
          content: commerceForm.script.trim(),
          languageMode: commerceForm.languageMode,
          explicitTargetLanguage: commerceForm.languageMode === "explicit" ? commerceForm.targetLanguage : undefined,
          targetDurationSeconds: commerceForm.targetDurationSeconds,
          targetPlatform: commerceForm.targetPlatform,
        });
      }

	  if (!setupSessionId) {
		throw new Error("项目创建会话缺失，请重新创建带货视频项目。");
	  }
	  setStage("启动项目准备流程");
	  const setupSession = await studioApi.getCommerceSetupSession(session, projectId, setupSessionId);
	  await studioApi.completeCommerceSetupSession(
		session,
		projectId,
		setupSessionId,
		{ expectedRevision: setupSession.revision },
		`${clientRequestId}:setup-complete`,
	  );

      window.localStorage.removeItem(commerceDraftStorageKey(activeWorkspaceId));
      setStage("");
      router.push(projectHref(projectId, "commerce/materials") as Route);
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

  function addProductImages(files: FileList | null) {
    if (!files) return;
    const accepted = Array.from(files).filter((file) => ["image/jpeg", "image/png", "image/webp"].includes(file.type));
    setProductImages((current) => [
      ...current,
      ...accepted.map((file, offset) => ({
        id: crypto.randomUUID(),
        file,
        previewUrl: URL.createObjectURL(file),
        role: "other",
        primary: current.length === 0 && offset === 0,
      })),
    ]);
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  function removeProductImage(id: string) {
    setProductImages((current) => {
      const removed = current.find((item) => item.id === id);
      if (removed) URL.revokeObjectURL(removed.previewUrl);
      const next = current.filter((item) => item.id !== id);
      if (removed?.primary && next.length > 0) next[0] = { ...next[0], primary: true };
      return next;
    });
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
          images={productImages}
          languages={commerceLanguages}
          optionsLoading={commerceOptionsLoading}
          blockers={commerceOptions?.blockers ?? []}
          fileInputRef={fileInputRef}
          onFiles={addProductImages}
          onRemoveImage={removeProductImage}
          onSetPrimary={(id) => setProductImages((current) => current.map((item) => ({ ...item, primary: item.id === id })))}
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
          <Button onClick={submit} disabled={busy || (projectKind === "commerce_video" && commerceOptionsLoading)}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Check className="mr-2 h-4 w-4" />}
            {projectKind === "commerce_video" ? "创建并准备分镜方案" : "创建项目"}
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
  images,
  languages,
  optionsLoading,
  blockers,
  fileInputRef,
  onFiles,
  onRemoveImage,
  onSetPrimary,
}: {
  form: CommerceProjectForm;
  setForm: React.Dispatch<React.SetStateAction<CommerceProjectForm>>;
  images: ProductImageDraft[];
  languages: CommerceProjectLanguageOption[];
  optionsLoading: boolean;
  blockers: string[];
  fileInputRef: React.RefObject<HTMLInputElement | null>;
  onFiles: (files: FileList | null) => void;
  onRemoveImage: (id: string) => void;
  onSetPrimary: (id: string) => void;
}) {
  return (
    <>
      <div className="grid gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="commerce-name">项目名称 *</Label>
          <Input id="commerce-name" value={form.name} onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="product-name">产品名称 *</Label>
          <Input id="product-name" value={form.productName} onChange={(event) => setForm((current) => ({
            ...current,
            productName: event.target.value,
            name: current.name || `${event.target.value}带货视频`,
          }))} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="brand">品牌</Label>
          <Input id="brand" value={form.brand} onChange={(event) => setForm((current) => ({ ...current, brand: event.target.value }))} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="selling-points">核心卖点</Label>
          <Input id="selling-points" value={form.sellingPoints} onChange={(event) => setForm((current) => ({ ...current, sellingPoints: event.target.value }))} placeholder="多个卖点用换行分隔" />
        </div>
      </div>

      <ConfigSection title="产品图片">
        <input ref={fileInputRef} type="file" className="hidden" accept="image/jpeg,image/png,image/webp" multiple onChange={(event) => onFiles(event.target.files)} />
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6">
          {images.map((image) => (
            <div key={image.id} className={cn("group relative aspect-square overflow-hidden rounded-md border bg-muted", image.primary && "border-primary ring-1 ring-primary")}>
              <Image src={image.previewUrl} alt={image.file.name} fill unoptimized className="object-cover" />
              <div className="absolute inset-x-0 bottom-0 flex items-center justify-between gap-1 bg-black/65 p-1.5 text-white">
                <button type="button" className="truncate px-1 text-xs" onClick={() => onSetPrimary(image.id)}>{image.primary ? "主图" : "设为主图"}</button>
                <button type="button" aria-label="移除图片" className="rounded p-1 hover:bg-white/20" onClick={() => onRemoveImage(image.id)}><Trash2 className="size-3.5" /></button>
              </div>
            </div>
          ))}
          <button type="button" className="flex aspect-square flex-col items-center justify-center gap-2 rounded-md border border-dashed text-sm text-muted-foreground transition hover:border-primary hover:text-primary" onClick={() => fileInputRef.current?.click()}>
            <Upload className="size-5" />
            添加图片
          </button>
        </div>
      </ConfigSection>

      <ConfigSection title="广告脚本">
        <div className="grid gap-3 md:grid-cols-[240px_1fr]">
          <div className="space-y-2">
            <Label htmlFor="script-title">脚本标题</Label>
            <Input id="script-title" value={form.scriptTitle} onChange={(event) => setForm((current) => ({ ...current, scriptTitle: event.target.value }))} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="target-platform">目标平台</Label>
            <Input id="target-platform" value={form.targetPlatform} onChange={(event) => setForm((current) => ({ ...current, targetPlatform: event.target.value }))} />
          </div>
        </div>
        <Textarea id="commerce-script" rows={10} value={form.script} onChange={(event) => setForm((current) => ({ ...current, script: event.target.value }))} placeholder="输入完整广告脚本，产品事实、旁白、屏幕文字和行动引导将分别结构化处理" />
        <div className="text-xs text-muted-foreground">{countSpeechUnits(form.script, form.targetLanguage)} 个计时单位，预计旁白 {estimateSpeechSeconds(form.script, form.targetLanguage)} 秒</div>
      </ConfigSection>

      <ConfigSection title="快速设置">
        <div className="grid gap-4 lg:grid-cols-3">
          <div className="space-y-2">
            <Label>目标时长</Label>
            <div className="flex gap-2">
              {[15, 30, 60].map((duration) => (
                <button key={duration} type="button" className={cn("h-10 flex-1 rounded-md border text-sm font-medium", form.targetDurationSeconds === duration ? "border-primary bg-primary/10 text-primary" : "bg-muted/30")} onClick={() => setForm((current) => ({ ...current, targetDurationSeconds: duration }))}>{duration} 秒</button>
              ))}
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="commerce-language-mode">视频语言</Label>
            <Select value={form.languageMode === "auto" ? "auto" : form.targetLanguage} onValueChange={(value) => setForm((current) => value === "auto" ? { ...current, languageMode: "auto", targetLanguage: "" } : { ...current, languageMode: "explicit", targetLanguage: value })}>
              <SelectTrigger id="commerce-language-mode"><SelectValue placeholder={optionsLoading ? "正在读取语言" : "选择语言"} /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">自动判断</SelectItem>
                {languages.map((language) => <SelectItem key={language.locale} value={language.locale}>{language.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>画面比例</Label>
            <div className="flex gap-2">
              {["9:16", "16:9", "1:1"].map((ratio) => (
                <button key={ratio} type="button" className={cn("h-10 flex-1 rounded-md border text-sm font-medium", form.videoRatio === ratio ? "border-primary bg-primary/10 text-primary" : "bg-muted/30")} onClick={() => setForm((current) => ({ ...current, videoRatio: ratio }))}>{ratio}</button>
              ))}
            </div>
          </div>
        </div>
        <details className="rounded-md border bg-muted/20 p-3">
          <summary className="cursor-pointer text-sm font-medium">高级设置</summary>
          <div className="mt-4 grid gap-4 md:grid-cols-3">
            <div className="space-y-2">
              <Label htmlFor="commerce-quality">图片质量</Label>
              <Select value={form.imageQuality} onValueChange={(value) => setForm((current) => ({ ...current, imageQuality: value }))}>
                <SelectTrigger id="commerce-quality"><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="standard">标准</SelectItem><SelectItem value="hd">高清</SelectItem></SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="commerce-audio">音频方式</Label>
              <Select value={form.audioStrategy} onValueChange={(value: "native_av" | "external_audio") => setForm((current) => ({ ...current, audioStrategy: value }))}>
                <SelectTrigger id="commerce-audio"><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="native_av">视频模型原生音频</SelectItem><SelectItem value="external_audio">独立音轨</SelectItem></SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="commerce-audio-requirement">原生音频要求</Label>
              <Select value={form.audioRequirement} onValueChange={(value: "preferred" | "required" | "disabled") => setForm((current) => ({ ...current, audioRequirement: value }))}>
                <SelectTrigger id="commerce-audio-requirement"><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="preferred">优先使用</SelectItem><SelectItem value="required">必须支持</SelectItem><SelectItem value="disabled">不使用</SelectItem></SelectContent>
              </Select>
            </div>
          </div>
        </details>
        {blockers.length > 0 ? (
          <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-100">
            {blockers.join("；")}
          </div>
        ) : null}
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

function commerceLanguageExecutable(language: CommerceProjectLanguageOption, audioStrategy: CommerceProjectForm["audioStrategy"]) {
  return language.textAvailable && language.imagePromptAvailable && language.videoPromptAvailable && (audioStrategy !== "native_av" || language.nativeAudioAvailable);
}

function commerceDraftStorageKey(workspaceId: string) {
  return `cineweave:commerce-project-draft:${workspaceId}`;
}

function persistCommerceDraft(workspaceId: string, draft: PersistedCommerceDraft) {
  window.localStorage.setItem(commerceDraftStorageKey(workspaceId), JSON.stringify(draft));
}

function splitLines(value: string) {
  return value.split(/[\n；;]+/).map((item) => item.trim()).filter(Boolean);
}

function countSpeechUnits(value: string, locale: string) {
  if (!value.trim()) return 0;
  if (!locale || locale.startsWith("zh")) return Array.from(value.replace(/\s|[\p{P}\p{S}]/gu, "")).length;
  return value.trim().split(/\s+/).filter(Boolean).length;
}

function estimateSpeechSeconds(value: string, locale: string) {
  const units = countSpeechUnits(value, locale);
  return Math.ceil(units / ((!locale || locale.startsWith("zh")) ? 3.5 : 2.5));
}

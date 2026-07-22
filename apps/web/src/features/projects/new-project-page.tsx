"use client";

import { useRouter } from "next/navigation";
import type { Route } from "next";
import { useMemo, useState } from "react";
import type { LucideIcon } from "lucide-react";
import { Check, Clapperboard, ImageIcon, Layers, Loader2, Monitor, RectangleHorizontal, RectangleVertical, Smartphone, Square } from "lucide-react";
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
import type { VideoProductionProfileKey } from "@/lib/types";
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
    <AppShell active="projects" title="新建项目" description="一次完成项目信息、画面参数、生产方式和风格手册。">
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

function NewProjectContent() {
  const router = useRouter();
  const { session, ready } = useStudioSession();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState({
    name: "",
    description: "",
    projectType: "短片",
    contentType: "剧本创作",
    videoRatio: "9:16",
    imageQuality: "standard",
    videoProductionProfileKey: "single_frame_i2v" as VideoProductionProfileKey,
    artStyle: defaultArtStyle,
    directorManualTemplateKey: DEFAULT_DIRECTOR_MANUAL_KEY,
    directorManualPromptVersionId: "",
    visualManualTemplateKey: DEFAULT_VISUAL_MANUAL_KEY,
    visualManualPromptVersionId: "",
    toonflowVisualStyle: "",
    toonflowStoryStyle: "",
  });
  const { data: manualTemplates = [], isLoading: manualTemplatesLoading } = useApiQuery({
    key: qk.projectManualTemplates(),
    queryFn: (activeSession) => studioApi.listProjectManualTemplates(activeSession).then((response) => response.items),
  });
  const { data: videoProductionProfileVersions = [], isLoading: videoProductionProfilesLoading } = useApiQuery({
    key: qk.videoProductionProfiles(),
    queryFn: (activeSession) => studioApi.listVideoProductionProfiles(activeSession).then((response) => response.items),
  });
  const directorManualOptions = useMemo(() => buildManualStyleOptions(manualTemplates, "director"), [manualTemplates]);
  const visualManualOptions = useMemo(() => buildManualStyleOptions(manualTemplates, "visual"), [manualTemplates]);
  const videoProductionProfileCards = useMemo(() => videoProductionProfileOptions.map((option) => {
    const profile = videoProductionProfileVersions
      .filter((candidate) => candidate.profileKey === option.value)
      .sort((left, right) => right.version - left.version)[0];
    return {
      ...option,
      available: profile?.available === true,
      description: profile?.description || option.description,
      version: profile?.version,
    };
  }), [videoProductionProfileVersions]);

  async function submit() {
    setError("");
    const workspaceId = session.workspaceId?.trim() ?? "";
    if (!ready || !workspaceId) {
      setError("当前账号没有可用工作区，请在权限管理中创建或分配工作区。");
      return;
    }
    if (!form.name.trim()) {
      setError("项目名称不能为空。");
      return;
    }
    if (!videoProductionProfileCards.some((option) => option.value === form.videoProductionProfileKey && option.available)) {
      setError("所选视频生产方案当前不可用，请刷新后重试。");
      return;
    }
    setBusy(true);
    try {
      const project = await studioApi.createProject(session, {
        workspaceId,
        name: form.name,
        description: form.description || undefined,
        projectType: form.projectType,
        contentType: form.contentType,
        videoRatio: form.videoRatio,
        artStyle: form.artStyle,
        directorManualPromptVersionId: form.directorManualPromptVersionId || undefined,
        visualManualPromptVersionId: form.visualManualPromptVersionId || undefined,
        imageQuality: form.imageQuality,
        videoProductionProfileKey: form.videoProductionProfileKey,
        compatibilityPolicy: "strict",
        settings: projectSettingsFromForm(form),
      });
      router.push(projectHref(project.id) as Route);
    } catch (cause) {
      setError(cause instanceof StudioApiError ? cause.message : "创建失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  function selectManual(option: ManualStyleOption) {
    if (option.kind === "director") {
      setForm((current) => ({
        ...current,
        directorManualTemplateKey: option.templateKey,
        directorManualPromptVersionId: option.promptVersionId,
        toonflowStoryStyle: option.styleSlug ?? "",
      }));
      return;
    }
    setForm((current) => ({
      ...current,
      artStyle: option.styleSlug ?? defaultArtStyle,
      visualManualTemplateKey: option.templateKey,
      visualManualPromptVersionId: option.promptVersionId,
      toonflowVisualStyle: option.styleSlug ?? "",
    }));
  }

  return (
    <section className="space-y-6 rounded-lg border bg-card p-4 shadow-sm md:p-6">
      <div className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr]">
        <div className="space-y-2">
          <Label htmlFor="name">项目名称 *</Label>
          <Input id="name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="projectType">项目类型</Label>
            <Select value={form.projectType} onValueChange={(v) => setForm({ ...form, projectType: v })}>
              <SelectTrigger id="projectType">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="短片">短片</SelectItem>
                <SelectItem value="漫剧">漫剧</SelectItem>
                <SelectItem value="广告">广告</SelectItem>
                <SelectItem value="角色 IP">角色 IP</SelectItem>
                <SelectItem value="其他">其他</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="contentType">内容类型</Label>
            <Select value={form.contentType} onValueChange={(v) => setForm({ ...form, contentType: v })}>
              <SelectTrigger id="contentType">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="小说改编">小说改编</SelectItem>
                <SelectItem value="剧本创作">剧本创作</SelectItem>
                <SelectItem value="分镜先行">分镜先行</SelectItem>
                <SelectItem value="自定义">自定义</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>

      <div className="space-y-2">
        <Label htmlFor="description">项目简介</Label>
        <Textarea
          id="description"
          rows={4}
          value={form.description}
          onChange={(e) => setForm({ ...form, description: e.target.value })}
        />
      </div>

      <ConfigSection title="画面比例">
        <div className="flex flex-wrap gap-3">
          {ratioOptions.map((option) => (
            <SegmentButton
              key={option.value}
              selected={form.videoRatio === option.value}
              icon={option.icon}
              label={option.label}
              hint={option.hint}
              onClick={() => setForm({ ...form, videoRatio: option.value })}
            />
          ))}
        </div>
      </ConfigSection>

      <ConfigSection title="生产方式">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          {videoProductionProfileCards.map((option) => (
            <VideoProductionProfileCard
              key={option.value}
              selected={form.videoProductionProfileKey === option.value}
              disabled={!option.available}
              icon={option.icon}
              title={option.title}
              description={option.description}
              version={option.version}
              onClick={() => option.available && setForm({ ...form, videoProductionProfileKey: option.value })}
            />
          ))}
        </div>
        {videoProductionProfilesLoading ? <div className="text-xs text-muted-foreground">正在读取可用生产方案</div> : null}
        <div className="flex flex-wrap items-center gap-3 pt-1">
          <div className="text-sm font-medium">图片质量</div>
          {qualityOptions.map((option) => (
            <button
              key={option.value}
              type="button"
              className={cn(
                "rounded-full border px-4 py-2 text-sm font-medium transition hover:border-primary/60",
                form.imageQuality === option.value ? "border-primary bg-primary/10 text-primary" : "bg-muted/40 text-muted-foreground",
              )}
              onClick={() => setForm({ ...form, imageQuality: option.value })}
            >
              {option.label}
            </button>
          ))}
        </div>
      </ConfigSection>

      <ConfigSection title="视觉手册">
        <ManualStyleSelector
          title="风格库"
          options={visualManualOptions}
          selectedTemplateKey={form.visualManualTemplateKey}
          loading={manualTemplatesLoading}
          layout="strip"
          onSelect={selectManual}
        />
      </ConfigSection>

      <ConfigSection title="导演手册">
        <ManualStyleSelector
          title="叙事库"
          options={directorManualOptions}
          selectedTemplateKey={form.directorManualTemplateKey}
          loading={manualTemplatesLoading}
          layout="strip"
          onSelect={selectManual}
        />
      </ConfigSection>

      <div className="flex items-center justify-between gap-4 border-t pt-5">
        <ErrorPanel message={error} />
        <div className="ml-auto flex gap-2">
          <Button variant="outline" onClick={() => router.back()}>
            取消
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Check className="mr-2 h-4 w-4" />}
            创建项目
          </Button>
        </div>
      </div>
    </section>
  );
}

function ConfigSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-semibold">{title}</h2>
      {children}
    </section>
  );
}

function SegmentButton({
  selected,
  icon: Icon,
  label,
  hint,
  onClick,
}: {
  selected: boolean;
  icon: LucideIcon;
  label: string;
  hint: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={cn(
        "flex min-w-36 items-center justify-center gap-2 rounded-full border px-5 py-3 text-sm font-semibold transition hover:border-primary/60",
        selected ? "border-primary bg-primary/10 text-primary" : "bg-muted/50 text-muted-foreground",
      )}
      onClick={onClick}
    >
      <Icon className="size-4" />
      <span>{label}</span>
      <span className="font-normal opacity-80">{hint}</span>
    </button>
  );
}

function VideoProductionProfileCard({
  selected,
  icon: Icon,
  title,
  description,
  version,
  disabled,
  onClick,
}: {
  selected: boolean;
  icon: LucideIcon;
  title: string;
  description: string;
  version?: number;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      className={cn(
        "flex min-h-24 flex-col items-start gap-2 rounded-lg border bg-muted/40 p-4 text-left transition hover:border-primary/60",
        selected && "border-primary bg-primary/10 text-primary shadow-sm",
        disabled && "cursor-not-allowed opacity-55",
      )}
      onClick={onClick}
    >
      <div className="flex w-full items-center justify-between gap-3">
        <span className="flex items-center gap-2 text-sm font-semibold">
          <Icon className="size-4" />
          {title}
        </span>
        {selected ? <Check className="size-4" /> : disabled ? <span className="text-xs font-normal text-muted-foreground">暂不可用</span> : version ? <span className="text-xs font-normal text-muted-foreground">v{version}</span> : null}
      </div>
      <span className={cn("text-xs", selected ? "text-primary/80" : "text-muted-foreground")}>{description}</span>
    </button>
  );
}

function projectSettingsFromForm(form: {
  toonflowVisualStyle: string;
  toonflowStoryStyle: string;
}) {
  const settings: Record<string, string> = {};
  if (form.toonflowVisualStyle) settings.toonflowVisualStyle = form.toonflowVisualStyle;
  if (form.toonflowStoryStyle) settings.toonflowStoryStyle = form.toonflowStoryStyle;
  return settings;
}

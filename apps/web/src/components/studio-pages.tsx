"use client";

import { AppShell, SectionTitle, Surface } from "@/components/app-shell";
import { EmptyState } from "@/components/empty-state";
import { ErrorPanel } from "@/components/error-panel";
import { MediaPreview } from "@/components/media-preview";
import { StatusBadge } from "@/components/status-badge";
import { studioApi } from "@/lib/api-client";
import { cn } from "@/lib/cn";
import { projectHref, workflowLabel } from "@/lib/routes";
import { useSessionDetails } from "@/lib/session-details";
import { useStudioSession } from "@/lib/session";
import type {
  AgentMessage,
  AgentSession,
  Artifact,
  CanonicalAsset,
  JsonRecord,
  JsonValue,
  ModelProfile,
  Organization,
  Permission,
  Project,
  ProjectSource,
  ProductionStatus,
  PromptTemplate,
  ProviderAccount,
  Role,
  Script,
  ScriptVersion,
  ShotAssetRequirement,
  StoryboardShot,
  Team,
  WorkflowNodeRun,
  WorkflowRun,
  Workspace,
} from "@/lib/types";
import {
  ArrowRight,
  Check,
  Clapperboard,
  Copy,
  Filter,
  ImageIcon,
  Loader2,
  MessageSquareText,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Save,
  Search,
  Send,
  Sparkles,
  Video,
  X,
} from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

type QueryState<TData> = {
  data: TData;
  loading: boolean;
  error: string;
  reload: () => void;
};

export function DashboardPage() {
  return (
    <AppShell active="dashboard" title="总览" description="查看项目进度，继续上次未完成的创作，或新建一个项目。">
      <DashboardContent />
    </AppShell>
  );
}

function DashboardContent() {
  const projects = useStudioQuery<Project[]>([], "dashboard:projects", async (session) => (await studioApi.listProjects(session)).items);
  const workflows = useStudioQuery<WorkflowRun[]>([], "dashboard:workflows", async (session) => (await studioApi.listWorkflowRuns(session)).items);
  const recentProjects = projects.data.slice(0, 5);
  const runningCount = workflows.data.filter((item) => ["queued", "running"].includes(item.status)).length;
  const completedCount = workflows.data.filter((item) => item.status === "succeeded").length;

  return (
    <SessionGate>
      <div className="grid gap-5">
        <Surface className="p-5">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <h2 className="text-3xl font-semibold text-slate-950">继续你的创作</h2>
              <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-600">查看项目进度，继续上次未完成的内容，或新建一个项目。</p>
            </div>
            <Link className="studio-button studio-button-primary" href={"/projects/new" as Route}>
              <Plus size={16} />
              新建项目
            </Link>
          </div>
        </Surface>

        <div className="grid gap-3 md:grid-cols-4">
          <SummaryTile label="全部项目" value={projects.data.length} detail="当前工作区可见项目" />
          <SummaryTile label="进行中" value={runningCount} detail="排队或运行中的工作流" />
          <SummaryTile label="已完成" value={completedCount} detail="已成功结束的工作流" />
          <SummaryTile label="最近更新" value={recentProjects[0] ? formatTime(recentProjects[0].updatedAt) : "暂无"} detail="按项目更新时间排序" />
        </div>

        <Surface>
          <SectionTitle title="项目进度" description="最近的项目会显示当前阶段、比例、画风和继续入口。" />
          <QueryBody state={projects}>
            {recentProjects.length ? (
              <div className="grid gap-3 p-4 lg:grid-cols-2">
                {recentProjects.map((project) => (
                  <ProjectCard key={project.id} project={project} />
                ))}
              </div>
            ) : (
              <EmptyState title="还没有项目" description="新建一个项目，设置视频比例、画风和内容类型，然后导入原文或剧本。" />
            )}
          </QueryBody>
        </Surface>

        <Surface>
          <SectionTitle title="最近更新" description="最近工作流、资产和成片更新会在这里汇总。" />
          <QueryBody state={workflows}>
            {workflows.data.length ? (
              <div className="divide-y divide-slate-200">
                {workflows.data.slice(0, 6).map((run) => (
                  <div className="grid gap-3 px-4 py-3 md:grid-cols-[1fr_auto_auto]" key={run.id}>
                    <div>
                      <p className="text-sm font-medium text-slate-900">{workflowLabel(stringFrom(run.input.workflowType) || "工作流")}</p>
                      <p className="mt-1 text-xs text-slate-500">{run.temporalWorkflowId}</p>
                    </div>
                    <StatusBadge status={run.status} />
                    <span className="text-xs text-slate-500">{formatTime(run.createdAt)}</span>
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState title="暂无更新" description="当你启动脚本、资产、分镜或视频生产工作流后，最近更新会出现在这里。" />
            )}
          </QueryBody>
        </Surface>
      </div>
    </SessionGate>
  );
}

export function ProjectsPage() {
  return (
    <AppShell active="projects" title="项目" description="只展示项目卡片；工作流、镜头和媒体资产保留在项目内部。">
      <ProjectsContent />
    </AppShell>
  );
}

function ProjectsContent() {
  const projects = useStudioQuery<Project[]>([], "projects:list", async (session) => (await studioApi.listProjects(session)).items);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("all");
  const filtered = projects.data.filter((project) => {
    const text = `${project.name} ${project.description ?? ""} ${project.projectType ?? ""} ${project.contentType ?? ""}`.toLowerCase();
    const matchesText = text.includes(query.trim().toLowerCase());
    const matchesStatus = status === "all" || (project.status ?? "active") === status;
    return matchesText && matchesStatus;
  });

  return (
    <SessionGate>
      <Surface className="mb-5 p-4">
        <div className="grid gap-3 lg:grid-cols-[1fr_180px_auto]">
          <label className="relative">
            <Search className="pointer-events-none absolute left-3 top-3 text-slate-500" size={15} />
            <input className="studio-input w-full pl-9" placeholder="搜索项目名称、简介或类型" value={query} onChange={(event) => setQuery(event.target.value)} />
          </label>
          <label className="relative">
            <Filter className="pointer-events-none absolute left-3 top-3 text-slate-500" size={15} />
            <select className="studio-input w-full pl-9" value={status} onChange={(event) => setStatus(event.target.value)}>
              <option value="all">全部状态</option>
              <option value="active">进行中</option>
              <option value="draft">草稿</option>
              <option value="archived">已归档</option>
            </select>
          </label>
          <Link className="studio-button studio-button-primary" href={"/projects/new" as Route}>
            <Plus size={16} />
            新建项目
          </Link>
        </div>
      </Surface>
      <QueryBody state={projects}>
        {filtered.length ? (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {filtered.map((project) => (
              <ProjectCard key={project.id} project={project} />
            ))}
          </div>
        ) : (
          <EmptyState title="没有匹配项目" description="调整搜索条件，或新建一个脚本驱动项目。" />
        )}
      </QueryBody>
    </SessionGate>
  );
}

export function NewProjectPage() {
  return (
    <AppShell active="projects" title="新建项目" description="四步完成项目设定、视频参数、风格手册和内容导入。">
      <NewProjectContent />
    </AppShell>
  );
}

function NewProjectContent() {
  const router = useRouter();
  const { session, ready } = useStudioSession();
  const [step, setStep] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState({
    name: "",
    description: "",
    projectType: "短片",
    contentType: "剧本创作",
    videoRatio: "16:9",
    imageQuality: "standard",
    productionMode: "silent_video",
    artStyle: "写实电影感",
    directorManual: "",
    visualManual: "",
    sourceMode: "none",
    sourceTitle: "",
    sourceContent: "",
    sourceFormat: "plain_text",
  });
  const steps = ["基础信息", "视频设定", "风格设定", "内容导入"];

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
    setBusy(true);
    try {
      const project = await studioApi.createProject(session, compactRecord({
        workspaceId,
        name: form.name,
        description: nullable(form.description),
        projectType: form.projectType,
        contentType: form.contentType,
        videoRatio: form.videoRatio,
        artStyle: form.artStyle,
        directorManual: form.directorManual,
        visualManual: form.visualManual,
        imageQuality: form.imageQuality,
        productionMode: form.productionMode,
      }));
      const hasSource = form.sourceMode !== "none" && form.sourceTitle.trim() && form.sourceContent.trim();
      if (hasSource) {
        await studioApi.createSource(session, project.id, compactRecord({
          sourceType: form.sourceMode,
          title: form.sourceTitle,
          content: form.sourceContent,
          contentFormat: form.sourceFormat,
        }));
      }
      router.push(projectHref(project.id, hasSource ? "sources" : "") as Route);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <SessionGate>
      <Surface>
        <div className="grid gap-4 border-b border-slate-200 p-4 md:grid-cols-4">
          {steps.map((label, index) => (
            <button
              className={cn("flex h-10 items-center gap-2 rounded-md px-3 text-sm", index === step ? "bg-blue-600 text-slate-950" : "bg-slate-50 text-slate-600 hover:text-slate-900")}
              key={label}
              onClick={() => setStep(index)}
              type="button"
            >
              <span className="grid h-5 w-5 place-items-center rounded bg-slate-200 text-xs">{index + 1}</span>
              {label}
            </button>
          ))}
        </div>
        <div className="p-5">
          {step === 0 ? (
            <div className="grid gap-4 md:grid-cols-2">
              <TextInput label="项目名称" value={form.name} onChange={(name) => setForm({ ...form, name })} />
              <SelectInput label="项目类型" value={form.projectType} values={["短片", "漫剧", "广告", "角色 IP", "其他"]} onChange={(projectType) => setForm({ ...form, projectType })} />
              <SelectInput label="内容类型" value={form.contentType} values={["小说改编", "剧本创作", "分镜先行", "自定义"]} onChange={(contentType) => setForm({ ...form, contentType })} />
              <TextAreaInput className="md:col-span-2" label="项目简介" value={form.description} onChange={(description) => setForm({ ...form, description })} />
            </div>
          ) : null}
          {step === 1 ? (
            <div className="grid gap-4 md:grid-cols-3">
              <SelectInput label="视频比例" value={form.videoRatio} values={["16:9", "9:16", "1:1", "4:3"]} onChange={(videoRatio) => setForm({ ...form, videoRatio })} />
              <SelectInput label="图片质量" value={form.imageQuality} values={["standard", "hd"]} labels={{ standard: "标准", hd: "高清" }} onChange={(imageQuality) => setForm({ ...form, imageQuality })} />
              <SelectInput
                label="生产模式"
                value={form.productionMode}
                values={["silent_video", "storyboard_only", "assets_only", "custom"]}
                labels={{ silent_video: "无声视频", storyboard_only: "仅分镜", assets_only: "仅资产", custom: "自定义" }}
                onChange={(productionMode) => setForm({ ...form, productionMode })}
              />
            </div>
          ) : null}
          {step === 2 ? (
            <div className="grid gap-4">
              <SelectInput
                label="画风风格"
                value={form.artStyle}
                values={["写实电影感", "国风动画", "二次元", "黑白漫画", "水彩插画", "赛博城市"]}
                onChange={(artStyle) => setForm({ ...form, artStyle })}
              />
              <TextAreaInput label="导演手册" value={form.directorManual} onChange={(directorManual) => setForm({ ...form, directorManual })} />
              <TextAreaInput label="视觉手册" value={form.visualManual} onChange={(visualManual) => setForm({ ...form, visualManual })} />
            </div>
          ) : null}
          {step === 3 ? (
            <div className="grid gap-4">
              <SelectInput
                label="内容导入"
                value={form.sourceMode}
                values={["none", "novel", "script"]}
                labels={{ none: "暂不导入", novel: "上传小说原文", script: "上传剧本原文" }}
                onChange={(sourceMode) => setForm({ ...form, sourceMode })}
              />
              {form.sourceMode !== "none" ? (
                <>
                  <div className="grid gap-4 md:grid-cols-2">
                    <TextInput label="内容标题" value={form.sourceTitle} onChange={(sourceTitle) => setForm({ ...form, sourceTitle })} />
                    <SelectInput
                      label="文本格式"
                      value={form.sourceFormat}
                      values={["plain_text", "markdown"]}
                      labels={{ plain_text: "纯文本", markdown: "Markdown" }}
                      onChange={(sourceFormat) => setForm({ ...form, sourceFormat })}
                    />
                  </div>
                  <TextAreaInput rows={10} label="正文" value={form.sourceContent} onChange={(sourceContent) => setForm({ ...form, sourceContent })} />
                </>
              ) : null}
            </div>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-200 p-4">
          <ErrorPanel message={error} />
          <div className="ml-auto flex gap-2">
            <button className="studio-button" disabled={step === 0} onClick={() => setStep((value) => Math.max(0, value - 1))} type="button">
              上一步
            </button>
            {step < steps.length - 1 ? (
              <button className="studio-button studio-button-primary" onClick={() => setStep((value) => Math.min(steps.length - 1, value + 1))} type="button">
                下一步
              </button>
            ) : (
              <button className="studio-button studio-button-primary" disabled={busy} onClick={submit} type="button">
                {busy ? <Loader2 className="animate-spin" size={16} /> : <Check size={16} />}
                创建项目
              </button>
            )}
          </div>
        </div>
      </Surface>
    </SessionGate>
  );
}

export function ProjectOverviewPage({ projectId }: { projectId: string }) {
  return (
    <AppShell active="projects" title="项目概览" description="围绕原文、剧本、资产、分镜和成片查看当前进度。" projectId={projectId} projectSection="">
      <ProjectOverviewContent projectId={projectId} />
    </AppShell>
  );
}

function ProjectOverviewContent({ projectId }: { projectId: string }) {
  const project = useStudioQuery<Project | null>(null, `project:${projectId}`, async (activeSession) => studioApi.getProject(activeSession, projectId));
  const production = useStudioQuery<ProductionStatus | null>(null, `project:${projectId}:production`, async (activeSession) => studioApi.getProductionStatus(activeSession, projectId));
  const scripts = useStudioQuery<Script[]>([], `project:${projectId}:scripts`, async (activeSession) => (await studioApi.listScripts(activeSession, projectId)).items);
  const assets = useStudioQuery<CanonicalAsset[]>([], `project:${projectId}:assets`, async (activeSession) => (await studioApi.listCanonicalAssets(activeSession, projectId)).items);
  const workflows = useStudioQuery<WorkflowRun[]>([], `project:${projectId}:runs`, async (activeSession) => (await studioApi.listWorkflowRuns(activeSession, projectId)).items);
  const artifacts = useStudioQuery<Artifact[]>([], `project:${projectId}:artifacts`, async (activeSession) => (await studioApi.listArtifacts(activeSession, projectId)).items);
  const latestRun = workflows.data[0];
  const finalVideo = artifacts.data.find((item) => item.type === "final_video");

  return (
    <SessionGate>
      <div className="grid gap-5">
        <QueryBody state={project}>
          {project.data ? (
            <Surface className="p-5">
              <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
                <div>
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-3xl font-semibold text-slate-950">{project.data.name}</h2>
                    <StatusBadge status={project.data.status ?? "active"} />
                  </div>
                  <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-600">{project.data.description || "暂无简介"}</p>
                  <div className="mt-4 flex flex-wrap gap-2 text-xs text-slate-600">
                    <Pill>{project.data.projectType || "未设置项目类型"}</Pill>
                    <Pill>{project.data.contentType || "未设置内容类型"}</Pill>
                    <Pill>{project.data.videoRatio || project.data.aspectRatio || "16:9"}</Pill>
                    <Pill>{project.data.artStyle || "未设置画风"}</Pill>
                  </div>
                </div>
                <Link className="studio-button studio-button-primary" href={projectHref(projectId, "production") as Route}>
                  <Play size={16} />
                  继续生产
                </Link>
              </div>
              {production.data ? (
                <div className="mt-5 grid gap-3 md:grid-cols-[1fr_auto] md:items-center">
                  <div>
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-slate-600">当前阶段：{productionStageLabel(production.data.overall.stage)}</span>
                      <span className="font-medium text-slate-900">{production.data.overall.progress}%</span>
                    </div>
                    <div className="mt-2 h-2 overflow-hidden rounded-full bg-slate-200">
                      <div className="h-full rounded-full bg-blue-600" style={{ width: `${Math.max(0, Math.min(100, production.data.overall.progress))}%` }} />
                    </div>
                  </div>
                  <StatusBadge status={production.data.overall.status} />
                </div>
              ) : null}
            </Surface>
          ) : null}
        </QueryBody>

        <Surface>
          <SectionTitle title="当前进度" description="正式生产链路从原文/剧本开始，逐步进入资产、分镜、镜头视频和最终成片。" />
          <div className="grid gap-3 p-4 md:grid-cols-5">
            <ProgressStep done={scripts.data.length > 0} title="原文/剧本" detail={`${scripts.data.length} 个剧本`} />
            <ProgressStep done={assets.data.length > 0} title="资产" detail={`${assets.data.length} 个基础资产`} />
            <ProgressStep done={workflows.data.some((item) => stringFrom(item.input.workflowType) === "script_to_storyboard")} title="分镜" detail="Storyboard Agent" />
            <ProgressStep done={workflows.data.some((item) => ["script_to_video", "full_production", "video_production"].includes(stringFrom(item.input.workflowType)))} title="镜头视频" detail="图片 / 视频生成" />
            <ProgressStep done={Boolean(finalVideo)} title="最终成片" detail={finalVideo?.storageKey ?? "等待合成"} />
          </div>
        </Surface>

        <div className="grid gap-5 xl:grid-cols-[1.2fr_0.8fr]">
          <Surface>
            <SectionTitle title="最近工作流" description="最近一次完整生产或视频生产会显示在顶部。" />
            {latestRun ? <WorkflowRow run={latestRun} /> : <EmptyState title="暂无工作流" description="在工作流页面启动 source_to_script、script_to_assets、script_to_storyboard 或 full_production。" />}
          </Surface>
          <Surface>
            <SectionTitle title="最终成片" description="当 final_video 生成后会显示视频预览。" />
            <div className="p-4">{finalVideo ? <MediaPreview artifact={finalVideo} /> : <EmptyState title="还没有最终成片" description="完成镜头视频后启动完整生产或合成流程。" />}</div>
          </Surface>
        </div>

        <div className="grid gap-5 xl:grid-cols-2">
          <Surface>
            <SectionTitle title="最近资产" description="角色、场景和道具会先作为基础资产沉淀。" />
            <div className="grid gap-3 p-4">
              {assets.data.slice(0, 6).map((asset) => (
                <AssetRow key={asset.id} asset={asset} />
              ))}
              {!assets.data.length ? <EmptyState title="还没有资产" description="先选择剧本并分析角色、场景和道具。" /> : null}
            </div>
          </Surface>
          <Surface>
            <SectionTitle title="最近媒体资产" description="优先显示 final_video、generated_video 和 generated_image。" />
            <div className="grid gap-3 p-4">
              {artifacts.data.slice(0, 4).map((artifact) => (
                <ArtifactRow key={artifact.id} artifact={artifact} />
              ))}
              {!artifacts.data.length ? <EmptyState title="还没有媒体资产" description="生成资产参考图、镜头图片或镜头视频后会出现在这里。" /> : null}
            </div>
          </Surface>
        </div>
      </div>
    </SessionGate>
  );
}

export function ProjectProductionPage({ projectId }: { projectId: string }) {
  return (
    <AppShell active="projects" title="生产看板" description="按阶段推进项目生产，检查、确认、重跑每一步，而不是一次性黑盒生成。" projectId={projectId} projectSection="production">
      <ProjectProductionContent projectId={projectId} />
    </AppShell>
  );
}

function ProjectProductionContent({ projectId }: { projectId: string }) {
  const { session } = useStudioSession();
  const status = useStudioQuery<ProductionStatus | null>(null, `production:${projectId}`, async (activeSession) => studioApi.getProductionStatus(activeSession, projectId));
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [lastWorkflowRunId, setLastWorkflowRunId] = useState("");

  async function runAction(action: string) {
    setBusy(action);
    setError("");
    setNotice("");
    try {
      const response = await studioApi.runProductionAction(session, projectId, compactRecord({ action, options: { maxShots: 3 } }));
      setLastWorkflowRunId(response.workflowRunId);
      setNotice(response.note || `${productionActionLabel(action)}已启动`);
      status.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy("");
    }
  }

  const nextAction = status.data ? nextProductionAction(status.data) : "";

  return (
    <SessionGate>
      <QueryBody state={status}>
        {status.data ? (
          <div className="grid gap-5">
            <Surface className="p-5">
              <div className="grid gap-5 xl:grid-cols-[1fr_320px]">
                <div>
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-3xl font-semibold text-slate-950">{status.data.project.name}</h2>
                    <StatusBadge status={status.data.overall.status} />
                  </div>
                  <div className="mt-4 flex flex-wrap gap-2">
                    <Pill>{status.data.project.projectType || "未设置项目类型"}</Pill>
                    <Pill>{status.data.project.contentType || "未设置内容类型"}</Pill>
                    <Pill>{status.data.project.videoRatio || "16:9"}</Pill>
                    <Pill>{status.data.project.artStyle || "未设置画风"}</Pill>
                  </div>
                  <div className="mt-5">
                    <div className="flex items-center justify-between text-sm">
                      <span className="text-slate-600">当前阶段：{productionStageLabel(status.data.overall.stage)}</span>
                      <span className="font-medium text-slate-900">{status.data.overall.progress}%</span>
                    </div>
                    <div className="mt-2 h-2 overflow-hidden rounded-full bg-slate-200">
                      <div className="h-full rounded-full bg-blue-600" style={{ width: `${Math.max(0, Math.min(100, status.data.overall.progress))}%` }} />
                    </div>
                  </div>
                </div>
                <div className="grid content-start gap-3">
                  {nextAction ? (
                    <button className="studio-button studio-button-primary justify-center" disabled={busy !== ""} onClick={() => runAction(nextAction)} type="button">
                      {busy ? <Loader2 className="animate-spin" size={16} /> : <Play size={16} />}
                      继续下一步
                    </button>
                  ) : null}
                  {status.data.stages.finalVideo.previewUrl ? (
                    <a className="studio-button justify-center" href={status.data.stages.finalVideo.previewUrl} rel="noreferrer" target="_blank">
                      <Video size={16} />
                      查看最终成片
                    </a>
                  ) : null}
                  {lastWorkflowRunId ? <p className="rounded-md border border-slate-200 bg-slate-50 p-3 text-xs text-slate-600">运行中 workflowRunId：{lastWorkflowRunId}</p> : null}
                  <ErrorPanel message={error} />
                  {notice ? <p className="text-sm text-blue-700">{notice}</p> : null}
                </div>
              </div>
            </Surface>

            {status.data.stages.finalVideo.previewUrl ? (
              <Surface className="overflow-hidden">
                <SectionTitle title="最终成片预览" description="final_video 生成后会优先显示在生产看板顶部。" />
                <div className="p-4">
                  <video className="aspect-video w-full rounded-md bg-black" controls src={status.data.stages.finalVideo.previewUrl} />
                </div>
              </Surface>
            ) : null}

            <div className="grid gap-4">
              <ProductionStageCard
                title="内容源"
                status={status.data.stages.source.status}
                description={productionStageDescription("source", status.data.stages.source.status)}
                metrics={[
                  metricText("小说原文", status.data.stages.source.novelSourceCount),
                  metricText("剧本原文", status.data.stages.source.scriptSourceCount),
                  status.data.stages.source.activeScriptTitle ? `当前剧本：${status.data.stages.source.activeScriptTitle}` : "当前剧本：暂无",
                ]}
                summary={status.data.stages.source.summary}
                primary={{ label: "让 Agent 生成剧本", action: "generate_script", disabled: status.data.stages.source.novelSourceCount + status.data.stages.source.scriptSourceCount === 0 }}
                secondary={{ label: "进入原文与剧本", href: projectHref(projectId, "sources") }}
                busy={busy}
                onRun={runAction}
              />
              <ProductionStageCard
                title="基础资产"
                status={status.data.stages.assets.status}
                description={productionStageDescription("assets", status.data.stages.assets.status)}
                metrics={[
                  metricText("角色", status.data.stages.assets.characterCount),
                  metricText("场景", status.data.stages.assets.sceneCount),
                  metricText("道具", status.data.stages.assets.propCount),
                  metricText("参考图", status.data.stages.assets.referenceImageCount),
                  metricText("待确认", status.data.stages.assets.pendingReviewCount),
                  metricText("人工修改", status.data.stages.assets.manualOverrideCount),
                  metricText("下游过期", status.data.stages.assets.downstreamStaleCount),
                ]}
                summary={[...(status.data.stages.assets.summary.character ?? []), ...(status.data.stages.assets.summary.scene ?? []), ...(status.data.stages.assets.summary.prop ?? [])]}
                primary={{ label: "分析剧本资产", action: "analyze_assets", disabled: !status.data.stages.source.activeScriptId }}
                secondary={{ label: "生成缺失参考图", action: "generate_asset_images", disabled: !status.data.stages.source.activeScriptId }}
                link={{ label: "进入资产", href: projectHref(projectId, "assets") }}
                busy={busy}
                onRun={runAction}
              />
              <ProductionStageCard
                title="分镜"
                status={status.data.stages.storyboard.status}
                description={productionStageDescription("storyboard", status.data.stages.storyboard.status)}
                metrics={[
                  metricText("镜头", status.data.stages.storyboard.shotCount),
                  metricText("已确认镜头", status.data.stages.storyboard.confirmedShotCount),
                  metricText("待确认", status.data.stages.storyboard.pendingReviewCount),
                  metricText("人工修改", status.data.stages.storyboard.manualOverrideCount),
                  metricText("需重生成", status.data.stages.storyboard.staleShotCount),
                ]}
                summary={status.data.stages.storyboard.summary}
                primary={{ label: "生成分镜", action: "generate_storyboard", disabled: !status.data.stages.source.activeScriptId }}
                secondary={{ label: "进入分镜镜头", href: projectHref(projectId, "storyboard") }}
                busy={busy}
                onRun={runAction}
              />
              <ProductionStageCard
                title="派生资产"
                status={status.data.stages.shotAssets.status}
                description={productionStageDescription("shot_assets", status.data.stages.shotAssets.status)}
                metrics={[
                  metricText("需求", status.data.stages.shotAssets.requirementCount),
                  metricText("角色派生", status.data.stages.shotAssets.characterRequirementCount),
                  metricText("场景派生", status.data.stages.shotAssets.sceneRequirementCount),
                  metricText("道具派生", status.data.stages.shotAssets.propRequirementCount),
                  metricText("派生参考图", status.data.stages.shotAssets.derivedImageCount),
                  metricText("待确认", status.data.stages.shotAssets.pendingReviewCount),
                  metricText("人工修改", status.data.stages.shotAssets.manualOverrideCount),
                  metricText("派生图过期", status.data.stages.shotAssets.staleRequirementCount),
                ]}
                summary={status.data.stages.shotAssets.summary}
                primary={{ label: "分析镜头派生资产", action: "analyze_shot_assets", disabled: !status.data.stages.source.activeScriptId }}
                secondary={{ label: "生成派生参考图", action: "generate_derived_asset_images", disabled: !status.data.stages.source.activeScriptId }}
                link={{ label: "进入分镜镜头", href: projectHref(projectId, "storyboard") }}
                busy={busy}
                onRun={runAction}
              />
              <ProductionStageCard
                title="镜头图片"
                status={status.data.stages.shotImages.status}
                description={productionStageDescription("shot_images", status.data.stages.shotImages.status)}
                metrics={shotMediaMetrics(status.data.stages.shotImages)}
                primary={{ label: "生成镜头图片", action: "generate_shot_images", disabled: !status.data.stages.source.activeScriptId }}
                secondary={{ label: "重新生成失败图片", action: "generate_shot_images", disabled: !status.data.stages.source.activeScriptId || status.data.stages.shotImages.failed === 0 }}
                link={{ label: "进入分镜镜头", href: projectHref(projectId, "storyboard") }}
                busy={busy}
                onRun={runAction}
              />
              <ProductionStageCard
                title="镜头视频"
                status={status.data.stages.shotVideos.status}
                description={productionStageDescription("shot_videos", status.data.stages.shotVideos.status)}
                metrics={shotMediaMetrics(status.data.stages.shotVideos)}
                primary={{ label: "生成镜头视频", action: "generate_shot_videos", disabled: !status.data.stages.source.activeScriptId }}
                secondary={{ label: "查看/取消运行任务", href: projectHref(projectId, "workflows") }}
                link={{ label: "进入分镜镜头", href: projectHref(projectId, "storyboard") }}
                busy={busy}
                onRun={runAction}
              />
              <ProductionStageCard
                title="最终成片"
                status={status.data.stages.finalVideo.status}
                description={productionStageDescription("final_video", status.data.stages.finalVideo.status)}
                metrics={[
                  status.data.stages.finalVideo.artifactId ? "最终成片：已生成" : "最终成片：未生成",
                  status.data.stages.finalVideo.storageKey ? `对象：${status.data.stages.finalVideo.storageKey}` : "时间线文件：等待合成",
                  status.data.stages.finalVideo.stale ? "最终成片可能不是最新" : "最终成片状态：最新",
                ]}
                primary={{ label: "合成最终成片", action: "compose_final_video", disabled: !status.data.stages.source.activeScriptId }}
                secondary={status.data.stages.finalVideo.previewUrl ? { label: "预览最终成片", href: status.data.stages.finalVideo.previewUrl } : { label: "进入媒体资产", href: projectHref(projectId, "vault") }}
                busy={busy}
                onRun={runAction}
              />
            </div>
          </div>
        ) : null}
      </QueryBody>
    </SessionGate>
  );
}

function ProductionStageCard({
  title,
  status,
  description,
  metrics,
  summary = [],
  primary,
  secondary,
  link,
  busy,
  onRun,
}: {
  title: string;
  status: string;
  description: string;
  metrics: string[];
  summary?: string[];
  primary: { label: string; action: string; disabled?: boolean };
  secondary?: { label: string; action?: string; href?: string; disabled?: boolean };
  link?: { label: string; href: string };
  busy: string;
  onRun: (action: string) => void;
}) {
  const busyThis = busy === primary.action || (secondary?.action ? busy === secondary.action : false);
  return (
    <Surface className="p-4">
      <div className="grid gap-4 xl:grid-cols-[1fr_320px]">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-lg font-semibold text-slate-900">{title}</h3>
            <StatusBadge status={status} />
          </div>
          <p className="mt-2 text-sm leading-6 text-slate-600">{description}</p>
          <div className="mt-4 grid gap-2 md:grid-cols-3">
            {metrics.map((item) => (
              <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-700" key={item}>
                {item}
              </div>
            ))}
          </div>
          {summary.length ? (
            <div className="mt-4 flex flex-wrap gap-2">
              {summary.slice(0, 8).map((item) => (
                <Pill key={item}>{item}</Pill>
              ))}
            </div>
          ) : null}
        </div>
        <div className="grid content-start gap-2">
          <button className="studio-button studio-button-primary justify-center" disabled={busy !== "" || primary.disabled} onClick={() => onRun(primary.action)} type="button">
            {busy === primary.action ? <Loader2 className="animate-spin" size={16} /> : <Play size={16} />}
            {primary.label}
          </button>
          {secondary?.action ? (
            <button className="studio-button justify-center" disabled={busy !== "" || secondary.disabled} onClick={() => secondary.action && onRun(secondary.action)} type="button">
              {busyThis ? <Loader2 className="animate-spin" size={16} /> : <Sparkles size={16} />}
              {secondary.label}
            </button>
          ) : null}
          {secondary?.href ? (
            secondary.href.startsWith("http") ? (
              <a className="studio-button justify-center" href={secondary.href} rel="noreferrer" target="_blank">
                <ArrowRight size={16} />
                {secondary.label}
              </a>
            ) : (
              <Link className="studio-button justify-center" href={secondary.href as Route}>
                <ArrowRight size={16} />
                {secondary.label}
              </Link>
            )
          ) : null}
          {link ? (
            <Link className="studio-button justify-center" href={link.href as Route}>
              <ArrowRight size={16} />
              {link.label}
            </Link>
          ) : null}
        </div>
      </div>
    </Surface>
  );
}

export function SourcesPage({ projectId }: { projectId: string }) {
  return (
    <AppShell active="projects" title="原文与剧本" description="左侧管理内容源，中间编辑剧本版本，右侧与 Script Agent 对话。" projectId={projectId} projectSection="sources">
      <SourcesContent projectId={projectId} />
    </AppShell>
  );
}

function SourcesContent({ projectId }: { projectId: string }) {
  const { session } = useStudioSession();
  const sources = useStudioQuery<ProjectSource[]>([], `sources:${projectId}`, async (activeSession) => (await studioApi.listSources(activeSession, projectId)).items);
  const scripts = useStudioQuery<Script[]>([], `scripts:${projectId}`, async (activeSession) => (await studioApi.listScripts(activeSession, projectId)).items);
  const sessions = useStudioQuery<AgentSession[]>([], `agent-sessions:${projectId}`, async (activeSession) => (await studioApi.listAgentSessions(activeSession, projectId)).items);
  const [selectedSourceId, setSelectedSourceId] = useState("");
  const [selectedScriptId, setSelectedScriptId] = useState("");
  const [selectedSessionId, setSelectedSessionId] = useState("");
  const [sourceDraft, setSourceDraft] = useState({ sourceType: "novel", title: "", content: "", contentFormat: "plain_text" });
  const [scriptDraft, setScriptDraft] = useState({ title: "", content: "", instruction: "" });
  const [agentText, setAgentText] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const effectiveSourceId = validSelection(selectedSourceId, sources.data);
  const effectiveScriptId = validSelection(selectedScriptId, scripts.data);
  const effectiveSessionId = validSelection(selectedSessionId, sessions.data);
  const scriptDetail = useStudioQuery<Script | null>(null, `script-detail:${projectId}:${effectiveScriptId}`, async (activeSession) =>
    effectiveScriptId ? studioApi.getScript(activeSession, projectId, effectiveScriptId) : Promise.resolve(null),
  );
  const versions = useStudioQuery<ScriptVersion[]>([], `script-versions:${projectId}:${effectiveScriptId}`, async (activeSession) =>
    effectiveScriptId ? (await studioApi.listScriptVersions(activeSession, projectId, effectiveScriptId)).items : Promise.resolve([]),
  );
  const messages = useStudioQuery<AgentMessage[]>([], `agent-messages:${projectId}:${effectiveSessionId}`, async (activeSession) =>
    effectiveSessionId ? (await studioApi.listAgentMessages(activeSession, projectId, effectiveSessionId)).items : Promise.resolve([]),
  );

  async function perform(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError("");
    setNotice("");
    try {
      await action();
      setNotice(`${label}已完成。`);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy("");
    }
  }

  const selectedSource = sources.data.find((item) => item.id === effectiveSourceId);
  const selectedScript = scriptDetail.data ?? scripts.data.find((item) => item.id === effectiveScriptId);

  return (
    <SessionGate>
      <div className="grid gap-5 xl:grid-cols-[280px_minmax(0,1fr)_340px]">
        <Surface>
          <SectionTitle title="内容源" description="添加小说原文或剧本原文。" />
          <div className="grid gap-3 p-4">
            <div className="grid gap-2">
              {sources.data.map((source) => (
                <button className={cn("rounded-lg border p-3 text-left", effectiveSourceId === source.id ? "border-blue-600/50 bg-blue-600/10" : "border-slate-200 bg-slate-50")} key={source.id} onClick={() => setSelectedSourceId(source.id)} type="button">
                  <div className="flex items-center justify-between gap-2">
                    <p className="text-sm font-medium text-slate-900">{source.title}</p>
                    <StatusBadge status={source.status} />
                  </div>
                  <p className="mt-1 text-xs text-slate-500">{source.sourceType === "novel" ? "小说原文" : "剧本原文"}</p>
                </button>
              ))}
              {!sources.data.length ? <EmptyState title="还没有内容源" description="添加小说原文或剧本原文，之后可让 Agent 生成剧本。" /> : null}
            </div>
            <div className="grid gap-2 border-t border-slate-200 pt-3">
              <SelectInput label="类型" value={sourceDraft.sourceType} values={["novel", "script"]} labels={{ novel: "小说原文", script: "剧本原文" }} onChange={(sourceType) => setSourceDraft({ ...sourceDraft, sourceType })} />
              <TextInput label="标题" value={sourceDraft.title} onChange={(title) => setSourceDraft({ ...sourceDraft, title })} />
              <TextAreaInput rows={7} label="正文" value={sourceDraft.content} onChange={(content) => setSourceDraft({ ...sourceDraft, content })} />
              <button
                className="studio-button studio-button-primary"
                disabled={busy !== ""}
                onClick={() =>
                  perform("添加内容源", async () => {
                    const created = await studioApi.createSource(session, projectId, compactRecord(sourceDraft));
                    setSelectedSourceId(created.id);
                    setSourceDraft({ ...sourceDraft, title: "", content: "" });
                    sources.reload();
                  })
                }
                type="button"
              >
                <Plus size={16} />
                添加内容源
              </button>
            </div>
          </div>
        </Surface>

        <Surface>
          <SectionTitle title="剧本版本" description="保存剧本正文、查看版本，并激活当前版本。" />
          <div className="grid gap-4 p-4">
            <div className="grid gap-3 md:grid-cols-[240px_1fr]">
              <div className="grid content-start gap-2">
                {scripts.data.map((script) => (
                  <button className={cn("rounded-lg border p-3 text-left", effectiveScriptId === script.id ? "border-blue-600/50 bg-blue-600/10" : "border-slate-200 bg-slate-50")} key={script.id} onClick={() => setSelectedScriptId(script.id)} type="button">
                    <p className="text-sm font-medium text-slate-900">{script.title}</p>
                    <p className="mt-1 text-xs text-slate-500">{script.currentVersionId ? "已激活版本" : "暂无版本"}</p>
                  </button>
                ))}
                {!scripts.data.length ? <EmptyState title="还没有剧本" description="导入剧本原文，或让 Script Agent 根据原文生成剧本。" /> : null}
              </div>
              <div className="grid gap-3">
                <TextInput label="剧本标题" value={scriptDraft.title} onChange={(title) => setScriptDraft({ ...scriptDraft, title })} />
                <TextAreaInput rows={14} label="剧本正文" value={scriptDraft.content || selectedScript?.currentVersion?.content || ""} onChange={(content) => setScriptDraft({ ...scriptDraft, content })} />
                <div className="flex flex-wrap gap-2">
                  <button
                    className="studio-button"
                    disabled={busy !== ""}
                    onClick={() =>
                      perform("保存剧本", async () => {
                        const created = await studioApi.createScript(session, projectId, compactRecord({
                          sourceId: effectiveSourceId || undefined,
                          title: scriptDraft.title || selectedSource?.title || "未命名剧本",
                          content: scriptDraft.content || selectedScript?.currentVersion?.content || "",
                          contentFormat: "markdown",
                          sourceType: "manual",
                        }));
                        setSelectedScriptId(created.id);
                        scripts.reload();
                      })
                    }
                    type="button"
                  >
                    <Save size={16} />
                    保存为剧本
                  </button>
                  {effectiveScriptId ? (
                    <button
                      className="studio-button"
                      disabled={busy !== ""}
                      onClick={() =>
                        perform("保存新版本", async () => {
                          const version = await studioApi.createScriptVersion(session, projectId, effectiveScriptId, compactRecord({
                            content: scriptDraft.content || selectedScript?.currentVersion?.content || "",
                            contentFormat: "markdown",
                            sourceType: "manual",
                            activate: true,
                          }));
                          await studioApi.activateScriptVersion(session, projectId, effectiveScriptId, version.id);
                          scriptDetail.reload();
                          versions.reload();
                        })
                      }
                      type="button"
                    >
                      <Copy size={16} />
                      保存并激活新版本
                    </button>
                  ) : null}
                </div>
                {versions.data.length ? (
                  <div className="grid gap-2">
                    {versions.data.map((version) => (
                      <div className="flex items-center justify-between rounded-md border border-slate-200 px-3 py-2 text-sm" key={version.id}>
                        <span>版本 {version.version}</span>
                        <button
                          className="text-blue-700 hover:text-blue-900"
                          onClick={() =>
                            perform("激活版本", async () => {
                              await studioApi.activateScriptVersion(session, projectId, effectiveScriptId, version.id);
                              scriptDetail.reload();
                              scripts.reload();
                            })
                          }
                          type="button"
                        >
                          激活此版本
                        </button>
                      </div>
                    ))}
                  </div>
                ) : null}
              </div>
            </div>
          </div>
        </Surface>

        <Surface>
          <SectionTitle title="Script Agent" description="生成剧本、改写当前剧本，或记录创作指令。" />
          <div className="grid gap-3 p-4">
            <div className="flex gap-2">
              <select className="studio-input min-w-0 flex-1" value={effectiveSessionId} onChange={(event) => setSelectedSessionId(event.target.value)}>
                <option value="">选择会话</option>
                {sessions.data.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.title || item.id}
                  </option>
                ))}
              </select>
              <button
                className="studio-button"
                onClick={() =>
                  perform("创建会话", async () => {
                    const created = await studioApi.createAgentSession(session, projectId, "剧本创作会话");
                    setSelectedSessionId(created.id);
                    sessions.reload();
                  })
                }
                type="button"
              >
                <Plus size={16} />
              </button>
            </div>
            <div className="grid max-h-72 gap-2 overflow-auto rounded-lg border border-slate-200 bg-slate-200 p-3">
              {messages.data.map((message) => (
                <div className={cn("rounded-md px-3 py-2 text-sm", message.role === "user" ? "ml-8 bg-blue-600/10 text-blue-900" : "mr-8 bg-slate-50 text-slate-800")} key={message.id}>
                  {message.content}
                </div>
              ))}
              {!messages.data.length ? <p className="text-sm text-slate-500">还没有对话。发送指令，或直接生成/改写剧本。</p> : null}
            </div>
            <TextAreaInput rows={5} label="Agent 指令" value={agentText} onChange={setAgentText} />
            <div className="grid gap-2">
              <button
                className="studio-button"
                disabled={!effectiveSessionId || busy !== ""}
                onClick={() =>
                  perform("发送指令", async () => {
                    await studioApi.createAgentMessage(session, projectId, effectiveSessionId, agentText);
                    setAgentText("");
                    messages.reload();
                  })
                }
                type="button"
              >
                <Send size={16} />
                发送用户指令
              </button>
              <button
                className="studio-button studio-button-primary"
                disabled={!effectiveSourceId || busy !== ""}
                onClick={() =>
                  perform("生成剧本", async () => {
                    const result = await studioApi.generateScript(session, projectId, compactRecord({ sourceId: effectiveSourceId, instruction: agentText, sessionId: effectiveSessionId || undefined }));
                    setSelectedScriptId(result.scriptId);
                    setScriptDraft({ ...scriptDraft, content: result.content });
                    scripts.reload();
                    messages.reload();
                  })
                }
                type="button"
              >
                <Sparkles size={16} />
                让 Agent 根据原文生成剧本
              </button>
              <button
                className="studio-button"
                disabled={!effectiveScriptId || busy !== ""}
                onClick={() =>
                  perform("改写剧本", async () => {
                    const result = await studioApi.rewriteScript(session, projectId, compactRecord({ scriptId: effectiveScriptId, instruction: agentText, sessionId: effectiveSessionId || undefined, activate: true }));
                    setScriptDraft({ ...scriptDraft, content: result.content });
                    scriptDetail.reload();
                    versions.reload();
                  })
                }
                type="button"
              >
                <MessageSquareText size={16} />
                让 Agent 改写当前剧本
              </button>
            </div>
            <ErrorPanel message={error} />
            {notice ? <p className="text-sm text-blue-700">{notice}</p> : null}
          </div>
        </Surface>
      </div>
    </SessionGate>
  );
}

export function AssetsPage({ projectId }: { projectId: string }) {
  return (
    <AppShell active="projects" title="资产" description="展示和管理角色、场景、道具等基础资产。" projectId={projectId} projectSection="assets">
      <AssetsContent projectId={projectId} />
    </AppShell>
  );
}

function AssetsContent({ projectId }: { projectId: string }) {
  const { session } = useStudioSession();
  const scripts = useStudioQuery<Script[]>([], `assets:scripts:${projectId}`, async (activeSession) => (await studioApi.listScripts(activeSession, projectId)).items);
  const assets = useStudioQuery<CanonicalAsset[]>([], `assets:list:${projectId}`, async (activeSession) => (await studioApi.listCanonicalAssets(activeSession, projectId)).items);
  const requirements = useStudioQuery<ShotAssetRequirement[]>([], `assets:reqs:${projectId}`, async (activeSession) => (await studioApi.listShotAssetRequirements(activeSession, projectId)).items);
  const [scriptId, setScriptId] = useState("");
  const [filter, setFilter] = useState("all");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [editingAsset, setEditingAsset] = useState<CanonicalAsset | null>(null);
  const effectiveScriptId = validSelection(scriptId, scripts.data);
  const filtered = assets.data.filter((asset) => filter === "all" || asset.assetType === filter);

  async function perform(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError("");
    try {
      await action();
      assets.reload();
      requirements.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy("");
    }
  }

  return (
    <SessionGate>
      <Surface className="mb-5 p-4">
        <div className="grid gap-3 lg:grid-cols-[1fr_220px_auto_auto]">
          <select className="studio-input" value={effectiveScriptId} onChange={(event) => setScriptId(event.target.value)}>
            <option value="">选择剧本</option>
            {scripts.data.map((script) => (
              <option key={script.id} value={script.id}>
                {script.title}
              </option>
            ))}
          </select>
          <select className="studio-input" value={filter} onChange={(event) => setFilter(event.target.value)}>
            <option value="all">全部资产</option>
            <option value="character">角色</option>
            <option value="scene">场景</option>
            <option value="prop">道具</option>
          </select>
          <button className="studio-button studio-button-primary" disabled={!effectiveScriptId || busy !== ""} onClick={() => perform("分析剧本资产", async () => void (await studioApi.analyzeScriptAssets(session, projectId, effectiveScriptId, { generateImages: false })))} type="button">
            <Sparkles size={16} />
            分析剧本资产
          </button>
          <button
            className="studio-button"
            disabled={busy !== ""}
            onClick={() => perform("生成缺失参考图", async () => void (await studioApi.runProductionAction(session, projectId, { action: "generate_asset_images", options: {} })))}
            type="button"
          >
            <ImageIcon size={16} />
            生成缺失参考图
          </button>
        </div>
        <ErrorPanel message={error} />
      </Surface>

      {filtered.length ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((asset) => {
            const linkedRequirements = requirements.data.filter((item) => item.assetId === asset.id);
            const staleRequirementCount = linkedRequirements.filter((item) => item.staleState && item.staleState !== "fresh").length;
            return (
              <Surface className="overflow-hidden" key={asset.id}>
                <div className="grid aspect-video place-items-center bg-slate-200 text-slate-400">
                  <ImageIcon size={28} />
                </div>
                <div className="grid gap-3 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <p className="font-medium text-slate-900">{asset.name}</p>
                      <p className="mt-1 text-xs text-slate-500">{assetTypeLabel(asset.assetType)}</p>
                    </div>
                    <div className="grid justify-items-end gap-1">
                      <StatusBadge status={asset.status} />
                      <StatusBadge status={asset.reviewStatus ?? "pending"} />
                      {asset.manualOverride ? <Pill>人工修改</Pill> : null}
                      {asset.staleState && asset.staleState !== "fresh" ? <StatusBadge status={asset.staleState} /> : null}
                    </div>
                  </div>
                  <p className="line-clamp-3 text-sm leading-6 text-slate-600">{asset.description}</p>
                  <p className="text-xs text-slate-500">参考图：{asset.referenceArtifactId || asset.referenceStorageKey ? "已生成" : "未生成"}</p>
                  <p className="text-xs text-slate-500">关联派生需求：{linkedRequirements.length}</p>
                  {staleRequirementCount ? <p className="text-xs text-amber-700">下游派生资产需重生成：{staleRequirementCount}</p> : null}
                  <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                    <button className="studio-button" disabled={busy !== ""} onClick={() => perform("确认资产", async () => void (await studioApi.reviewAsset(session, projectId, asset.id, { reviewStatus: "approved" })))} type="button">
                      <Check size={16} />
                      确认资产
                    </button>
                    <button className="studio-button" disabled={busy !== ""} onClick={() => perform("标记需修改", async () => void (await studioApi.reviewAsset(session, projectId, asset.id, { reviewStatus: "needs_edit" })))} type="button">
                      <X size={16} />
                      需修改
                    </button>
                    <button className="studio-button" disabled={busy !== ""} onClick={() => setEditingAsset(asset)} type="button">
                      <Pencil size={16} />
                      编辑资产
                    </button>
                    <button className="studio-button" disabled={busy !== ""} onClick={() => perform("重新生成参考图", async () => void (await studioApi.regenerate(session, projectId, { targetType: "canonical_asset_image", targetId: asset.id, options: { force: true } })))} type="button">
                      <RefreshCw size={16} />
                      参考图
                    </button>
                  </div>
                </div>
              </Surface>
            );
          })}
        </div>
      ) : (
        <EmptyState title="还没有资产" description="选择剧本后点击“分析剧本资产”，提取角色、场景和道具。" />
      )}
      <AssetEditDialog
        asset={editingAsset}
        busy={busy !== ""}
        onClose={() => setEditingAsset(null)}
        onSave={(body) =>
          perform("保存资产修订", async () => {
            if (!editingAsset) {
              return;
            }
            await studioApi.updateCanonicalAsset(session, projectId, editingAsset.id, body);
            setEditingAsset(null);
          })
        }
      />
    </SessionGate>
  );
}

export function StoryboardPage({ projectId }: { projectId: string }) {
  return (
    <AppShell active="projects" title="分镜镜头" description="展示从剧本生成的分镜，以及每个镜头的派生资产需求。" projectId={projectId} projectSection="storyboard">
      <StoryboardContent projectId={projectId} />
    </AppShell>
  );
}

function StoryboardContent({ projectId }: { projectId: string }) {
  const { session } = useStudioSession();
  const scripts = useStudioQuery<Script[]>([], `storyboard:scripts:${projectId}`, async (activeSession) => (await studioApi.listScripts(activeSession, projectId)).items);
  const workflows = useStudioQuery<WorkflowRun[]>([], `storyboard:runs:${projectId}`, async (activeSession) => (await studioApi.listWorkflowRuns(activeSession, projectId)).items);
  const requirements = useStudioQuery<ShotAssetRequirement[]>([], `storyboard:reqs:${projectId}`, async (activeSession) => (await studioApi.listShotAssetRequirements(activeSession, projectId)).items);
  const [scriptId, setScriptId] = useState("");
  const [workflowRunId, setWorkflowRunId] = useState("");
  const [maxShots, setMaxShots] = useState("3");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [editingShot, setEditingShot] = useState<StoryboardShot | null>(null);
  const [editingRequirement, setEditingRequirement] = useState<ShotAssetRequirement | null>(null);
  const storyboardRuns = workflows.data.filter((run) => ["script_to_storyboard", "script_to_video", "full_production"].includes(stringFrom(run.input.workflowType)));
  const effectiveScriptId = validSelection(scriptId, scripts.data);
  const effectiveWorkflowRunId = validSelection(workflowRunId, storyboardRuns);
  const shots = useStudioQuery<StoryboardShot[]>([], `storyboard:shots:${effectiveWorkflowRunId}`, async (activeSession) =>
    effectiveWorkflowRunId ? (await studioApi.listWorkflowShots(activeSession, effectiveWorkflowRunId)).items : Promise.resolve([]),
  );

  async function perform(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError("");
    try {
      await action();
      workflows.reload();
      requirements.reload();
      shots.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy("");
    }
  }

  async function startScriptVideo(shouldSkipCompose: boolean) {
    await studioApi.createWorkflowRun(
      session,
      compactRecord({
        projectId,
        workflowType: "script_to_video",
        input: {
          scriptId: effectiveScriptId,
          maxShots: Number(maxShots),
          generateImages: true,
          generateDerivedAssets: true,
          skipCompose: shouldSkipCompose,
        },
      }),
    );
  }

  return (
    <SessionGate>
      <Surface className="mb-5 p-4">
        <div className="grid gap-3 xl:grid-cols-[1fr_120px_auto_auto_auto_auto]">
          <select className="studio-input" value={effectiveScriptId} onChange={(event) => setScriptId(event.target.value)}>
            <option value="">选择剧本</option>
            {scripts.data.map((script) => (
              <option key={script.id} value={script.id}>
                {script.title}
              </option>
            ))}
          </select>
          <input className="studio-input" min={1} max={3} type="number" value={maxShots} onChange={(event) => setMaxShots(event.target.value)} />
          <button className="studio-button studio-button-primary" disabled={!effectiveScriptId || busy !== ""} onClick={() => perform("生成分镜", async () => void (await studioApi.generateStoryboard(session, projectId, effectiveScriptId, { maxShots: Number(maxShots), generateDerivedAssets: false })))} type="button">
            <Clapperboard size={16} />
            生成分镜
          </button>
          <button className="studio-button" disabled={!effectiveScriptId || busy !== ""} onClick={() => perform("分析镜头派生资产", async () => void (await studioApi.generateStoryboard(session, projectId, effectiveScriptId, { maxShots: Number(maxShots), generateDerivedAssets: true })))} type="button">
            <Sparkles size={16} />
            分析派生资产
          </button>
          <button className="studio-button" disabled={!effectiveScriptId || busy !== ""} onClick={() => perform("生成镜头图片", async () => startScriptVideo(true))} type="button">
            <ImageIcon size={16} />
            生成镜头图片
          </button>
          <button className="studio-button" disabled={!effectiveScriptId || busy !== ""} onClick={() => perform("生成镜头视频", async () => startScriptVideo(false))} type="button">
            <Video size={16} />
            生成镜头视频
          </button>
        </div>
        <ErrorPanel message={error} />
      </Surface>

      <div className="mb-5 flex gap-2 overflow-x-auto">
        {storyboardRuns
          .map((run) => (
            <button className={cn("rounded-md border px-3 py-2 text-sm", effectiveWorkflowRunId === run.id ? "border-blue-600/60 bg-blue-600/10" : "border-slate-200 bg-slate-50")} key={run.id} onClick={() => setWorkflowRunId(run.id)} type="button">
              {workflowLabel(stringFrom(run.input.workflowType))} · {formatTime(run.createdAt)}
            </button>
          ))}
      </div>

      {shots.data.length ? (
        <div className="grid gap-4">
          {shots.data.map((shot) => {
            const shotRequirements = requirements.data.filter((item) => item.storyboardShotId === shot.id);
            return (
              <Surface className="grid gap-4 p-4 xl:grid-cols-[240px_minmax(0,1fr)_320px]" key={shot.id}>
                <MediaPreview shot={shot} />
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="font-semibold text-slate-900">镜头 {shot.shotNo}</h3>
                    <StatusBadge status={shot.status} />
                    <StatusBadge status={shot.reviewStatus ?? "pending"} />
                    {shot.manualOverride ? <Pill>人工修改</Pill> : null}
                    {shot.staleState && shot.staleState !== "fresh" ? <StatusBadge status={shot.staleState} /> : null}
                  </div>
                  <p className="mt-3 text-sm leading-6 text-slate-700">{shot.visual || "暂无视觉描述"}</p>
                  <dl className="mt-4 grid gap-2 text-sm text-slate-600 md:grid-cols-2">
                    <Meta label="运镜" value={shot.camera} />
                    <Meta label="动作" value={shot.motion} />
                    <Meta label="情绪" value={shot.mood} />
                    <Meta label="时长" value={shot.durationSeconds ? `${shot.durationSeconds}s` : "未设置"} />
                  </dl>
                  <div className="mt-4 flex flex-wrap gap-2">
                    <button className="studio-button" disabled={busy !== ""} onClick={() => perform("确认镜头", async () => void (await studioApi.reviewStoryboardShot(session, projectId, shot.id, { reviewStatus: "approved" })))} type="button">
                      <Check size={16} />
                      确认镜头
                    </button>
                    <button className="studio-button" disabled={busy !== ""} onClick={() => perform("标记镜头需修改", async () => void (await studioApi.reviewStoryboardShot(session, projectId, shot.id, { reviewStatus: "needs_edit" })))} type="button">
                      <X size={16} />
                      需修改
                    </button>
                    <button className="studio-button" disabled={busy !== ""} onClick={() => setEditingShot(shot)} type="button">
                      <Pencil size={16} />
                      编辑镜头
                    </button>
                    <button className="studio-button" disabled={busy !== ""} onClick={() => perform("重新生成镜头图片", async () => void (await studioApi.regenerate(session, projectId, { targetType: "shot_image", targetId: shot.id, options: { force: true } })))} type="button">
                      <RefreshCw size={16} />
                      镜头图
                    </button>
                    <button className="studio-button" disabled={busy !== ""} onClick={() => perform("重新生成镜头视频", async () => void (await studioApi.regenerate(session, projectId, { targetType: "shot_video", targetId: shot.id, options: { force: true } })))} type="button">
                      <Video size={16} />
                      镜头视频
                    </button>
                  </div>
                </div>
                <div className="grid content-start gap-2">
                  <p className="text-sm font-medium text-slate-900">派生资产需求</p>
                  {shotRequirements.map((req) => (
                    <div className="rounded-md border border-slate-200 bg-slate-50 p-3 text-xs leading-5 text-slate-600" key={req.id}>
                      <div className="flex items-start justify-between gap-2">
                        <p className="font-medium text-slate-800">
                          {assetTypeLabel(req.assetType)}：{req.assetName || req.assetId}
                        </p>
                        <div className="grid justify-items-end gap-1">
                          <StatusBadge status={req.reviewStatus ?? "pending"} />
                          {req.manualOverride ? <Pill>人工修改</Pill> : null}
                          {req.staleState && req.staleState !== "fresh" ? <StatusBadge status={req.staleState} /> : null}
                        </div>
                      </div>
                      <p>服装：{req.costume || "未指定"}</p>
                      <p>姿态：{req.pose || "未指定"}</p>
                      <p>表情：{req.expression || "未指定"}</p>
                      <p>动作：{req.action || "未指定"}</p>
                      <p>状态：{req.sceneState || req.propState || "未指定"}</p>
                      <div className="mt-2 flex flex-wrap gap-2">
                        <button className="text-blue-700" onClick={() => perform("确认派生资产需求", async () => void (await studioApi.reviewShotAssetRequirement(session, projectId, req.id, { reviewStatus: "approved" })))} type="button">
                          确认需求
                        </button>
                        <button className="text-amber-700" onClick={() => perform("标记派生资产需修改", async () => void (await studioApi.reviewShotAssetRequirement(session, projectId, req.id, { reviewStatus: "needs_edit" })))} type="button">
                          需修改
                        </button>
                        <button className="text-blue-700" onClick={() => setEditingRequirement(req)} type="button">
                          编辑需求
                        </button>
                        <button className="text-blue-700" onClick={() => perform("重新生成派生资产图", async () => void (await studioApi.regenerate(session, projectId, { targetType: "derived_asset_image", targetId: req.id, options: { force: true } })))} type="button">
                          重生成派生图
                        </button>
                      </div>
                    </div>
                  ))}
                  {!shotRequirements.length ? <p className="text-sm text-slate-500">暂无派生资产需求。</p> : null}
                </div>
              </Surface>
            );
          })}
        </div>
      ) : (
        <EmptyState title="还没有分镜" description="选择剧本后生成分镜，系统会展示镜头、参与资产和派生资产需求。" />
      )}
      <ShotEditDialog
        busy={busy !== ""}
        shot={editingShot}
        onClose={() => setEditingShot(null)}
        onSave={(body) =>
          perform("保存镜头修订", async () => {
            if (!editingShot) {
              return;
            }
            await studioApi.updateStoryboardShot(session, projectId, editingShot.id, body);
            setEditingShot(null);
          })
        }
      />
      <RequirementEditDialog
        busy={busy !== ""}
        requirement={editingRequirement}
        onClose={() => setEditingRequirement(null)}
        onSave={(body) =>
          perform("保存派生资产需求修订", async () => {
            if (!editingRequirement) {
              return;
            }
            await studioApi.updateShotAssetRequirement(session, projectId, editingRequirement.id, body);
            setEditingRequirement(null);
          })
        }
      />
    </SessionGate>
  );
}

function AssetEditDialog({
  asset,
  busy,
  onClose,
  onSave,
}: {
  asset: CanonicalAsset | null;
  busy: boolean;
  onClose: () => void;
  onSave: (body: JsonRecord) => Promise<void>;
}) {
  if (!asset) {
    return null;
  }
  return <AssetEditDialogForm key={asset.id} asset={asset} busy={busy} onClose={onClose} onSave={onSave} />;
}

function AssetEditDialogForm({
  asset,
  busy,
  onClose,
  onSave,
}: {
  asset: CanonicalAsset;
  busy: boolean;
  onClose: () => void;
  onSave: (body: JsonRecord) => Promise<void>;
}) {
  const [form, setForm] = useState(assetEditForm(asset));
  const [localError, setLocalError] = useState("");

  async function submit() {
    setLocalError("");
    const parsedTraits = parseJsonRecordInput(form.visualTraits);
    if (parsedTraits.error) {
      setLocalError(parsedTraits.error);
      return;
    }
    if (!form.name.trim() || !form.description.trim()) {
      setLocalError("名称和描述不能为空");
      return;
    }
    await onSave(
      compactRecord({
        assetType: form.assetType,
        name: form.name,
        description: form.description,
        basePrompt: form.basePrompt,
        visualTraits: parsedTraits.value,
      }),
    );
  }

  return (
    <EditDialogShell title="编辑基础资产" error={localError} onClose={onClose}>
      <div className="grid gap-3">
        <SelectInput label="类型" value={form.assetType} values={["character", "scene", "prop"]} labels={{ character: "角色", scene: "场景", prop: "道具" }} onChange={(assetType) => setForm({ ...form, assetType })} />
        <TextInput label="名称" value={form.name} onChange={(name) => setForm({ ...form, name })} />
        <TextAreaInput label="描述" rows={4} value={form.description} onChange={(description) => setForm({ ...form, description })} />
        <TextAreaInput label="基础提示词" rows={4} value={form.basePrompt} onChange={(basePrompt) => setForm({ ...form, basePrompt })} />
        <TextAreaInput label="视觉 traits JSON" rows={7} value={form.visualTraits} onChange={(visualTraits) => setForm({ ...form, visualTraits })} />
        <div className="flex justify-end gap-2">
          <button className="studio-button" disabled={busy} onClick={onClose} type="button">
            取消
          </button>
          <button className="studio-button studio-button-primary" disabled={busy} onClick={submit} type="button">
            {busy ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}
            保存修订
          </button>
        </div>
      </div>
    </EditDialogShell>
  );
}

function ShotEditDialog({
  shot,
  busy,
  onClose,
  onSave,
}: {
  shot: StoryboardShot | null;
  busy: boolean;
  onClose: () => void;
  onSave: (body: JsonRecord) => Promise<void>;
}) {
  if (!shot) {
    return null;
  }
  return <ShotEditDialogForm key={shot.id} shot={shot} busy={busy} onClose={onClose} onSave={onSave} />;
}

function ShotEditDialogForm({
  shot,
  busy,
  onClose,
  onSave,
}: {
  shot: StoryboardShot;
  busy: boolean;
  onClose: () => void;
  onSave: (body: JsonRecord) => Promise<void>;
}) {
  const [form, setForm] = useState(shotEditForm(shot));
  const [localError, setLocalError] = useState("");

  async function submit() {
    setLocalError("");
    const durationSeconds = form.durationSeconds.trim() ? Number(form.durationSeconds) : undefined;
    if (durationSeconds !== undefined && (!Number.isFinite(durationSeconds) || durationSeconds <= 0)) {
      setLocalError("时长必须大于 0");
      return;
    }
    await onSave(
      compactRecord({
        visual: form.visual,
        camera: form.camera,
        motion: form.motion,
        mood: form.mood,
        durationSeconds,
        imagePrompt: form.imagePrompt,
        videoPrompt: form.videoPrompt,
      }),
    );
  }

  return (
    <EditDialogShell title={`编辑镜头 ${shot.shotNo}`} error={localError} onClose={onClose}>
      <div className="grid gap-3">
        <TextAreaInput label="画面描述" rows={4} value={form.visual} onChange={(visual) => setForm({ ...form, visual })} />
        <div className="grid gap-3 md:grid-cols-3">
          <TextInput label="运镜" value={form.camera} onChange={(camera) => setForm({ ...form, camera })} />
          <TextInput label="动作" value={form.motion} onChange={(motion) => setForm({ ...form, motion })} />
          <TextInput label="情绪" value={form.mood} onChange={(mood) => setForm({ ...form, mood })} />
        </div>
        <TextInput label="时长秒" value={form.durationSeconds} onChange={(durationSeconds) => setForm({ ...form, durationSeconds })} />
        <TextAreaInput label="图片提示词" rows={4} value={form.imagePrompt} onChange={(imagePrompt) => setForm({ ...form, imagePrompt })} />
        <TextAreaInput label="视频提示词" rows={4} value={form.videoPrompt} onChange={(videoPrompt) => setForm({ ...form, videoPrompt })} />
        <div className="flex justify-end gap-2">
          <button className="studio-button" disabled={busy} onClick={onClose} type="button">
            取消
          </button>
          <button className="studio-button studio-button-primary" disabled={busy} onClick={submit} type="button">
            {busy ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}
            保存修订
          </button>
        </div>
      </div>
    </EditDialogShell>
  );
}

function RequirementEditDialog({
  requirement,
  busy,
  onClose,
  onSave,
}: {
  requirement: ShotAssetRequirement | null;
  busy: boolean;
  onClose: () => void;
  onSave: (body: JsonRecord) => Promise<void>;
}) {
  if (!requirement) {
    return null;
  }
  return <RequirementEditDialogForm key={requirement.id} requirement={requirement} busy={busy} onClose={onClose} onSave={onSave} />;
}

function RequirementEditDialogForm({
  requirement,
  busy,
  onClose,
  onSave,
}: {
  requirement: ShotAssetRequirement;
  busy: boolean;
  onClose: () => void;
  onSave: (body: JsonRecord) => Promise<void>;
}) {
  const [form, setForm] = useState(requirementEditForm(requirement));

  async function submit() {
    await onSave(
      compactRecord({
        costume: form.costume,
        pose: form.pose,
        expression: form.expression,
        action: form.action,
        cameraRelation: form.cameraRelation,
        sceneState: form.sceneState,
        propState: form.propState,
        prompt: form.prompt,
      }),
    );
  }

  return (
    <EditDialogShell title="编辑派生资产需求" onClose={onClose}>
      <div className="grid gap-3">
        <div className="grid gap-3 md:grid-cols-2">
          <TextInput label="服装" value={form.costume} onChange={(costume) => setForm({ ...form, costume })} />
          <TextInput label="姿态" value={form.pose} onChange={(pose) => setForm({ ...form, pose })} />
          <TextInput label="表情" value={form.expression} onChange={(expression) => setForm({ ...form, expression })} />
          <TextInput label="动作" value={form.action} onChange={(action) => setForm({ ...form, action })} />
          <TextInput label="镜头关系" value={form.cameraRelation} onChange={(cameraRelation) => setForm({ ...form, cameraRelation })} />
          <TextInput label="场景状态" value={form.sceneState} onChange={(sceneState) => setForm({ ...form, sceneState })} />
          <TextInput label="道具状态" value={form.propState} onChange={(propState) => setForm({ ...form, propState })} />
        </div>
        <TextAreaInput label="派生图提示词" rows={5} value={form.prompt} onChange={(prompt) => setForm({ ...form, prompt })} />
        <div className="flex justify-end gap-2">
          <button className="studio-button" disabled={busy} onClick={onClose} type="button">
            取消
          </button>
          <button className="studio-button studio-button-primary" disabled={busy} onClick={submit} type="button">
            {busy ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}
            保存修订
          </button>
        </div>
      </div>
    </EditDialogShell>
  );
}

function EditDialogShell({ title, error = "", children, onClose }: { title: string; error?: string; children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4">
      <div className="max-h-[90vh] w-full max-w-3xl overflow-y-auto rounded-lg border border-slate-200 bg-slate-50 shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
          <h3 className="text-base font-semibold text-slate-900">{title}</h3>
          <button className="rounded-md p-1 text-slate-600 hover:bg-slate-200 hover:text-slate-900" onClick={onClose} type="button">
            <X size={18} />
          </button>
        </div>
        <div className="grid gap-3 p-4">
          <ErrorPanel message={error} />
          {children}
        </div>
      </div>
    </div>
  );
}

export function WorkflowsPage({ projectId }: { projectId: string }) {
  return (
    <AppShell active="projects" title="工作流" description="项目内部工作流入口，用于启动脚本驱动生产链路。" projectId={projectId} projectSection="workflows">
      <WorkflowsContent projectId={projectId} />
    </AppShell>
  );
}

function WorkflowsContent({ projectId }: { projectId: string }) {
  const { session } = useStudioSession();
  const sources = useStudioQuery<ProjectSource[]>([], `wf:sources:${projectId}`, async (activeSession) => (await studioApi.listSources(activeSession, projectId)).items);
  const scripts = useStudioQuery<Script[]>([], `wf:scripts:${projectId}`, async (activeSession) => (await studioApi.listScripts(activeSession, projectId)).items);
  const runs = useStudioQuery<WorkflowRun[]>([], `wf:runs:${projectId}`, async (activeSession) => (await studioApi.listWorkflowRuns(activeSession, projectId)).items);
  const [workflowType, setWorkflowType] = useState("full_production");
  const [sourceId, setSourceId] = useState("");
  const [scriptId, setScriptId] = useState("");
  const [selectedRunId, setSelectedRunId] = useState("");
  const [maxShots, setMaxShots] = useState("3");
  const [generateAssets, setGenerateAssets] = useState(true);
  const [generateDerivedAssets, setGenerateDerivedAssets] = useState(true);
  const [skipCompose, setSkipCompose] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const effectiveSourceId = validSelection(sourceId, sources.data);
  const effectiveScriptId = validSelection(scriptId, scripts.data);
  const effectiveRunId = validSelection(selectedRunId, runs.data);
  const nodes = useStudioQuery<WorkflowNodeRun[]>([], `wf:nodes:${effectiveRunId}`, async (activeSession) =>
    effectiveRunId ? (await studioApi.listWorkflowNodes(activeSession, effectiveRunId)).items : Promise.resolve([]),
  );

  async function startWorkflow() {
    setBusy("启动工作流");
    setError("");
    try {
      const input: JsonRecord = {};
      if (workflowType === "source_to_script") {
        input.sourceId = effectiveSourceId;
      }
      if (["script_to_assets", "script_to_storyboard", "script_to_video", "full_production"].includes(workflowType)) {
        input.scriptId = effectiveScriptId;
        input.maxShots = Number(maxShots);
        input.generateAssets = generateAssets;
        input.generateDerivedAssets = generateDerivedAssets;
        input.skipCompose = skipCompose;
      }
      if (workflowType === "video_production") {
        input.maxShots = Number(maxShots);
        input.skipCompose = skipCompose;
      }
      const run = await studioApi.createWorkflowRun(session, compactRecord({ projectId, workflowType, prompt: "", input }));
      setSelectedRunId(run.id);
      runs.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy("");
    }
  }

  return (
    <SessionGate>
      <div className="grid gap-5 xl:grid-cols-[360px_minmax(0,1fr)]">
        <Surface>
          <SectionTitle title="启动工作流" description="根据类型填写 sourceId、scriptId 和生产选项。" />
          <div className="grid gap-3 p-4">
            <SelectInput
              label="工作流类型"
              value={workflowType}
              values={["source_to_script", "script_to_assets", "script_to_storyboard", "script_to_video", "full_production", "video_production"]}
              labels={{
                source_to_script: "从原文生成剧本",
                script_to_assets: "分析剧本资产",
                script_to_storyboard: "生成分镜",
                script_to_video: "剧本生成视频",
                full_production: "完整生产",
                video_production: "兼容旧视频生产",
              }}
              onChange={setWorkflowType}
            />
            {workflowType === "source_to_script" ? <SelectFromList label="内容源" value={effectiveSourceId} items={sources.data} getLabel={(item) => item.title} onChange={setSourceId} /> : null}
            {["script_to_assets", "script_to_storyboard", "script_to_video", "full_production"].includes(workflowType) ? <SelectFromList label="剧本" value={effectiveScriptId} items={scripts.data} getLabel={(item) => item.title} onChange={setScriptId} /> : null}
            <TextInput label="最大镜头数" value={maxShots} onChange={setMaxShots} />
            <Toggle label="生成基础资产参考图" checked={generateAssets} onChange={setGenerateAssets} />
            <Toggle label="生成派生资产参考图" checked={generateDerivedAssets} onChange={setGenerateDerivedAssets} />
            <Toggle label="跳过最终合成" checked={skipCompose} onChange={setSkipCompose} />
            <button className="studio-button studio-button-primary" disabled={busy !== ""} onClick={startWorkflow} type="button">
              {busy ? <Loader2 className="animate-spin" size={16} /> : <Play size={16} />}
              启动工作流
            </button>
            <ErrorPanel message={error} />
          </div>
        </Surface>

        <Surface>
          <SectionTitle title="工作流列表" description="查看状态、输入摘要、输出摘要和节点详情。" />
          <div className="grid gap-0 divide-y divide-slate-200">
            {runs.data.map((run) => (
              <button className={cn("grid gap-3 px-4 py-3 text-left md:grid-cols-[1fr_auto_auto]", effectiveRunId === run.id ? "bg-blue-600/10" : "hover:bg-slate-50")} key={run.id} onClick={() => setSelectedRunId(run.id)} type="button">
                <div>
                  <p className="text-sm font-medium text-slate-900">{workflowLabel(stringFrom(run.input.workflowType))}</p>
                  <p className="mt-1 text-xs text-slate-500">{inputSummary(run.input)}</p>
                </div>
                <StatusBadge status={run.status} />
                <span className="text-xs text-slate-500">{formatTime(run.createdAt)}</span>
              </button>
            ))}
            {!runs.data.length ? <EmptyState title="暂无工作流" description="从左侧选择生产链路并启动。" /> : null}
          </div>
        </Surface>
      </div>
      <Surface className="mt-5">
        <SectionTitle title="节点详情" description="显示节点名称、状态、错误和输出摘要。" />
        <div className="grid gap-2 p-4">
          {nodes.data.map((node) => (
            <div className="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3 md:grid-cols-[1fr_auto]" key={node.id}>
              <div>
                <p className="text-sm font-medium text-slate-900">{node.nodeKey}</p>
                <p className="mt-1 text-xs text-slate-500">{node.nodeType}</p>
                {node.errorMessage ? <p className="mt-2 text-xs text-rose-200">{node.errorMessage}</p> : null}
              </div>
              <StatusBadge status={node.status} />
            </div>
          ))}
          {!nodes.data.length ? <EmptyState title="未选择节点" description="选择一个工作流后，节点详情会显示在这里。" /> : null}
        </div>
      </Surface>
    </SessionGate>
  );
}

export function VaultPage({ projectId }: { projectId: string }) {
  return (
    <AppShell active="projects" title="媒体资产" description="查看项目内产物、参考图、分镜 JSON、镜头图片、镜头视频和最终成片。" projectId={projectId} projectSection="vault">
      <VaultContent projectId={projectId} />
    </AppShell>
  );
}

function VaultContent({ projectId }: { projectId: string }) {
  const artifacts = useStudioQuery<Artifact[]>([], `vault:${projectId}`, async (activeSession) => (await studioApi.listArtifacts(activeSession, projectId)).items);
  const [query, setQuery] = useState("");
  const [type, setType] = useState("all");
  const priority = ["final_video", "generated_video", "generated_image"];
  const sorted = [...artifacts.data].sort((a, b) => priorityIndex(a.type, priority) - priorityIndex(b.type, priority));
  const filtered = sorted.filter((artifact) => {
    const matchesType = type === "all" || artifact.type === type;
    const text = `${artifact.type} ${artifact.storageKey ?? ""}`.toLowerCase();
    return matchesType && text.includes(query.toLowerCase());
  });
  const types = Array.from(new Set(artifacts.data.map((item) => item.type)));

  return (
    <SessionGate>
      <Surface className="mb-5 p-4">
        <div className="grid gap-3 md:grid-cols-[1fr_220px]">
          <input className="studio-input" placeholder="搜索类型或存储键" value={query} onChange={(event) => setQuery(event.target.value)} />
          <select className="studio-input" value={type} onChange={(event) => setType(event.target.value)}>
            <option value="all">全部类型</option>
            {types.map((item) => (
              <option key={item} value={item}>
                {artifactTypeLabel(item)}
              </option>
            ))}
          </select>
        </div>
      </Surface>
      {filtered.length ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((artifact) => (
            <Surface className="overflow-hidden" key={artifact.id}>
              <MediaPreview artifact={artifact} />
              <div className="grid gap-2 p-4">
                <p className="font-medium text-slate-900">{artifactTypeLabel(artifact.type)}</p>
                <p className="truncate text-xs text-slate-500">{artifact.storageKey ?? "无存储键"}</p>
                <div className="flex flex-wrap gap-2">
                  {artifact.previewUrl ? (
                    <a className="studio-button" href={artifact.previewUrl} rel="noreferrer" target="_blank">
                      打开预览链接
                    </a>
                  ) : null}
                  <button className="studio-button" onClick={() => navigator.clipboard.writeText(artifact.storageKey ?? "")} type="button">
                    <Copy size={16} />
                    复制存储键
                  </button>
                </div>
              </div>
            </Surface>
          ))}
        </div>
      ) : (
        <EmptyState title="还没有媒体资产" description="生成资产参考图、分镜、镜头图片或最终视频后会出现在这里。" />
      )}
    </SessionGate>
  );
}

export function ProjectSettingsPage({ projectId }: { projectId: string }) {
  return (
    <AppShell active="projects" title="项目设置" description="编辑项目类型、内容类型、视频比例、画风手册和默认模型配置。" projectId={projectId} projectSection="settings">
      <ProjectSettingsContent projectId={projectId} />
    </AppShell>
  );
}

function ProjectSettingsContent({ projectId }: { projectId: string }) {
  const { session } = useStudioSession();
  const project = useStudioQuery<Project | null>(null, `settings:${projectId}`, async (activeSession) => studioApi.getProject(activeSession, projectId));
  const [draft, setDraft] = useState<Partial<Project>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const form = project.data ? { ...project.data, ...draft } : null;

  function updateDraft(patch: Partial<Project>) {
    setDraft((current) => ({ ...current, ...patch }));
  }

  async function save() {
    if (!form) {
      return;
    }
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await studioApi.updateProject(session, projectId, compactRecord({
        name: form.name,
        description: nullable(form.description ?? ""),
        projectType: form.projectType ?? "",
        contentType: form.contentType ?? "",
        videoRatio: form.videoRatio ?? "16:9",
        artStyle: form.artStyle ?? "",
        directorManual: form.directorManual ?? "",
        visualManual: form.visualManual ?? "",
        imageQuality: form.imageQuality ?? "standard",
        productionMode: form.productionMode ?? "silent_video",
        imageModelProfileKey: form.imageModelProfileKey ?? "image_generation_default",
        videoModelProfileKey: form.videoModelProfileKey ?? "video_generation_default",
        scriptModelProfileKey: form.scriptModelProfileKey ?? "script_agent_default",
      }));
      setDraft({});
      setNotice("项目设置已保存。");
      project.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  if (!form) {
    return (
      <SessionGate>
        <QueryBody state={project}>{null}</QueryBody>
      </SessionGate>
    );
  }

  return (
    <SessionGate>
      <Surface>
        <SectionTitle title="项目设置" description="这些字段会被后续 Workflow 和 Prompt 读取。" />
        <div className="grid gap-4 p-5 md:grid-cols-2">
          <TextInput label="项目名称" value={form.name ?? ""} onChange={(name) => updateDraft({ name })} />
          <TextInput label="项目类型" value={form.projectType ?? ""} onChange={(projectType) => updateDraft({ projectType })} />
          <TextInput label="内容类型" value={form.contentType ?? ""} onChange={(contentType) => updateDraft({ contentType })} />
          <TextInput label="视频比例" value={form.videoRatio ?? ""} onChange={(videoRatio) => updateDraft({ videoRatio })} />
          <TextInput label="画风风格" value={form.artStyle ?? ""} onChange={(artStyle) => updateDraft({ artStyle })} />
          <TextInput label="图片质量" value={form.imageQuality ?? ""} onChange={(imageQuality) => updateDraft({ imageQuality })} />
          <TextInput label="生产模式" value={form.productionMode ?? ""} onChange={(productionMode) => updateDraft({ productionMode })} />
          <TextInput label="默认图片模型配置" value={form.imageModelProfileKey ?? ""} onChange={(imageModelProfileKey) => updateDraft({ imageModelProfileKey })} />
          <TextInput label="默认视频模型配置" value={form.videoModelProfileKey ?? ""} onChange={(videoModelProfileKey) => updateDraft({ videoModelProfileKey })} />
          <TextInput label="默认脚本模型配置" value={form.scriptModelProfileKey ?? ""} onChange={(scriptModelProfileKey) => updateDraft({ scriptModelProfileKey })} />
          <TextAreaInput className="md:col-span-2" label="项目简介" value={form.description ?? ""} onChange={(description) => updateDraft({ description })} />
          <TextAreaInput className="md:col-span-2" label="导演手册" value={form.directorManual ?? ""} onChange={(directorManual) => updateDraft({ directorManual })} />
          <TextAreaInput className="md:col-span-2" label="视觉手册" value={form.visualManual ?? ""} onChange={(visualManual) => updateDraft({ visualManual })} />
        </div>
        <div className="flex items-center justify-between gap-3 border-t border-slate-200 p-4">
          <div>
            <ErrorPanel message={error} />
            {notice ? <p className="text-sm text-blue-700">{notice}</p> : null}
          </div>
          <button className="studio-button studio-button-primary" disabled={busy} onClick={save} type="button">
            {busy ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}
            保存设置
          </button>
        </div>
      </Surface>
    </SessionGate>
  );
}

export function ProvidersPage() {
  return (
    <AppShell active="providers" title="供应商中心" description="管理供应商账号与模型配置，不展示成本大盘或熔断监控。">
      <ProvidersContent />
    </AppShell>
  );
}

function ProvidersContent() {
  const { session } = useStudioSession();
  const accounts = useStudioQuery<ProviderAccount[]>([], "providers:accounts", async (session) => (await studioApi.listProviderAccounts(session)).items);
  const profiles = useStudioQuery<ModelProfile[]>([], "providers:profiles", async (session) => (await studioApi.listModelProfiles(session)).items);
  const [accountName, setAccountName] = useState("New API");
  const [accountBaseUrl, setAccountBaseUrl] = useState("https://api.openai.com/v1");
  const [accountApiKey, setAccountApiKey] = useState("");
  const [profileKey, setProfileKey] = useState("script_agent_default");
  const [profileName, setProfileName] = useState("脚本 Agent 默认配置");
  const [profilePurpose, setProfilePurpose] = useState("script");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function perform(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError("");
    setNotice("");
    try {
      await action();
      setNotice(`${label}已完成。`);
      accounts.reload();
      profiles.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy("");
    }
  }

  return (
    <SessionGate>
      <div className="grid gap-5 xl:grid-cols-[360px_minmax(0,1fr)]">
        <Surface>
          <SectionTitle title="创建供应商" description="先接入兼容 OpenAI 的账号，再创建模型配置。" />
          <div className="grid gap-3 p-4">
            <TextInput label="账号名称" value={accountName} onChange={setAccountName} />
            <TextInput label="接口地址" value={accountBaseUrl} onChange={setAccountBaseUrl} />
            <TextInput label="访问密钥" value={accountApiKey} onChange={setAccountApiKey} />
            <button
              className="studio-button studio-button-primary"
              disabled={busy !== "" || !accountName.trim() || !accountBaseUrl.trim()}
              onClick={() =>
                perform("创建供应商账号", async () => {
                  await studioApi.createProviderAccount(
                    session,
                    compactRecord({
                      organizationId: session.organizationId,
                      connectorKey: "openai_compatible",
                      name: accountName,
                      baseUrl: accountBaseUrl,
                      authType: "bearer",
                      credential: accountApiKey.trim() ? { apiKey: accountApiKey.trim() } : undefined,
                      config: {},
                    }),
                  );
                  setAccountApiKey("");
                })
              }
              type="button"
            >
              {busy === "创建供应商账号" ? <Loader2 className="animate-spin" size={16} /> : <Plus size={16} />}
              创建账号
            </button>
            <div className="h-px bg-slate-200" />
            <TextInput label="模型配置键" value={profileKey} onChange={setProfileKey} />
            <TextInput label="模型配置名称" value={profileName} onChange={setProfileName} />
            <TextInput label="用途" value={profilePurpose} onChange={setProfilePurpose} />
            <button
              className="studio-button"
              disabled={busy !== "" || !profileKey.trim() || !profileName.trim() || !profilePurpose.trim()}
              onClick={() =>
                perform("创建模型配置", async () => {
                  await studioApi.createModelProfile(
                    session,
                    compactRecord({
                      profileKey,
                      name: profileName,
                      purpose: profilePurpose,
                      routingStrategy: "priority_with_fallback",
                      fallbackStrategy: {},
                    }),
                  );
                })
              }
              type="button"
            >
              创建模型配置
            </button>
            <ErrorPanel message={error} />
            {notice ? <p className="text-sm text-blue-700">{notice}</p> : null}
          </div>
        </Surface>
        <Surface>
          <SectionTitle title="供应商账号" description="用于供应商网关的上游账号。" />
          <ListBlock items={accounts.data} empty="还没有供应商账号" render={(item) => <SimpleRow title={item.displayName || item.name || item.id} detail={item.providerType || "兼容 OpenAI"} status={item.status} />} />
        </Surface>
        <Surface className="xl:col-start-2">
          <SectionTitle title="模型配置" description="脚本、图片和视频生产通过模型配置路由。" />
          <ListBlock items={profiles.data} empty="还没有模型配置" render={(item) => <SimpleRow title={item.profileKey} detail={item.purpose || "未设置用途"} status={item.status || "active"} />} />
        </Surface>
      </div>
    </SessionGate>
  );
}

export function PromptsPage() {
  return (
    <AppShell active="prompts" title="提示词中心" description="查看脚本、资产、分镜和镜头生产所需的提示词模板。">
      <PromptsContent />
    </AppShell>
  );
}

function PromptsContent() {
  const { session } = useStudioSession();
  const templates = useStudioQuery<PromptTemplate[]>([], "prompts:templates", async (session) => (await studioApi.listPromptTemplates(session)).items);
  const [templateKey, setTemplateKey] = useState("storyboard_planner_custom");
  const [templateName, setTemplateName] = useState("分镜规划自定义提示词");
  const [purpose, setPurpose] = useState("storyboard");
  const [modality, setModality] = useState("text");
  const [taskType, setTaskType] = useState("storyboard_planning");
  const [selectedTemplateId, setSelectedTemplateId] = useState("");
  const [versionTitle, setVersionTitle] = useState("初始版本");
  const [versionContent, setVersionContent] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const effectiveTemplateId = validSelection(selectedTemplateId, templates.data);

  async function perform(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError("");
    setNotice("");
    try {
      await action();
      setNotice(`${label}已完成。`);
      templates.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy("");
    }
  }

  return (
    <SessionGate>
      <div className="grid gap-5 xl:grid-cols-[380px_minmax(0,1fr)]">
        <Surface>
          <SectionTitle title="创建提示词" description="为脚本、资产、分镜或镜头生产维护组织级提示词。" />
          <div className="grid gap-3 p-4">
            <TextInput label="模板键" value={templateKey} onChange={setTemplateKey} />
            <TextInput label="名称" value={templateName} onChange={setTemplateName} />
            <TextInput label="用途" value={purpose} onChange={setPurpose} />
            <TextInput label="模态" value={modality} onChange={setModality} />
            <TextInput label="任务类型" value={taskType} onChange={setTaskType} />
            <button
              className="studio-button studio-button-primary"
              disabled={busy !== "" || !templateKey.trim() || !templateName.trim() || !purpose.trim() || !modality.trim() || !taskType.trim()}
              onClick={() =>
                perform("创建提示词模板", async () => {
                  const created = await studioApi.createPromptTemplate(session, compactRecord({ templateKey, name: templateName, purpose, modality, taskType }));
                  setSelectedTemplateId(created.id);
                })
              }
              type="button"
            >
              创建模板
            </button>
            <div className="h-px bg-slate-200" />
            <SelectFromList label="选择模板" value={effectiveTemplateId} items={templates.data} getLabel={(item) => item.name || item.templateKey} onChange={setSelectedTemplateId} />
            <TextInput label="版本标题" value={versionTitle} onChange={setVersionTitle} />
            <TextAreaInput rows={8} label="提示词内容" value={versionContent} onChange={setVersionContent} />
            <button
              className="studio-button"
              disabled={!effectiveTemplateId || busy !== "" || !versionContent.trim()}
              onClick={() =>
                perform("创建并激活版本", async () => {
                  await studioApi.createPromptVersion(
                    session,
                    effectiveTemplateId,
                    compactRecord({
                      title: versionTitle,
                      content: versionContent,
                      contentFormat: "text",
                      variablesSchema: {},
                      metadata: {},
                      activate: true,
                    }),
                  );
                  setVersionContent("");
                })
              }
              type="button"
            >
              创建并激活版本
            </button>
            <ErrorPanel message={error} />
            {notice ? <p className="text-sm text-blue-700">{notice}</p> : null}
          </div>
        </Surface>
        <Surface>
          <SectionTitle title="提示词模板" description="生产链路中的提示词注册表版本入口。" />
          <ListBlock
            items={templates.data}
            empty="还没有提示词模板"
            render={(item) => <SimpleRow title={item.name || item.templateKey} detail={`${item.templateKey} · ${item.taskType || item.modality || "未设置任务"}`} status={item.status} />}
          />
        </Surface>
      </div>
    </SessionGate>
  );
}

export function AccessPage() {
  return (
    <AppShell active="access" title="权限管理" description="查看组织、工作区、团队、角色和权限。">
      <AccessContent />
    </AppShell>
  );
}

function AccessContent() {
  const { session } = useStudioSession();
  const organizations = useStudioQuery<Organization[]>([], "access:orgs", async (session) => (await studioApi.listOrganizations(session)).items);
  const workspaces = useStudioQuery<Workspace[]>([], "access:workspaces", async (session) => (await studioApi.listWorkspaces(session)).items);
  const teams = useStudioQuery<Team[]>([], "access:teams", async (session) => (await studioApi.listTeams(session)).items);
  const roles = useStudioQuery<Role[]>([], "access:roles", async (session) => (await studioApi.listRoles(session)).items);
  const permissions = useStudioQuery<Permission[]>([], "access:permissions", async (session) => (await studioApi.listPermissions(session)).items);
  const [teamName, setTeamName] = useState("");
  const [teamDescription, setTeamDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function createTeam() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await studioApi.createTeam(session, compactRecord({ name: teamName, description: nullable(teamDescription) }));
      setTeamName("");
      setTeamDescription("");
      setNotice("团队已创建。");
      teams.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <SessionGate>
      <div className="grid gap-5 xl:grid-cols-2">
        <Surface>
          <SectionTitle title="创建团队" description="先创建团队，再在后续权限策略中绑定角色。" />
          <div className="grid gap-3 p-4">
            <TextInput label="团队名称" value={teamName} onChange={setTeamName} />
            <TextAreaInput label="团队说明" value={teamDescription} onChange={setTeamDescription} />
            <button className="studio-button studio-button-primary" disabled={busy || !teamName.trim()} onClick={createTeam} type="button">
              {busy ? <Loader2 className="animate-spin" size={16} /> : <Plus size={16} />}
              创建团队
            </button>
            <ErrorPanel message={error} />
            {notice ? <p className="text-sm text-blue-700">{notice}</p> : null}
          </div>
        </Surface>
        <Surface>
          <SectionTitle title="组织与工作区" />
          <div className="grid gap-3 p-4">
            {organizations.data.map((item) => (
              <SimpleRow key={item.id} title={item.name} detail={item.id} status="active" />
            ))}
            {workspaces.data.map((item) => (
              <SimpleRow key={item.id} title={item.name} detail={`工作区 · ${item.id}`} status="active" />
            ))}
          </div>
        </Surface>
        <Surface>
          <SectionTitle title="团队与角色" />
          <div className="grid gap-3 p-4">
            {teams.data.map((item) => (
              <SimpleRow key={item.id} title={item.name} detail="团队" status={item.status} />
            ))}
            {roles.data.map((item) => (
              <SimpleRow key={item.id} title={item.name || item.roleKey} detail={item.roleKey} status="active" />
            ))}
          </div>
        </Surface>
        <Surface className="xl:col-span-2">
          <SectionTitle title="权限" description="细粒度 RBAC 权限列表。" />
          <div className="grid gap-2 p-4 md:grid-cols-2 xl:grid-cols-3">
            {permissions.data.map((item) => (
              <div className="rounded-md border border-slate-200 bg-slate-50 p-3" key={item.permissionKey}>
                <p className="text-sm font-medium text-slate-900">{item.name || item.permissionKey}</p>
                <p className="mt-1 text-xs text-slate-500">{item.description || item.permissionKey}</p>
              </div>
            ))}
          </div>
        </Surface>
      </div>
    </SessionGate>
  );
}

export function GlobalSettingsPage() {
  return (
    <AppShell active="settings" title="设置" description="查看当前账号、组织和本机登录状态。">
      <SettingsContent />
    </AppShell>
  );
}

function SettingsContent() {
  const router = useRouter();
  const { session, clearSession } = useStudioSession();
  const details = useSessionDetails();

  async function logout() {
    if (session.refreshToken.trim()) {
      await studioApi.logout(session.refreshToken).catch(() => undefined);
    }
    clearSession();
    router.replace("/login" as Route);
  }

  return (
    <SessionGate>
      <Surface>
        <SectionTitle title="账号信息" description="当前浏览器保存的是登录会话，不再需要手动维护认证信息。" />
        <div className="grid gap-4 p-4 md:grid-cols-2">
          <InfoTile label="显示名称" value={session.user?.displayName || "未设置"} />
          <InfoTile label="邮箱" value={session.user?.email || "未设置"} />
          <InfoTile label="当前组织" value={details.organizationName || (session.organizationId ? "已连接" : "未连接")} />
          <InfoTile label="当前工作区" value={details.workspaceName || (session.workspaceId ? "已连接" : "未连接")} />
          <div className="md:col-span-2">
            <button className="studio-button" onClick={logout} type="button">
              <X size={16} />
              退出登录
            </button>
          </div>
        </div>
      </Surface>
    </SessionGate>
  );
}

function SessionGate({ children }: { children: React.ReactNode }) {
  const { hydrated, ready } = useStudioSession();
  if (!hydrated) {
    return <LoadingPanel />;
  }
  if (!ready) {
    return <LoadingPanel />;
  }
  return <>{children}</>;
}

function useStudioQuery<TData>(initial: TData, key: string, loader: (session: ReturnType<typeof useStudioSession>["session"]) => Promise<TData>): QueryState<TData> {
  const { session, hydrated, ready } = useStudioSession();
  const initialRef = useRef(initial);
  const loaderRef = useRef(loader);
  const [reloadIndex, setReloadIndex] = useState(0);
  const [data, setData] = useState<TData>(initial);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    loaderRef.current = loader;
  }, [loader]);

  useEffect(() => {
    if (!hydrated || !ready) {
      setData(initialRef.current);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError("");
    loaderRef
      .current(session)
      .then((result) => {
        if (!cancelled) {
          setData(result);
        }
      })
      .catch((cause: unknown) => {
        if (!cancelled) {
          setError(errorMessage(cause));
          setData(initialRef.current);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [hydrated, key, ready, reloadIndex, session]);

  return { data, loading, error, reload: () => setReloadIndex((value) => value + 1) };
}

function QueryBody<TData>({ state, children }: { state: QueryState<TData>; children: React.ReactNode }) {
  if (state.loading) {
    return <LoadingPanel />;
  }
  if (state.error) {
    return <div className="p-4"><ErrorPanel message={state.error} /></div>;
  }
  return <>{children}</>;
}

function validSelection<TItem extends { id: string }>(selectedId: string, items: TItem[]) {
  if (selectedId && items.some((item) => item.id === selectedId)) {
    return selectedId;
  }
  return items[0]?.id ?? "";
}

function LoadingPanel() {
  return (
    <div className="grid min-h-40 place-items-center text-sm text-slate-500">
      <span className="inline-flex items-center gap-2">
        <Loader2 className="animate-spin" size={16} />
        正在加载
      </span>
    </div>
  );
}

function ProjectCard({ project }: { project: Project }) {
  return (
    <Link className="group rounded-lg border border-slate-200 bg-slate-50 p-4 transition hover:border-blue-600/40 hover:bg-slate-50" href={projectHref(project.id) as Route}>
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-base font-semibold text-slate-900">{project.name}</h3>
          <p className="mt-2 line-clamp-2 text-sm leading-6 text-slate-600">{project.description || "暂无简介"}</p>
        </div>
        <StatusBadge status={project.status ?? "active"} />
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <Pill>{project.projectType || "未设置类型"}</Pill>
        <Pill>{project.contentType || "未设置内容"}</Pill>
        <Pill>{project.videoRatio || project.aspectRatio || "16:9"}</Pill>
        <Pill>{project.artStyle || "未设置画风"}</Pill>
      </div>
      <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-slate-200">
        <div className="h-full w-2/5 rounded-full bg-blue-600 transition group-hover:w-3/5" />
      </div>
      <div className="mt-4 flex items-center justify-between text-xs text-slate-500">
        <span>最近更新：{formatTime(project.updatedAt)}</span>
        <span className="inline-flex items-center gap-1 text-blue-700">
          打开项目 <ArrowRight size={13} />
        </span>
      </div>
    </Link>
  );
}

function SummaryTile({ label, value, detail }: { label: string; value: string | number; detail: string }) {
  return (
    <Surface className="p-4">
      <p className="text-sm text-slate-500">{label}</p>
      <p className="mt-2 text-2xl font-semibold text-slate-900">{value}</p>
      <p className="mt-1 text-xs text-slate-500">{detail}</p>
    </Surface>
  );
}

function ProgressStep({ done, title, detail }: { done: boolean; title: string; detail: string }) {
  return (
    <div className={cn("rounded-lg border p-3", done ? "border-blue-600/35 bg-blue-600/10" : "border-slate-200 bg-slate-50")}>
      <div className="flex items-center gap-2">
        <span className={cn("grid h-6 w-6 place-items-center rounded-md", done ? "bg-blue-600 text-white" : "bg-slate-200 text-slate-500")}>{done ? <Check size={14} /> : <X size={14} />}</span>
        <p className="text-sm font-medium text-slate-900">{title}</p>
      </div>
      <p className="mt-2 text-xs leading-5 text-slate-500">{detail}</p>
    </div>
  );
}

function AssetRow({ asset }: { asset: CanonicalAsset }) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
      <div>
        <p className="text-sm font-medium text-slate-900">{asset.name}</p>
        <p className="mt-1 text-xs text-slate-500">{assetTypeLabel(asset.assetType)} · {asset.description}</p>
      </div>
      <StatusBadge status={asset.status} />
    </div>
  );
}

function ArtifactRow({ artifact }: { artifact: Artifact }) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
      <div className="min-w-0">
        <p className="text-sm font-medium text-slate-900">{artifactTypeLabel(artifact.type)}</p>
        <p className="mt-1 truncate text-xs text-slate-500">{artifact.storageKey ?? artifact.id}</p>
      </div>
      {artifact.previewUrl ? (
        <a className="text-xs text-blue-700" href={artifact.previewUrl} rel="noreferrer" target="_blank">
          预览
        </a>
      ) : null}
    </div>
  );
}

function WorkflowRow({ run }: { run: WorkflowRun }) {
  return (
    <div className="grid gap-3 p-4 md:grid-cols-[1fr_auto]">
      <div>
        <p className="font-medium text-slate-900">{workflowLabel(stringFrom(run.input.workflowType))}</p>
        <p className="mt-1 text-xs text-slate-500">{run.temporalWorkflowId}</p>
        <p className="mt-2 text-sm text-slate-600">{inputSummary(run.input)}</p>
      </div>
      <StatusBadge status={run.status} />
    </div>
  );
}

function InfoTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-slate-50 p-3">
      <p className="text-xs font-medium text-slate-500">{label}</p>
      <p className="mt-1 truncate text-sm font-medium text-slate-950">{value}</p>
    </div>
  );
}

function SimpleRow({ title, detail, status }: { title: string; detail: string; status: string }) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-slate-900">{title}</p>
        <p className="mt-1 truncate text-xs text-slate-500">{detail}</p>
      </div>
      <StatusBadge status={status} />
    </div>
  );
}

function ListBlock<TItem>({ items, empty, render }: { items: TItem[]; empty: string; render: (item: TItem) => React.ReactNode }) {
  return (
    <div className="grid gap-3 p-4">
      {items.map((item, index) => (
        <div key={index}>{render(item)}</div>
      ))}
      {!items.length ? <EmptyState title={empty} description="请使用本页创建入口完成初始化，或先确认当前会话是否有管理权限。" /> : null}
    </div>
  );
}

function TextInput({ label, value, onChange, className = "" }: { label: string; value: string; onChange: (value: string) => void; className?: string }) {
  return (
    <label className={`grid gap-1 text-sm ${className}`}>
      <span className="text-slate-500">{label}</span>
      <input className="studio-input w-full" value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function TextAreaInput({ label, value, onChange, rows = 5, className = "" }: { label: string; value: string; onChange: (value: string) => void; rows?: number; className?: string }) {
  return (
    <label className={`grid gap-1 text-sm ${className}`}>
      <span className="text-slate-500">{label}</span>
      <textarea className="studio-textarea w-full resize-y" rows={rows} value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function SelectInput({ label, value, values, labels, onChange }: { label: string; value: string; values: string[]; labels?: Record<string, string>; onChange: (value: string) => void }) {
  return (
    <label className="grid gap-1 text-sm">
      <span className="text-slate-500">{label}</span>
      <select className="studio-input w-full" value={value} onChange={(event) => onChange(event.target.value)}>
        {values.map((item) => (
          <option key={item} value={item}>
            {labels?.[item] ?? item}
          </option>
        ))}
      </select>
    </label>
  );
}

function SelectFromList<TItem extends { id: string }>({ label, value, items, getLabel, onChange }: { label: string; value: string; items: TItem[]; getLabel: (item: TItem) => string; onChange: (value: string) => void }) {
  return (
    <label className="grid gap-1 text-sm">
      <span className="text-slate-500">{label}</span>
      <select className="studio-input w-full" value={value} onChange={(event) => onChange(event.target.value)}>
        <option value="">请选择</option>
        {items.map((item) => (
          <option key={item.id} value={item.id}>
            {getLabel(item)}
          </option>
        ))}
      </select>
    </label>
  );
}

function Toggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="flex items-center justify-between gap-4 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-700">
      {label}
      <input checked={checked} onChange={(event) => onChange(event.target.checked)} type="checkbox" />
    </label>
  );
}

function Meta({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt className="text-slate-500">{label}</dt>
      <dd className="mt-1 text-slate-700">{value || "未设置"}</dd>
    </div>
  );
}

function Pill({ children }: { children: React.ReactNode }) {
  return <span className="rounded-md border border-slate-200 bg-slate-50 px-2 py-1 text-[12px] text-slate-600">{children}</span>;
}

function productionActionLabel(action: string) {
  switch (action) {
    case "generate_script":
      return "生成剧本";
    case "analyze_assets":
      return "分析剧本资产";
    case "generate_asset_images":
      return "生成基础资产参考图";
    case "generate_storyboard":
      return "生成分镜";
    case "analyze_shot_assets":
      return "分析派生资产";
    case "generate_derived_asset_images":
      return "生成派生参考图";
    case "generate_shot_images":
      return "生成镜头图片";
    case "generate_shot_videos":
      return "生成镜头视频";
    case "compose_final_video":
      return "合成最终成片";
    case "run_full_production":
      return "完整生产";
    default:
      return "生产动作";
  }
}

function productionStageLabel(stage: string) {
  switch (stage) {
    case "source":
      return "内容源";
    case "assets":
      return "基础资产";
    case "storyboard":
      return "分镜";
    case "shot_assets":
      return "派生资产";
    case "shot_images":
      return "镜头图片";
    case "shot_videos":
      return "镜头视频";
    case "final_video":
      return "最终成片";
    default:
      return stage || "未开始";
  }
}

function productionStageDescription(stage: string, status: string) {
  if (status === "needs_review") {
    return "仍有未确认内容。可以继续运行后续阶段，但建议先检查并确认输出。";
  }
  if (status === "failed") {
    return "本阶段存在失败项，可以修正输入后重新运行该阶段。";
  }
  if (status === "running") {
    return "本阶段正在运行，完成后看板会显示新的数量和状态。";
  }
  switch (stage) {
    case "source":
      return "导入小说原文或剧本原文，也可以让 Agent 从原文生成可生产剧本。";
    case "assets":
      return "从剧本中沉淀角色、场景和道具，并为缺失项目生成基础参考图。";
    case "storyboard":
      return "根据当前剧本生成镜头列表，确认镜头后进入派生资产和镜头生产。";
    case "shot_assets":
      return "分析每个镜头需要的角色服装、姿态、场景状态和道具变化。";
    case "shot_images":
      return "为每个镜头生成静态图，作为后续视频生成的视觉依据。";
    case "shot_videos":
      return "基于镜头图片生成短视频片段，失败项可在本阶段重跑。";
    case "final_video":
      return "将已完成的镜头视频合成为最终成片，并在 Vault 中保存。";
    default:
      return "按阶段推进生产流程。";
  }
}

function nextProductionAction(status: ProductionStatus) {
  switch (status.overall.stage) {
    case "source":
      return status.stages.source.novelSourceCount + status.stages.source.scriptSourceCount > 0 ? "generate_script" : "";
    case "assets":
      return status.stages.assets.missingReferenceImageCount > 0 && status.stages.assets.pendingReviewCount === 0 ? "generate_asset_images" : "analyze_assets";
    case "storyboard":
      return "generate_storyboard";
    case "shot_assets":
      return status.stages.shotAssets.missingDerivedImageCount > 0 && status.stages.shotAssets.pendingReviewCount === 0 ? "generate_derived_asset_images" : "analyze_shot_assets";
    case "shot_images":
      return "generate_shot_images";
    case "shot_videos":
      return "generate_shot_videos";
    case "final_video":
      return status.stages.finalVideo.status === "ready" ? "" : "compose_final_video";
    default:
      return "";
  }
}

function metricText(label: string, value: number) {
  return `${label}：${value}`;
}

function shotMediaMetrics(stage: ProductionStatus["stages"]["shotImages"]) {
  return [
    metricText("总数", stage.total),
    metricText("已完成", stage.succeeded),
    metricText("运行中", stage.running),
    metricText("失败", stage.failed),
    metricText("待生成", stage.pending),
    metricText("已过期", stage.stale),
  ];
}

function assetEditForm(asset: CanonicalAsset | null) {
  return {
    assetType: asset?.assetType ?? "character",
    name: asset?.name ?? "",
    description: asset?.description ?? "",
    basePrompt: asset?.basePrompt ?? "",
    visualTraits: jsonRecordInputText(asset?.visualTraits),
  };
}

function shotEditForm(shot: StoryboardShot | null) {
  return {
    visual: shot?.visual ?? "",
    camera: shot?.camera ?? "",
    motion: shot?.motion ?? "",
    mood: shot?.mood ?? "",
    durationSeconds: shot?.durationSeconds ? String(shot.durationSeconds) : "",
    imagePrompt: shot?.imagePrompt ?? "",
    videoPrompt: shot?.videoPrompt ?? "",
  };
}

function requirementEditForm(requirement: ShotAssetRequirement | null) {
  return {
    costume: requirement?.costume ?? "",
    pose: requirement?.pose ?? "",
    expression: requirement?.expression ?? "",
    action: requirement?.action ?? "",
    cameraRelation: requirement?.cameraRelation ?? "",
    sceneState: requirement?.sceneState ?? "",
    propState: requirement?.propState ?? "",
    prompt: requirement?.prompt ?? "",
  };
}

function jsonRecordInputText(value?: JsonRecord) {
  return JSON.stringify(value ?? {}, null, 2);
}

function parseJsonRecordInput(raw: string): { value?: JsonRecord; error?: string } {
  const trimmed = raw.trim();
  if (!trimmed) {
    return { value: {} };
  }
  try {
    const parsed = JSON.parse(trimmed) as JsonValue;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return { error: "JSON 必须是对象" };
    }
    return { value: parsed as JsonRecord };
  } catch (cause) {
    return { error: cause instanceof Error ? cause.message : "JSON 解析失败" };
  }
}

function compactRecord(record: Record<string, unknown>): JsonRecord {
  const out: JsonRecord = {};
  for (const [key, value] of Object.entries(record)) {
    if (value === undefined) {
      continue;
    }
    if (value && typeof value === "object" && !Array.isArray(value)) {
      out[key] = compactRecord(value as Record<string, unknown>);
      continue;
    }
    out[key] = value as JsonValue;
  }
  return out;
}

function nullable(value: string | null | undefined) {
  const trimmed = String(value ?? "").trim();
  return trimmed ? trimmed : null;
}

function formatTime(value?: string) {
  if (!value) {
    return "暂无";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date);
}

function stringFrom(value: unknown) {
  return typeof value === "string" ? value : "";
}

function inputSummary(input: JsonRecord) {
  const workflowType = stringFrom(input.workflowType);
  const nested = input.input && typeof input.input === "object" && !Array.isArray(input.input) ? (input.input as JsonRecord) : {};
  const scriptId = stringFrom(nested.scriptId);
  const sourceId = stringFrom(nested.sourceId);
  return [workflowType ? workflowLabel(workflowType) : "", scriptId ? `剧本 ${scriptId}` : "", sourceId ? `内容源 ${sourceId}` : ""].filter(Boolean).join(" · ") || "无输入摘要";
}

function assetTypeLabel(type?: string) {
  switch (type) {
    case "character":
      return "角色";
    case "scene":
      return "场景";
    case "prop":
      return "道具";
    default:
      return type || "资产";
  }
}

function artifactTypeLabel(type: string) {
  switch (type) {
    case "final_video":
      return "最终成片";
    case "generated_video":
      return "镜头视频";
    case "generated_image":
      return "生成图片";
    case "storyboard_json":
      return "分镜 JSON";
    default:
      return type;
  }
}

function priorityIndex(value: string, priority: string[]) {
  const index = priority.indexOf(value);
  return index === -1 ? priority.length : index;
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : "请求失败，请稍后重试。";
}

"use client";

import { ArrowLeft, ArrowRight, CheckCircle2, FileText, Film, Loader2, Palette, Settings2 } from "lucide-react";
import Link from "next/link";
import { FormEvent, useMemo, useState } from "react";

const apiBase = trimTrailingSlash(process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080");

const steps = [
  { key: "basic", label: "基础信息", icon: FileText },
  { key: "video", label: "视频设定", icon: Film },
  { key: "style", label: "风格设定", icon: Palette },
  { key: "source", label: "内容导入", icon: Settings2 },
] as const;

type WizardState = {
  apiToken: string;
  organizationId: string;
  workspaceId: string;
  name: string;
  description: string;
  projectType: string;
  contentType: string;
  videoRatio: string;
  imageQuality: string;
  productionMode: string;
  artStyle: string;
  directorManual: string;
  visualManual: string;
  sourceType: "none" | "novel" | "script";
  sourceTitle: string;
  sourceContent: string;
  sourceFormat: "plain_text" | "markdown";
};

const initialState: WizardState = {
  apiToken: "",
  organizationId: "",
  workspaceId: "",
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
  sourceType: "none",
  sourceTitle: "",
  sourceContent: "",
  sourceFormat: "plain_text",
};

export default function NewProjectPage() {
  const [stepIndex, setStepIndex] = useState(0);
  const [form, setForm] = useState<WizardState>(initialState);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ projectId: string; sourceId?: string } | null>(null);
  const [error, setError] = useState("");
  const activeStep = steps[stepIndex];
  const canSubmit = useMemo(
    () => form.apiToken.trim() !== "" && form.organizationId.trim() !== "" && form.workspaceId.trim() !== "" && form.name.trim() !== "",
    [form.apiToken, form.name, form.organizationId, form.workspaceId],
  );

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setResult(null);
    if (!canSubmit) {
      setError("请填写 API Token、组织、工作区和项目名称。");
      return;
    }
    setBusy(true);
    try {
      const project = await postJSON<{ id: string }>("/api/projects", form, {
        workspaceId: form.workspaceId,
        name: form.name,
        description: emptyToNull(form.description),
        projectType: form.projectType,
        contentType: form.contentType,
        videoRatio: form.videoRatio,
        artStyle: form.artStyle,
        directorManual: form.directorManual,
        visualManual: form.visualManual,
        imageQuality: form.imageQuality,
        productionMode: form.productionMode,
      });
      let sourceId: string | undefined;
      if (form.sourceType !== "none" && form.sourceTitle.trim() !== "" && form.sourceContent.trim() !== "") {
        const source = await postJSON<{ id: string }>(`/api/projects/${project.id}/sources`, form, {
          sourceType: form.sourceType,
          title: form.sourceTitle,
          content: form.sourceContent,
          contentFormat: form.sourceFormat,
        });
        sourceId = source.id;
      }
      setResult({ projectId: project.id, sourceId });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "创建失败。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="min-h-screen bg-[var(--background)]">
      <header className="border-b border-[var(--line)] bg-white px-5 py-4">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3">
          <div>
            <Link className="inline-flex items-center gap-2 text-sm text-[var(--muted)]" href="/">
              <ArrowLeft size={16} />
              控制台
            </Link>
            <h1 className="mt-2 text-2xl font-semibold">新建项目</h1>
          </div>
          <div className="text-sm text-[var(--muted)]">项目设定 → 内容导入 → 剧本/资产/分镜生产</div>
        </div>
      </header>

      <form className="mx-auto grid max-w-6xl gap-5 px-5 py-6 lg:grid-cols-[240px_1fr]" onSubmit={submit}>
        <nav className="h-fit border border-[var(--line)] bg-white p-2" aria-label="项目创建步骤">
          {steps.map((step, index) => {
            const Icon = step.icon;
            const active = step.key === activeStep.key;
            return (
              <button
                className={`flex h-11 w-full items-center gap-3 rounded px-3 text-left text-sm ${
                  active ? "bg-[var(--foreground)] text-white" : "text-[var(--muted)] hover:bg-[var(--panel-soft)]"
                }`}
                key={step.key}
                onClick={() => setStepIndex(index)}
                type="button"
              >
                <Icon size={16} />
                {step.label}
              </button>
            );
          })}
        </nav>

        <section className="border border-[var(--line)] bg-white">
          <div className="border-b border-[var(--line)] px-5 py-4">
            <h2 className="text-base font-semibold">{activeStep.label}</h2>
          </div>

          <div className="grid gap-4 p-5">
            {activeStep.key === "basic" ? <BasicStep form={form} setForm={setForm} /> : null}
            {activeStep.key === "video" ? <VideoStep form={form} setForm={setForm} /> : null}
            {activeStep.key === "style" ? <StyleStep form={form} setForm={setForm} /> : null}
            {activeStep.key === "source" ? <SourceStep form={form} setForm={setForm} /> : null}
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-[var(--line)] px-5 py-4">
            <button
              className="inline-flex h-10 items-center gap-2 rounded border border-[var(--line)] px-3 text-sm disabled:opacity-50"
              disabled={stepIndex === 0}
              onClick={() => setStepIndex((value) => Math.max(0, value - 1))}
              type="button"
            >
              <ArrowLeft size={16} />
              上一步
            </button>
            <div className="flex flex-wrap items-center gap-3">
              {error ? <p className="text-sm text-[var(--rose)]">{error}</p> : null}
              {result ? (
                <a className="inline-flex h-10 items-center gap-2 rounded border border-[var(--line)] px-3 text-sm" href={`/projects/${result.projectId}`}>
                  <CheckCircle2 size={16} />
                  打开项目
                </a>
              ) : null}
              {stepIndex < steps.length - 1 ? (
                <button
                  className="inline-flex h-10 items-center gap-2 rounded bg-[var(--foreground)] px-3 text-sm font-medium text-white"
                  onClick={() => setStepIndex((value) => Math.min(steps.length - 1, value + 1))}
                  type="button"
                >
                  下一步
                  <ArrowRight size={16} />
                </button>
              ) : (
                <button className="inline-flex h-10 items-center gap-2 rounded bg-[var(--foreground)] px-3 text-sm font-medium text-white disabled:opacity-60" disabled={busy} type="submit">
                  {busy ? <Loader2 className="animate-spin" size={16} /> : <CheckCircle2 size={16} />}
                  创建项目
                </button>
              )}
            </div>
          </div>
        </section>
      </form>
    </main>
  );
}

function BasicStep({ form, setForm }: StepProps) {
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Field label="API Token" value={form.apiToken} onChange={(apiToken) => setForm({ ...form, apiToken })} type="password" />
      <Field label="组织 ID" value={form.organizationId} onChange={(organizationId) => setForm({ ...form, organizationId })} />
      <Field label="工作区 ID" value={form.workspaceId} onChange={(workspaceId) => setForm({ ...form, workspaceId })} />
      <Field label="项目名称" value={form.name} onChange={(name) => setForm({ ...form, name })} />
      <Field label="项目类型" value={form.projectType} onChange={(projectType) => setForm({ ...form, projectType })} />
      <Field label="内容类型" value={form.contentType} onChange={(contentType) => setForm({ ...form, contentType })} />
      <label className="grid gap-1 text-sm md:col-span-2">
        <span className="text-[var(--muted)]">项目简介</span>
        <textarea className="min-h-24 rounded border border-[var(--line)] px-3 py-2 outline-none focus:border-[var(--foreground)]" value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} />
      </label>
    </div>
  );
}

function VideoStep({ form, setForm }: StepProps) {
  return (
    <div className="grid gap-4 md:grid-cols-3">
      <SelectField label="视频比例" value={form.videoRatio} values={["16:9", "9:16", "1:1", "4:3"]} onChange={(videoRatio) => setForm({ ...form, videoRatio })} />
      <SelectField label="图片质量" value={form.imageQuality} values={["standard", "hd", "1K", "2K", "4K"]} onChange={(imageQuality) => setForm({ ...form, imageQuality })} />
      <SelectField label="生产模式" value={form.productionMode} values={["silent_video", "storyboard_only", "assets_only", "custom"]} onChange={(productionMode) => setForm({ ...form, productionMode })} />
    </div>
  );
}

function StyleStep({ form, setForm }: StepProps) {
  return (
    <div className="grid gap-4">
      <Field label="画风风格" value={form.artStyle} onChange={(artStyle) => setForm({ ...form, artStyle })} />
      <label className="grid gap-1 text-sm">
        <span className="text-[var(--muted)]">导演手册</span>
        <textarea className="min-h-28 rounded border border-[var(--line)] px-3 py-2 outline-none focus:border-[var(--foreground)]" value={form.directorManual} onChange={(event) => setForm({ ...form, directorManual: event.target.value })} />
      </label>
      <label className="grid gap-1 text-sm">
        <span className="text-[var(--muted)]">视觉手册</span>
        <textarea className="min-h-28 rounded border border-[var(--line)] px-3 py-2 outline-none focus:border-[var(--foreground)]" value={form.visualManual} onChange={(event) => setForm({ ...form, visualManual: event.target.value })} />
      </label>
    </div>
  );
}

function SourceStep({ form, setForm }: StepProps) {
  return (
    <div className="grid gap-4">
      <SelectField label="内容类型" value={form.sourceType} values={["none", "novel", "script"]} onChange={(sourceType) => setForm({ ...form, sourceType: sourceType as WizardState["sourceType"] })} />
      {form.sourceType !== "none" ? (
        <>
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="内容标题" value={form.sourceTitle} onChange={(sourceTitle) => setForm({ ...form, sourceTitle })} />
            <SelectField label="文本格式" value={form.sourceFormat} values={["plain_text", "markdown"]} onChange={(sourceFormat) => setForm({ ...form, sourceFormat: sourceFormat as WizardState["sourceFormat"] })} />
          </div>
          <label className="grid gap-1 text-sm">
            <span className="text-[var(--muted)]">正文</span>
            <textarea className="min-h-56 rounded border border-[var(--line)] px-3 py-2 outline-none focus:border-[var(--foreground)]" value={form.sourceContent} onChange={(event) => setForm({ ...form, sourceContent: event.target.value })} />
          </label>
        </>
      ) : null}
    </div>
  );
}

type StepProps = {
  form: WizardState;
  setForm: (form: WizardState) => void;
};

function Field({ label, value, onChange, type = "text" }: { label: string; value: string; onChange: (value: string) => void; type?: string }) {
  return (
    <label className="grid gap-1 text-sm">
      <span className="text-[var(--muted)]">{label}</span>
      <input className="h-10 rounded border border-[var(--line)] px-3 outline-none focus:border-[var(--foreground)]" type={type} value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function SelectField({ label, value, values, onChange }: { label: string; value: string; values: string[]; onChange: (value: string) => void }) {
  return (
    <label className="grid gap-1 text-sm">
      <span className="text-[var(--muted)]">{label}</span>
      <select className="h-10 rounded border border-[var(--line)] bg-white px-3 outline-none focus:border-[var(--foreground)]" value={value} onChange={(event) => onChange(event.target.value)}>
        {values.map((item) => (
          <option key={item} value={item}>
            {item}
          </option>
        ))}
      </select>
    </label>
  );
}

async function postJSON<TData>(path: string, form: WizardState, body: unknown): Promise<TData> {
  const response = await fetch(`${apiBase}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${form.apiToken.trim()}`,
      "X-Organization-Id": form.organizationId.trim(),
    },
    body: JSON.stringify(body),
  });
  const envelope = (await response.json()) as { data?: TData; error?: { message: string } };
  if (!response.ok || !envelope.data) {
    throw new Error(envelope.error?.message ?? `HTTP ${response.status}`);
  }
  return envelope.data;
}

function emptyToNull(value: string) {
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

function trimTrailingSlash(value: string) {
  return value.replace(/\/+$/, "");
}

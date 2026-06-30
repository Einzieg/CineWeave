import {
  Boxes,
  Clapperboard,
  FileText,
  FolderKanban,
  ImageIcon,
  Library,
  Settings2,
  Workflow,
} from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import type { ReactNode } from "react";

const projectNav = [
  { label: "项目概览", icon: FolderKanban },
  { label: "原文与剧本", icon: FileText },
  { label: "资产", icon: Boxes },
  { label: "分镜镜头", icon: Clapperboard },
  { label: "工作流", icon: Workflow },
  { label: "媒体资产", icon: Library },
  { label: "项目设置", icon: Settings2 },
];

export default async function ProjectWorkspacePage({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;

  return (
    <main className="min-h-screen bg-[var(--background)]">
      <header className="border-b border-[var(--line)] bg-white px-5 py-4">
        <div className="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-3">
          <div>
            <Link className="text-sm text-[var(--muted)]" href="/">
              控制台
            </Link>
            <h1 className="mt-2 text-2xl font-semibold">项目工作台</h1>
            <p className="mt-1 max-w-3xl text-sm text-[var(--muted)]">{projectId}</p>
          </div>
          <Link className="inline-flex h-10 items-center gap-2 rounded bg-[var(--foreground)] px-3 text-sm font-medium text-white" href={"/projects/new" as Route}>
            <FolderKanban size={16} />
            新建项目
          </Link>
        </div>
      </header>

      <div className="mx-auto grid max-w-7xl gap-5 px-5 py-6 lg:grid-cols-[240px_1fr]">
        <aside className="h-fit border border-[var(--line)] bg-white p-2">
          <nav className="grid gap-1" aria-label="项目导航">
            {projectNav.map((item, index) => {
              const Icon = item.icon;
              return (
                <a
                  className={`flex h-11 items-center gap-3 rounded px-3 text-sm ${
                    index === 0 ? "bg-[var(--foreground)] text-white" : "text-[var(--muted)] hover:bg-[var(--panel-soft)]"
                  }`}
                  href={`#section-${index}`}
                  key={item.label}
                >
                  <Icon size={16} />
                  {item.label}
                </a>
              );
            })}
          </nav>
        </aside>

        <section className="grid gap-5">
          <section className="border border-[var(--line)] bg-white" id="section-0">
            <SectionHeader icon={<FolderKanban size={18} />} title="项目概览" />
            <div className="grid gap-3 p-4 md:grid-cols-4">
              {["项目设定", "内容源", "基础资产", "分镜镜头"].map((label) => (
                <div className="border border-[var(--line)] p-3" key={label}>
                  <p className="text-xs text-[var(--muted)]">{label}</p>
                  <p className="mt-2 text-lg font-semibold">待同步</p>
                </div>
              ))}
            </div>
          </section>

          <section className="border border-[var(--line)] bg-white" id="section-1">
            <SectionHeader icon={<FileText size={18} />} title="原文与剧本" />
            <div className="grid gap-3 p-4 lg:grid-cols-2">
              <Panel title="内容源" items={["小说原文", "剧本原文", "章节拆分"]} />
              <Panel title="Script Agent" items={["生成剧本", "改写剧本", "版本激活"]} />
            </div>
          </section>

          <section className="border border-[var(--line)] bg-white" id="section-2">
            <SectionHeader icon={<Boxes size={18} />} title="资产" />
            <div className="grid gap-3 p-4 md:grid-cols-3">
              <Panel title="角色" items={["稳定名称", "形象描述", "参考图"]} />
              <Panel title="场景" items={["可拍摄空间", "环境状态", "参考图"]} />
              <Panel title="道具" items={["可见物件", "剧情作用", "参考图"]} />
            </div>
          </section>

          <section className="border border-[var(--line)] bg-white" id="section-3">
            <SectionHeader icon={<Clapperboard size={18} />} title="分镜镜头" />
            <div className="grid gap-3 p-4">
              {[1, 2, 3].map((shotNo) => (
                <div className="grid gap-3 border border-[var(--line)] p-3 lg:grid-cols-[120px_1fr_1fr] lg:items-center" key={shotNo}>
                  <div className="grid aspect-video place-items-center bg-[var(--panel-soft)] text-[var(--muted)]">
                    <ImageIcon size={24} />
                  </div>
                  <div>
                    <p className="text-sm font-semibold">Shot {shotNo}</p>
                    <p className="mt-1 text-sm text-[var(--muted)]">visual / camera / motion / mood</p>
                  </div>
                  <p className="text-sm text-[var(--muted)]">参与资产、服装、姿态、表情、动作、场景状态、道具状态</p>
                </div>
              ))}
            </div>
          </section>

          <section className="grid gap-5 lg:grid-cols-2">
            <div className="border border-[var(--line)] bg-white" id="section-4">
              <SectionHeader icon={<Workflow size={18} />} title="工作流" />
              <Panel className="p-4" title="生产链路" items={["script_to_assets", "script_to_storyboard", "video_production + scriptId"]} />
            </div>
            <div className="border border-[var(--line)] bg-white" id="section-5">
              <SectionHeader icon={<Library size={18} />} title="媒体资产" />
              <Panel className="p-4" title="Vault" items={["基础资产图", "派生资产图", "镜头图片", "镜头视频", "final_video"]} />
            </div>
          </section>

          <section className="border border-[var(--line)] bg-white" id="section-6">
            <SectionHeader icon={<Settings2 size={18} />} title="项目设置" />
            <div className="grid gap-3 p-4 md:grid-cols-3">
              <Panel title="视频" items={["videoRatio", "imageQuality", "productionMode"]} />
              <Panel title="模型" items={["scriptModelProfileKey", "imageModelProfileKey", "videoModelProfileKey"]} />
              <Panel title="风格" items={["artStyle", "directorManual", "visualManual"]} />
            </div>
          </section>
        </section>
      </div>
    </main>
  );
}

function SectionHeader({ icon, title }: { icon: ReactNode; title: string }) {
  return (
    <div className="flex items-center gap-2 border-b border-[var(--line)] px-4 py-3">
      {icon}
      <h2 className="text-sm font-semibold">{title}</h2>
    </div>
  );
}

function Panel({ title, items, className = "" }: { title: string; items: string[]; className?: string }) {
  return (
    <div className={`border border-[var(--line)] p-3 ${className}`}>
      <p className="text-sm font-semibold">{title}</p>
      <div className="mt-3 flex flex-wrap gap-2">
        {items.map((item) => (
          <span className="rounded border border-[var(--line)] px-2 py-1 text-xs text-[var(--muted)]" key={item}>
            {item}
          </span>
        ))}
      </div>
    </div>
  );
}

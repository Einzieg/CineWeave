"use client";

import Link from "next/link";
import type { Route } from "next";
import { useState } from "react";
import { ArrowRight, Filter, Plus, Search } from "lucide-react";
import { AppShell, Surface } from "@/components/layout/app-shell";
import { StatusBadge } from "@/components/shared/status-badge";
import { EmptyState } from "@/components/shared/empty-state";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiQuery } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { projectHref } from "@/lib/routes";
import type { Project } from "@/lib/types";

export function ProjectsPage() {
  return (
    <AppShell active="projects" title="项目" description="只展示项目卡片；工作流、镜头和媒体资产保留在项目内部。">
      <ProjectsContent />
    </AppShell>
  );
}

function ProjectsContent() {
  const { data: projects = [], isLoading } = useApiQuery({
    key: qk.projects(),
    queryFn: (session) => studioApi.listProjects(session).then((r) => r.items),
  });

  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("all");

  const filtered = projects.filter((project) => {
    const text = `${project.name} ${project.description ?? ""} ${project.projectType ?? ""} ${project.contentType ?? ""}`.toLowerCase();
    const matchesText = text.includes(query.trim().toLowerCase());
    const matchesStatus = status === "all" || (project.status ?? "active") === status;
    return matchesText && matchesStatus;
  });

  return (
    <>
      <Surface className="mb-5 p-4">
        <div className="grid gap-3 lg:grid-cols-[1fr_180px_auto]">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
            <Input
              className="pl-9"
              placeholder="搜索项目名称、简介或类型"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger>
              <Filter className="mr-2 h-4 w-4 text-muted-foreground" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="active">进行中</SelectItem>
              <SelectItem value="draft">草稿</SelectItem>
              <SelectItem value="archived">已归档</SelectItem>
            </SelectContent>
          </Select>
          <Button asChild>
            <Link href="/projects/new">
              <Plus className="mr-2 h-4 w-4" />
              新建项目
            </Link>
          </Button>
        </div>
      </Surface>

      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <Skeleton key={i} className="h-56" />
          ))}
        </div>
      ) : filtered.length ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((project) => (
            <ProjectCard key={project.id} project={project} />
          ))}
        </div>
      ) : (
        <EmptyState title="没有匹配项目" description="调整搜索条件，或新建一个脚本驱动项目。" />
      )}
    </>
  );
}

function ProjectCard({ project }: { project: Project }) {
  return (
    <Link
      href={projectHref(project.id) as Route}
      className="group rounded-lg border bg-card p-4 transition-colors hover:border-primary/40 hover:bg-accent"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <h3 className="text-base font-semibold">{project.name}</h3>
          <p className="mt-2 line-clamp-2 text-sm leading-6 text-muted-foreground">
            {project.description || "暂无简介"}
          </p>
        </div>
        <StatusBadge status={project.status ?? "active"} />
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <Badge variant="outline">{project.projectType || "未设置类型"}</Badge>
        <Badge variant="outline">{project.contentType || "未设置内容"}</Badge>
        <Badge variant="outline">{project.videoRatio || project.aspectRatio || "16:9"}</Badge>
        <Badge variant="outline">{project.artStyle || "未设置画风"}</Badge>
      </div>
      <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full w-2/5 rounded-full bg-primary transition-all group-hover:w-3/5" />
      </div>
      <div className="mt-4 flex items-center justify-between text-xs text-muted-foreground">
        <span>最近更新：{project.updatedAt ? formatTime(project.updatedAt) : "未知"}</span>
        <span className="inline-flex items-center gap-1 text-primary">
          打开项目 <ArrowRight className="h-3 w-3" />
        </span>
      </div>
    </Link>
  );
}

function formatTime(iso: string) {
  const date = new Date(iso);
  const now = new Date();
  const diff = now.getTime() - date.getTime();
  const minutes = Math.floor(diff / 60000);
  const hours = Math.floor(diff / 3600000);
  const days = Math.floor(diff / 86400000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes} 分钟前`;
  if (hours < 24) return `${hours} 小时前`;
  if (days < 7) return `${days} 天前`;
  return date.toLocaleDateString("zh-CN");
}

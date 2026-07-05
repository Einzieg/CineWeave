"use client";

import { useRouter } from "next/navigation";
import type { Route } from "next";
import { useState } from "react";
import { Check, Loader2 } from "lucide-react";
import { AppShell } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Card, CardContent } from "@/components/ui/card";
import { ErrorPanel } from "@/components/shared/error-panel";
import { useStudioSession } from "@/lib/session";
import { studioApi, StudioApiError } from "@/lib/api-client";
import { projectHref } from "@/lib/routes";

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
  const [currentTab, setCurrentTab] = useState("basic");
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
  });

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
      const project = await studioApi.createProject(session, {
        workspaceId,
        name: form.name,
        description: form.description || null,
        projectType: form.projectType,
        contentType: form.contentType,
        videoRatio: form.videoRatio,
        artStyle: form.artStyle,
        directorManual: form.directorManual || null,
        visualManual: form.visualManual || null,
        imageQuality: form.imageQuality,
        productionMode: form.productionMode,
      });
      router.push(projectHref(project.id) as Route);
    } catch (cause) {
      setError(cause instanceof StudioApiError ? cause.message : "创建失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardContent className="pt-6">
        <Tabs value={currentTab} onValueChange={setCurrentTab}>
          <TabsList className="grid w-full grid-cols-4">
            <TabsTrigger value="basic">基础信息</TabsTrigger>
            <TabsTrigger value="video">视频设定</TabsTrigger>
            <TabsTrigger value="style">风格设定</TabsTrigger>
            <TabsTrigger value="import">内容导入</TabsTrigger>
          </TabsList>

          <TabsContent value="basic" className="space-y-4">
            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="name">项目名称 *</Label>
                <Input id="name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              </div>
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
            <div className="space-y-2">
              <Label htmlFor="description">项目简介</Label>
              <Textarea
                id="description"
                rows={4}
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
              />
            </div>
          </TabsContent>

          <TabsContent value="video" className="space-y-4">
            <div className="grid gap-4 md:grid-cols-3">
              <div className="space-y-2">
                <Label htmlFor="videoRatio">视频比例</Label>
                <Select value={form.videoRatio} onValueChange={(v) => setForm({ ...form, videoRatio: v })}>
                  <SelectTrigger id="videoRatio">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="16:9">16:9</SelectItem>
                    <SelectItem value="9:16">9:16</SelectItem>
                    <SelectItem value="1:1">1:1</SelectItem>
                    <SelectItem value="4:3">4:3</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="imageQuality">图片质量</Label>
                <Select value={form.imageQuality} onValueChange={(v) => setForm({ ...form, imageQuality: v })}>
                  <SelectTrigger id="imageQuality">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="standard">标准</SelectItem>
                    <SelectItem value="hd">高清</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="productionMode">生产模式</Label>
                <Select value={form.productionMode} onValueChange={(v) => setForm({ ...form, productionMode: v })}>
                  <SelectTrigger id="productionMode">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="silent_video">无声视频</SelectItem>
                    <SelectItem value="storyboard_only">仅分镜</SelectItem>
                    <SelectItem value="assets_only">仅资产</SelectItem>
                    <SelectItem value="custom">自定义</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
          </TabsContent>

          <TabsContent value="style" className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="artStyle">画风风格</Label>
              <Select value={form.artStyle} onValueChange={(v) => setForm({ ...form, artStyle: v })}>
                <SelectTrigger id="artStyle">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="写实电影感">写实电影感</SelectItem>
                  <SelectItem value="国风动画">国风动画</SelectItem>
                  <SelectItem value="二次元">二次元</SelectItem>
                  <SelectItem value="黑白漫画">黑白漫画</SelectItem>
                  <SelectItem value="水彩插画">水彩插画</SelectItem>
                  <SelectItem value="赛博城市">赛博城市</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="directorManual">导演手册</Label>
              <Textarea
                id="directorManual"
                rows={4}
                value={form.directorManual}
                onChange={(e) => setForm({ ...form, directorManual: e.target.value })}
                placeholder="描述你对镜头、剪辑、节奏的偏好"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="visualManual">视觉手册</Label>
              <Textarea
                id="visualManual"
                rows={4}
                value={form.visualManual}
                onChange={(e) => setForm({ ...form, visualManual: e.target.value })}
                placeholder="描述整体视觉风格、色调、光影偏好"
              />
            </div>
          </TabsContent>

          <TabsContent value="import" className="space-y-4">
            <p className="text-sm text-muted-foreground">
              创建项目后可在原文与剧本页面导入内容
            </p>
          </TabsContent>
        </Tabs>

        <div className="mt-6 flex items-center justify-between gap-4">
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
      </CardContent>
    </Card>
  );
}

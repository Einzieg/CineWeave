"use client";

import { useState } from "react";
import { useApiQuery } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { AppShell, Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FileText, Eye } from "lucide-react";

export function PromptsPage() {
  const [selectedCategory, setSelectedCategory] = useState<string>("all");

  // 获取提示词模板列表
  const { data: templatesData, isLoading } = useApiQuery({
    key: qk.promptTemplates(),
    queryFn: (session) => studioApi.listPromptTemplates(session),
  });

  const templates = templatesData?.items || [];

  // 根据分类筛选
  const filteredTemplates = selectedCategory === "all"
    ? templates
    : templates.filter((template) => promptTemplateCategory(template) === selectedCategory);

  const categories = [
    { value: "all", label: "全部" },
    { value: "script", label: "剧本生成" },
    { value: "asset", label: "资产分析" },
    { value: "storyboard", label: "分镜设计" },
    { value: "shot", label: "镜头生成" },
    { value: "review", label: "审查修复" },
  ];

  return (
    <AppShell active="prompts" title="提示词中心" description="查看和管理提示词模板">
      <Surface>
        <SectionTitle title="提示词模板" description="管理脚本、资产、分镜等提示词" />

        <div className="p-4">
          <Tabs value={selectedCategory} onValueChange={setSelectedCategory}>
            <TabsList>
              {categories.map((cat) => (
                <TabsTrigger key={cat.value} value={cat.value}>
                  {cat.label}
                </TabsTrigger>
              ))}
            </TabsList>

            <div className="mt-6">
              {isLoading && <Skeleton className="h-64" />}

              {!isLoading && filteredTemplates.length === 0 && (
                <div className="rounded-lg border border-dashed p-12 text-center">
                  <FileText className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
                  <p className="mt-4 text-sm text-muted-foreground">暂无模板</p>
                </div>
              )}

              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {filteredTemplates.map((template) => (
                  <div
                    key={template.id}
                    className="group relative overflow-hidden rounded-lg border bg-card transition hover:shadow-md"
                  >
                    <div className="p-4 space-y-3">
                      {/* 头部 */}
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <FileText className="h-4 w-4 text-muted-foreground" />
                          <Badge variant="outline">{promptTemplateCategory(template)}</Badge>
                        </div>
                        <Badge variant="secondary" className="text-xs">{template.status}</Badge>
                      </div>

                      {/* 标题和描述 */}
                      <div>
                        <h3 className="font-medium leading-tight">{template.name}</h3>
                        {template.taskType && (
                          <p className="mt-1.5 text-sm text-muted-foreground line-clamp-2">
                            {template.taskType}
                          </p>
                        )}
                      </div>

                      {/* 元信息 */}
                      <div className="flex items-center gap-3 text-xs text-muted-foreground">
                        <span>版本: {template.activeVersionId?.slice(0, 8) || "未激活"}</span>
                        {template.modality && (
                          <span>· {template.modality}</span>
                        )}
                      </div>

                      {/* 操作按钮 */}
                      <div className="flex gap-2 pt-2">
                        <Button
                          size="sm"
                          variant="outline"
                          className="flex-1"
                          disabled
                        >
                          <Eye className="mr-1.5 h-3 w-3" />
                          查看
                        </Button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </Tabs>
        </div>
      </Surface>
    </AppShell>
  );
}

function promptTemplateCategory(template: { purpose?: string; modality?: string; taskType?: string }) {
  return template.purpose || template.modality || template.taskType || "prompt";
}

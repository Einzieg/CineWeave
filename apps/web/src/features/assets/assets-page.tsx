"use client";

import { useState } from "react";
import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Image as ImageIcon, Upload, Wand2, User, MapPin, Package } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";

export function AssetsPage({
  projectId,
  initialAssetId = ""
}: {
  projectId: string;
  initialAssetId?: string;
}) {
  const [selectedAssetId, setSelectedAssetId] = useState<string | null>(initialAssetId || null);
  const invalidate = useInvalidateKeys();

  // 获取资产列表
  const { data: assets = [], isLoading } = useApiQuery({
    key: qk.assets(projectId),
    queryFn: (session) =>
      studioApi.listCanonicalAssets(session, projectId).then(r => r.items || []),
  });

  // 生成资产卡片
  const generateCardMutation = useApiMutation({
    mutationFn: (session, assetId: string) =>
      studioApi.generateAssetCard(session, projectId, assetId, {}),
    onSuccess: () => {
      toast.success("资产卡片生成已启动");
      invalidate([qk.assets(projectId)]);
    },
    onError: (error) => {
      toast.error("生成失败：" + error.message);
    },
  });

  // 生成资产图像
  const generateImageMutation = useApiMutation({
    mutationFn: (session, assetId: string) =>
      studioApi.generateAssetImage(session, projectId, assetId, {}),
    onSuccess: () => {
      toast.success("资产图像生成已启动");
      invalidate([qk.assets(projectId)]);
    },
    onError: (error) => {
      toast.error("生成失败：" + error.message);
    },
  });

  const getAssetIcon = (type: string) => {
    switch (type) {
      case "character": return User;
      case "scene": return MapPin;
      case "prop": return Package;
      default: return ImageIcon;
    }
  };

  return (
    <Surface>
      <SectionTitle title="资产管理" description="管理角色、场景、道具等核心资产" />

      <Tabs defaultValue="assets" className="p-4">
        <TabsList>
          <TabsTrigger value="assets">
            核心资产
            <Badge variant="secondary" className="ml-2">{assets.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="requirements">镜头需求</TabsTrigger>
          <TabsTrigger value="vault">媒体库</TabsTrigger>
        </TabsList>

        <TabsContent value="assets" className="space-y-4">
          {isLoading && <Skeleton className="h-64" />}

          {!isLoading && assets.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <ImageIcon className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
              <p className="mt-4 text-sm text-muted-foreground">暂无资产</p>
              <p className="mt-1 text-xs text-muted-foreground">从剧本分析或手动创建资产</p>
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {assets.map((asset: any) => {
              const Icon = getAssetIcon(asset.assetType);
              return (
                <button
                  key={asset.id}
                  onClick={() => setSelectedAssetId(asset.id)}
                  className={cn(
                    "group relative overflow-hidden rounded-lg border bg-card p-4 text-left transition hover:shadow-md",
                    selectedAssetId === asset.id && "ring-2 ring-primary"
                  )}
                >
                  {/* 预览图 */}
                  {asset.primaryReferenceArtifactId ? (
                    <div className="mb-3 aspect-square overflow-hidden rounded-md bg-muted">
                      <div className="flex h-full items-center justify-center text-muted-foreground">
                        <ImageIcon className="h-8 w-8" />
                      </div>
                    </div>
                  ) : (
                    <div className="mb-3 flex aspect-square items-center justify-center rounded-md border-2 border-dashed">
                      <Icon className="h-8 w-8 text-muted-foreground" />
                    </div>
                  )}

                  {/* 信息 */}
                  <div className="space-y-2">
                    <div className="flex items-start justify-between gap-2">
                      <h3 className="font-medium leading-tight">{asset.name}</h3>
                      <Badge variant="outline">{asset.assetType}</Badge>
                    </div>
                    {asset.description && (
                      <p className="line-clamp-2 text-xs text-muted-foreground">
                        {asset.description}
                      </p>
                    )}

                    {/* 操作按钮 */}
                    <div className="flex gap-1.5 pt-2">
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={(e) => {
                          e.stopPropagation();
                          generateCardMutation.mutate(asset.id);
                        }}
                        disabled={generateCardMutation.isPending}
                      >
                        <Wand2 className="mr-1 h-3 w-3" />
                        生成卡片
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={(e) => {
                          e.stopPropagation();
                          generateImageMutation.mutate(asset.id);
                        }}
                        disabled={generateImageMutation.isPending}
                      >
                        <ImageIcon className="mr-1 h-3 w-3" />
                        生成图像
                      </Button>
                    </div>
                  </div>
                </button>
              );
            })}
          </div>
        </TabsContent>

        <TabsContent value="requirements">
          <div className="rounded-lg border border-dashed p-12 text-center">
            <p className="text-sm text-muted-foreground">镜头资产需求功能开发中...</p>
          </div>
        </TabsContent>

        <TabsContent value="vault">
          <div className="rounded-lg border border-dashed p-12 text-center">
            <p className="text-sm text-muted-foreground">媒体库功能开发中...</p>
          </div>
        </TabsContent>
      </Tabs>
    </Surface>
  );
}

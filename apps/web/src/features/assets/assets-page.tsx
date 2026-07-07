"use client";

import { useMemo, useState } from "react";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Check, ExternalLink, Image as ImageIcon, MapPin, Package, RefreshCw, User, Wand2, X } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { artifactTypeLabel, assetTypeLabel, requirementTypeLabel, statusLabel } from "@/lib/labels";
import type { Artifact, CanonicalAsset, ShotAssetRequirement } from "@/lib/types";

export function AssetsPage({
  projectId,
  initialAssetId = "",
}: {
  projectId: string;
  initialAssetId?: string;
}) {
  const [selectedAssetId, setSelectedAssetId] = useState<string | null>(initialAssetId || null);
  const invalidate = useInvalidateKeys();

  const { data: assets = [], isLoading } = useApiQuery({
    key: qk.assets(projectId),
    queryFn: (session) => studioApi.listCanonicalAssets(session, projectId).then((response) => response.items || []),
  });
  const { data: requirements = [], isLoading: requirementsLoading } = useApiQuery({
    key: qk.requirements(projectId),
    queryFn: (session) => studioApi.listShotAssetRequirements(session, projectId).then((response) => response.items || []),
  });
  const { data: artifacts = [], isLoading: artifactsLoading } = useApiQuery({
    key: qk.artifacts(projectId),
    queryFn: (session) => studioApi.listArtifacts(session, projectId).then((response) => response.items || []),
  });

  const selectedAsset = useMemo(() => assets.find((asset) => asset.id === selectedAssetId) ?? assets[0] ?? null, [assets, selectedAssetId]);
  const { data: selectedReferences = [] } = useApiQuery({
    key: qk.assetReferences(projectId, selectedAsset?.id ?? ""),
    queryFn: (session) => studioApi.listAssetReferences(session, projectId, selectedAsset!.id, true).then((response) => response.items || []),
    enabled: !!selectedAsset,
  });

  const artifactPreviewById = useMemo(() => {
    const previews = new Map<string, string>();
    for (const artifact of artifacts) {
      if (artifact.previewUrl) {
        previews.set(artifact.id, artifact.previewUrl);
      }
    }
    return previews;
  }, [artifacts]);

  const generateCardMutation = useApiMutation({
    mutationFn: (session, assetId: string) => studioApi.generateAssetCard(session, projectId, assetId, {}),
    onSuccess: () => {
      toast.success("资产卡片生成已启动");
      invalidate([qk.assets(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const generateImageMutation = useApiMutation({
    mutationFn: (session, assetId: string) => studioApi.generateAssetImage(session, projectId, assetId, {}),
    onSuccess: (_response, assetId) => {
      toast.success("资产图像生成已启动");
      invalidate([qk.assets(projectId), qk.assetReferences(projectId, assetId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const generateRequirementMutation = useApiMutation({
    mutationFn: (session, requirementId: string) => studioApi.generateDerivedAssetImage(session, projectId, requirementId),
    onSuccess: () => {
      toast.success("派生图像生成已启动");
      invalidate([qk.requirements(projectId), qk.assets(projectId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const reviewRequirementMutation = useApiMutation({
    mutationFn: (session, payload: { requirementId: string; reviewStatus: "approved" | "needs_edit" }) =>
      studioApi.reviewShotAssetRequirement(session, projectId, payload.requirementId, { reviewStatus: payload.reviewStatus }),
    onSuccess: () => {
      invalidate([qk.requirements(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const getAssetIcon = (type: string) => {
    switch (type) {
      case "character":
        return User;
      case "scene":
        return MapPin;
      case "prop":
        return Package;
      default:
        return ImageIcon;
    }
  };

  return (
    <Surface>
      <SectionTitle title="资产管理" description="管理角色、场景、道具和镜头派生素材" />

      <Tabs defaultValue="assets" className="p-4">
        <TabsList>
          <TabsTrigger value="assets">
            核心资产
            <Badge variant="secondary" className="ml-2">{assets.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="requirements">
            镜头需求
            <Badge variant="secondary" className="ml-2">{requirements.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="vault">
            媒体库
            <Badge variant="secondary" className="ml-2">{artifacts.length}</Badge>
          </TabsTrigger>
        </TabsList>

        <TabsContent value="assets" className="space-y-4">
          {isLoading && <Skeleton className="h-64" />}

          {!isLoading && assets.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <ImageIcon className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
              <p className="mt-4 text-sm text-muted-foreground">暂无资产</p>
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {assets.map((asset) => {
              const Icon = getAssetIcon(asset.assetType);
              const previewUrl = assetPreviewUrl(asset, selectedAsset?.id === asset.id ? selectedReferences : undefined, artifactPreviewById);
              return (
                <button
                  key={asset.id}
                  onClick={() => setSelectedAssetId(asset.id)}
                  className={cn(
                    "group relative overflow-hidden rounded-lg border bg-card p-4 text-left transition hover:shadow-md",
                    selectedAsset?.id === asset.id && "ring-2 ring-primary",
                  )}
                >
                  <div className="mb-3 aspect-square overflow-hidden rounded-md bg-muted">
                    {previewUrl ? (
                      <img alt={asset.name} className="h-full w-full object-cover" src={previewUrl} />
                    ) : (
                      <div className="flex h-full items-center justify-center">
                        <Icon className="h-8 w-8 text-muted-foreground" />
                      </div>
                    )}
                  </div>

                  <div className="space-y-2">
                    <div className="flex items-start justify-between gap-2">
                      <h3 className="font-medium leading-tight">{asset.name}</h3>
                      <Badge variant="outline">{assetTypeLabel(asset.assetType)}</Badge>
                    </div>
                    {asset.description && <p className="line-clamp-2 text-xs text-muted-foreground">{asset.description}</p>}
                    <div className="flex gap-1.5 pt-2">
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={(event) => {
                          event.stopPropagation();
                          generateCardMutation.mutate(asset.id);
                        }}
                        disabled={generateCardMutation.isPending}
                      >
                        <Wand2 className="mr-1 h-3 w-3" />
                        卡片
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={(event) => {
                          event.stopPropagation();
                          generateImageMutation.mutate(asset.id);
                        }}
                        disabled={generateImageMutation.isPending}
                      >
                        <ImageIcon className="mr-1 h-3 w-3" />
                        图像
                      </Button>
                    </div>
                  </div>
                </button>
              );
            })}
          </div>
        </TabsContent>

        <TabsContent value="requirements" className="space-y-3">
          {requirementsLoading && <Skeleton className="h-48" />}
          {!requirementsLoading && requirements.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <p className="text-sm text-muted-foreground">暂无镜头资产需求</p>
            </div>
          )}
          <div className="grid gap-3">
            {requirements.map((requirement) => {
              const previewUrl = requirementPreviewUrl(requirement, artifactPreviewById);
              return (
                <div key={requirement.id} className="grid gap-3 rounded-lg border p-3 sm:grid-cols-[160px_1fr]">
                  <div className="aspect-video overflow-hidden rounded-md bg-muted">
                    {previewUrl ? (
                      <img alt={requirement.assetName || requirement.assetId} className="h-full w-full object-cover" src={previewUrl} />
                    ) : (
                      <div className="flex h-full items-center justify-center">
                        <ImageIcon className="h-6 w-6 text-muted-foreground" />
                      </div>
                    )}
                  </div>
                  <div className="grid gap-2">
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div>
                        <div className="font-medium">{requirement.assetName || requirement.assetId}</div>
                        <div className="text-xs text-muted-foreground">
                          {assetTypeLabel(requirement.assetType || requirement.asset?.assetType)} · {requirementTypeLabel(requirement.requirementType)}
                        </div>
                      </div>
                      <div className="flex gap-2">
                        <Badge variant="outline">{statusLabel(requirement.status)}</Badge>
                        <Badge variant="secondary">{statusLabel(requirement.reviewStatus || "pending")}</Badge>
                      </div>
                    </div>
                    <p className="line-clamp-2 text-sm text-muted-foreground">{requirement.prompt || requirement.action || requirement.roleInShot || "未设置描述"}</p>
                    <div className="flex flex-wrap gap-2">
                      <Button size="sm" variant="outline" onClick={() => reviewRequirementMutation.mutate({ requirementId: requirement.id, reviewStatus: "approved" })}>
                        <Check className="mr-1 h-3.5 w-3.5" />
                        确认
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => reviewRequirementMutation.mutate({ requirementId: requirement.id, reviewStatus: "needs_edit" })}>
                        <X className="mr-1 h-3.5 w-3.5" />
                        需修改
                      </Button>
                      <Button size="sm" onClick={() => generateRequirementMutation.mutate(requirement.id)} disabled={generateRequirementMutation.isPending}>
                        <RefreshCw className="mr-1 h-3.5 w-3.5" />
                        生成图像
                      </Button>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </TabsContent>

        <TabsContent value="vault" className="space-y-3">
          {artifactsLoading && <Skeleton className="h-48" />}
          {!artifactsLoading && artifacts.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <p className="text-sm text-muted-foreground">暂无媒体文件</p>
            </div>
          )}
          <ArtifactGrid artifacts={artifacts} />
        </TabsContent>
      </Tabs>
    </Surface>
  );
}

function ArtifactGrid({ artifacts }: { artifacts: Artifact[] }) {
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {artifacts.map((artifact) => (
        <div key={artifact.id} className="overflow-hidden rounded-lg border">
          <ArtifactPreview artifact={artifact} />
          <div className="grid gap-2 p-3">
            <div className="flex items-center justify-between gap-2">
              <Badge variant="outline">{artifactTypeLabel(artifact.type)}</Badge>
              {artifact.previewUrl ? (
                <a className="inline-flex items-center gap-1 text-sm text-primary" href={artifact.previewUrl} rel="noreferrer" target="_blank">
                  打开
                  <ExternalLink className="h-3.5 w-3.5" />
                </a>
              ) : null}
            </div>
            <p className="truncate text-xs text-muted-foreground">{artifact.storageKey || artifact.id}</p>
          </div>
        </div>
      ))}
    </div>
  );
}

function ArtifactPreview({ artifact }: { artifact: Artifact }) {
  if (artifact.previewUrl && artifact.mimeType?.startsWith("video/")) {
    return <video className="aspect-video w-full bg-black object-cover" controls src={artifact.previewUrl} />;
  }
  if (artifact.previewUrl) {
    return <img alt={artifact.storageKey || artifact.id} className="aspect-video w-full bg-muted object-cover" src={artifact.previewUrl} />;
  }
  return (
    <div className="grid aspect-video place-items-center bg-muted">
      <ImageIcon className="h-6 w-6 text-muted-foreground" />
    </div>
  );
}

function assetPreviewUrl(asset: CanonicalAsset, references: CanonicalAsset["references"], artifacts: Map<string, string>) {
  const primaryReference = references?.find((reference) => reference.isPrimary && reference.previewUrl) ?? references?.find((reference) => reference.previewUrl);
  return primaryReference?.previewUrl
    ?? idPreview(asset.primaryReferenceArtifactId, artifacts)
    ?? idPreview(asset.referenceArtifactId, artifacts)
    ?? "";
}

function requirementPreviewUrl(requirement: ShotAssetRequirement, artifacts: Map<string, string>) {
  return idPreview(requirement.derivedArtifactId, artifacts)
    ?? idPreview(requirement.asset?.primaryReferenceArtifactId, artifacts)
    ?? idPreview(requirement.asset?.referenceArtifactId, artifacts)
    ?? "";
}

function idPreview(id: string | undefined | null, artifacts: Map<string, string>) {
  return id ? artifacts.get(id) : undefined;
}

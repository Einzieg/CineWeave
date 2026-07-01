"use client";

import { ImageIcon, Pencil, RefreshCw, Check, X } from "lucide-react";
import { StatusBadge } from "@/components/status-badge";
import type { CanonicalAsset, ShotAssetRequirement, StoryboardShotRequirementDetail } from "@/lib/types";

type RequirementItem = ShotAssetRequirement | StoryboardShotRequirementDetail;

export function AssetCardPanel({
  requirements,
  busy,
  onOpenAsset,
  onEditRequirement,
  onReviewRequirement,
  onRegenerateRequirement,
}: {
  requirements: RequirementItem[];
  busy: boolean;
  onOpenAsset: (asset: CanonicalAsset) => void;
  onEditRequirement: (requirement: ShotAssetRequirement) => void;
  onReviewRequirement: (requirement: ShotAssetRequirement, reviewStatus: "approved" | "needs_edit") => void;
  onRegenerateRequirement: (requirement: ShotAssetRequirement) => void;
}) {
  if (!requirements.length) {
    return <p className="text-sm text-slate-500">暂无派生资产需求</p>;
  }
  return (
    <div className="grid gap-3">
      {requirements.map((requirement) => {
        const asset = requirement.asset;
        const previewUrl = requirementPreviewUrl(requirement);
        return (
          <div className="overflow-hidden rounded-md border border-slate-200 bg-white" key={requirement.id}>
            <div className="grid aspect-video place-items-center bg-slate-100">
              {previewUrl ? (
                <div aria-label={asset?.name || requirement.assetName || requirement.assetId} className="h-full w-full bg-cover bg-center" role="img" style={{ backgroundImage: `url(${previewUrl})` }} />
              ) : (
                <ImageIcon className="text-slate-400" size={28} />
              )}
            </div>
            <div className="grid gap-3 p-3">
              <div className="flex items-start justify-between gap-3">
                <button className="text-left" disabled={!asset || busy} onClick={() => asset && onOpenAsset(asset)} type="button">
                  <p className="font-medium text-slate-900">{asset?.name || requirement.assetName || requirement.assetId}</p>
                  <p className="mt-1 text-xs text-slate-500">{assetTypeLabel(requirement.assetType ?? asset?.assetType)} · {requirement.requirementType || "镜头需求"}</p>
                </button>
                <div className="grid justify-items-end gap-1">
                  <StatusBadge status={requirement.reviewStatus ?? "pending"} />
                  {requirement.staleState && requirement.staleState !== "fresh" ? <StatusBadge status={requirement.staleState} /> : null}
                </div>
              </div>
              <div className="grid gap-1 text-xs leading-5 text-slate-600">
                <p>服装：{requirement.costume || "未指定"}</p>
                <p>姿态：{requirement.pose || "未指定"}</p>
                <p>表情：{requirement.expression || "未指定"}</p>
                <p>动作：{requirement.action || "未指定"}</p>
                <p>状态：{requirement.sceneState || requirement.propState || "未指定"}</p>
                {asset?.consistencyPrompt ? <p className="line-clamp-2">一致性：{asset.consistencyPrompt}</p> : null}
              </div>
              <div className="grid grid-cols-2 gap-2">
                <button className="studio-button" disabled={busy} onClick={() => onReviewRequirement(requirement, "approved")} type="button">
                  <Check size={15} />
                  确认
                </button>
                <button className="studio-button" disabled={busy} onClick={() => onReviewRequirement(requirement, "needs_edit")} type="button">
                  <X size={15} />
                  需修改
                </button>
                <button className="studio-button" disabled={busy} onClick={() => onEditRequirement(requirement)} type="button">
                  <Pencil size={15} />
                  编辑
                </button>
                <button className="studio-button" disabled={busy} onClick={() => onRegenerateRequirement(requirement)} type="button">
                  <RefreshCw size={15} />
                  重生成
                </button>
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function requirementPreviewUrl(requirement: RequirementItem) {
  const detail = requirement as StoryboardShotRequirementDetail;
  const primaryReference = requirement.asset?.references?.find((reference) => reference.isPrimary && reference.status === "ready") ?? requirement.asset?.references?.find((reference) => reference.status === "ready");
  return detail.derivedPreviewUrl ?? primaryReference?.previewUrl ?? "";
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

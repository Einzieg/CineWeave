/* eslint-disable @next/next/no-img-element */
import { FileJson, ImageIcon, Video } from "lucide-react";
import type { Artifact, StoryboardShot } from "@/lib/types";

export function MediaPreview({ artifact, shot, className = "" }: { artifact?: Artifact; shot?: StoryboardShot; className?: string }) {
  const url = artifact?.previewUrl ?? shot?.imagePreviewUrl ?? shot?.videoPreviewUrl;
  const mimeType = artifact?.mimeType ?? "";
  const storageKey = artifact?.storageKey ?? shot?.imageStorageKey ?? shot?.videoStorageKey ?? "尚未生成";
  const isVideo = mimeType.startsWith("video/") || Boolean(shot?.videoPreviewUrl);
  const isImage = mimeType.startsWith("image/") || Boolean(shot?.imagePreviewUrl);

  return (
    <div className={`overflow-hidden rounded-lg border border-white/10 bg-black/30 ${className}`}>
      <div className="grid aspect-video place-items-center bg-zinc-950">
        {url && isVideo ? <video className="h-full w-full object-cover" controls src={url} /> : null}
        {url && isImage && !isVideo ? <img alt={storageKey} className="h-full w-full object-cover" src={url} /> : null}
        {!url ? (
          <div className="grid gap-2 text-center text-zinc-500">
            {isVideo ? <Video className="mx-auto" size={24} /> : isImage ? <ImageIcon className="mx-auto" size={24} /> : <FileJson className="mx-auto" size={24} />}
            <span className="text-xs">暂无预览</span>
          </div>
        ) : null}
      </div>
      <div className="truncate border-t border-white/10 px-3 py-2 text-xs text-zinc-400">{storageKey}</div>
    </div>
  );
}

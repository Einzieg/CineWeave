"use client";

import Image from "next/image";
import { Check, Clapperboard, ImageIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { JsonRecord, PromptTemplate } from "@/lib/types";

export const DEFAULT_DIRECTOR_MANUAL_KEY = "default_director_manual";
export const DEFAULT_VISUAL_MANUAL_KEY = "default_visual_manual";

export type ManualStyleOption = {
  kind: "director" | "visual";
  templateKey: string;
  promptVersionId: string;
  name: string;
  description: string;
  badge: string;
  styleSlug?: string;
  imageSrc?: string;
  isDefault: boolean;
};

type ManualStyleSelectorProps = {
  title: string;
  options: ManualStyleOption[];
  selectedTemplateKey?: string;
  loading?: boolean;
  layout?: "grid" | "strip";
  onSelect: (option: ManualStyleOption) => void;
};

const defaultManualImages = {
  director: "/toonflow/director-styles/default_director_manual-image2.png",
  visual: "/toonflow/visual-styles/default_visual_manual-image2.png",
} as const;

const visualStyleInfo: Record<string, { name: string; description: string; image: string }> = {
  "2d_90s_japanese_anime": {
    name: "90 年代日漫",
    description: "手绘赛璐璐、暖调怀旧、清晰线条",
    image: "/toonflow/visual-styles/2d_90s_japanese_anime-image2.png",
  },
  "2d_chinese_guofeng": {
    name: "2D 国风",
    description: "新国潮色彩、东方纹样、动画化角色",
    image: "/toonflow/visual-styles/2d_chinese_guofeng-image2.png",
  },
  "2d_flat_design": {
    name: "2D 扁平插画",
    description: "简洁造型、平面色块、商业插画质感",
    image: "/toonflow/visual-styles/2d_flat_design-image2.png",
  },
  "2d_mature_urban_romance": {
    name: "2D 都市情感",
    description: "成熟都市、细腻表情、情绪光影",
    image: "/toonflow/visual-styles/2d_mature_urban_romance-image2.png",
  },
  "3d_anime_render": {
    name: "3D 动画渲染",
    description: "三维动画、柔和材质、清晰电影光",
    image: "/toonflow/visual-styles/3d_anime_render-image2.png",
  },
  "3d_chinese_traditional": {
    name: "3D 国风传统",
    description: "传统器物、东方建筑、写意空间",
    image: "/toonflow/visual-styles/3d_chinese_traditional-image2.png",
  },
  "3d_clay_stopmotion": {
    name: "3D 黏土定格",
    description: "手作质感、黏土纹理、定格动画风",
    image: "/toonflow/visual-styles/3d_clay_stopmotion-image2.png",
  },
  "3d_guofeng_cyber": {
    name: "3D 国风赛博",
    description: "国风结构、霓虹材质、未来城市",
    image: "/toonflow/visual-styles/3d_guofeng_cyber-image2.png",
  },
  realpeople_ancient_chinese: {
    name: "真人古风",
    description: "真人质感、古装服化、电影级光影",
    image: "/toonflow/visual-styles/realpeople_ancient_chinese-image2.png",
  },
  realpeople_modern_city: {
    name: "真人现代都市",
    description: "现代城市、真实人物、商业影像质感",
    image: "/toonflow/visual-styles/realpeople_modern_city-image2.png",
  },
  realpeople_urban_modern: {
    name: "真人都市写实",
    description: "都市写实、自然光影、生活化场景",
    image: "/toonflow/visual-styles/realpeople_urban_modern-image2.png",
  },
};

const directorStyleInfo: Record<string, { name: string; description: string; image: string }> = {
  comedy_humor: { name: "幽默喜剧", description: "误会、反差、节奏包袱和轻快调度", image: "/toonflow/director-styles/comedy_humor-image2.png" },
  coming_of_age: { name: "成长青春", description: "自我发现、关系变化和青春群像", image: "/toonflow/director-styles/coming_of_age-image2.png" },
  commerce_short_drama: { name: "商业短剧", description: "强钩子、快转化、产品剧情融合", image: "/toonflow/director-styles/commerce_short_drama-image2.png" },
  family_warmth: { name: "家庭温情", description: "亲情关系、日常细节和温暖节奏", image: "/toonflow/director-styles/family_warmth-image2.png" },
  historical_epic: { name: "历史史诗", description: "时代格局、仪式感场面和命运选择", image: "/toonflow/director-styles/historical_epic-image2.png" },
  horror_supernatural: { name: "恐怖灵异", description: "信息遮蔽、威胁暗示和悬疑节奏", image: "/toonflow/director-styles/horror_supernatural-image2.png" },
  hot_blooded_action: { name: "热血动作", description: "目标冲突、动作升级和力量景别", image: "/toonflow/director-styles/hot_blooded_action-image2.png" },
  mystery_thriller: { name: "悬疑惊悚", description: "线索递进、反转控制和紧张调度", image: "/toonflow/director-styles/mystery_thriller-image2.png" },
  psychological_drama: { name: "心理剧情", description: "内心冲突、微表情和压抑氛围", image: "/toonflow/director-styles/psychological_drama-image2.png" },
  scifi_future_epic: { name: "科幻史诗", description: "宏大设定、未来秩序和视觉奇观", image: "/toonflow/director-styles/scifi_future_epic-image2.png" },
  scifi_post_apocalypse: { name: "末世科幻", description: "生存压力、废土空间和秩序崩塌", image: "/toonflow/director-styles/scifi_post_apocalypse-image2.png" },
  sweet_romance_novel: { name: "甜宠言情", description: "暧昧推进、情绪拉扯和高甜节点", image: "/toonflow/director-styles/sweet_romance_novel-image2.png" },
  urban_workplace_drama: { name: "都市职场", description: "职场目标、关系博弈和现实节奏", image: "/toonflow/director-styles/urban_workplace_drama-image2.png" },
  xianxia_fantasy: { name: "仙侠玄幻", description: "境界体系、仙凡空间和东方奇观", image: "/toonflow/director-styles/xianxia_fantasy-image2.png" },
};

export function ManualStyleSelector({ title, options, selectedTemplateKey, loading, layout = "grid", onSelect }: ManualStyleSelectorProps) {
  if (layout === "strip") {
    return (
      <div className="flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm font-semibold">{title}</div>
          {selectedTemplateKey ? <Badge variant="outline">已选择</Badge> : null}
        </div>
        {loading ? (
          <div className="flex gap-3 overflow-hidden">
            {Array.from({ length: 6 }).map((_, index) => (
              <Skeleton key={index} className="h-28 w-36 shrink-0 rounded-lg" />
            ))}
          </div>
        ) : options.length > 0 ? (
          <div className="flex gap-3 overflow-x-auto pb-2">
            {options.map((option) => (
              <ManualStyleCard
                key={option.templateKey}
                option={option}
                selected={selectedTemplateKey === option.templateKey}
                compact
                onSelect={() => onSelect(option)}
              />
            ))}
          </div>
        ) : (
          <div className="rounded-lg border p-4 text-sm text-muted-foreground">暂无可用手册</div>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <div className="text-sm font-medium">{title}</div>
        {selectedTemplateKey ? <Badge variant="outline">已选择</Badge> : null}
      </div>
      <ScrollArea className="h-[360px] rounded-lg border">
        {loading ? (
          <div className="grid gap-3 p-3 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 6 }).map((_, index) => (
              <Skeleton key={index} className="aspect-[4/3] rounded-lg" />
            ))}
          </div>
        ) : options.length > 0 ? (
          <div className="grid gap-3 p-3 sm:grid-cols-2 xl:grid-cols-3">
            {options.map((option) => (
              <ManualStyleCard
                key={option.templateKey}
                option={option}
                selected={selectedTemplateKey === option.templateKey}
                onSelect={() => onSelect(option)}
              />
            ))}
          </div>
        ) : (
          <div className="p-4 text-sm text-muted-foreground">暂无可用手册</div>
        )}
      </ScrollArea>
    </div>
  );
}

export function buildManualStyleOptions(templates: PromptTemplate[], kind: "director" | "visual"): ManualStyleOption[] {
  const purpose = kind === "director" ? "director_manual" : "visual_manual";
  return templates
    .filter((template) => template.purpose === purpose && template.status === "active" && template.activeVersion?.id)
    .map((template) => manualOptionFromTemplate(template, kind))
    .sort((left, right) => {
      if (left.isDefault !== right.isDefault) return left.isDefault ? -1 : 1;
      return left.name.localeCompare(right.name, "zh-Hans-CN");
    });
}

export function selectedToonflowStyleFromSettings(settings: JsonRecord | undefined, key: "toonflowVisualStyle" | "toonflowStoryStyle") {
  const value = settings?.[key];
  return typeof value === "string" ? value : "";
}

export function withToonflowSetting(settings: JsonRecord | undefined, key: "toonflowVisualStyle" | "toonflowStoryStyle", value?: string): JsonRecord {
  const next: JsonRecord = isJsonRecord(settings) ? { ...settings } : {};
  if (value) {
    next[key] = value;
  } else {
    delete next[key];
  }
  return next;
}

function ManualStyleCard({ option, selected, compact, onSelect }: { option: ManualStyleOption; selected: boolean; compact?: boolean; onSelect: () => void }) {
  const Icon = option.kind === "visual" ? ImageIcon : Clapperboard;
  return (
    <button
      type="button"
      className={cn(
        "group overflow-hidden rounded-lg border bg-card text-left shadow-sm outline-none transition hover:border-primary/60 focus-visible:ring-[3px] focus-visible:ring-ring/50",
        compact && "w-36 shrink-0",
        selected && "border-primary ring-2 ring-primary/20",
      )}
      onClick={onSelect}
    >
      <div className="relative aspect-[4/3] overflow-hidden bg-muted">
        {option.imageSrc ? (
          <Image src={option.imageSrc} alt={option.name} fill sizes="(min-width: 1280px) 180px, (min-width: 640px) 40vw, 80vw" className="object-cover transition-transform group-hover:scale-[1.03]" />
        ) : (
          <div className="flex size-full items-center justify-center bg-muted text-muted-foreground">
            <div className="flex flex-col items-center gap-2">
              <Icon />
              <span className="text-base font-medium">{option.name}</span>
            </div>
          </div>
        )}
        <div className="absolute inset-x-0 bottom-0 bg-background/90 p-2 backdrop-blur">
          <div className="truncate text-sm font-medium">{option.name}</div>
          {!compact ? <div className="truncate text-xs text-muted-foreground">{option.description}</div> : null}
        </div>
        {selected ? (
          <Badge className={cn("absolute right-2 top-2", compact && "size-6 justify-center rounded-full p-0")} variant="default">
            <Check data-icon="inline-start" />
            {compact ? null : "已选"}
          </Badge>
        ) : null}
      </div>
      <div className={cn("flex items-center justify-between gap-2 p-2", compact && "hidden")}>
        <Badge variant={option.isDefault ? "secondary" : "outline"}>{option.badge}</Badge>
        <span className="truncate text-xs text-muted-foreground">{option.isDefault ? "系统默认" : "Toonflow"}</span>
      </div>
    </button>
  );
}

function manualOptionFromTemplate(template: PromptTemplate, kind: "director" | "visual"): ManualStyleOption {
  const promptVersionId = template.activeVersion?.id ?? "";
  const defaultKey = kind === "director" ? DEFAULT_DIRECTOR_MANUAL_KEY : DEFAULT_VISUAL_MANUAL_KEY;
  const isDefault = template.templateKey === defaultKey;
  const styleSlug = parseToonflowManualSlug(template.templateKey, kind);
  const info = kind === "visual" && styleSlug ? visualStyleInfo[styleSlug] : kind === "director" && styleSlug ? directorStyleInfo[styleSlug] : undefined;
  return {
    kind,
    templateKey: template.templateKey,
    promptVersionId,
    name: isDefault ? (kind === "director" ? "默认导演手册" : "默认视觉手册") : info?.name ?? template.name,
    description: info?.description ?? template.description ?? (kind === "director" ? "通用导演规划与分镜规则" : "通用资产与分镜视觉规则"),
    badge: isDefault ? "默认" : kind === "director" ? "叙事" : "视觉",
    styleSlug,
    imageSrc: isDefault ? defaultManualImages[kind] : info?.image,
    isDefault,
  };
}

function parseToonflowManualSlug(templateKey: string, kind: "director" | "visual") {
  const prefix = kind === "director" ? "toonflow_director_manual_" : "toonflow_visual_manual_";
  return templateKey.startsWith(prefix) ? templateKey.slice(prefix.length) : undefined;
}

function isJsonRecord(value: unknown): value is JsonRecord {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

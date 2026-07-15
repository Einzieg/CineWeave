"use client";

import NextImage from "next/image";
import type { ReactNode } from "react";
import { Check, Image as ImageIcon } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";

export type ShotReferenceMode = "auto" | "custom" | "none";

export type ShotReferenceOption = {
  key: string;
  sourceType: string;
  title: string;
  assetName?: string;
  previewUrl?: string;
  autoSelected: boolean;
};

export function ReferenceModeSelector({
  value,
  customDisabled,
  disabled = false,
  onChange,
}: {
  value: ShotReferenceMode;
  customDisabled: boolean;
  disabled?: boolean;
  onChange: (mode: ShotReferenceMode) => void;
}) {
  return (
    <div className="grid grid-cols-3 gap-1 rounded-md bg-muted p-1">
      <ModeButton active={value === "auto"} onClick={() => onChange("auto")} disabled={disabled}>自动</ModeButton>
      <ModeButton active={value === "custom"} onClick={() => onChange("custom")} disabled={disabled || customDisabled}>手动选择</ModeButton>
      <ModeButton active={value === "none"} onClick={() => onChange("none")} disabled={disabled}>不使用</ModeButton>
    </div>
  );
}

export function ReferenceOptionCard({
  option,
  checked,
  disabled,
  loadPreview = true,
  sourceLabel,
  onCheckedChange,
  onOpen,
}: {
  option: ShotReferenceOption;
  checked: boolean;
  disabled: boolean;
  loadPreview?: boolean;
  sourceLabel: string;
  onCheckedChange: (checked: boolean) => void;
  onOpen: () => void;
}) {
  return (
    <div className={cn("grid grid-cols-[76px_1fr] gap-3 rounded-md border bg-background p-2", checked && "border-primary/60 bg-primary/[0.03]")}>
      <button type="button" className="relative h-16 overflow-hidden rounded bg-muted" onClick={onOpen} disabled={!loadPreview || !option.previewUrl}>
        {loadPreview && option.previewUrl ? (
          <NextImage src={option.previewUrl} alt={option.title} fill unoptimized sizes="76px" className="object-cover" />
        ) : (
          <span className="grid h-full place-items-center"><ImageIcon className="size-5 text-muted-foreground/50" /></span>
        )}
      </button>
      <label className={cn("flex min-w-0 cursor-pointer items-start gap-2", disabled && "cursor-default")}>
        <Checkbox checked={checked} disabled={disabled} onCheckedChange={(value) => onCheckedChange(value === true)} />
        <span className="min-w-0">
          <span className="block truncate text-xs font-medium">{option.assetName || option.title}</span>
          <span className="mt-1 block text-[11px] text-muted-foreground">{sourceLabel}</span>
          {option.autoSelected ? <span className="mt-1 inline-flex items-center gap-1 text-[11px] text-primary"><Check className="size-3" />自动采用</span> : null}
        </span>
      </label>
    </div>
  );
}

function ModeButton({ active, disabled, onClick, children }: { active: boolean; disabled?: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      className={cn("rounded px-2 py-1.5 text-xs font-medium text-muted-foreground transition-colors", active && "bg-background text-foreground shadow-sm", disabled && "cursor-not-allowed opacity-40")}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

export function normalizeReferenceMode(value?: string): ShotReferenceMode {
  return value === "custom" || value === "none" ? value : "auto";
}

export function formatGenerationTime(value?: string) {
  if (!value) return "未记录时间";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN", { hour12: false });
}

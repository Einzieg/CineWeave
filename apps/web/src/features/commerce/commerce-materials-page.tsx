"use client";

import NextImage from "next/image";
import { useMemo, useRef, useState } from "react";
import {
  Check,
  ImagePlus,
  Loader2,
  Maximize2,
  Package,
  Save,
  Star,
  Trash2,
  Upload,
} from "lucide-react";
import { toast } from "sonner";

import { SectionTitle, Surface } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { studioApi } from "@/lib/api-client";
import { userFacingErrorMessage } from "@/lib/error-localization";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type {
  CommerceProduct,
  CommerceProductReference,
  JsonRecord,
  JsonValue,
} from "@/lib/types";

type ProductReferenceRole = "front" | "back" | "detail" | "usage" | "logo" | "other";

type ProductDraft = {
  name: string;
  brand: string;
  sellingPoints: string;
  immutableFeatures: string;
  prohibitedClaims: string;
  notes: string;
};

const emptyDraft: ProductDraft = {
  name: "",
  brand: "",
  sellingPoints: "",
  immutableFeatures: "",
  prohibitedClaims: "",
  notes: "",
};

const referenceRoles: Array<{ value: ProductReferenceRole; label: string }> = [
  { value: "front", label: "商品正面" },
  { value: "back", label: "商品背面" },
  { value: "detail", label: "细节特写" },
  { value: "usage", label: "使用场景" },
  { value: "logo", label: "品牌标识" },
  { value: "other", label: "其他图片" },
];

export function CommerceMaterialsPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const uploadInputRef = useRef<HTMLInputElement | null>(null);
  const [draftOverride, setDraftOverride] = useState<ProductDraft | null>(null);
  const [preview, setPreview] = useState<CommerceProductReference | null>(null);

  const productQuery = useApiQuery({
    key: qk.commerceProduct(projectId),
    queryFn: (session) => studioApi.getCommerceProduct(session, projectId),
    retry: false,
  });
  const product = productQuery.data;

  const referencesQuery = useApiQuery({
    key: qk.commerceProductReferences(projectId, "active"),
    queryFn: (session) => studioApi.listCommerceProductReferences(session, projectId, "active"),
    enabled: Boolean(product?.id),
  });
  const references = useMemo(
    () => [...(referencesQuery.data?.items ?? [])].sort((left, right) => left.ordinal - right.ordinal),
    [referencesQuery.data?.items],
  );
  const persistedDraft = useMemo(() => productToDraft(product), [product]);
  const draft = draftOverride ?? persistedDraft;
  const updateDraft = (patch: Partial<ProductDraft>) => {
    setDraftOverride((current) => ({ ...(current ?? persistedDraft), ...patch }));
  };

  const saveProduct = useApiMutation({
    mutationFn: (session, input: ProductDraft) =>
      studioApi.createCommerceProductVersion(session, projectId, {
        expectedRevision: product?.revision ?? 0,
        name: input.name.trim(),
        brand: input.brand.trim(),
        sellingPoints: lines(input.sellingPoints),
        immutableFeatures: { items: lines(input.immutableFeatures) },
        prohibitedClaims: lines(input.prohibitedClaims),
        metadata: { ...(product?.metadata ?? {}), notes: input.notes.trim() },
      }),
    onSuccess: () => {
      setDraftOverride(null);
      invalidate([
        qk.commerceProduct(projectId),
        qk.commerceScriptUnitsRoot(projectId),
      ]);
      toast.success("商品配置已保存");
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "商品配置保存失败")),
  });

  const uploadReferences = useApiMutation({
    mutationFn: async (session, files: File[]) => {
      const uploaded: CommerceProductReference[] = [];
      for (const [index, file] of files.entries()) {
        const ticket = await studioApi.createCommerceProductReferenceUpload(
          session,
          projectId,
          { fileName: file.name, mimeType: file.type },
          `commerce-product-reference-${crypto.randomUUID()}`,
        );
        await studioApi.uploadCommerceProductReferenceFile(ticket, file);
        uploaded.push(
          await studioApi.completeCommerceProductReferenceUpload(session, projectId, {
            uploadId: ticket.uploadId,
            referenceRole: "other",
            setPrimary: references.length === 0 && index === 0,
          }),
        );
      }
      return uploaded;
    },
    onSuccess: (items) => {
      invalidate([
        qk.commerceProduct(projectId),
        qk.commerceProductReferencesRoot(projectId),
      ]);
      toast.success(`已上传 ${items.length} 张商品图片`);
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "商品图片上传失败")),
  });

  const updateReference = useApiMutation({
    mutationFn: (
      session,
      input: {
        item: CommerceProductReference;
        referenceRole?: string;
        setPrimary?: boolean;
      },
    ) =>
      studioApi.updateCommerceProductReference(session, projectId, input.item.id, {
        expectedRevision: input.item.revision,
        referenceRole: input.referenceRole,
        setPrimary: input.setPrimary,
      }),
    onSuccess: () => {
      invalidate([
        qk.commerceProduct(projectId),
        qk.commerceProductReferencesRoot(projectId),
      ]);
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "商品图片更新失败")),
  });

  const archiveReference = useApiMutation({
    mutationFn: (session, item: CommerceProductReference) =>
      studioApi.archiveCommerceProductReference(session, projectId, item.id, item.revision),
    onSuccess: () => {
      invalidate([qk.commerceProductReferencesRoot(projectId)]);
      toast.success("商品图片已移除");
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "商品图片移除失败")),
  });

  const isInitialLoading = productQuery.isLoading && !product;
  if (isInitialLoading) {
    return <CommerceMaterialsSkeleton />;
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">商品配置</h1>
          <p className="mt-1 text-sm text-muted-foreground">商品资料与参考图会作为每条广告视频的默认输入。</p>
        </div>
        <Button
          type="button"
          disabled={!draft.name.trim() || saveProduct.isPending}
          onClick={() => saveProduct.mutate(draft)}
        >
          {saveProduct.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
          保存商品
        </Button>
      </div>

      <Surface>
        <SectionTitle title="商品资料" />
        <div className="grid gap-5 p-4 xl:grid-cols-2">
          <div className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="商品名称" required>
                <Input
                  aria-label="商品名称"
                  value={draft.name}
                  onChange={(event) => updateDraft({ name: event.target.value })}
                  placeholder="输入商品名称"
                />
              </Field>
              <Field label="品牌">
                <Input
                  aria-label="品牌"
                  value={draft.brand}
                  onChange={(event) => updateDraft({ brand: event.target.value })}
                  placeholder="输入品牌名称"
                />
              </Field>
            </div>
            <Field label="核心卖点">
              <Textarea
                aria-label="核心卖点"
                className="min-h-32 resize-y"
                value={draft.sellingPoints}
                onChange={(event) => updateDraft({ sellingPoints: event.target.value })}
                placeholder="每行一个卖点"
              />
            </Field>
            <Field label="备注">
              <Textarea
                aria-label="备注"
                className="min-h-24 resize-y"
                value={draft.notes}
                onChange={(event) => updateDraft({ notes: event.target.value })}
                placeholder="补充拍摄或使用信息"
              />
            </Field>
          </div>

          <div className="space-y-4">
            <Field label="必须保持的外观特征">
              <Textarea
                aria-label="必须保持的外观特征"
                className="min-h-32 resize-y"
                value={draft.immutableFeatures}
                onChange={(event) => updateDraft({ immutableFeatures: event.target.value })}
                placeholder="每行一个不可改变的商品特征"
              />
            </Field>
            <Field label="禁用声明">
              <Textarea
                aria-label="禁用声明"
                className="min-h-32 resize-y"
                value={draft.prohibitedClaims}
                onChange={(event) => updateDraft({ prohibitedClaims: event.target.value })}
                placeholder="每行一条禁止出现的宣传说法"
              />
            </Field>
          </div>
        </div>
      </Surface>

      <Surface>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
          <div>
            <h2 className="text-sm font-semibold">商品参考图</h2>
            <p className="mt-1 text-sm text-muted-foreground">未单独指定时，视频会优先使用主图及其余商品图片。</p>
          </div>
          <input
            ref={uploadInputRef}
            className="hidden"
            type="file"
            accept="image/jpeg,image/png,image/webp"
            multiple
            onChange={(event) => {
              const files = Array.from(event.target.files ?? []);
              event.currentTarget.value = "";
              if (files.length) uploadReferences.mutate(files);
            }}
          />
          <Button
            type="button"
            variant="outline"
            disabled={!product?.id || uploadReferences.isPending}
            onClick={() => uploadInputRef.current?.click()}
          >
            {uploadReferences.isPending ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />}
            上传图片
          </Button>
        </div>

        <div className="p-4">
          {!product?.id ? (
            <EmptyReferences
              icon={<Package className="size-5" />}
              title="先保存商品资料"
              description="保存后即可上传商品参考图。"
            />
          ) : referencesQuery.isLoading ? (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {[1, 2, 3, 4].map((item) => <Skeleton key={item} className="h-56" />)}
            </div>
          ) : references.length ? (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {references.map((item) => (
                <ProductReferenceItem
                  key={item.id}
                  item={item}
                  updating={updateReference.isPending && updateReference.variables?.item.id === item.id}
                  removing={archiveReference.isPending && archiveReference.variables?.id === item.id}
                  onPreview={() => setPreview(item)}
                  onRoleChange={(referenceRole) => updateReference.mutate({ item, referenceRole })}
                  onSetPrimary={() => updateReference.mutate({ item, setPrimary: true })}
                  onRemove={() => archiveReference.mutate(item)}
                />
              ))}
            </div>
          ) : (
            <EmptyReferences
              icon={<ImagePlus className="size-5" />}
              title="还没有商品参考图"
              description="上传至少一张清晰商品图，第一张会自动设为主图。"
            />
          )}
        </div>
      </Surface>

      <Dialog open={Boolean(preview)} onOpenChange={(open) => !open && setPreview(null)}>
        <DialogContent className="max-w-5xl">
          <DialogHeader>
            <DialogTitle>商品参考图</DialogTitle>
          </DialogHeader>
          {preview?.previewUrl ? (
            <div className="relative h-[72vh] min-h-80 overflow-hidden bg-muted">
              <NextImage
                src={preview.previewUrl}
                alt="商品参考图大图"
                fill
                unoptimized
                className="object-contain"
                sizes="90vw"
              />
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ProductReferenceItem({
  item,
  updating,
  removing,
  onPreview,
  onRoleChange,
  onSetPrimary,
  onRemove,
}: {
  item: CommerceProductReference;
  updating: boolean;
  removing: boolean;
  onPreview: () => void;
  onRoleChange: (value: ProductReferenceRole) => void;
  onSetPrimary: () => void;
  onRemove: () => void;
}) {
  const selectedRole = item.isPrimary ? "front" : normalizeReferenceRole(item.referenceRole);
  return (
    <article className="overflow-hidden rounded-md border bg-background">
      <button
        type="button"
        className="group relative block h-36 w-full overflow-hidden bg-muted"
        onClick={onPreview}
        title="查看大图"
      >
        {item.previewUrl ? (
          <NextImage
            src={item.previewUrl}
            alt="商品参考图"
            fill
            unoptimized
            className="object-cover transition-transform group-hover:scale-[1.02]"
            sizes="(max-width: 768px) 50vw, 25vw"
          />
        ) : (
          <div className="flex h-full items-center justify-center text-muted-foreground">
            <ImagePlus className="size-6" />
          </div>
        )}
        <span className="absolute right-2 top-2 rounded bg-background/90 p-1.5 opacity-0 shadow-sm transition-opacity group-hover:opacity-100">
          <Maximize2 className="size-4" />
        </span>
        {item.isPrimary ? (
          <Badge className="absolute left-2 top-2 gap-1 bg-emerald-600 text-white hover:bg-emerald-600">
            <Star className="size-3 fill-current" />
            主图
          </Badge>
        ) : null}
      </button>
      <div className="space-y-3 p-3">
        <Select
          value={selectedRole}
          disabled={updating || removing}
          onValueChange={(value) => onRoleChange(value as ProductReferenceRole)}
        >
          <SelectTrigger className="h-8 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {referenceRoles.map((role) => (
              <SelectItem key={role.value} value={role.value}>{role.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex items-center justify-between gap-2">
          <Button
            type="button"
            size="sm"
            variant={item.isPrimary ? "secondary" : "outline"}
            disabled={item.isPrimary || updating || removing}
            onClick={onSetPrimary}
          >
            {updating ? <Loader2 className="size-3.5 animate-spin" /> : item.isPrimary ? <Check className="size-3.5" /> : <Star className="size-3.5" />}
            {item.isPrimary ? "当前主图" : "设为主图"}
          </Button>
          <Button
            type="button"
            size="icon"
            variant="ghost"
            className="size-8 text-destructive hover:text-destructive"
            disabled={removing || updating}
            onClick={onRemove}
            title="移除图片"
          >
            {removing ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
          </Button>
        </div>
      </div>
    </article>
  );
}

function EmptyReferences({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <div className="flex min-h-48 flex-col items-center justify-center gap-2 border border-dashed text-center">
      <span className="text-muted-foreground">{icon}</span>
      <p className="text-sm font-medium">{title}</p>
      <p className="text-sm text-muted-foreground">{description}</p>
    </div>
  );
}

function Field({
  label,
  required = false,
  children,
}: {
  label: string;
  required?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      <Label>
        {label}
        {required ? <span className="ml-1 text-destructive">*</span> : null}
      </Label>
      {children}
    </div>
  );
}

function CommerceMaterialsSkeleton() {
  return (
    <div className="space-y-5">
      <Skeleton className="h-10 w-52" />
      <Skeleton className="h-[430px] w-full" />
      <Skeleton className="h-[300px] w-full" />
    </div>
  );
}

function lines(value: string) {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function productToDraft(product?: CommerceProduct): ProductDraft {
  const version = product?.currentVersion;
  if (!version) return emptyDraft;
  return {
    name: version.name,
    brand: version.brand,
    sellingPoints: jsonListToLines(version.sellingPoints),
    immutableFeatures: immutableFeaturesToLines(version.immutableFeatures),
    prohibitedClaims: jsonListToLines(version.prohibitedClaims),
    notes: metadataNotes(product.metadata),
  };
}

function jsonListToLines(value: JsonValue) {
  if (!Array.isArray(value)) return "";
  return value.filter((item): item is string => typeof item === "string").join("\n");
}

function immutableFeaturesToLines(value: JsonValue) {
  if (!value || Array.isArray(value) || typeof value !== "object") return "";
  const record = value as JsonRecord;
  if (Array.isArray(record.items)) {
    return record.items.filter((item): item is string => typeof item === "string").join("\n");
  }
  return Object.entries(record)
    .map(([key, item]) => typeof item === "string" ? `${key}：${item}` : "")
    .filter(Boolean)
    .join("\n");
}

function metadataNotes(metadata: JsonRecord) {
  return typeof metadata.notes === "string" ? metadata.notes : "";
}

function normalizeReferenceRole(value: string): ProductReferenceRole {
  return referenceRoles.some((role) => role.value === value) ? value as ProductReferenceRole : "other";
}

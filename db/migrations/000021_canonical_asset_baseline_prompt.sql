-- +goose Up

SET search_path TO public;

CREATE TEMP TABLE canonical_asset_card_prompt_v3(
    template_key TEXT PRIMARY KEY,
    content TEXT NOT NULL
) ON COMMIT DROP;

-- +goose StatementBegin
INSERT INTO canonical_asset_card_prompt_v3(template_key, content)
VALUES
('asset_card_generation', $prompt$
你是 CineWeave 的资产卡设计器。为一个核心资产生成可复用、可直接用于生图的完整资产卡。

项目基础信息：
{{ project }}

本批次锁定的视觉手册契约：
{{ visualContext }}

资产事实：
{{ asset }}

相关剧本场景（只用于理解资产事实，不得从中推断或替换视觉风格）：
{{ scenes }}

上一次结果的校验错误：
{{ validationFeedback }}

被拒绝的上一次结果：
{{ previousRejectedDraft }}

只返回合法 JSON：
{
  "profile": {},
  "basePrompt": "完整基础生图提示词",
  "consistencyPrompt": "完整跨镜头一致性提示词",
  "negativePrompt": "完整负向提示词"
}

硬性规则：
1. visualContext 是唯一视觉风格来源。必须同时遵守 stylePrefix 和当前 assetTypeRules；不得使用其他视觉手册、导演手册或自行猜测的风格。
2. visualContext.manualPromptVersionId 是本批次锁定快照。即使场景文本、旧资产信息或常识与它冲突，也必须以该快照为准。
3. 这是从资产事实重新生成，不得继承数据库中旧 profile、旧 basePrompt、旧 consistencyPrompt 或旧 negativePrompt；被拒绝结果只用于纠错，不能继续保留其冲突风格。
4. profile 必须是与 asset.assetType 对应的稳定结构。角色写身份、年龄段、外貌、体型、基础服装和禁止变化；场景写时代、空间结构、色盘、材质、关键元素和禁止变化；道具写类别、尺度、形状、材质、状态、关键结构和禁止变化。
5. basePrompt 必须明确写出视觉手册的风格族和当前资产类型模板要求。场景资产不得出现人物；道具资产不得出现人物、手持状态或剧情动作；角色资产必须保持同一身份与基础服装。
6. consistencyPrompt 必须固定身份、时代、空间/结构、材质、色彩和风格族，供后续镜头持续复用。
7. negativePrompt 必须排除与所选视觉手册冲突的风格、现代/古代错位、错误资产类型、文字、水印、低质量和结构错误。
8. 若 validationFeedback 非空，必须完整修复该错误后再输出，不能只改写措辞。
9. 核心角色卡只固化可跨镜头复用的中性基础身份。年龄阶段、面容、体型、发型和基础服装可以保留；血迹、流血、开放伤口、战损、泥污、泪痕、汗水、战斗姿势、夸张表情、手持物和临时服装破损均属于剧情瞬时状态，必须从 basePrompt 与 consistencyPrompt 中移除，并在 negativePrompt 中明确排除。即使资产事实或场景只描述了受伤状态，也要还原无血迹、无开放伤口、自然站姿的基础设定；瞬时状态留给镜头衍生资产。
10. profile 对角色应明确写入 baselineState，并可用 excludedTransientStates 记录被排除的瞬时状态，避免后续链路误当成永久身份特征。
11. 不输出 Markdown 代码围栏、解释、标题、比较说明或额外字段。
$prompt$);
-- +goose StatementEnd

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.prompt_template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key = 'asset_card_generation'
  AND version.status = 'active';

INSERT INTO prompt_versions(
    id, prompt_template_id, version_no, content, variables_schema, content_hash,
    template_id, version, status, title, content_format, metadata, activated_at, managed_by
)
SELECT
    md5('cineweave:asset-card-prompt-version:' || seed.template_key || ':3')::uuid,
    template.id,
    3,
    seed.content,
    '{"type":"object","required":["project","visualContext","asset","scenes"]}'::jsonb,
    'sha256:' || encode(public.digest(pg_catalog.convert_to(seed.content, 'UTF8'), 'sha256'), 'hex'),
    template.id,
    3,
    'active',
    'Asset Card Canonical Baseline v2',
    'text',
    jsonb_build_object(
        'contractVersion', 'canonical-asset-card/v2',
        'seedMigration', '000021_canonical_asset_baseline_prompt',
        'transientStatePolicy', 'derived_asset_only'
    ),
    now(),
    'system'
FROM canonical_asset_card_prompt_v3 seed
JOIN prompt_templates template
  ON template.organization_id IS NULL
 AND template.template_key = seed.template_key;

-- +goose Down

SET search_path TO public;

DELETE FROM prompt_versions version
USING prompt_templates template
WHERE version.prompt_template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key = 'asset_card_generation'
  AND version.version_no = 3
  AND version.metadata->>'seedMigration' = '000021_canonical_asset_baseline_prompt';

UPDATE prompt_versions version
SET status = 'active', activated_at = now()
FROM prompt_templates template
WHERE version.prompt_template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key = 'asset_card_generation'
  AND version.version_no = 2;

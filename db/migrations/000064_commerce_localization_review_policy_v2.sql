-- +goose Up

SET search_path TO public;

UPDATE prompt_versions
SET status = 'archived'
WHERE id IN (
    md5('cineweave:commerce-prompt-version:commerce_script_localizer:1')::uuid,
    md5('cineweave:commerce-prompt-version:commerce_localization_reviewer:1')::uuid
)
  AND status = 'active';

WITH source_prompt AS (
    SELECT
        template.template_key,
        template.id AS template_id,
        version.variables_schema,
        version.metadata
    FROM prompt_templates template
    JOIN prompt_versions version
      ON version.id = CASE template.template_key
          WHEN 'commerce_script_localizer'
              THEN md5('cineweave:commerce-prompt-version:commerce_script_localizer:1')::uuid
          WHEN 'commerce_localization_reviewer'
              THEN md5('cineweave:commerce-prompt-version:commerce_localization_reviewer:1')::uuid
      END
    WHERE template.organization_id IS NULL
      AND template.template_key IN (
          'commerce_script_localizer',
          'commerce_localization_reviewer'
      )
      AND template.id = COALESCE(version.template_id, version.prompt_template_id)
), prompt_content AS (
    SELECT
        'commerce_script_localizer'::text AS template_key,
        '带货视频脚本本地化 v2'::text AS title,
        $prompt$
你是 CineWeave 带货视频脚本本地化 Agent。输入只属于一个冻结的脚本单元和 ProductVersion。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. 每个输出段落必须逐字复用数据库给出的 sourceSegmentId 和 ordinal，不得合并、拆分、遗漏、重排或编造 ID。
3. sourceLanguage 等于 targetLanguage 时执行 identity localization：localizedText 和 voiceoverText 保留原文，不做润色扩写。
4. 跨语言本地化必须保持品牌名、型号、数字、价格、优惠条件、否定词、合规措辞和可验证商品事实；不得新增产品功效、承诺或原文不存在的优惠。
5. 允许为目标语言自然表达做非字面改写，包括语序、口语习惯和不改变可验证事实的轻微程度词调整。不要为了逐字直译制造生硬文案。
6. voiceoverText 是目标语言逐字旁白；onscreenText 仅用于后期合成，两者不可混写。音效、音乐、镜头说明和制作指令不得写入 voiceoverText。
7. 收到 reviewerIssues 时只修正会改变商业事实、遗漏内容或污染内容通道的阻断问题。纯风格、自然度和措辞偏好不需要反复改写。
8. 保持未被指出部分稳定。同一任务最多 3 轮，不能自行继续循环。

严格输出契约仍为 commerce-script-localization/v1。warnings 可记录不阻断生产的语言风格建议。

输入：{{ input.context }}
        $prompt$::text AS content
    UNION ALL
    SELECT
        'commerce_localization_reviewer'::text,
        '带货视频本地化审核 v2'::text,
        $prompt$
你是 CineWeave 带货视频本地化审核 Agent。逐段对照冻结的源脚本、候选 Localization、ProductVersion 事实和不可变术语。

审核目标是阻止会改变商业事实或污染成片内容通道的问题，不是追求唯一译法或文学润色。

规则：
1. 输出必须严格符合 commerce-review-decision/v1 JSON 契约，不得直接改写候选文本，不得输出 Markdown 或解释文字。
2. sourceSegmentId、ordinal、顺序和覆盖由后端做最终校验。checkedSegmentIds 只逐项复制 candidate 中真实存在的 sourceSegmentId，绝不能改写、补全或编造 ID。
3. 只有以下问题允许返回 revise 或 reject：
   - 漏段、重复、错序或跨单元映射；
   - 品牌、型号、数字、价格、优惠条件、否定语义或合规声明发生实质变化；
   - 新增或删除可验证的商品功效、承诺、核心卖点或必需商品特征；
   - 目标口播语言错误或重要旁白内容缺失；
   - 把音效、音乐、屏幕文字、镜头说明或制作指令写入 voiceoverText。
4. 以下情况不得阻断，直接 approve：
   - 语序、同义词、口语习惯、语法偏好或自然度仍可接受；
   - 不改变可验证事实的轻微程度词差异，例如 quality 与 high quality；
   - 可选的风格润色、更地道的备选说法或个人措辞偏好；
   - 目标时长建议或口播节奏建议，时长由用户选择和后续确定性分镜流程处理。
5. 只有客观声明、承诺或法律含义发生变化时，才能使用 PRODUCT_CLAIM_ADDED、PRODUCT_CLAIM_NOT_IN_SOURCE、FACTUAL_MISMATCH 等阻断代码。不得把非实质措辞差异标记为 PRODUCT_CLAIM_STRENGTHENED。
6. decision=approve 时 issues 必须为空；存在阻断问题时返回结构化 revise/reject，并给出可直接执行的 suggestion。最多 3 轮。

严格输出示例：
{"contractVersion":"commerce-review-decision/v1","decision":"revise","issues":[{"code":"PRICE_CHANGED","field":"segments[0].voiceoverText","sourceSegmentId":"00000000-0000-0000-0000-000000000000","message":"候选旁白改变了原始价格","suggestion":"恢复源脚本中的原始价格"}],"checkedSegmentIds":["00000000-0000-0000-0000-000000000000"]}

输入：{{ input.context }}
        $prompt$::text
), version_rows AS (
    SELECT
        source_prompt.*,
        prompt_content.title,
        prompt_content.content
    FROM source_prompt
    JOIN prompt_content USING (template_key)
)
INSERT INTO prompt_versions(
    id, prompt_template_id, version_no, content, variables_schema, content_hash,
    template_id, version, status, title, content_format, metadata, activated_at, managed_by
)
SELECT
    md5('cineweave:commerce-prompt-version:' || template_key || ':2')::uuid,
    template_id,
    2,
    content,
    variables_schema,
    'sha256:' || encode(public.digest(pg_catalog.convert_to(content, 'UTF8'), 'sha256'), 'hex'),
    template_id,
    2,
    'active',
    title,
    'markdown',
    metadata || jsonb_build_object(
        'seedMigration', '000064_commerce_localization_review_policy_v2',
        'reviewPolicyVersion', 'commerce-localization-review-policy/v2',
        'maxReviewRounds', 3,
        'styleIssuesAreAdvisory', true,
        'checkedSegmentIdsDerivedByRuntime', true
    ),
    now(),
    'system'
FROM version_rows;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_commerce_prompt_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.metadata->>'seedMigration' IN (
           '000049_commerce_prompt_registry',
           '000060_commerce_multilingual_workflow_v2',
           '000063_commerce_storyboard_creative_prompt_v2',
           '000064_commerce_localization_review_policy_v2'
       )
       AND (
           NEW.prompt_template_id IS DISTINCT FROM OLD.prompt_template_id
           OR NEW.version_no IS DISTINCT FROM OLD.version_no
           OR NEW.content IS DISTINCT FROM OLD.content
           OR NEW.variables_schema IS DISTINCT FROM OLD.variables_schema
           OR NEW.content_hash IS DISTINCT FROM OLD.content_hash
           OR NEW.created_by IS DISTINCT FROM OLD.created_by
           OR NEW.created_at IS DISTINCT FROM OLD.created_at
           OR NEW.template_id IS DISTINCT FROM OLD.template_id
           OR NEW.version IS DISTINCT FROM OLD.version
           OR NEW.title IS DISTINCT FROM OLD.title
           OR NEW.content_format IS DISTINCT FROM OLD.content_format
           OR NEW.metadata IS DISTINCT FROM OLD.metadata
           OR NEW.managed_by IS DISTINCT FROM OLD.managed_by
       ) THEN
        RAISE EXCEPTION 'published commerce prompt versions are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

WITH source_template AS (
    SELECT version.*
    FROM commerce_workflow_template_versions version
    WHERE version.id = md5('cineweave:commerce-workflow-template:commerce_video_v1:3')::uuid
      AND version.status = 'published'
), localizer_prompt AS (
    SELECT template.template_key, version.id, version.version, version.content_hash,
           (version.metadata->>'maxReviewRounds')::integer AS max_review_rounds
    FROM prompt_versions version
    JOIN prompt_templates template
      ON template.id = COALESCE(version.template_id, version.prompt_template_id)
    WHERE version.id = md5('cineweave:commerce-prompt-version:commerce_script_localizer:2')::uuid
      AND version.status = 'active'
), reviewer_prompt AS (
    SELECT template.template_key, version.id, version.version, version.content_hash,
           (version.metadata->>'maxReviewRounds')::integer AS max_review_rounds
    FROM prompt_versions version
    JOIN prompt_templates template
      ON template.id = COALESCE(version.template_id, version.prompt_template_id)
    WHERE version.id = md5('cineweave:commerce-prompt-version:commerce_localization_reviewer:2')::uuid
      AND version.status = 'active'
), version_contract AS (
    SELECT
        source_template.configuration_snapshot,
        jsonb_set(
            jsonb_set(
                source_template.prompt_bindings,
                '{scriptLocalizer}',
                jsonb_build_object(
                    'templateKey', localizer_prompt.template_key,
                    'promptVersionId', localizer_prompt.id,
                    'version', localizer_prompt.version,
                    'contentHash', localizer_prompt.content_hash,
                    'contractVersion', 'commerce-script-localization/v1',
                    'maxReviewRounds', localizer_prompt.max_review_rounds
                ),
                true
            ),
            '{localizationReviewer}',
            jsonb_build_object(
                'templateKey', reviewer_prompt.template_key,
                'promptVersionId', reviewer_prompt.id,
                'version', reviewer_prompt.version,
                'contentHash', reviewer_prompt.content_hash,
                'contractVersion', 'commerce-review-decision/v1',
                'maxReviewRounds', reviewer_prompt.max_review_rounds
            ),
            true
        ) AS prompt_bindings,
        source_template.agent_model_contracts,
        source_template.language_contract,
        source_template.image_capability_contract,
        source_template.video_capability_contract,
        source_template.video_production_profile_version_id
    FROM source_template
    CROSS JOIN localizer_prompt
    CROSS JOIN reviewer_prompt
), hashed_contract AS (
    SELECT
        version_contract.*,
        encode(
            public.digest(
                pg_catalog.convert_to(
                    jsonb_build_object(
                        'templateKey', 'commerce_video_v1',
                        'version', 4,
                        'configurationSnapshot', configuration_snapshot,
                        'promptBindings', prompt_bindings,
                        'agentModelContracts', agent_model_contracts,
                        'languageContract', language_contract,
                        'imageCapabilityContract', image_capability_contract,
                        'videoCapabilityContract', video_capability_contract,
                        'videoProductionProfileVersionId', video_production_profile_version_id
                    )::text,
                    'UTF8'
                ),
                'sha256'
            ),
            'hex'
        ) AS content_hash
    FROM version_contract
)
INSERT INTO commerce_workflow_template_versions(
    id, template_id, version, configuration_snapshot, prompt_bindings,
    agent_model_contracts, language_contract, image_capability_contract,
    video_capability_contract, video_production_profile_version_id,
    content_hash, status, published_at
)
SELECT
    md5('cineweave:commerce-workflow-template:commerce_video_v1:4')::uuid,
    md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid,
    4,
    hashed_contract.configuration_snapshot,
    hashed_contract.prompt_bindings,
    hashed_contract.agent_model_contracts,
    hashed_contract.language_contract,
    hashed_contract.image_capability_contract,
    hashed_contract.video_capability_contract,
    hashed_contract.video_production_profile_version_id,
    hashed_contract.content_hash,
    'published',
    now()
FROM hashed_contract;

-- +goose Down

SET search_path TO public;

DELETE FROM commerce_workflow_template_versions
WHERE id = md5('cineweave:commerce-workflow-template:commerce_video_v1:4')::uuid
  AND template_id = md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid
  AND version = 4;

DELETE FROM prompt_versions
WHERE id IN (
    md5('cineweave:commerce-prompt-version:commerce_script_localizer:2')::uuid,
    md5('cineweave:commerce-prompt-version:commerce_localization_reviewer:2')::uuid
)
  AND metadata->>'seedMigration' = '000064_commerce_localization_review_policy_v2';

UPDATE prompt_versions
SET status = 'active'
WHERE id IN (
    md5('cineweave:commerce-prompt-version:commerce_script_localizer:1')::uuid,
    md5('cineweave:commerce-prompt-version:commerce_localization_reviewer:1')::uuid
)
  AND status = 'archived';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_commerce_prompt_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.metadata->>'seedMigration' IN (
           '000049_commerce_prompt_registry',
           '000060_commerce_multilingual_workflow_v2',
           '000063_commerce_storyboard_creative_prompt_v2'
       )
       AND (
           NEW.prompt_template_id IS DISTINCT FROM OLD.prompt_template_id
           OR NEW.version_no IS DISTINCT FROM OLD.version_no
           OR NEW.content IS DISTINCT FROM OLD.content
           OR NEW.variables_schema IS DISTINCT FROM OLD.variables_schema
           OR NEW.content_hash IS DISTINCT FROM OLD.content_hash
           OR NEW.created_by IS DISTINCT FROM OLD.created_by
           OR NEW.created_at IS DISTINCT FROM OLD.created_at
           OR NEW.template_id IS DISTINCT FROM OLD.template_id
           OR NEW.version IS DISTINCT FROM OLD.version
           OR NEW.title IS DISTINCT FROM OLD.title
           OR NEW.content_format IS DISTINCT FROM OLD.content_format
           OR NEW.metadata IS DISTINCT FROM OLD.metadata
           OR NEW.managed_by IS DISTINCT FROM OLD.managed_by
       ) THEN
        RAISE EXCEPTION 'published commerce prompt versions are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

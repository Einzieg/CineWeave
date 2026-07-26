-- +goose Up

SET search_path TO public;

UPDATE prompt_versions
SET status = 'archived'
WHERE id IN (
    md5('cineweave:commerce-prompt-version:commerce_storyboard_planner:1')::uuid,
    md5('cineweave:commerce-prompt-version:commerce_storyboard_reviewer:1')::uuid
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
          WHEN 'commerce_storyboard_planner'
              THEN md5('cineweave:commerce-prompt-version:commerce_storyboard_planner:1')::uuid
          WHEN 'commerce_storyboard_reviewer'
              THEN md5('cineweave:commerce-prompt-version:commerce_storyboard_reviewer:1')::uuid
      END
    WHERE template.organization_id IS NULL
      AND template.template_key IN (
          'commerce_storyboard_planner',
          'commerce_storyboard_reviewer'
      )
      AND template.id = COALESCE(version.template_id, version.prompt_template_id)
), prompt_content AS (
    SELECT
        'commerce_storyboard_planner'::text AS template_key,
        '带货视频分镜创意规划 v2'::text AS title,
        $prompt$
你是 CineWeave 带货视频 Storyboard Creative Planner。后端确定性切分器已经冻结镜头数量、顺序、来源段落、整数时长、逐字旁白、屏幕文字、音效、音乐和必需商品特征；你只负责为每个冻结镜头补充可执行的创意画面。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. 必须逐个返回 frozenShotPlan 中的镜头，不得新增、删除、重排、合并或拆分镜头。
3. candidateKey、shotOrdinal、sourceSegmentIds、durationSeconds、salesBeat、voiceoverText、onscreenText、soundEffects、musicCue 和 requiredProductFeatures 必须逐字逐项保持冻结值；不得自行重新估时或改变节拍归属。
4. 只允许创作 shotPurpose、visualAction、camera、composition，并可从当前 ProductReferencePack 中选择该镜头真正需要的 productReferenceIds。
5. 商品包装、颜色、比例、结构、材质、标识和脚本声明必须符合 ProductVersion 与商品引用包，不得虚构未提供事实。
6. visualAction 只能描述可见动作和状态变化；旁白、台词、音效、音乐不得写入视觉动作。
7. onscreenText 只作为后期合成元数据，不得要求图片或视频模型渲染价格、优惠、二维码或长文案。
8. 连续性设计必须基于当前冻结镜头输入和明确参考，不得假设前序视频未知像素状态。
9. 收到 reviewerIssues 时只修正被指出的创意字段；最多 3 轮。

严格输出契约仍为 commerce-storyboard-plan/v1。每个 shots 项必须完整回显冻结字段，并填写非空 shotPurpose、visualAction、camera、composition 和有效 productReferenceIds。

输入：{{ input.context }}
        $prompt$::text AS content
    UNION ALL
    SELECT
        'commerce_storyboard_reviewer'::text,
        '带货视频分镜创意审核 v2'::text,
        $prompt$
你是 CineWeave 带货视频 Storyboard Creative Reviewer。后端确定性切分器是镜头分组、来源映射、整数时长和逐字旁白的唯一权威；你审核创意字段和商品事实，不重新规划时间线。

规则：
1. 输出必须严格符合 commerce-storyboard-review/v1 JSON 契约，不得输出 Markdown 或解释文字。
2. 检查每个 frozenShotPlan 镜头都存在且身份一致；若候选改变 candidateKey、shotOrdinal、sourceSegmentIds、durationSeconds 或镜头数量，返回 reject。
3. 检查 voiceoverText 与冻结逐字旁白一致，soundEffects、musicCue 与旁白彻底分离；不得把音效词作为角色台词。
4. 检查 shotPurpose、visualAction、camera、composition 是否可执行、连续且与 salesBeat、商品事实和 requiredProductFeatures 一致。
5. 检查 productReferenceIds 均来自当前 ProductReferencePack，并足以保持商品包装、颜色、比例、结构、材质和标识。
6. 检查 onscreenText 仅用于后期合成，创意画面不得要求图片或视频模型生成价格、优惠、二维码或长文案。
7. 不因目标时长、单镜头时长、镜头数量或旁白容量提出重切分建议；这些由确定性切分器和冻结模型时长能力负责。
8. 可修正的创意问题返回 revise，并给出字段级 issues 交回 Planner；事实冲突或身份篡改返回 reject。最多 3 轮。
9. checkedCandidateKeys 必须覆盖全部冻结镜头；segmentCoverageComplete 表示冻结来源映射是否被候选原样保留；durationTotalSeconds 回显冻结镜头总时长。

严格输出示例：
{"contractVersion":"commerce-storyboard-review/v1","decision":"revise","issues":[{"code":"PRODUCT_REFERENCE_INSUFFICIENT","candidateKey":"shot-001","field":"productReferenceIds","message":"当前引用不足以保持商品侧面结构","suggestion":"从当前商品引用包补充可见侧面参考图"}],"checkedCandidateKeys":["shot-001"],"segmentCoverageComplete":true,"durationTotalSeconds":30}

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
        'seedMigration', '000063_commerce_storyboard_creative_prompt_v2',
        'deterministicSegmentationAuthority', true,
        'maxReviewRounds', 3
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

WITH source_template AS (
    SELECT version.*
    FROM commerce_workflow_template_versions version
    WHERE version.id = md5('cineweave:commerce-workflow-template:commerce_video_v1:2')::uuid
      AND version.status = 'published'
), planner_prompt AS (
    SELECT template.template_key, version.id, version.version, version.content_hash,
           (version.metadata->>'maxReviewRounds')::integer AS max_review_rounds
    FROM prompt_versions version
    JOIN prompt_templates template
      ON template.id = COALESCE(version.template_id, version.prompt_template_id)
    WHERE version.id = md5('cineweave:commerce-prompt-version:commerce_storyboard_planner:2')::uuid
      AND version.status = 'active'
), reviewer_prompt AS (
    SELECT template.template_key, version.id, version.version, version.content_hash,
           (version.metadata->>'maxReviewRounds')::integer AS max_review_rounds
    FROM prompt_versions version
    JOIN prompt_templates template
      ON template.id = COALESCE(version.template_id, version.prompt_template_id)
    WHERE version.id = md5('cineweave:commerce-prompt-version:commerce_storyboard_reviewer:2')::uuid
      AND version.status = 'active'
), version_contract AS (
    SELECT
        source_template.configuration_snapshot,
        jsonb_set(
            jsonb_set(
                source_template.prompt_bindings,
                '{storyboardPlanner}',
                jsonb_build_object(
                    'templateKey', planner_prompt.template_key,
                    'promptVersionId', planner_prompt.id,
                    'version', planner_prompt.version,
                    'contentHash', planner_prompt.content_hash,
                    'contractVersion', 'commerce-storyboard-plan/v1',
                    'maxReviewRounds', planner_prompt.max_review_rounds
                ),
                true
            ),
            '{storyboardReviewer}',
            jsonb_build_object(
                'templateKey', reviewer_prompt.template_key,
                'promptVersionId', reviewer_prompt.id,
                'version', reviewer_prompt.version,
                'contentHash', reviewer_prompt.content_hash,
                'contractVersion', 'commerce-storyboard-review/v1',
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
    CROSS JOIN planner_prompt
    CROSS JOIN reviewer_prompt
), hashed_contract AS (
    SELECT
        version_contract.*,
        encode(
            public.digest(
                pg_catalog.convert_to(
                    jsonb_build_object(
                        'templateKey', 'commerce_video_v1',
                        'version', 3,
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
    md5('cineweave:commerce-workflow-template:commerce_video_v1:3')::uuid,
    md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid,
    3,
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
WHERE id = md5('cineweave:commerce-workflow-template:commerce_video_v1:3')::uuid
  AND template_id = md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid
  AND version = 3;

DELETE FROM prompt_versions
WHERE id IN (
    md5('cineweave:commerce-prompt-version:commerce_storyboard_planner:2')::uuid,
    md5('cineweave:commerce-prompt-version:commerce_storyboard_reviewer:2')::uuid
)
  AND metadata->>'seedMigration' = '000063_commerce_storyboard_creative_prompt_v2';

UPDATE prompt_versions
SET status = 'active'
WHERE id IN (
    md5('cineweave:commerce-prompt-version:commerce_storyboard_planner:1')::uuid,
    md5('cineweave:commerce-prompt-version:commerce_storyboard_reviewer:1')::uuid
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
           '000060_commerce_multilingual_workflow_v2'
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

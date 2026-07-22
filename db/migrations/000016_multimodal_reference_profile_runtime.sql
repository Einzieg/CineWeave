-- +goose Up

SET search_path TO public;

ALTER TABLE shot_reference_pack_items
    ADD COLUMN media_type TEXT,
    ADD COLUMN semantics TEXT;

UPDATE shot_reference_pack_items
SET media_type = CASE
        WHEN role IN ('video_reference', 'motion_reference') THEN 'video'
        WHEN role = 'audio_reference' THEN 'audio'
        ELSE 'image'
    END,
    semantics = CASE role
        WHEN 'first_frame' THEN 'output_start_frame'
        WHEN 'last_frame' THEN 'output_end_frame'
        WHEN 'storyboard_sheet' THEN 'ordered_keyframe_sheet'
        WHEN 'character_identity' THEN 'character_identity'
        WHEN 'character_costume' THEN 'character_costume'
        WHEN 'scene_identity' THEN 'scene_identity'
        WHEN 'scene_spatial' THEN 'scene_spatial_layout'
        WHEN 'prop_identity' THEN 'prop_identity'
        WHEN 'continuity_hint' THEN 'cross_shot_continuity_hint'
        WHEN 'motion_reference' THEN 'motion_guidance'
        WHEN 'video_reference' THEN 'video_guidance'
        WHEN 'audio_reference' THEN 'audio_guidance'
        WHEN 'style_reference' THEN 'visual_style_guidance'
        ELSE 'semantic_guidance'
    END;

ALTER TABLE shot_reference_pack_items
    ALTER COLUMN media_type SET NOT NULL,
    ALTER COLUMN semantics SET NOT NULL,
    ADD CONSTRAINT shot_reference_pack_items_media_type_check
        CHECK (media_type IN ('image', 'video', 'audio')),
    ADD CONSTRAINT shot_reference_pack_items_semantics_check
        CHECK (semantics <> '' AND semantics = lower(semantics));

CREATE INDEX shot_reference_pack_items_media_role_idx
    ON shot_reference_pack_items(reference_pack_id, media_type, role, priority DESC);

CREATE TEMP TABLE multimodal_reference_prompt_v2(
    template_key TEXT PRIMARY KEY,
    content TEXT NOT NULL
) ON COMMIT DROP;

-- +goose StatementBegin
INSERT INTO multimodal_reference_prompt_v2(template_key, content)
VALUES
('video_profile.multimodal_reference.anchor.plan', $prompt$
你是多模态参考模式的主构图锚点规划器。plannedEntryState 是当前镜头唯一首帧状态，typed ReferencePack 中角色、场景、道具、风格和软连续性引用各自具有独立 role、mediaType 和 semantics。
规划必须列出当前镜头实际出现的 required asset UUID，保持角色身份、服装、场景空间和道具归属，不得把参考图中的构图、人物集合或摄影机位置误当成首帧硬状态。continuity_hint 只能提供低优先级身份和动作阶段提示。
输出严格 JSON：{"composition":"...","requiredAssetIds":["uuid"],"referenceRoles":["character_identity"],"camera":"...","blocking":"...","actionStart":"...","forbiddenVisibleText":true}。
输入：{{ input.context }}
$prompt$),
('video_profile.multimodal_reference.anchor.generate', $prompt$
根据 plannedEntryState、Transition、typed ReferencePack、PromptContextPlan、项目手册和图片模型限制，生成当前镜头唯一干净首帧提示词。
每个引用只能按自己的 role 和 semantics 使用：角色图只约束对应角色身份，场景图只约束环境与空间，道具图只约束道具，style_reference 只约束风格，continuity_hint 不得覆盖当前镜头人物集合、机位或构图。禁止台词、字幕、引号、编号、说明文字、水印和 UI。
输出严格 JSON：{"prompt":"最终图片提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.multimodal_reference.anchor.review', $prompt$
审核候选首帧提示词和 typed ReferencePack。逐项核对 required 角色、场景和道具是否与 plannedEntryState 对应，任何引用串位、把软参考当硬首帧、错误人物集合、错误场景、台词或可见文字泄漏都必须拒绝。
输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.multimodal_reference.video.generate', $prompt$
你正在为原生支持 first_frame_plus_references 的视频模型生成当前镜头提示词。first_frame 是唯一输出起点；character_identity、scene_identity、prop_identity、style_reference、motion_reference、video_reference 和 audio_reference 只能按各自 semantics 提供指导，不能互换或覆盖 ShotState。
动作、表演和运镜必须完全执行当前分集剧本与 ShotState；verbatimDialogueCues 中的中文台词逐字保留。nativeAudioRequired=true 时明确要求对应角色按时序说出原中文台词并保持口型、环境音同步。不得新增参考素材中存在但当前镜头未要求的人物或道具。
输出严格 JSON：{"prompt":"最终视频提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"referenceUsage":[],"notes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.multimodal_reference.video.review', $prompt$
审核候选视频提示词是否严格使用 first_frame 作为起点，并逐项按 typed ReferencePack 的 role、mediaType、semantics 使用角色、场景、道具、风格、视频和音频参考。任何引用串位、超过模型输入上限、缺失 required asset、加入未要求人物、改写中文台词或违反原生音频契约都必须拒绝。
输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整视频提示词","negativePrompt":"负面约束","dialogueLines":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$);
-- +goose StatementEnd

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key IN (SELECT template_key FROM multimodal_reference_prompt_v2)
  AND version.status = 'active';

INSERT INTO prompt_versions(
    id, prompt_template_id, version_no, content, variables_schema, content_hash,
    template_id, version, status, title, content_format, metadata, activated_at, managed_by
)
SELECT
    md5('cineweave:video-profile-prompt-version:' || seed.template_key || ':2')::uuid,
    template.id,
    2,
    seed.content,
    '{"type":"object","required":["input"],"properties":{"input":{"type":"object","required":["context"]}}}'::jsonb,
    'sha256:' || encode(public.digest(pg_catalog.convert_to(seed.content, 'UTF8'), 'sha256'), 'hex'),
    template.id,
    2,
    'active',
    template.name || ' v2',
    'text',
    jsonb_build_object(
        'contractVersion', 'video-production-prompt/v2',
        'profileKey', 'multimodal_reference',
        'role', replace(seed.template_key, 'video_profile.multimodal_reference.', ''),
        'seedMigration', '000016_multimodal_reference_profile_runtime'
    ),
    now(),
    'system'
FROM multimodal_reference_prompt_v2 seed
JOIN prompt_templates template
  ON template.organization_id IS NULL
 AND template.template_key = seed.template_key;

WITH profile_contract AS (
    SELECT
        '{"anchorRoles":["planned_first_frame"],"crossShotTailPolicy":"soft","sameShotSegmentContinuity":"previous_segment_tail_plus_semantic_references","requiresAnchorReview":true,"deterministicReferencePacking":true,"semanticReferenceRoles":["character_identity","character_costume","scene_identity","scene_spatial","prop_identity","style_reference","continuity_hint","motion_reference","video_reference","audio_reference"]}'::jsonb AS configuration,
        '{"taskType":"video.reference_to_video","initialInputContract":"first_frame_plus_references","inputContract":"first_frame_plus_references","allowedContinuationInputContracts":["video_extension","first_frame_plus_references"],"supportsFirstFrame":true,"supportsSemanticReferenceImages":true,"minimumSemanticReferenceImages":1,"maximumSemanticReferenceImages":8,"maximumReferenceVideos":2,"maximumReferenceAudios":2}'::jsonb AS capability_requirements,
        '{"anchorPlan":"video_profile.multimodal_reference.anchor.plan","anchorGenerate":"video_profile.multimodal_reference.anchor.generate","anchorReview":"video_profile.multimodal_reference.anchor.review","videoGenerate":"video_profile.multimodal_reference.video.generate","videoReview":"video_profile.multimodal_reference.video.review"}'::jsonb AS prompt_contract
)
INSERT INTO video_production_profile_versions(
    id, profile_id, version, lifecycle_state, implementation_state,
    configuration, capability_requirements, prompt_contract, input_contract_version,
    configuration_hash, prompt_contract_hash, published_at
)
SELECT
    '10000000-0000-4000-9000-000000000103'::uuid,
    profile.id,
    2,
    'published',
    'available',
    contract.configuration,
    contract.capability_requirements,
    contract.prompt_contract,
    'video-input-contract/v3',
    encode(public.digest(pg_catalog.convert_to(contract.configuration::text, 'UTF8'), 'sha256'), 'hex'),
    encode(public.digest(pg_catalog.convert_to(contract.prompt_contract::text, 'UTF8'), 'sha256'), 'hex'),
    now()
FROM video_production_profiles profile
CROSS JOIN profile_contract contract
WHERE profile.profile_key = 'multimodal_reference';

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM project_video_production_bindings
        WHERE profile_version_id = '10000000-0000-4000-9000-000000000103'::uuid
    ) THEN
        RAISE EXCEPTION 'cannot roll back multimodal reference profile v2 after project bindings exist';
    END IF;
END;
$$;
-- +goose StatementEnd

DELETE FROM video_production_profile_versions
WHERE id = '10000000-0000-4000-9000-000000000103'::uuid;

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.multimodal_reference.%'
  AND version.version = 2
  AND version.metadata->>'seedMigration' = '000016_multimodal_reference_profile_runtime';

DELETE FROM prompt_versions version
USING prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.multimodal_reference.%'
  AND version.version = 2
  AND version.metadata->>'seedMigration' = '000016_multimodal_reference_profile_runtime';

UPDATE prompt_versions version
SET status = 'active', activated_at = now()
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.multimodal_reference.%'
  AND version.version = 1
  AND version.metadata->>'seedMigration' = '000010_video_production_prompt_contracts';

DROP INDEX IF EXISTS shot_reference_pack_items_media_role_idx;

ALTER TABLE shot_reference_pack_items
    DROP CONSTRAINT IF EXISTS shot_reference_pack_items_semantics_check,
    DROP CONSTRAINT IF EXISTS shot_reference_pack_items_media_type_check,
    DROP COLUMN IF EXISTS semantics,
    DROP COLUMN IF EXISTS media_type;

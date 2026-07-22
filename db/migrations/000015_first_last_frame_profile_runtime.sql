-- +goose Up

SET search_path TO public;

CREATE TEMP TABLE first_last_frame_prompt_v2(
    template_key TEXT PRIMARY KEY,
    content TEXT NOT NULL
) ON COMMIT DROP;

-- +goose StatementBegin
INSERT INTO first_last_frame_prompt_v2(template_key, content)
VALUES
('video_profile.first_last_frame.anchor.plan', $prompt$
你是首尾帧衔接模式的锚点规划器。输入中的 plannedEntryState 和 plannedExitState 是同一个 StoryboardShot 的权威首尾状态。
两帧必须保持角色身份、appearanceVersionId、costumeVariantId、场景、道具身份、空间轴和项目视觉手册一致；角色位移、朝向、道具变化和机位变化必须能在当前镜头整数秒时长内完成。不得混入下一镜头状态。
输出严格 JSON：{"composition":"...","requiredAssetIds":["uuid"],"camera":"...","blocking":"...","actionStart":"...","actionEnd":"...","forbiddenVisibleText":true}。
输入：{{ input.context }}
$prompt$),
('video_profile.first_last_frame.anchor.generate', $prompt$
根据输入中的 anchorRole、对应 ShotState、Transition、typed ReferencePack、PromptContextPlan、项目手册和图片模型限制，为当前镜头生成一张视觉锚点图片提示词。
anchorRole=planned_first_frame 时只描述动作起点；anchorRole=planned_last_frame 时只描述同镜头动作完成后的可达终点。不得描述时间序列，不得加入下一镜头的人物、场景或动作。禁止台词、字幕、引号、编号、说明文字、水印和 UI。
输出严格 JSON：{"prompt":"最终图片提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.first_last_frame.anchor.review', $prompt$
审核候选锚点图片提示词是否忠实表达 anchorRole 对应的 ShotState，并与同镜头另一端状态保持角色、服装、场景、道具、空间轴和视觉风格一致。审核动作与机位变化能否在镜头时长内完成。
任何 required asset 缺失、身份漂移、错误场景、越过尾状态、台词或可见文字泄漏都必须拒绝。输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.first_last_frame.video.generate', $prompt$
你正在为支持首帧和尾帧双输入的视频模型生成当前镜头提示词。已审核 first_frame 是唯一动作起点，已审核 last_frame 是必须到达且不可越过的终点。
动作、表演、角色位移、道具变化和运镜必须在单次模型请求与当前镜头时长内平滑完成，不得切换场景、增加人物、改变身份或进入下一镜头。verbatimDialogueCues 中的中文台词必须逐字保留；nativeAudioRequired=true 时明确要求对应角色按时序说出原中文台词并保持口型和环境音同步。
输出严格 JSON：{"prompt":"最终视频提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"notes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.first_last_frame.video.review', $prompt$
审核候选视频提示词是否严格从 first_frame 到达 last_frame，人物身份、服装、场景、道具和空间轴不漂移，动作与运镜在当前整数秒时长和模型单次时长内可执行。
逐字核对全部中文 dialogue cue 和原生音频要求。新增人物或场景、越过尾帧、缺失或改写台词、需要多段请求、超过模型限制时必须拒绝。输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整视频提示词","negativePrompt":"负面约束","dialogueLines":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$);
-- +goose StatementEnd

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key IN (SELECT template_key FROM first_last_frame_prompt_v2)
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
        'profileKey', 'first_last_frame',
        'role', replace(seed.template_key, 'video_profile.first_last_frame.', ''),
        'seedMigration', '000015_first_last_frame_profile_runtime'
    ),
    now(),
    'system'
FROM first_last_frame_prompt_v2 seed
JOIN prompt_templates template
  ON template.organization_id IS NULL
 AND template.template_key = seed.template_key;

WITH profile_contract AS (
    SELECT
        '{"anchorRoles":["planned_first_frame","planned_last_frame"],"crossShotTailPolicy":"none","sameShotSegmentContinuity":"none","requiresAnchorReview":true,"requiresPairReview":true,"singleRequestPerShot":true}'::jsonb AS configuration,
        '{"taskType":"video.image_to_video","initialInputContract":"first_last_frames","inputContract":"first_last_frames","allowedContinuationInputContracts":[],"supportsFirstFrame":true,"supportsLastFrame":true,"minimumReferenceImages":2,"maximumReferenceImages":2,"singleRequestPerShot":true}'::jsonb AS capability_requirements,
        '{"anchorPlan":"video_profile.first_last_frame.anchor.plan","anchorGenerate":"video_profile.first_last_frame.anchor.generate","anchorReview":"video_profile.first_last_frame.anchor.review","videoGenerate":"video_profile.first_last_frame.video.generate","videoReview":"video_profile.first_last_frame.video.review"}'::jsonb AS prompt_contract
)
INSERT INTO video_production_profile_versions(
    id, profile_id, version, lifecycle_state, implementation_state,
    configuration, capability_requirements, prompt_contract, input_contract_version,
    configuration_hash, prompt_contract_hash, published_at
)
SELECT
    '10000000-0000-4000-9000-000000000102'::uuid,
    profile.id,
    2,
    'published',
    'available',
    contract.configuration,
    contract.capability_requirements,
    contract.prompt_contract,
    'video-input-contract/v2',
    encode(public.digest(pg_catalog.convert_to(contract.configuration::text, 'UTF8'), 'sha256'), 'hex'),
    encode(public.digest(pg_catalog.convert_to(contract.prompt_contract::text, 'UTF8'), 'sha256'), 'hex'),
    now()
FROM video_production_profiles profile
CROSS JOIN profile_contract contract
WHERE profile.profile_key = 'first_last_frame';

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM project_video_production_bindings
        WHERE profile_version_id = '10000000-0000-4000-9000-000000000102'::uuid
    ) THEN
        RAISE EXCEPTION 'cannot roll back first/last frame profile v2 after project bindings exist';
    END IF;
END;
$$;
-- +goose StatementEnd

DELETE FROM video_production_profile_versions
WHERE id = '10000000-0000-4000-9000-000000000102'::uuid;

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.first_last_frame.%'
  AND version.version = 2
  AND version.metadata->>'seedMigration' = '000015_first_last_frame_profile_runtime';

DELETE FROM prompt_versions version
USING prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.first_last_frame.%'
  AND version.version = 2
  AND version.metadata->>'seedMigration' = '000015_first_last_frame_profile_runtime';

UPDATE prompt_versions version
SET status = 'active', activated_at = now()
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.first_last_frame.%'
  AND version.version = 1
  AND version.metadata->>'seedMigration' = '000010_video_production_prompt_contracts';

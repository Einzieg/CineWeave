-- +goose Up

SET search_path TO public;

DROP INDEX IF EXISTS shot_visual_anchors_one_approved;
CREATE UNIQUE INDEX shot_visual_anchors_one_approved
    ON shot_visual_anchors(storyboard_shot_id, anchor_role)
    WHERE status = 'ready' AND review_status = 'approved' AND anchor_role <> 'storyboard_panel';

CREATE TABLE storyboard_sheet_manifests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    sheet_anchor_id UUID NOT NULL UNIQUE REFERENCES shot_visual_anchors(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL,
    contract_version TEXT NOT NULL,
    planned_duration_ticks BIGINT NOT NULL,
    timeline_timebase BIGINT NOT NULL,
    video_aspect_ratio TEXT NOT NULL,
    sheet_aspect_ratio TEXT NOT NULL,
    grid_rows INTEGER NOT NULL,
    grid_columns INTEGER NOT NULL,
    panel_count INTEGER NOT NULL,
    entry_state_hash TEXT NOT NULL,
    exit_state_hash TEXT NOT NULL,
    manifest JSONB NOT NULL,
    manifest_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    review_status TEXT NOT NULL DEFAULT 'pending',
    reviewer_prompt_version_id UUID REFERENCES prompt_versions(id) ON DELETE SET NULL,
    reviewer_provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
    reviewer_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
    reviewer_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ,
    CONSTRAINT storyboard_sheet_manifests_revision_check CHECK (revision > 0),
    CONSTRAINT storyboard_sheet_manifests_duration_check CHECK (planned_duration_ticks > 0 AND timeline_timebase > 0),
    CONSTRAINT storyboard_sheet_manifests_grid_check CHECK (
        grid_rows > 0 AND grid_columns > 0 AND panel_count BETWEEN 3 AND 6 AND grid_rows * grid_columns >= panel_count
    ),
    CONSTRAINT storyboard_sheet_manifests_hash_check CHECK (
        entry_state_hash ~ '^[0-9a-f]{64}$' AND exit_state_hash ~ '^[0-9a-f]{64}$' AND manifest_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT storyboard_sheet_manifests_status_check CHECK (status IN ('draft', 'processing', 'ready', 'failed', 'stale', 'archived')),
    CONSTRAINT storyboard_sheet_manifests_review_check CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_edit')),
    CONSTRAINT storyboard_sheet_manifests_json_check CHECK (
        jsonb_typeof(manifest) = 'object' AND jsonb_typeof(reviewer_output) = 'object' AND jsonb_typeof(metadata) = 'object'
    ),
    CONSTRAINT storyboard_sheet_manifests_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT storyboard_sheet_manifests_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    UNIQUE(storyboard_shot_id, revision),
    UNIQUE(id, project_id, production_generation_id, storyboard_shot_id)
);

CREATE UNIQUE INDEX storyboard_sheet_manifests_one_active
    ON storyboard_sheet_manifests(storyboard_shot_id)
    WHERE status IN ('draft', 'processing', 'ready');

CREATE INDEX storyboard_sheet_manifests_project_status_idx
    ON storyboard_sheet_manifests(project_id, status, created_at DESC);

CREATE TRIGGER storyboard_sheet_manifests_set_updated_at
BEFORE UPDATE ON storyboard_sheet_manifests
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE storyboard_sheet_panels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    storyboard_shot_id UUID NOT NULL,
    manifest_id UUID NOT NULL,
    visual_anchor_id UUID NOT NULL UNIQUE REFERENCES shot_visual_anchors(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    grid_row INTEGER NOT NULL,
    grid_column INTEGER NOT NULL,
    time_tick BIGINT NOT NULL,
    normalized_position INTEGER NOT NULL,
    stage TEXT NOT NULL,
    action_stage TEXT NOT NULL,
    expected_state JSONB NOT NULL,
    expected_state_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'planned',
    review_status TEXT NOT NULL DEFAULT 'pending',
    artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
    storage_key TEXT,
    content_hash TEXT,
    crop JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT storyboard_sheet_panels_ordinal_check CHECK (ordinal > 0),
    CONSTRAINT storyboard_sheet_panels_grid_check CHECK (grid_row >= 0 AND grid_column >= 0),
    CONSTRAINT storyboard_sheet_panels_time_check CHECK (time_tick >= 0 AND normalized_position BETWEEN 0 AND 1000),
    CONSTRAINT storyboard_sheet_panels_state_hash_check CHECK (expected_state_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT storyboard_sheet_panels_status_check CHECK (status IN ('planned', 'cropped', 'failed', 'stale', 'archived')),
    CONSTRAINT storyboard_sheet_panels_review_check CHECK (review_status IN ('pending', 'approved', 'rejected', 'needs_edit')),
    CONSTRAINT storyboard_sheet_panels_json_check CHECK (
        jsonb_typeof(expected_state) = 'object' AND jsonb_typeof(crop) = 'object' AND jsonb_typeof(metadata) = 'object'
    ),
    CONSTRAINT storyboard_sheet_panels_manifest_fk
        FOREIGN KEY (manifest_id, project_id, production_generation_id, storyboard_shot_id)
        REFERENCES storyboard_sheet_manifests(id, project_id, production_generation_id, storyboard_shot_id) ON DELETE CASCADE,
    CONSTRAINT storyboard_sheet_panels_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT,
    CONSTRAINT storyboard_sheet_panels_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE,
    UNIQUE(manifest_id, ordinal),
    UNIQUE(manifest_id, grid_row, grid_column)
);

CREATE INDEX storyboard_sheet_panels_manifest_order_idx
    ON storyboard_sheet_panels(manifest_id, ordinal);

CREATE TRIGGER storyboard_sheet_panels_set_updated_at
BEFORE UPDATE ON storyboard_sheet_panels
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TEMP TABLE storyboard_sheet_prompt_v2(
    template_key TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    purpose TEXT NOT NULL,
    content TEXT NOT NULL
) ON COMMIT DROP;

-- +goose StatementBegin
INSERT INTO storyboard_sheet_prompt_v2(template_key, name, purpose, content)
VALUES
('video_profile.storyboard_sheet.anchor.plan', '分镜板 Manifest 规划 v2', '规划当前镜头不可变的 PanelManifest', $prompt$
你是分镜板模式的 PanelManifest 审核规划器。后端已经根据镜头整数时长、24 FPS 时间基准、plannedEntryState 和 plannedExitState 确定性编译 PanelManifest；你不得增删、重排或跨镜头改写 panel。
逐项检查每个 panel 的 ordinal、timeTick、stage、actionStage、expectedState 是否属于同一个 Storyboard Shot，首 panel 必须是 entry，尾 panel 必须是 exit，中间 panel 必须形成可达动作序列。输出严格 JSON：{"approved":true,"issues":[],"corrections":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.storyboard_sheet.anchor.generate', '分镜板图片提示词 v2', '生成一张无文字的有序关键帧分镜板', $prompt$
根据 PanelManifest 生成一张由 {{ input.context.panelManifest.panelCount }} 个等尺寸画格组成的单张分镜板。严格使用 {{ input.context.panelManifest.rows }} 行 × {{ input.context.panelManifest.columns }} 列网格，并按 ordinal 从左到右、从上到下排列；多余网格区域保持纯净背景，不得生成额外画格。
每个画格只表现对应 timeTick/actionStage/expectedState，人物身份、服装、场景、道具和空间轴保持一致，动作阶段按时间顺序推进。画格之间不得复制同一动作阶段。
整张图和所有画格禁止任何可见文字、数字、编号、字幕、台词、对话框、标签、边框标题、水印、标志和 UI。不得把分镜板描述成视频首帧。
输出严格 JSON：{"prompt":"完整图片提示词","negativePrompt":"文字，数字，编号，字幕，对话框，标签，水印，标志，UI，额外画格，动作乱序，身份漂移，错误场景","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.storyboard_sheet.anchor.review', '分镜板图片提示词审核 v2', '审核分镜板提示词的网格、顺序和无文字约束', $prompt$
审核候选分镜板图片提示词是否逐项覆盖 PanelManifest，严格保持 panel 数量、网格位置、动作时间顺序、人物身份、场景、道具和机位演进。任何可见文字、编号、字幕、对话框、水印、额外画格、动作乱序或把整张板当作首帧的表述都必须拒绝。
输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.storyboard_sheet.video.generate', '分镜板视频提示词 v2', '按已审核 PanelManifest 和分镜板生成视频提示词', $prompt$
你正在为原生支持 storyboard_sheet_reference 的视频模型生成提示词。整张分镜板只是有序动作指导，不是输出首帧；必须按 PanelManifest 的 ordinal/timeTick/actionStage 执行当前镜头动作，保持人物、场景、道具、机位和空间轴连续，不得跨入下一镜头。
verbatimDialogueCues 中的中文台词逐字保留；nativeAudioRequired=true 时明确要求对应角色按时序说出原中文台词并同步环境音。输出严格 JSON：{"prompt":"完整视频提示词","negativePrompt":"动作乱序，身份漂移，错误场景，字幕，水印","dialogueLines":[],"sourceAnchors":[],"notes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.storyboard_sheet.video.review', '分镜板视频提示词审核 v2', '审核视频提示词遵循 PanelManifest 与输入契约', $prompt$
审核候选视频提示词是否把 storyboard_sheet 当作 ordered_keyframe_sheet 语义参考而不是 first_frame，是否按 PanelManifest 顺序和 timeTick 完成动作，是否保持当前镜头人物、场景、道具与机位，并逐字保留中文台词。模型不具备 storyboard_sheet_reference Input Contract、动作顺序错误或跨镜头时必须拒绝。
输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整视频提示词","negativePrompt":"负面约束","dialogueLines":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.storyboard_sheet.output.review', '分镜板成图审核', '使用多模态文本模型审核实际分镜板成图', $prompt$
你是分镜板实际成图审核器。对照 PanelManifest 和随请求提供的整张分镜板图片，检查：实际画格数等于 panelCount；从左到右、从上到下的动作顺序与 ordinal/timeTick 一致；首尾状态正确；人物身份、服装、场景、道具和空间轴一致；整张图完全没有文字、数字、编号、字幕、对话框、标签、水印、标志或 UI。
不得因为画面美观而放过任何硬错误。输出严格 JSON：{"approved":true,"panelCountObserved":3,"ordered":true,"noVisibleText":true,"identityConsistent":true,"sceneConsistent":true,"actionSequenceValid":true,"issues":[]}。
输入：{{ input.context }}
$prompt$);
-- +goose StatementEnd

INSERT INTO prompt_templates(
    id, organization_id, template_key, name, purpose, description, modality,
    task_type, scope, status, is_system, managed_by
)
SELECT
    md5('cineweave:video-profile-template:' || seed.template_key)::uuid,
    NULL, seed.template_key, seed.name, seed.purpose,
    'Storyboard sheet runtime prompt contract', 'text', 'text.generate',
    'system', 'active', true, 'system'
FROM storyboard_sheet_prompt_v2 seed
ON CONFLICT DO NOTHING;

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key IN (SELECT template_key FROM storyboard_sheet_prompt_v2)
  AND version.status = 'active';

INSERT INTO prompt_versions(
    id, prompt_template_id, version_no, content, variables_schema, content_hash,
    template_id, version, status, title, content_format, metadata, activated_at, managed_by
)
SELECT
    md5('cineweave:video-profile-prompt-version:' || seed.template_key || ':2')::uuid,
    template.id, 2, seed.content,
    '{"type":"object","required":["input"],"properties":{"input":{"type":"object","required":["context"]}}}'::jsonb,
    'sha256:' || encode(public.digest(pg_catalog.convert_to(seed.content, 'UTF8'), 'sha256'), 'hex'),
    template.id, 2, 'active', seed.name, 'text',
    jsonb_build_object(
        'contractVersion', 'video-production-prompt/v2',
        'panelManifestVersion', 'storyboard-sheet-panel-manifest/v1',
        'profileKey', 'storyboard_sheet',
        'seedMigration', '000017_storyboard_sheet_profile_runtime'
    ),
    now(), 'system'
FROM storyboard_sheet_prompt_v2 seed
JOIN prompt_templates template
  ON template.organization_id IS NULL AND template.template_key = seed.template_key;

WITH profile_contract AS (
    SELECT
        '{"anchorRoles":["storyboard_sheet","storyboard_panel"],"crossShotTailPolicy":"none","sameShotSegmentContinuity":"video_extension","requiresAnchorReview":true,"panelManifestRequired":true,"panelManifestContractVersion":"storyboard-sheet-panel-manifest/v1","deterministicPanelCropping":true,"actualOutputReviewRequired":true}'::jsonb AS configuration,
        '{"taskType":"video.reference_to_video","initialInputContract":"storyboard_sheet_reference","inputContract":"storyboard_sheet_reference","allowedContinuationInputContracts":["video_extension"],"supportsStoryboardSheetReference":true,"requiredImageModelFamilies":["gpt-image-2"],"minimumPanels":3,"maximumPanels":6}'::jsonb AS capability_requirements,
        '{"anchorPlan":"video_profile.storyboard_sheet.anchor.plan","anchorGenerate":"video_profile.storyboard_sheet.anchor.generate","anchorReview":"video_profile.storyboard_sheet.anchor.review","videoGenerate":"video_profile.storyboard_sheet.video.generate","videoReview":"video_profile.storyboard_sheet.video.review","outputReview":"video_profile.storyboard_sheet.output.review"}'::jsonb AS prompt_contract
)
INSERT INTO video_production_profile_versions(
    id, profile_id, version, lifecycle_state, implementation_state,
    configuration, capability_requirements, prompt_contract, input_contract_version,
    configuration_hash, prompt_contract_hash, published_at
)
SELECT
    '10000000-0000-4000-9000-000000000104'::uuid,
    profile.id, 2, 'published', 'available',
    contract.configuration, contract.capability_requirements, contract.prompt_contract,
    'video-input-contract/v4',
    encode(public.digest(pg_catalog.convert_to(contract.configuration::text, 'UTF8'), 'sha256'), 'hex'),
    encode(public.digest(pg_catalog.convert_to(contract.prompt_contract::text, 'UTF8'), 'sha256'), 'hex'),
    now()
FROM video_production_profiles profile
CROSS JOIN profile_contract contract
WHERE profile.profile_key = 'storyboard_sheet';

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM project_video_production_bindings
        WHERE profile_version_id = '10000000-0000-4000-9000-000000000104'::uuid
    ) THEN
        RAISE EXCEPTION 'cannot roll back storyboard sheet profile v2 after project bindings exist';
    END IF;
END;
$$;
-- +goose StatementEnd

DELETE FROM video_production_profile_versions
WHERE id = '10000000-0000-4000-9000-000000000104'::uuid;

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.storyboard_sheet.%'
  AND version.version = 2
  AND version.metadata->>'seedMigration' = '000017_storyboard_sheet_profile_runtime';

DELETE FROM prompt_versions version
USING prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.storyboard_sheet.%'
  AND version.version = 2
  AND version.metadata->>'seedMigration' = '000017_storyboard_sheet_profile_runtime';

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key = 'video_profile.storyboard_sheet.output.review';

UPDATE prompt_versions version
SET status = 'active', activated_at = now()
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.storyboard_sheet.%'
  AND version.version = 1
  AND version.metadata->>'seedMigration' = '000010_video_production_prompt_contracts';

DROP TABLE IF EXISTS storyboard_sheet_panels;
DROP TABLE IF EXISTS storyboard_sheet_manifests;

DELETE FROM shot_visual_anchors WHERE anchor_role = 'storyboard_panel';
DROP INDEX IF EXISTS shot_visual_anchors_one_approved;
CREATE UNIQUE INDEX shot_visual_anchors_one_approved
    ON shot_visual_anchors(storyboard_shot_id, anchor_role)
    WHERE status = 'ready' AND review_status = 'approved';

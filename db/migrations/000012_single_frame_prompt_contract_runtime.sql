-- +goose Up

SET search_path TO public;

CREATE TEMP TABLE single_frame_prompt_v2(
    template_key TEXT PRIMARY KEY,
    content TEXT NOT NULL
) ON COMMIT DROP;

-- +goose StatementBegin
INSERT INTO single_frame_prompt_v2(template_key, content)
VALUES
('video_profile.single_frame_i2v.anchor.plan', $prompt$
你是图生视频首帧规划器。依据输入中的 ShotState、Transition、ReferencePack、项目手册和模型能力，只规划当前镜头自己的干净起始画面。
不得继承前一镜头像素，不得遗漏 required asset，不得出现台词、字幕、引号、编号、说明文字或水印。
输出严格 JSON：{"composition":"...","requiredAssetIds":["uuid"],"camera":"...","blocking":"...","actionStart":"...","forbiddenVisibleText":true}。
输入：{{ input.context }}
$prompt$),
('video_profile.single_frame_i2v.anchor.generate', $prompt$
依据输入中的不可变 PromptContextPlan、ShotState、Transition、typed ReferencePack、项目手册版本和模型限制，生成当前镜头 planned_first_frame 的图片提示词。
只描述单张可见起始画面，包含 required 人物、场景、道具、机位、构图、站位、表情、光线和动作起点。禁止台词、字幕、引号、编号、对白含义、未来动作序列、视频运镜过程、说明文字、水印和 UI。
输出严格 JSON：{"prompt":"最终图片提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.single_frame_i2v.anchor.review', $prompt$
审核候选图片提示词是否忠实表达输入中的 ShotState、required assets、机位、站位、动作起点、ReferencePack 和项目手册，并确认没有台词或可见文字。
不得改写结构化事实。发现问题时在 finalPrompt 中给出修正后的完整图片提示词；任何 required asset 缺失、人物串位、错误场景或文字泄漏必须拒绝。
输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.single_frame_i2v.video.generate', $prompt$
你正在为单首帧图生视频模型生成当前镜头的视频提示词。唯一硬视觉输入是已审核 planned_first_frame；动作、运镜和表演必须从该首帧状态可达，不得切换镜头，不得变形成新人物或新场景。
依据同一 PromptContextPlan、ShotState、Transition、ReferencePack、镜头时长、画面比例和模型能力生成。verbatimDialogueCues 中的中文台词必须逐字保留，不得翻译、缩写、润色或遗漏；没有台词时不得编造。nativeAudioRequired=true 时明确要求对应说话人按顺序说出原中文台词并保持口型、环境音和画面同步。
输出严格 JSON：{"prompt":"最终视频提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"notes":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.single_frame_i2v.video.review', $prompt$
审核候选视频提示词是否从当前已审核首帧可达，忠实执行当前镜头动作、运镜、时长、画面比例和逐字中文台词，并符合模型原生音频能力。
Reviewer 必须使用输入中的同一 PromptContextPlan hash。拒绝新增人物或场景、跨镜头变形、遗漏或改写 dialogue cue、错误音频要求以及超过模型限制。
输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整视频提示词","negativePrompt":"负面约束","dialogueLines":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$);
-- +goose StatementEnd

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key IN (SELECT template_key FROM single_frame_prompt_v2)
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
        'profileKey', 'single_frame_i2v',
        'role', replace(seed.template_key, 'video_profile.single_frame_i2v.', ''),
        'seedMigration', '000012_single_frame_prompt_contract_runtime'
    ),
    now(),
    'system'
FROM single_frame_prompt_v2 seed
JOIN prompt_templates template
  ON template.organization_id IS NULL
 AND template.template_key = seed.template_key;

-- +goose Down

SET search_path TO public;

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.single_frame_i2v.%'
  AND version.version = 2
  AND version.metadata->>'seedMigration' = '000012_single_frame_prompt_contract_runtime';

DELETE FROM prompt_versions version
USING prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.single_frame_i2v.%'
  AND version.version = 2
  AND version.metadata->>'seedMigration' = '000012_single_frame_prompt_contract_runtime';

UPDATE prompt_versions version
SET status = 'active', activated_at = now()
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key LIKE 'video_profile.single_frame_i2v.%'
  AND version.version = 1
  AND version.metadata->>'seedMigration' = '000010_video_production_prompt_contracts';

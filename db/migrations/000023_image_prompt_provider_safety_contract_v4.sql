-- +goose Up

SET search_path TO public;

CREATE TEMP TABLE single_frame_prompt_v4(
    template_key TEXT PRIMARY KEY,
    content TEXT NOT NULL
) ON COMMIT DROP;

-- +goose StatementBegin
INSERT INTO single_frame_prompt_v4(template_key, content)
VALUES
('video_profile.single_frame_i2v.anchor.generate', $prompt$
依据输入中的不可变 PromptContextPlan、ShotState、Transition、typed ReferencePack、项目手册版本和模型限制，生成当前镜头 planned_first_frame 的图片提示词。
只描述单张可见起始画面，包含 required 人物、场景、道具、机位、构图、站位、表情、光线和动作起点。禁止台词、字幕、引号、编号、对白含义、未来动作序列、视频运镜过程、说明文字、水印和 UI。
对暴力、伤害或战后场面只保留剧情必要的非图形化结果，使用战损服装、克制的暗色污痕、远景倒地轮廓、烟尘和肃杀氛围表达；不得放大原文细节。prompt 与 negativePrompt 均不得复述血泊、滴血、血珠、流血、血液飞溅、大面积血污、血痕、开放伤口、尸体、骸骨、肢体残留、肢解、残肢、内脏或同义图形化描述。
输出严格 JSON：{"prompt":"最终图片提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}。
输入：{{ input.context }}
$prompt$),
('video_profile.single_frame_i2v.anchor.review', $prompt$
审核候选图片提示词是否忠实表达输入中的 ShotState、required assets、机位、站位、动作起点、ReferencePack 和项目手册，并确认没有台词或可见文字。
不得改写结构化事实。发现问题时在 finalPrompt 中给出修正后的完整图片提示词；任何 required asset 缺失、人物串位、错误场景或文字泄漏必须拒绝。
同时审核上游图片模型安全可执行性：暴力、伤害或战后场面只能保留剧情必要的非图形化结果，改写为战损服装、克制的暗色污痕、远景倒地轮廓、烟尘和气氛。prompt 与 negativePrompt 均不得复述血泊、滴血、血珠、流血、血液飞溅、大面积血污、血痕、开放伤口、尸体、骸骨、肢体残留、肢解、残肢、内脏或同义图形化描述。审核不得因安全改写而删除人物身份、动作、构图或剧情结果。
输出严格 JSON：{"approved":true,"prompt":"候选提示词","finalPrompt":"审核后的完整提示词","negativePrompt":"负面约束","dialogueLines":[],"sourceAnchors":[],"issues":[],"changes":[]}。
输入：{{ input.context }}
$prompt$);
-- +goose StatementEnd

UPDATE prompt_versions version
SET status = 'archived'
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key IN (SELECT template_key FROM single_frame_prompt_v4)
  AND version.status = 'active';

INSERT INTO prompt_versions(
    id, prompt_template_id, version_no, content, variables_schema, content_hash,
    template_id, version, status, title, content_format, metadata, activated_at, managed_by
)
SELECT
    md5('cineweave:video-profile-prompt-version:' || seed.template_key || ':4')::uuid,
    template.id,
    4,
    seed.content,
    '{"type":"object","required":["input"],"properties":{"input":{"type":"object","required":["context"]}}}'::jsonb,
    'sha256:' || encode(public.digest(pg_catalog.convert_to(seed.content, 'UTF8'), 'sha256'), 'hex'),
    template.id,
    4,
    'active',
    template.name || ' v4',
    'text',
    jsonb_build_object(
        'contractVersion', 'video-production-prompt/v4',
        'profileKey', 'single_frame_i2v',
        'role', replace(seed.template_key, 'video_profile.single_frame_i2v.', ''),
        'seedMigration', '000023_image_prompt_provider_safety_contract_v4'
    ),
    now(),
    'system'
FROM single_frame_prompt_v4 seed
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
  AND template.template_key IN (
      'video_profile.single_frame_i2v.anchor.generate',
      'video_profile.single_frame_i2v.anchor.review'
  )
  AND version.version = 4
  AND version.metadata->>'seedMigration' = '000023_image_prompt_provider_safety_contract_v4';

DELETE FROM prompt_versions version
USING prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key IN (
      'video_profile.single_frame_i2v.anchor.generate',
      'video_profile.single_frame_i2v.anchor.review'
  )
  AND version.version = 4
  AND version.metadata->>'seedMigration' = '000023_image_prompt_provider_safety_contract_v4';

UPDATE prompt_versions version
SET status = 'active', activated_at = now()
FROM prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
  AND template.template_key IN (
      'video_profile.single_frame_i2v.anchor.generate',
      'video_profile.single_frame_i2v.anchor.review'
  )
  AND version.version = 3
  AND version.metadata->>'seedMigration' = '000022_image_prompt_provider_safety_contract';

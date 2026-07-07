CREATE TABLE IF NOT EXISTS project_manual_bindings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  manual_kind TEXT NOT NULL,
  prompt_version_id UUID NOT NULL REFERENCES prompt_versions(id) ON DELETE RESTRICT,
  status TEXT NOT NULL DEFAULT 'active',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT project_manual_bindings_kind_check CHECK (manual_kind IN ('director', 'visual')),
  CONSTRAINT project_manual_bindings_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX IF NOT EXISTS idx_project_manual_bindings_project
  ON project_manual_bindings(project_id, manual_kind, status);

CREATE UNIQUE INDEX IF NOT EXISTS idx_project_manual_bindings_one_active
  ON project_manual_bindings(project_id, manual_kind)
  WHERE status = 'active';

DROP TRIGGER IF EXISTS project_manual_bindings_set_updated_at ON project_manual_bindings;
CREATE TRIGGER project_manual_bindings_set_updated_at
BEFORE UPDATE ON project_manual_bindings
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, 'default_director_manual', '默认导演手册', 'CineWeave 默认导演工作手册。', 'director_manual', 'text', 'text.generate', 'system', 'active', true
WHERE NOT EXISTS (
  SELECT 1 FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'default_director_manual'
);

UPDATE prompt_templates
SET
  name = '默认导演手册',
  description = 'CineWeave 默认导演工作手册。',
  purpose = 'director_manual',
  modality = 'text',
  task_type = 'text.generate',
  scope = 'system',
  status = 'active',
  is_system = true
WHERE organization_id IS NULL AND template_key = 'default_director_manual';

WITH tmpl AS (
  SELECT id FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'default_director_manual' ORDER BY created_at LIMIT 1
),
content AS (
  SELECT $manual$# 默认导演手册

## README
- 目标：把项目原文忠实转化为可生产的短视频流程。
- 原则：优先保留原文关键事件、人物动机、场景因果和重要台词。
- 工作方式：每一步都必须能回溯到原文、剧本、分镜或资产记录。

## 导演规划
- 先确认内容范围，再生成剧本、资产、分镜和视频。
- 镜头必须服务剧情推进，不额外添加无关台词、桥段或人物关系。
- 对无法确认的信息保持克制，标记待确认，不擅自扩写为新设定。

## 分镜表设计
- 每个镜头必须包含镜头编号、时长、景别、机位、画面、运动、情绪、图片提示词和视频提示词。
- 分镜顺序必须遵循剧本场景顺序和原文事件顺序。
- 修改分镜后，相关镜头图片、镜头视频和最终成片必须标记为需要重生成。$manual$::text AS value
)
INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT tmpl.id, tmpl.id, 1, 1, 'active', 'System v1', content.value, 'markdown', '{}'::jsonb,
       '{"seed":"system","manualKind":"director","manualSections":["README","director_planning","storyboard_table_design"]}'::jsonb,
       'sha256:' || encode(digest(content.value, 'sha256'), 'hex'), now()
FROM tmpl, content
WHERE NOT EXISTS (SELECT 1 FROM prompt_versions WHERE template_id = tmpl.id);

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, 'default_visual_manual', '默认视觉手册', 'CineWeave 默认视觉一致性手册。', 'visual_manual', 'text', 'image.generate', 'system', 'active', true
WHERE NOT EXISTS (
  SELECT 1 FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'default_visual_manual'
);

UPDATE prompt_templates
SET
  name = '默认视觉手册',
  description = 'CineWeave 默认视觉一致性手册。',
  purpose = 'visual_manual',
  modality = 'text',
  task_type = 'image.generate',
  scope = 'system',
  status = 'active',
  is_system = true
WHERE organization_id IS NULL AND template_key = 'default_visual_manual';

WITH tmpl AS (
  SELECT id FROM prompt_templates WHERE organization_id IS NULL AND template_key = 'default_visual_manual' ORDER BY created_at LIMIT 1
),
content AS (
  SELECT $manual$# 默认视觉手册

## 角色模板
- 记录姓名、年龄感、身份、体型、五官、发型、服装基调、标志物和禁用变化。
- 角色主参考图必须优先保持脸型、发型、服装轮廓和色彩识别点。

## 角色衍生模板
- 记录镜头内服装、姿态、表情、动作、情绪和与场景的空间关系。
- 衍生图只能表达镜头状态，不得把服装、姿态或阶段误写为新角色。

## 场景模板
- 记录地点、时代、空间结构、光线、天气、材质、色彩、可复用背景元素。

## 场景衍生模板
- 记录镜头时刻、机位、遮挡关系、光线变化、人物进入方向和环境状态。

## 道具模板
- 记录道具名称、功能、尺寸、材质、颜色、磨损状态、与角色的绑定关系。

## 道具衍生模板
- 记录道具在镜头中的位置、持有者、运动方式和剧情功能。

## 分镜模板
- 镜头图片提示词必须组合角色、场景、道具、景别、机位、构图、光线和情绪。
- 不添加字幕、水印、分屏、拼贴、漫画气泡或无关额外角色。

## 分镜视频模板
- 视频提示词必须保持参考图中的人物、场景、光线、构图和美术风格。
- 明确运动方向、镜头运动、时长、比例和清晰度要求。
- 需要参考图、首尾帧或视频参考时，必须按模型能力选择请求方式。$manual$::text AS value
)
INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT tmpl.id, tmpl.id, 1, 1, 'active', 'System v1', content.value, 'markdown', '{}'::jsonb,
       '{"seed":"system","manualKind":"visual","manualSections":["character","character_derived","scene","scene_derived","prop","prop_derived","storyboard","storyboard_video"]}'::jsonb,
       'sha256:' || encode(digest(content.value, 'sha256'), 'hex'), now()
FROM tmpl, content
WHERE NOT EXISTS (SELECT 1 FROM prompt_versions WHERE template_id = tmpl.id);

INSERT INTO schema_migrations(version) VALUES ('000038_project_manual_bindings')
ON CONFLICT (version) DO NOTHING;

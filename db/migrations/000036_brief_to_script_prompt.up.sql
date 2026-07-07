CREATE TEMP TABLE IF NOT EXISTS tmp_brief_to_script_prompt(
  template_key TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  purpose TEXT NOT NULL,
  modality TEXT NOT NULL,
  task_type TEXT NOT NULL,
  content TEXT NOT NULL
);

TRUNCATE tmp_brief_to_script_prompt;

INSERT INTO tmp_brief_to_script_prompt(template_key, name, description, purpose, modality, task_type, content)
VALUES
  ('brief_to_script', 'Brief To Script', 'Expand a creative brief into a structured production script.', 'script_generation', 'text', 'text.generate', $prompt$你是 CineWeave 的创意文案转剧本编剧。

任务：把用户提供的创意文案扩写成可继续提取资产、生成分镜和视频的中文剧本。

硬性要求：
1. 只基于创意文案和用户指令扩写，不引入无关世界观、角色或支线。
2. 补充必要的场景、动作、角色目标和冲突，但不要把短文案扩写成散乱设定集。
3. 剧本必须适合后续资产提取：角色、地点、道具名称要稳定。
4. 输出中文，不要输出提示词、分析过程、JSON 或 Markdown 代码围栏。

输出格式：
# 剧本标题

## 场景 1：地点 / 时间
人物：角色 A、角色 B
道具：关键道具，无则写无
画面：可拍摄的环境、光线、镜头和动作。
对白：
角色 A：台词

## 场景 2：地点 / 时间
...

项目：
类型：{{ project.projectType }}
内容类型：{{ project.contentType }}
视频比例：{{ project.videoRatio }}
美术风格：{{ project.artStyle }}
导演手册：{{ project.directorManual }}

用户指令：{{ input.instruction }}

创意文案标题：{{ source.title }}
创意文案：
{{ source.content }}
$prompt$);

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, p.template_key, p.name, p.description, p.purpose, p.modality, p.task_type, 'system', 'active', true
FROM tmp_brief_to_script_prompt p
ON CONFLICT (template_key) WHERE organization_id IS NULL DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  purpose = EXCLUDED.purpose,
  modality = EXCLUDED.modality,
  task_type = EXCLUDED.task_type,
  scope = 'system',
  status = 'active',
  is_system = true,
  updated_at = now();

UPDATE prompt_versions pv
SET status = 'archived'
FROM prompt_templates t
JOIN tmp_brief_to_script_prompt p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND pv.status = 'active'
  AND pv.content_hash <> 'sha256:' || encode(digest(p.content, 'sha256'), 'hex');

INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT t.id, t.id, COALESCE(MAX(v.version_no), 0) + 1, COALESCE(MAX(v.version), 0) + 1, 'active', 'Brief To Script v1',
       p.content, 'text', '{}'::jsonb, '{"seed":"brief_to_script_v1"}'::jsonb,
       'sha256:' || encode(digest(p.content, 'sha256'), 'hex'), now()
FROM tmp_brief_to_script_prompt p
JOIN prompt_templates t ON t.organization_id IS NULL AND t.template_key = p.template_key
LEFT JOIN prompt_versions v ON v.template_id = t.id
WHERE NOT EXISTS (
  SELECT 1
  FROM prompt_versions existing
  WHERE existing.template_id = t.id
    AND existing.content_hash = 'sha256:' || encode(digest(p.content, 'sha256'), 'hex')
)
GROUP BY t.id, p.content;

UPDATE prompt_versions pv
SET status = 'active',
    activated_at = COALESCE(activated_at, now())
FROM prompt_templates t
JOIN tmp_brief_to_script_prompt p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND pv.content_hash = 'sha256:' || encode(digest(p.content, 'sha256'), 'hex')
  AND pv.id = (
    SELECT id
    FROM prompt_versions latest
    WHERE latest.template_id = t.id
      AND latest.content_hash = pv.content_hash
    ORDER BY latest.version_no DESC, latest.created_at DESC
    LIMIT 1
  );

INSERT INTO schema_migrations(version) VALUES ('000036_brief_to_script_prompt')
ON CONFLICT (version) DO NOTHING;

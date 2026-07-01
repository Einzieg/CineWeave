DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM schema_migrations WHERE version = '000015_novel_events_adaptation_plans'
  ) THEN
    DROP TABLE IF EXISTS adaptation_plans CASCADE;
    DROP TABLE IF EXISTS novel_event_links CASCADE;
    DROP TABLE IF EXISTS novel_events CASCADE;
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS novel_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_id UUID NOT NULL REFERENCES project_sources(id) ON DELETE CASCADE,
  chapter_id UUID NULL REFERENCES novel_chapters(id) ON DELETE CASCADE,
  event_index INTEGER NOT NULL,
  sequence_no INTEGER NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  event_type TEXT NULL,
  importance INTEGER NOT NULL DEFAULT 3,
  timeline_hint TEXT NULL,
  location_hint TEXT NULL,
  emotional_tone TEXT NULL,
  conflict TEXT NULL,
  outcome TEXT NULL,
  adaptation_hint TEXT NULL,
  characters JSONB NOT NULL DEFAULT '[]',
  scenes JSONB NOT NULL DEFAULT '[]',
  props JSONB NOT NULL DEFAULT '[]',
  keywords JSONB NOT NULL DEFAULT '[]',
  raw_excerpt TEXT NULL,
  review_status TEXT NOT NULL DEFAULT 'pending',
  manual_override BOOLEAN NOT NULL DEFAULT false,
  stale_state TEXT NOT NULL DEFAULT 'fresh',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  edited_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  edited_at TIMESTAMPTZ NULL,
  UNIQUE(chapter_id, event_index)
);

CREATE INDEX IF NOT EXISTS idx_novel_events_project ON novel_events(project_id, sequence_no);
CREATE INDEX IF NOT EXISTS idx_novel_events_source ON novel_events(source_id, sequence_no);
CREATE INDEX IF NOT EXISTS idx_novel_events_chapter ON novel_events(chapter_id, event_index);
CREATE INDEX IF NOT EXISTS idx_novel_events_review ON novel_events(project_id, review_status);

DROP TRIGGER IF EXISTS novel_events_set_updated_at ON novel_events;
CREATE TRIGGER novel_events_set_updated_at
BEFORE UPDATE ON novel_events
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS novel_event_links (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_event_id UUID NOT NULL REFERENCES novel_events(id) ON DELETE CASCADE,
  target_event_id UUID NOT NULL REFERENCES novel_events(id) ON DELETE CASCADE,
  link_type TEXT NOT NULL,
  description TEXT NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(source_event_id, target_event_id, link_type)
);

CREATE INDEX IF NOT EXISTS idx_novel_event_links_project ON novel_event_links(project_id);
CREATE INDEX IF NOT EXISTS idx_novel_event_links_source ON novel_event_links(source_event_id);
CREATE INDEX IF NOT EXISTS idx_novel_event_links_target ON novel_event_links(target_event_id);

CREATE TABLE IF NOT EXISTS adaptation_plans (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_id UUID NULL REFERENCES project_sources(id) ON DELETE SET NULL,
  script_id UUID NULL REFERENCES scripts(id) ON DELETE SET NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft',
  target_format TEXT NOT NULL DEFAULT 'short_video',
  target_duration_seconds INTEGER NULL,
  max_shots INTEGER NULL,
  selected_event_ids JSONB NOT NULL DEFAULT '[]',
  structure JSONB NOT NULL DEFAULT '{}',
  content TEXT NOT NULL DEFAULT '',
  prompt_version_id UUID NULL REFERENCES prompt_versions(id) ON DELETE SET NULL,
  prompt_hash TEXT NULL,
  review_status TEXT NOT NULL DEFAULT 'pending',
  manual_override BOOLEAN NOT NULL DEFAULT false,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  edited_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  edited_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_adaptation_plans_project ON adaptation_plans(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_adaptation_plans_source ON adaptation_plans(source_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_adaptation_plans_status ON adaptation_plans(project_id, status);
CREATE INDEX IF NOT EXISTS idx_adaptation_plans_review ON adaptation_plans(project_id, review_status);

DROP TRIGGER IF EXISTS adaptation_plans_set_updated_at ON adaptation_plans;
CREATE TRIGGER adaptation_plans_set_updated_at
BEFORE UPDATE ON adaptation_plans
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

INSERT INTO permissions(permission_key, name, description) VALUES
  ('novel_event.read', 'Novel Event Read', 'Read novel events and links'),
  ('novel_event.write', 'Novel Event Write', 'Create or update novel events and links'),
  ('adaptation_plan.read', 'Adaptation Plan Read', 'Read adaptation plans'),
  ('adaptation_plan.write', 'Adaptation Plan Write', 'Create or update adaptation plans')
ON CONFLICT (permission_key) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description;

WITH role_grants(role_key, permission_key) AS (
  VALUES
    ('project_owner', 'novel_event.read'),
    ('project_owner', 'novel_event.write'),
    ('project_owner', 'adaptation_plan.read'),
    ('project_owner', 'adaptation_plan.write'),
    ('project_editor', 'novel_event.read'),
    ('project_editor', 'novel_event.write'),
    ('project_editor', 'adaptation_plan.read'),
    ('project_editor', 'adaptation_plan.write'),
    ('project_viewer', 'novel_event.read'),
    ('project_viewer', 'adaptation_plan.read')
)
INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, g.permission_key
FROM role_grants g
JOIN roles r ON r.organization_id IS NULL AND r.role_key = g.role_key
JOIN permissions p ON p.permission_key = g.permission_key
ON CONFLICT DO NOTHING;

WITH seed_prompts(template_key, name, description, purpose, modality, task_type, content) AS (
  VALUES
    ('novel_event_extraction', 'Novel Event Extraction', 'Extract structured chapter events from novel text.', 'novel_event_extraction', 'text', 'text.generate', $prompt$You are CineWeave's novel event extraction agent.

Extract 3 to 8 key filmable story events from the chapter.

Rules:
- Return valid JSON only.
- Do not return markdown.
- Do not return explanations.
- Events must be usable for visual adaptation or plot progression.
- Abstract inner thought should become an event only when it changes action, conflict, or story direction.
- characters, scenes, and props must use stable names.
- Do not treat clothing, posture, expressions, or states as character names.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Art style: {{ project.artStyle }}

Source:
- Title: {{ source.title }}
- Chapter: {{ chapter.title }}

Chapter content:
{{ chapter.content }}

Return JSON:
{
  "events": [
    {
      "title": "事件标题",
      "summary": "事件摘要",
      "eventType": "conflict",
      "importance": 4,
      "timelineHint": "清晨",
      "locationHint": "火车站",
      "emotionalTone": "紧张、期待",
      "conflict": "人物目标与阻碍",
      "outcome": "事件结果",
      "adaptationHint": "适合改编为开场镜头",
      "characters": ["林初"],
      "scenes": ["清晨火车站"],
      "props": ["旧相机"],
      "keywords": ["晨光", "列车", "等待"],
      "rawExcerpt": "原文关键片段"
    }
  ],
  "links": [
    {
      "sourceEventIndex": 1,
      "targetEventIndex": 2,
      "linkType": "causes",
      "description": "事件 1 导致事件 2"
    }
  ]
}$prompt$),
    ('adaptation_plan_generation', 'Adaptation Plan Generation', 'Generate a trackable adaptation plan from approved novel events.', 'adaptation_plan', 'text', 'text.generate', $prompt$You are CineWeave's adaptation planning agent.

Create an adaptation plan from confirmed novel events.

Rules:
- Return valid JSON only.
- Do not return markdown.
- Prefer events that are visual, dramatic, and useful for the target format.
- Explain omissions with practical adaptation reasons.
- Keep the plan compatible with silent video production unless the instruction says otherwise.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Video ratio: {{ project.videoRatio }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}
- Visual manual: {{ project.visualManual }}

Target:
- Format: {{ input.targetFormat }}
- Duration seconds: {{ input.targetDurationSeconds }}
- Max shots: {{ input.maxShots }}
- Instruction: {{ input.instruction }}

Events:
{{ events.items }}

Return JSON:
{
  "title": "改编计划标题",
  "logline": "一句话故事",
  "theme": "主题",
  "structure": {
    "opening": "开场",
    "development": "发展",
    "climax": "高潮",
    "ending": "结尾"
  },
  "selectedEvents": ["event-id-or-index"],
  "omittedEvents": [
    {
      "event": "事件标题",
      "reason": "删减原因"
    }
  ],
  "visualStrategy": "视觉策略",
  "characterStrategy": "角色策略",
  "shotStrategy": "镜头策略",
  "estimatedShots": 3,
  "notes": "注意事项"
}$prompt$),
    ('script_from_adaptation_plan', 'Script From Adaptation Plan', 'Generate structured script content from an adaptation plan.', 'script_generation', 'text', 'text.generate', $prompt$You are CineWeave's script agent.

Generate a structured script from the adaptation plan and selected events.

Rules:
- Return only markdown script content.
- Do not include commentary outside the script.
- Keep scene structure clear enough for Asset Agent and Storyboard Agent.
- Prefer visual action and production-ready scene descriptions.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Video ratio: {{ project.videoRatio }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}

Instruction: {{ input.instruction }}

Adaptation plan:
{{ plan.content }}

Selected events:
{{ events.items }}

Suggested format:
# 剧本标题

## 故事概述

## 场景 1
- 地点：
- 人物：
- 道具：
- 画面：
- 动作：
- 台词：
- 情绪：
- 备注：
$prompt$)
)
INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, template_key, name, description, purpose, modality, task_type, 'system', 'active', true
FROM seed_prompts
ON CONFLICT DO NOTHING;

WITH seed_prompts(template_key, content) AS (
  VALUES
    ('novel_event_extraction', $prompt$You are CineWeave's novel event extraction agent.

Extract 3 to 8 key filmable story events from the chapter.

Rules:
- Return valid JSON only.
- Do not return markdown.
- Do not return explanations.
- Events must be usable for visual adaptation or plot progression.
- Abstract inner thought should become an event only when it changes action, conflict, or story direction.
- characters, scenes, and props must use stable names.
- Do not treat clothing, posture, expressions, or states as character names.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Art style: {{ project.artStyle }}

Source:
- Title: {{ source.title }}
- Chapter: {{ chapter.title }}

Chapter content:
{{ chapter.content }}

Return JSON:
{
  "events": [
    {
      "title": "事件标题",
      "summary": "事件摘要",
      "eventType": "conflict",
      "importance": 4,
      "timelineHint": "清晨",
      "locationHint": "火车站",
      "emotionalTone": "紧张、期待",
      "conflict": "人物目标与阻碍",
      "outcome": "事件结果",
      "adaptationHint": "适合改编为开场镜头",
      "characters": ["林初"],
      "scenes": ["清晨火车站"],
      "props": ["旧相机"],
      "keywords": ["晨光", "列车", "等待"],
      "rawExcerpt": "原文关键片段"
    }
  ],
  "links": [
    {
      "sourceEventIndex": 1,
      "targetEventIndex": 2,
      "linkType": "causes",
      "description": "事件 1 导致事件 2"
    }
  ]
}$prompt$),
    ('adaptation_plan_generation', $prompt$You are CineWeave's adaptation planning agent.

Create an adaptation plan from confirmed novel events.

Rules:
- Return valid JSON only.
- Do not return markdown.
- Prefer events that are visual, dramatic, and useful for the target format.
- Explain omissions with practical adaptation reasons.
- Keep the plan compatible with silent video production unless the instruction says otherwise.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Video ratio: {{ project.videoRatio }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}
- Visual manual: {{ project.visualManual }}

Target:
- Format: {{ input.targetFormat }}
- Duration seconds: {{ input.targetDurationSeconds }}
- Max shots: {{ input.maxShots }}
- Instruction: {{ input.instruction }}

Events:
{{ events.items }}

Return JSON:
{
  "title": "改编计划标题",
  "logline": "一句话故事",
  "theme": "主题",
  "structure": {
    "opening": "开场",
    "development": "发展",
    "climax": "高潮",
    "ending": "结尾"
  },
  "selectedEvents": ["event-id-or-index"],
  "omittedEvents": [
    {
      "event": "事件标题",
      "reason": "删减原因"
    }
  ],
  "visualStrategy": "视觉策略",
  "characterStrategy": "角色策略",
  "shotStrategy": "镜头策略",
  "estimatedShots": 3,
  "notes": "注意事项"
}$prompt$),
    ('script_from_adaptation_plan', $prompt$You are CineWeave's script agent.

Generate a structured script from the adaptation plan and selected events.

Rules:
- Return only markdown script content.
- Do not include commentary outside the script.
- Keep scene structure clear enough for Asset Agent and Storyboard Agent.
- Prefer visual action and production-ready scene descriptions.

Project:
- Type: {{ project.projectType }}
- Content type: {{ project.contentType }}
- Video ratio: {{ project.videoRatio }}
- Art style: {{ project.artStyle }}
- Director manual: {{ project.directorManual }}

Instruction: {{ input.instruction }}

Adaptation plan:
{{ plan.content }}

Selected events:
{{ events.items }}

Suggested format:
# 剧本标题

## 故事概述

## 场景 1
- 地点：
- 人物：
- 道具：
- 画面：
- 动作：
- 台词：
- 情绪：
- 备注：
$prompt$)
),
tmpl AS (
  SELECT pt.id, sp.template_key, sp.content
  FROM seed_prompts sp
  JOIN prompt_templates pt ON pt.organization_id IS NULL AND pt.template_key = sp.template_key
)
INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT tmpl.id, tmpl.id, 1, 1, 'active', 'System v1', tmpl.content, 'text', '{}'::jsonb, '{"seed":"system"}'::jsonb,
       'sha256:' || encode(digest(tmpl.content, 'sha256'), 'hex'), now()
FROM tmpl
WHERE NOT EXISTS (SELECT 1 FROM prompt_versions WHERE template_id = tmpl.id);

INSERT INTO schema_migrations(version) VALUES ('000015_novel_events_adaptation_plans')
ON CONFLICT (version) DO NOTHING;

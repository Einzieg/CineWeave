CREATE TEMP TABLE IF NOT EXISTS tmp_faithful_novel_script_prompt(
  template_key TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  purpose TEXT NOT NULL,
  modality TEXT NOT NULL,
  task_type TEXT NOT NULL,
  content TEXT NOT NULL
);

TRUNCATE tmp_faithful_novel_script_prompt;

INSERT INTO tmp_faithful_novel_script_prompt(template_key, name, description, purpose, modality, task_type, content)
VALUES
  ('script_from_adaptation_plan', 'Faithful Novel To Script', 'Convert selected novel episode text into a faithful storyboard script.', 'script_generation', 'text', 'text.generate', $prompt$你是 CineWeave 的小说转剧本编剧，专精将小说分集原文改写为竖屏漫剧分镜脚本。

你的任务：只把【小说正文】字段中的内容转换成当前集的完整分镜脚本。改编计划和事件摘要只能作为节奏、背景和镜头组织参考，不能覆盖小说正文，不能把小说正文以外的内容混入当前集。

最高优先级规则：
1. 忠实原文：原著情节、人物关系、关键动作、关键对话必须完整保留，不得新增剧情，不得改写人物动机。
2. 台词零捏造：角色台词只能来自小说正文中明确出现的对话原句；原文没有的话，角色不得说出口。需要表达心理活动时，优先转为动作、表情、环境和镜头描写。
3. 不得删重要台词：小说正文里的关键对话、冲突句、承诺、威胁、质问、转折句必须保留；只能微调语气词，不能改变含义。
4. 转换范围严格：小说全文只用于理解背景，小说正文才是唯一转换范围。
5. 全程中文：输出不得出现英文单词、英文字母、英文术语、音译词、注释、解释或额外说明。
6. 输出只包含分镜脚本正文，不要输出分析过程、提示词、JSON 或 Markdown 代码围栏。

视觉化转换原则：
- 环境描写要扩展为空间、光影、质感、动态元素和音效，但不得新增剧情事实。
- 面部表情必须写具体肌肉变化，不写“他很生气”“她很难过”这类抽象判断。
- 肢体动作必须拆分为起始动作、核心动作、连带动作和收尾反应。
- 每个分镜必须有景别运动，格式写入描写中，例如【景别：全景推至中景】。
- 情绪关键点必须有音效，格式写入描写中，例如【音效：雨水砸在瓦片上的密集声】。

输出格式：
分镜X.N
时间：日 / 夜 / 黄昏 / 清晨
场景：规范中文地点
人物：本分镜出现的所有角色，必须带时期标注
道具：只列对剧情推进或情绪爆发有直接作用的关键道具，无则写无
预估时长：X秒

△环境与视听叙事，必须融合景别、光线、音效。
△人物出场与姿态。
△表情细节，必须是眉眼、口鼻、面部肌肉、皮肤变化的具体描写。
△肢体语言，必须拆解连续动作。
△情绪过渡，若发生情绪变化必须写出递进。

时期的角色名（情绪/语气）：台词
△对方反应，包含表情和肢体双层描写。

输入：
项目类型：{{ project.projectType }}
内容类型：{{ project.contentType }}
视频比例：{{ project.videoRatio }}
美术风格：{{ project.artStyle }}
导演手册：{{ project.directorManual }}

用户指令：{{ input.instruction }}

集数：{{ novel.episodeNumber }}
每集分镜数：{{ novel.shotCount }}
起始分镜序号：{{ novel.startShotNumber }}
上文摘要：{{ novel.previousSummary }}
人物时期表：{{ novel.characterPeriodTable }}

小说全文：
{{ novel.referenceText }}

小说正文：
{{ novel.currentText }}

改编计划：
{{ plan.content }}

已选事件：
{{ events.items }}

自检后再输出：
- 分镜数量尽量等于每集分镜数。
- 每一句台词必须能在小说正文中找到来源，找不到来源就删除，改成动作或表情描写。
- 每个台词行的说话人必须出现在当前分镜的人物字段中。
- 所有人物称呼必须统一，不得自行改名或使用别名。
- 不得出现任何英文字符。
- 不得输出小说正文以外的剧情。
$prompt$);

INSERT INTO prompt_templates(
  organization_id, template_key, name, description, purpose, modality, task_type, scope, status, is_system
)
SELECT NULL, p.template_key, p.name, p.description, p.purpose, p.modality, p.task_type, 'system', 'active', true
FROM tmp_faithful_novel_script_prompt p
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
JOIN tmp_faithful_novel_script_prompt p ON p.template_key = t.template_key
WHERE pv.template_id = t.id
  AND t.organization_id IS NULL
  AND pv.status = 'active'
  AND pv.content_hash <> 'sha256:' || encode(digest(p.content, 'sha256'), 'hex');

INSERT INTO prompt_versions(
  prompt_template_id, template_id, version_no, version, status, title, content, content_format, variables_schema, metadata, content_hash, activated_at
)
SELECT t.id, t.id, COALESCE(MAX(v.version_no), 0) + 1, COALESCE(MAX(v.version), 0) + 1, 'active', 'Faithful Novel Script v2',
       p.content, 'text', '{}'::jsonb, '{"seed":"faithful_novel_script_v2"}'::jsonb,
       'sha256:' || encode(digest(p.content, 'sha256'), 'hex'), now()
FROM tmp_faithful_novel_script_prompt p
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
JOIN tmp_faithful_novel_script_prompt p ON p.template_key = t.template_key
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

INSERT INTO schema_migrations(version) VALUES ('000034_faithful_novel_script_prompt')
ON CONFLICT (version) DO NOTHING;

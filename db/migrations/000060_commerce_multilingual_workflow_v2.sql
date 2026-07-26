-- +goose Up

SET search_path TO public;

-- Keep the published v1 prompt addressable by immutable workflow snapshots,
-- but make the spoken-language-aware resolver the active system version.
UPDATE prompt_versions
SET status = 'archived'
WHERE id = md5('cineweave:commerce-prompt-version:commerce_language_resolver:1')::uuid
  AND status = 'active';

WITH source_prompt AS (
    SELECT
        template.id AS template_id,
        version.variables_schema,
        version.metadata
    FROM prompt_templates template
    JOIN prompt_versions version
      ON version.id = md5('cineweave:commerce-prompt-version:commerce_language_resolver:1')::uuid
    WHERE template.organization_id IS NULL
      AND template.template_key = 'commerce_language_resolver'
      AND template.id = COALESCE(version.template_id, version.prompt_template_id)
), resolver AS (
    SELECT
        $prompt$
你是 CineWeave 带货视频语言解析器。根据冻结的单个脚本单元、用户语言模式、模板允许的 BCP 47 locale 和目标平台，自动确定实际口播语言。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. sourceLanguage 和 targetLanguage 必须是输入允许的规范 BCP 47 tag；不得发明 locale。
3. explicit 模式下，targetLanguage 必须逐字等于用户指定值。
4. auto 模式不允许要求用户确认。必须在最多 3 轮内给出一个可执行结果；无法可靠判断时返回契约错误供下一轮修正。
5. 语言判断优先级：明确的 VOICEOVER/NARRATION/旁白/口播语言标注 > 实际台词和旁白文本 > 屏幕文字 > 场景描述和制作指令。英文场景说明不能覆盖明确标注为 Malay、Bahasa Melayu、中文等语言的口播。
6. 脚本可包含多种书写语言。languageComposition 可以为 mixed，但 targetLanguage 必须表示观众实际听到的主要口播语言，needsUserConfirmation 必须为 false。
7. 不得翻译、改写或补充脚本，不得读取其他脚本单元。

严格输出契约：
{"contractVersion":"commerce-language-resolution/v1","sourceLanguage":"ms-MY","targetLanguage":"ms-MY","confidence":0.98,"languageComposition":"mixed","needsUserConfirmation":false,"reasoning":"脚本明确标注 VOICEOVER (Malay)，实际口播为马来语，英文仅用于场景说明","issues":[]}

输入：{{ input.context }}
        $prompt$::text AS content
)
INSERT INTO prompt_versions(
    id, prompt_template_id, version_no, content, variables_schema, content_hash,
    template_id, version, status, title, content_format, metadata, activated_at, managed_by
)
SELECT
    md5('cineweave:commerce-prompt-version:commerce_language_resolver:2')::uuid,
    source_prompt.template_id,
    2,
    resolver.content,
    source_prompt.variables_schema,
    'sha256:' || encode(public.digest(pg_catalog.convert_to(resolver.content, 'UTF8'), 'sha256'), 'hex'),
    source_prompt.template_id,
    2,
    'active',
    '带货视频语言解析器 v2',
    'markdown',
    source_prompt.metadata || jsonb_build_object(
        'seedMigration', '000060_commerce_multilingual_workflow_v2',
        'automaticDecisionOnly', true,
        'spokenLanguagePriority', true
    ),
    now(),
    'system'
FROM source_prompt
CROSS JOIN resolver;

-- Extend immutability to the new system prompt version.
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

WITH source_template AS (
    SELECT version.*
    FROM commerce_workflow_template_versions version
    WHERE version.id = md5('cineweave:commerce-workflow-template:commerce_video_v1:1')::uuid
      AND version.status = 'published'
), resolver_prompt AS (
    SELECT
        template.template_key,
        version.id,
        version.version,
        version.content_hash,
        (version.metadata->>'maxReviewRounds')::integer AS max_review_rounds
    FROM prompt_versions version
    JOIN prompt_templates template
      ON template.id = COALESCE(version.template_id, version.prompt_template_id)
    WHERE version.id = md5('cineweave:commerce-prompt-version:commerce_language_resolver:2')::uuid
      AND version.status = 'active'
), locale_codes AS (
    SELECT $json$
      ["zh-CN","zh-TW","en-US","en-GB","ms-MY","id-ID","ja-JP","ko-KR","th-TH","vi-VN","es-ES","es-MX","pt-BR","fr-FR","de-DE","it-IT","ru-RU","ar-SA","hi-IN","tr-TR"]
    $json$::jsonb AS value
), multilingual_contract AS (
    SELECT $json$
    {
      "contractVersion": "commerce-language-contract/v2",
      "defaultLocale": "zh-CN",
      "resolver": {
        "autoConfidenceThreshold": 0.80,
        "confirmationMode": "disabled",
        "mixedLanguagePolicy": "spoken_content_priority",
        "unsupportedLocalePolicy": "fail",
        "spokenLanguageDirectivePriority": true,
        "localeFormat": "BCP47"
      },
      "localization": {
        "maxReviewRounds": 3,
        "identityLocalizationRequired": true,
        "preserve": ["brand","model","number","price","offer_condition","negation","qualifier","compliance_claim"]
      },
      "locales": [
        {"locale":"zh-CN","label":"简体中文","textDirection":"ltr","lineBreakPolicy":"cjk_phrase_boundary","timingPolicy":{"version":"zh-cn-voiceover/v2","unit":"han_character","normalUnitsPerSecond":3.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"zh-TW","label":"繁体中文","textDirection":"ltr","lineBreakPolicy":"cjk_phrase_boundary","timingPolicy":{"version":"zh-tw-voiceover/v2","unit":"han_character","normalUnitsPerSecond":3.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"en-US","label":"英语（美国）","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"en-us-voiceover/v2","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"en-GB","label":"英语（英国）","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"en-gb-voiceover/v2","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"ms-MY","label":"马来语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"ms-my-voiceover/v1","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"id-ID","label":"印度尼西亚语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"id-id-voiceover/v1","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"ja-JP","label":"日语","textDirection":"ltr","lineBreakPolicy":"cjk_phrase_boundary","timingPolicy":{"version":"ja-jp-voiceover/v1","unit":"mora","normalUnitsPerSecond":4.0,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"ko-KR","label":"韩语","textDirection":"ltr","lineBreakPolicy":"phrase_boundary","timingPolicy":{"version":"ko-kr-voiceover/v1","unit":"syllable","normalUnitsPerSecond":3.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"th-TH","label":"泰语","textDirection":"ltr","lineBreakPolicy":"character_boundary","timingPolicy":{"version":"th-th-voiceover/v1","unit":"character","normalUnitsPerSecond":4.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"vi-VN","label":"越南语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"vi-vn-voiceover/v1","unit":"word","normalUnitsPerSecond":3.0,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"es-ES","label":"西班牙语（西班牙）","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"es-es-voiceover/v1","unit":"word","normalUnitsPerSecond":2.7,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"es-MX","label":"西班牙语（墨西哥）","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"es-mx-voiceover/v1","unit":"word","normalUnitsPerSecond":2.7,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"pt-BR","label":"葡萄牙语（巴西）","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"pt-br-voiceover/v1","unit":"word","normalUnitsPerSecond":2.7,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"fr-FR","label":"法语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"fr-fr-voiceover/v1","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"de-DE","label":"德语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"de-de-voiceover/v1","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"it-IT","label":"意大利语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"it-it-voiceover/v1","unit":"word","normalUnitsPerSecond":2.7,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"ru-RU","label":"俄语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"ru-ru-voiceover/v1","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"ar-SA","label":"阿拉伯语","textDirection":"rtl","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"ar-sa-voiceover/v1","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"hi-IN","label":"印地语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"hi-in-voiceover/v1","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}},
        {"locale":"tr-TR","label":"土耳其语","textDirection":"ltr","lineBreakPolicy":"word_boundary","timingPolicy":{"version":"tr-tr-voiceover/v1","unit":"word","normalUnitsPerSecond":2.5,"commaPauseSeconds":0.15,"sentencePauseSeconds":0.35,"segmentGapSeconds":0.10,"allowedOverrunSeconds":0}}
      ]
    }
    $json$::jsonb AS value
), version_contract AS (
    SELECT
        source_template.configuration_snapshot,
        jsonb_set(
            source_template.prompt_bindings,
            '{languageResolver}',
            jsonb_build_object(
                'templateKey', resolver_prompt.template_key,
                'promptVersionId', resolver_prompt.id,
                'version', resolver_prompt.version,
                'contentHash', resolver_prompt.content_hash,
                'contractVersion', 'commerce-language-resolution/v1',
                'maxReviewRounds', resolver_prompt.max_review_rounds
            ),
            true
        ) AS prompt_bindings,
        source_template.agent_model_contracts,
        multilingual_contract.value AS language_contract,
        source_template.image_capability_contract || jsonb_build_object(
            'capabilityApprovalRequired', false,
            'supportedPromptLanguages', locale_codes.value
        ) AS image_capability_contract,
        source_template.video_capability_contract || jsonb_build_object(
            'capabilityApprovalRequired', false,
            'supportedPromptLanguages', locale_codes.value,
            'nativeAudioLanguages', locale_codes.value,
            'nativeAudioLanguageApprovalRequired', false
        ) AS video_capability_contract,
        source_template.video_production_profile_version_id
    FROM source_template
    CROSS JOIN resolver_prompt
    CROSS JOIN locale_codes
    CROSS JOIN multilingual_contract
), hashed_contract AS (
    SELECT
        version_contract.*,
        encode(
            public.digest(
                pg_catalog.convert_to(
                    jsonb_build_object(
                        'templateKey', 'commerce_video_v1',
                        'version', 2,
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
    md5('cineweave:commerce-workflow-template:commerce_video_v1:2')::uuid,
    md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid,
    2,
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
WHERE id = md5('cineweave:commerce-workflow-template:commerce_video_v1:2')::uuid
  AND template_id = md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid
  AND version = 2;

DELETE FROM prompt_versions
WHERE id = md5('cineweave:commerce-prompt-version:commerce_language_resolver:2')::uuid
  AND metadata->>'seedMigration' = '000060_commerce_multilingual_workflow_v2';

UPDATE prompt_versions
SET status = 'active'
WHERE id = md5('cineweave:commerce-prompt-version:commerce_language_resolver:1')::uuid
  AND status = 'archived';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_commerce_prompt_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.metadata->>'seedMigration' = '000049_commerce_prompt_registry'
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

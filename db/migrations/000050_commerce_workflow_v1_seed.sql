-- +goose Up

SET search_path TO public;

-- +goose StatementBegin
DO $$
DECLARE
    commerce_prompt_count INTEGER;
BEGIN
    SELECT count(*)
    INTO commerce_prompt_count
    FROM prompt_versions version
    JOIN prompt_templates template ON template.id = version.template_id
    WHERE template.organization_id IS NULL
      AND template.template_key IN (
          'commerce_language_resolver',
          'commerce_script_localizer',
          'commerce_localization_reviewer',
          'commerce_script_organizer',
          'commerce_storyboard_planner',
          'commerce_storyboard_reviewer',
          'commerce_image_prompt_agent',
          'commerce_image_fidelity_reviewer',
          'commerce_video_prompt_agent',
          'commerce_video_prompt_reviewer'
      )
      AND version.status = 'active'
      AND version.managed_by = 'system'
      AND version.metadata->>'seedMigration' = '000049_commerce_prompt_registry';

    IF commerce_prompt_count <> 10 THEN
        RAISE EXCEPTION 'commerce_video_v1 requires exactly 10 active prompt contracts, found %', commerce_prompt_count
            USING ERRCODE = '23514';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM video_production_profile_versions profile_version
        JOIN video_production_profiles profile ON profile.id = profile_version.profile_id
        WHERE profile_version.id = '10000000-0000-4000-9000-000000000001'::uuid
          AND profile.profile_key = 'single_frame_i2v'
          AND profile_version.version = 1
          AND profile_version.lifecycle_state = 'published'
          AND profile_version.implementation_state = 'available'
    ) THEN
        RAISE EXCEPTION 'commerce_video_v1 requires published single_frame_i2v profile version 1'
            USING ERRCODE = '23514';
    END IF;
END;
$$;
-- +goose StatementEnd

INSERT INTO commerce_workflow_templates(
    id, organization_id, template_key, name, description, status
)
VALUES (
    md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid,
    NULL,
    'commerce_video_v1',
    '带货视频标准工作流',
    '多脚本单元、多语言、商品保真、单首帧图生视频的不可变业务工作流模板',
    'active'
);

WITH prompt_set AS (
    SELECT
        version.metadata->>'agentRole' AS role_key,
        template.template_key,
        version.id,
        version.version,
        version.content_hash,
        version.metadata->>'contractVersion' AS contract_version,
        (version.metadata->>'maxReviewRounds')::integer AS max_review_rounds
    FROM prompt_versions version
    JOIN prompt_templates template ON template.id = version.template_id
    WHERE template.organization_id IS NULL
      AND template.template_key IN (
          'commerce_language_resolver',
          'commerce_script_localizer',
          'commerce_localization_reviewer',
          'commerce_script_organizer',
          'commerce_storyboard_planner',
          'commerce_storyboard_reviewer',
          'commerce_image_prompt_agent',
          'commerce_image_fidelity_reviewer',
          'commerce_video_prompt_agent',
          'commerce_video_prompt_reviewer'
      )
      AND version.status = 'active'
      AND version.managed_by = 'system'
      AND version.metadata->>'seedMigration' = '000049_commerce_prompt_registry'
), prompt_binding AS (
    SELECT jsonb_object_agg(
        role_key,
        jsonb_build_object(
            'templateKey', template_key,
            'promptVersionId', id,
            'version', version,
            'contentHash', content_hash,
            'contractVersion', contract_version,
            'maxReviewRounds', max_review_rounds
        )
        ORDER BY role_key
    ) AS value
    FROM prompt_set
), seed_contract AS (
    SELECT
        $json$
        {
          "contractVersion": "commerce-workflow-configuration/v1",
          "durations": [15, 30, 60],
          "aspectRatios": ["9:16", "16:9", "1:1"],
          "imageQualities": ["standard", "hd"],
          "languageModes": ["auto", "explicit"],
          "audioStrategies": ["native_av", "external_audio"],
          "audioRequirements": ["preferred", "required", "disabled"],
          "defaults": {
            "durationSeconds": 30,
            "aspectRatio": "9:16",
            "imageQuality": "standard",
            "languageMode": "auto",
            "audioStrategy": "native_av",
            "audioRequirement": "preferred",
            "fpsNumerator": 24,
            "fpsDenominator": 1,
            "timelineTimebase": 90000
          },
          "productReferences": {
            "minimum": 1,
            "recommendedMinimum": 3,
            "recommendedMaximum": 8,
            "primaryRequired": true
          },
          "reviewPolicy": {
            "maxReviewRounds": 3,
            "maxLocalizationRevisionRounds": 3,
            "maxStoryboardRevisionRounds": 3,
            "maxImagePromptRevisionRounds": 3,
            "maxVideoPromptRevisionRounds": 3,
            "maxAutomaticImageRegenerations": 0,
            "reviewerFeedbackMode": "structured_issues",
            "onExhausted": "needs_user_review"
          },
          "batchPolicy": {
            "concurrencySource": "organization_provider_limits",
            "partialCompletion": true,
            "retryFailedItemsOnly": true
          }
        }
        $json$::jsonb AS configuration_snapshot,
        prompt_binding.value AS prompt_bindings,
        $json$
        {
          "languageResolver": {"label":"语言解析","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"bcp47Locale":true}},
          "scriptLocalizer": {"label":"脚本本地化","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"segmentIdentityPreservation":true}},
          "localizationReviewer": {"label":"本地化审核","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"reviewDecisionContract":"commerce-review-decision/v1"}},
          "scriptOrganizer": {"label":"脚本整理","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"segmentIdentityPreservation":true}},
          "storyboardPlanner": {"label":"分镜规划","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"fullUnitContext":true}},
          "storyboardReviewer": {"label":"分镜审核","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"fullUnitContext":true}},
          "imagePromptAgent": {"label":"图片提示词","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"modelCapabilityContext":true}},
          "imageFidelityReviewer": {"label":"商品保真审核","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"multimodalImageInput":true}},
          "videoPromptAgent": {"label":"视频提示词","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"fullUnitContext":true,"modelCapabilityContext":true,"multimodalImageInput":true}},
          "videoPromptReviewer": {"label":"视频提示词审核","profileKey":"script_agent_default","taskType":"text.generate","modality":"text","usesInputLanguage":true,"usesOutputLanguage":true,"capabilityRequirements":{"structuredJson":true,"fullUnitContext":true,"modelCapabilityContext":true,"multimodalImageInput":true}}
        }
        $json$::jsonb AS agent_model_contracts,
        $json$
        {
          "contractVersion": "commerce-language-contract/v1",
          "defaultLocale": "zh-CN",
          "resolver": {
            "autoConfidenceThreshold": 0.85,
            "mixedLanguageRequiresConfirmation": true,
            "unsupportedLocaleRequiresConfirmation": true,
            "localeFormat": "BCP47"
          },
          "localization": {
            "maxReviewRounds": 3,
            "identityLocalizationRequired": true,
            "preserve": ["brand", "model", "number", "price", "offer_condition", "negation", "qualifier", "compliance_claim"]
          },
          "locales": [
            {
              "locale": "zh-CN",
              "label": "简体中文",
              "textDirection": "ltr",
              "fontStack": ["Noto Sans CJK SC", "Microsoft YaHei", "PingFang SC", "sans-serif"],
              "lineBreakPolicy": "cjk_phrase_boundary",
              "timingPolicy": {
                "version": "zh-cn-voiceover/v1",
                "unit": "han_character",
                "normalUnitsPerSecond": 3.5,
                "slowUnitsPerSecond": 3.0,
                "fastUnitsPerSecond": 4.0,
                "commaPauseSeconds": 0.15,
                "sentencePauseSeconds": 0.35,
                "segmentGapSeconds": 0.10,
                "minimumShotSeconds": 1
              }
            },
            {
              "locale": "en-US",
              "label": "英语（美国）",
              "textDirection": "ltr",
              "fontStack": ["Inter", "Arial", "sans-serif"],
              "lineBreakPolicy": "word_boundary",
              "timingPolicy": {
                "version": "en-us-voiceover/v1",
                "unit": "word",
                "normalUnitsPerSecond": 2.5,
                "slowUnitsPerSecond": 2.0,
                "fastUnitsPerSecond": 3.0,
                "commaPauseSeconds": 0.15,
                "sentencePauseSeconds": 0.35,
                "segmentGapSeconds": 0.10,
                "minimumShotSeconds": 1
              }
            }
          ]
        }
        $json$::jsonb AS language_contract,
        $json$
        {
          "contractVersion": "commerce-image-capability/v1",
          "label": "商品参考图生成",
          "profileKey": "image_generation_default",
          "taskType": "image.generate",
          "modality": "image",
          "usesPromptLanguage": true,
          "capabilityApprovalRequired": true,
          "referenceInput": {
            "required": true,
            "minimum": 1,
            "maximum": 8,
            "requestModes": ["multipart", "json_url", "json_base64"]
          },
          "aspectRatios": ["9:16", "16:9", "1:1"],
          "qualities": ["standard", "hd"],
          "resolutions": ["1024x1792", "1792x1024", "1024x1024"],
          "outputFormats": ["png", "jpeg", "webp"],
          "supportedPromptLanguages": ["zh-CN", "en-US"],
          "forbidVisibleOverlayText": true
        }
        $json$::jsonb AS image_capability_contract,
        $json$
        {
          "contractVersion": "commerce-video-capability/v1",
          "label": "单首帧镜头视频生成",
          "profileKey": "video_generation_default",
          "taskType": "video.image_to_video",
          "modality": "video",
          "usesPromptLanguage": true,
          "usesNativeAudio": true,
          "capabilityApprovalRequired": true,
          "videoProductionProfile": {
            "profileKey": "single_frame_i2v",
            "profileVersion": 1,
            "profileVersionId": "10000000-0000-4000-9000-000000000001",
            "compatibilityPolicy": "strict"
          },
          "request": {
            "asyncTaskRequired": true,
            "pollingRequired": true,
            "firstFrameRequired": true,
            "lastFrameAllowed": false,
            "videoReferenceAllowed": false,
            "minimumReferenceImages": 1,
            "maximumReferenceImages": 1
          },
          "duration": {
            "integerSecondsRequired": true,
            "mustBelongToApprovedCapabilitySnapshot": true
          },
          "aspectRatios": ["9:16", "16:9", "1:1"],
          "supportedPromptLanguages": ["zh-CN", "en-US"],
          "nativeAudioLanguages": ["zh-CN", "en-US"],
          "nativeAudioLanguageApprovalRequired": true,
          "outputFormats": ["mp4"]
        }
        $json$::jsonb AS video_capability_contract
    FROM prompt_binding
), hashed_contract AS (
    SELECT
        seed_contract.*,
        encode(
            public.digest(
                pg_catalog.convert_to(
                    jsonb_build_object(
                        'templateKey', 'commerce_video_v1',
                        'version', 1,
                        'configurationSnapshot', configuration_snapshot,
                        'promptBindings', prompt_bindings,
                        'agentModelContracts', agent_model_contracts,
                        'languageContract', language_contract,
                        'imageCapabilityContract', image_capability_contract,
                        'videoCapabilityContract', video_capability_contract,
                        'videoProductionProfileVersionId', '10000000-0000-4000-9000-000000000001'
                    )::text,
                    'UTF8'
                ),
                'sha256'
            ),
            'hex'
        ) AS content_hash
    FROM seed_contract
)
INSERT INTO commerce_workflow_template_versions(
    id, template_id, version, configuration_snapshot, prompt_bindings,
    agent_model_contracts, language_contract, image_capability_contract,
    video_capability_contract, video_production_profile_version_id,
    content_hash, status, published_at
)
SELECT
    md5('cineweave:commerce-workflow-template:commerce_video_v1:1')::uuid,
    md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid,
    1,
    hashed_contract.configuration_snapshot,
    hashed_contract.prompt_bindings,
    hashed_contract.agent_model_contracts,
    hashed_contract.language_contract,
    hashed_contract.image_capability_contract,
    hashed_contract.video_capability_contract,
    '10000000-0000-4000-9000-000000000001'::uuid,
    hashed_contract.content_hash,
    'published',
    now()
FROM hashed_contract;

-- +goose Down

SET search_path TO public;

DELETE FROM commerce_workflow_template_versions
WHERE id = md5('cineweave:commerce-workflow-template:commerce_video_v1:1')::uuid
  AND template_id = md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid
  AND version = 1;

DELETE FROM commerce_workflow_templates template
WHERE template.id = md5('cineweave:commerce-workflow-template:commerce_video_v1')::uuid
  AND template.organization_id IS NULL
  AND template.template_key = 'commerce_video_v1'
  AND NOT EXISTS (
      SELECT 1
      FROM commerce_workflow_template_versions version
      WHERE version.template_id = template.id
  );

CREATE TABLE IF NOT EXISTS provider_model_capability_presets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  preset_key TEXT UNIQUE NOT NULL,
  display_name TEXT NOT NULL,
  modality TEXT NOT NULL CHECK (modality IN ('text', 'image', 'video', 'audio', 'embedding', 'multimodal')),
  match_patterns JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(match_patterns) = 'array'),
  task_types JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(task_types) = 'array'),
  input_limits JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(input_limits) = 'object'),
  output_limits JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(output_limits) = 'object'),
  quality_tiers JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(quality_tiers) = 'array'),
  provider_options_schema JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(provider_options_schema) = 'object'),
  pricing_policy JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(pricing_policy) = 'object'),
  priority INTEGER NOT NULL DEFAULT 100,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS provider_model_capability_presets_set_updated_at ON provider_model_capability_presets;
CREATE TRIGGER provider_model_capability_presets_set_updated_at
BEFORE UPDATE ON provider_model_capability_presets
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS provider_model_capability_presets_enabled_priority_idx
  ON provider_model_capability_presets(enabled, priority, preset_key);

INSERT INTO provider_model_capability_presets(
  preset_key, display_name, modality, match_patterns, task_types,
  input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy, priority, enabled
) VALUES
(
  'gpt-4o',
  'GPT-4o',
  'multimodal',
  '["gpt-4o", "openai/gpt-4o", "azure/gpt-4o"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":false,"supportsReasoningLevels":false,"supportsMultimodalInput":true,"supportedInputTypes":["text","image"],"requestModes":["chat_completions","responses"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  10,
  true
),
(
  'gpt-4o-mini',
  'GPT-4o mini',
  'multimodal',
  '["gpt-4o-mini", "openai/gpt-4o-mini", "azure/gpt-4o-mini"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":false,"supportsReasoningLevels":false,"supportsMultimodalInput":true,"supportedInputTypes":["text","image"],"requestModes":["chat_completions","responses"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  10,
  true
),
(
  'gpt-4.1',
  'GPT-4.1',
  'multimodal',
  '["gpt-4.1", "gpt-4.1-*", "openai/gpt-4.1*"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":false,"supportsReasoningLevels":false,"supportsMultimodalInput":true,"supportedInputTypes":["text","image"],"requestModes":["chat_completions","responses"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  20,
  true
),
(
  'o3',
  'OpenAI o3',
  'multimodal',
  '["o3", "o3-*", "openai/o3*"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":true,"supportsReasoningLevels":true,"reasoningLevels":["low","medium","high"],"supportsMultimodalInput":true,"supportedInputTypes":["text","image"],"requestModes":["responses"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  20,
  true
),
(
  'o4-mini',
  'OpenAI o4 mini',
  'multimodal',
  '["o4-mini", "o4-mini-*", "openai/o4-mini*"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":true,"supportsReasoningLevels":true,"reasoningLevels":["low","medium","high"],"supportsMultimodalInput":true,"supportedInputTypes":["text","image"],"requestModes":["responses"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  20,
  true
),
(
  'gpt-image-1',
  'GPT Image 1',
  'image',
  '["gpt-image-1", "gpt-image-1*", "openai/gpt-image-1*"]'::jsonb,
  '["image.generate"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{"responseFormats":["url","b64_json"]}'::jsonb,
  '["standard","hd"]'::jsonb,
  '{"imageEndpointKey":"image_generate","xCapabilities":{"supportsAsyncTask":false,"supportsReferences":true,"referenceTypes":["image"],"requestModes":["images.generate","images.edit"],"responseFormats":["url","b64_json"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  10,
  true
),
(
  'dall-e-3',
  'DALL-E 3',
  'image',
  '["dall-e-3", "openai/dall-e-3"]'::jsonb,
  '["image.generate"]'::jsonb,
  '{"inputTypes":["text"]}'::jsonb,
  '{"responseFormats":["url","b64_json"]}'::jsonb,
  '["standard","hd"]'::jsonb,
  '{"imageEndpointKey":"image_generate","xCapabilities":{"supportsAsyncTask":false,"supportsReferences":false,"referenceTypes":[],"requestModes":["images.generate"],"responseFormats":["url","b64_json"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  20,
  true
),
(
  'deepseek-chat',
  'DeepSeek Chat',
  'text',
  '["deepseek-chat", "deepseek/deepseek-chat"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":false,"supportsReasoningLevels":false,"supportsMultimodalInput":false,"requestModes":["chat_completions"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  10,
  true
),
(
  'deepseek-reasoner',
  'DeepSeek Reasoner',
  'text',
  '["deepseek-reasoner", "deepseek/deepseek-reasoner"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":true,"supportsReasoningLevels":true,"reasoningLevels":["low","medium","high"],"supportsMultimodalInput":false,"requestModes":["chat_completions"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  10,
  true
),
(
  'doubao-seed-1.6',
  'Doubao Seed 1.6',
  'text',
  '["doubao-seed-1.6", "volcengine/doubao-seed-1.6"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":false,"supportsReasoningLevels":false,"supportsMultimodalInput":false,"requestModes":["chat_completions"]}}'::jsonb,
  '{"currency":"CNY"}'::jsonb,
  10,
  true
),
(
  'doubao-seed-1.6-thinking',
  'Doubao Seed 1.6 Thinking',
  'text',
  '["doubao-seed-1.6-thinking", "volcengine/doubao-seed-1.6-thinking"]'::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  '{"inputTypes":["text"]}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsReasoning":true,"supportsReasoningLevels":true,"reasoningLevels":["low","medium","high"],"supportsMultimodalInput":false,"requestModes":["chat_completions"]}}'::jsonb,
  '{"currency":"CNY"}'::jsonb,
  10,
  true
),
(
  'doubao-seedream-4',
  'Seedream 4.x',
  'image',
  '["doubao-seedream-4-*", "volcengine/doubao-seedream-4-*"]'::jsonb,
  '["image.generate"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{"responseFormats":["url","b64_json"]}'::jsonb,
  '["1K","2K","3K","4K"]'::jsonb,
  '{"imageEndpointKey":"image_generate","xCapabilities":{"supportsAsyncTask":false,"supportsReferences":true,"referenceTypes":["image"],"requestModes":["sync"],"supportedTiers":["1K","2K","3K","4K"],"responseFormats":["url","b64_json"]}}'::jsonb,
  '{"currency":"CNY"}'::jsonb,
  10,
  true
),
(
  'doubao-seedream-3',
  'Seedream 3.x',
  'image',
  '["doubao-seedream-3-*", "volcengine/doubao-seedream-3-*"]'::jsonb,
  '["image.generate"]'::jsonb,
  '{"inputTypes":["text"]}'::jsonb,
  '{"responseFormats":["url","b64_json"]}'::jsonb,
  '["1K","2K","4K"]'::jsonb,
  '{"imageEndpointKey":"image_generate","xCapabilities":{"supportsAsyncTask":false,"supportsReferences":false,"referenceTypes":[],"requestModes":["sync"],"supportedTiers":["1K","2K","4K"],"responseFormats":["url","b64_json"]}}'::jsonb,
  '{"currency":"CNY"}'::jsonb,
  20,
  true
),
(
  'doubao-seedance',
  'Seedance',
  'video',
  '["doubao-seedance-*", "volcengine/doubao-seedance-*"]'::jsonb,
  '["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{"outputTypes":["video","last_frame"]}'::jsonb,
  '["720p","1080p"]'::jsonb,
  '{"videoCreateEndpointKey":"video_create","videoPollEndpointKey":"video_poll","videoCancelEndpointKey":"video_cancel","xCapabilities":{"supportsAsyncTask":true,"supportsReferenceImages":true,"supportsFirstFrame":true,"supportsLastFrame":true,"supportsVideoReference":false,"requestModes":["async_create","poll","cancel"],"referenceTypes":["image","first_frame","last_frame"],"durations":[5,10],"resolutions":["720p","1080p"]}}'::jsonb,
  '{"currency":"CNY"}'::jsonb,
  10,
  true
),
(
  'kling-video',
  'Kling Video',
  'video',
  '["kling-*", "kuaishou/kling-*"]'::jsonb,
  '["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"]'::jsonb,
  '{"inputTypes":["text","image"]}'::jsonb,
  '{"outputTypes":["video"]}'::jsonb,
  '["standard","pro"]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":true,"supportsReferenceImages":true,"supportsFirstFrame":true,"supportsLastFrame":true,"supportsVideoReference":false,"requestModes":["async_create","poll","cancel"],"referenceTypes":["image","first_frame","last_frame"]}}'::jsonb,
  '{"currency":"CNY"}'::jsonb,
  30,
  true
),
(
  'veo-video',
  'Veo Video',
  'video',
  '["veo-*", "google/veo-*"]'::jsonb,
  '["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"]'::jsonb,
  '{"inputTypes":["text","image","video"]}'::jsonb,
  '{"outputTypes":["video"]}'::jsonb,
  '["720p","1080p"]'::jsonb,
  '{"xCapabilities":{"supportsAsyncTask":true,"supportsReferenceImages":true,"supportsFirstFrame":true,"supportsLastFrame":false,"supportsVideoReference":true,"requestModes":["async_create","poll","cancel"],"referenceTypes":["image","first_frame","video"]}}'::jsonb,
  '{"currency":"USD"}'::jsonb,
  30,
  true
)
ON CONFLICT (preset_key) DO UPDATE SET
  display_name = EXCLUDED.display_name,
  modality = EXCLUDED.modality,
  match_patterns = EXCLUDED.match_patterns,
  task_types = EXCLUDED.task_types,
  input_limits = EXCLUDED.input_limits,
  output_limits = EXCLUDED.output_limits,
  quality_tiers = EXCLUDED.quality_tiers,
  provider_options_schema = EXCLUDED.provider_options_schema,
  pricing_policy = EXCLUDED.pricing_policy,
  priority = EXCLUDED.priority,
  enabled = EXCLUDED.enabled,
  updated_at = now();

INSERT INTO schema_migrations(version) VALUES ('000026_model_capability_registry')
ON CONFLICT (version) DO NOTHING;

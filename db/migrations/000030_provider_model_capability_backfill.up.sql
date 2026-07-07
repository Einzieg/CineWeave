INSERT INTO provider_model_capabilities(
  provider_model_id, task_types, input_limits, output_limits,
  quality_tiers, provider_options_schema, pricing_policy
)
SELECT
  m.id,
  CASE m.modality
    WHEN 'image' THEN '["image.generate"]'::jsonb
    WHEN 'video' THEN '["video.text_to_video","video.image_to_video","video.create_task","video.poll_task","video.cancel_task"]'::jsonb
    WHEN 'embedding' THEN '["embedding.create"]'::jsonb
    WHEN 'multimodal' THEN '["text.generate","text.stream","image.generate","video.create_task","video.poll_task"]'::jsonb
    ELSE '["text.generate","text.stream"]'::jsonb
  END,
  '{}'::jsonb,
  '{}'::jsonb,
  '[]'::jsonb,
  CASE m.modality
    WHEN 'image' THEN '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":false,"supportsReferences":false,"requestModes":["images.generate"]}}'::jsonb
    WHEN 'video' THEN '{"xCapabilities":{"supportsAsyncTask":true,"supportsStreaming":false,"supportsReferenceImages":false,"supportsFirstFrame":false,"supportsLastFrame":false,"supportsVideoReference":false,"requestModes":["async_create","poll","cancel"]}}'::jsonb
    WHEN 'embedding' THEN '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":false,"requestModes":["embedding.create"]}}'::jsonb
    WHEN 'multimodal' THEN '{"xCapabilities":{"supportsAsyncTask":true,"supportsStreaming":true,"supportsMultimodalInput":true,"requestModes":["chat_completions","images.generate","async_create","poll"]}}'::jsonb
    ELSE '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsMultimodalInput":false,"requestModes":["chat_completions"]}}'::jsonb
  END,
  '{}'::jsonb
FROM provider_models m
WHERE m.status = 'active'
  AND NOT EXISTS (
    SELECT 1 FROM provider_model_capabilities c WHERE c.provider_model_id = m.id
  );

UPDATE provider_model_capabilities c
SET task_types = CASE m.modality
    WHEN 'image' THEN '["image.generate"]'::jsonb
    WHEN 'video' THEN '["video.text_to_video","video.image_to_video","video.create_task","video.poll_task","video.cancel_task"]'::jsonb
    WHEN 'embedding' THEN '["embedding.create"]'::jsonb
    WHEN 'multimodal' THEN '["text.generate","text.stream","image.generate","video.create_task","video.poll_task"]'::jsonb
    ELSE '["text.generate","text.stream"]'::jsonb
  END,
  provider_options_schema = CASE
    WHEN c.provider_options_schema IS NULL OR c.provider_options_schema = '{}'::jsonb THEN
      CASE m.modality
        WHEN 'image' THEN '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":false,"supportsReferences":false,"requestModes":["images.generate"]}}'::jsonb
        WHEN 'video' THEN '{"xCapabilities":{"supportsAsyncTask":true,"supportsStreaming":false,"supportsReferenceImages":false,"supportsFirstFrame":false,"supportsLastFrame":false,"supportsVideoReference":false,"requestModes":["async_create","poll","cancel"]}}'::jsonb
        WHEN 'embedding' THEN '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":false,"requestModes":["embedding.create"]}}'::jsonb
        WHEN 'multimodal' THEN '{"xCapabilities":{"supportsAsyncTask":true,"supportsStreaming":true,"supportsMultimodalInput":true,"requestModes":["chat_completions","images.generate","async_create","poll"]}}'::jsonb
        ELSE '{"xCapabilities":{"supportsAsyncTask":false,"supportsStreaming":true,"supportsMultimodalInput":false,"requestModes":["chat_completions"]}}'::jsonb
      END
    ELSE c.provider_options_schema
  END
FROM provider_models m
WHERE c.provider_model_id = m.id
  AND m.status = 'active'
  AND (
    c.task_types IS NULL
    OR c.task_types = '[]'::jsonb
    OR c.task_types = '{}'::jsonb
  );

INSERT INTO schema_migrations(version)
VALUES ('000030_provider_model_capability_backfill')
ON CONFLICT (version) DO NOTHING;

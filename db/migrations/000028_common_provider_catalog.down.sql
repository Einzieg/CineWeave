DELETE FROM provider_catalog_entries
WHERE provider_key IN (
  'openrouter',
  'ollama',
  'google_gemini',
  'alibaba_dashscope',
  'zhipu_glm',
  'baidu_qianfan',
  'xunfei_spark',
  'minimax'
);

UPDATE provider_catalog_entries
SET
  display_name = '自定义 OpenAI 兼容',
  description = '接入兼容 OpenAI Chat Completions / Images 的自定义服务。',
  default_base_url = 'https://api.openai.com/v1'
WHERE provider_key = 'openai_compatible_custom';

DELETE FROM schema_migrations
WHERE version = '000028_common_provider_catalog';

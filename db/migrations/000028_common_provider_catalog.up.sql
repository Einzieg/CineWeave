UPDATE provider_catalog_entries
SET
  display_name = 'OpenAI 兼容',
  description = '默认渠道。适用于 OpenAI official、New API、One API、LiteLLM 以及其他兼容 /v1 协议的网关。',
  category = 'multimodal',
  logo_key = 'openai',
  default_base_url = 'https://api.openai.com/v1',
  default_auth_type = 'bearer',
  is_official = true,
  enabled = true
WHERE provider_key = 'openai_compatible_custom';

INSERT INTO provider_catalog_entries(
  provider_key, name, display_name, description, provider_type, category,
  logo_key, docs_url, default_base_url, default_auth_type,
  connector_manifest, model_templates, supported_task_types, setup_schema,
  enabled, is_official
) VALUES
(
  'openrouter',
  'openrouter',
  'OpenRouter',
  'OpenRouter OpenAI-compatible gateway. 模型能力会优先匹配内置 OpenRouter 常用模型预设。',
  'openai_compatible',
  'multimodal',
  'openrouter',
  'https://openrouter.ai/docs',
  'https://openrouter.ai/api/v1',
  'bearer',
  '{}'::jsonb,
  $json$[
    {"modelKey":"deepseek/deepseek-v4-flash","displayName":"DeepSeek V4 Flash","modality":"text","taskTypes":["text.generate","text.stream"],"pricingPolicy":{"currency":"USD"}},
    {"modelKey":"google/gemini-2.5-flash-image","displayName":"Gemini 2.5 Flash Image","modality":"image","taskTypes":["image.generate"],"pricingPolicy":{"currency":"USD"}},
    {"modelKey":"google/veo-3.1-fast","displayName":"Veo 3.1 Fast","modality":"video","taskTypes":["video.create_task","video.poll_task","video.cancel_task"],"pricingPolicy":{"currency":"USD"}}
  ]$json$::jsonb,
  '["text.generate","text.stream","image.generate","video.create_task","video.poll_task","video.cancel_task"]'::jsonb,
  $json${
    "defaultConfig":{"runtime":"openai_compatible","modelsEndpoint":"/models","chatCompletionsEndpoint":"/chat/completions","imagesGenerationsEndpoint":"/images/generations"},
    "fields":[
      {"key":"modelsEndpoint","label":"模型列表路径","type":"text","required":false,"defaultValue":"/models"},
      {"key":"chatCompletionsEndpoint","label":"文本生成路径","type":"text","required":false,"defaultValue":"/chat/completions"},
      {"key":"imagesGenerationsEndpoint","label":"图片生成路径","type":"text","required":false,"defaultValue":"/images/generations"}
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'ollama',
  'ollama',
  'Ollama',
  'Ollama OpenAI-compatible local or LAN model service.',
  'openai_compatible',
  'multimodal',
  'ollama',
  'https://github.com/ollama/ollama/blob/main/docs/openai.md',
  'http://host.docker.internal:11434/v1',
  'none',
  '{}'::jsonb,
  $json$[
    {"modelKey":"llama3.1","displayName":"Llama 3.1","modality":"text","taskTypes":["text.generate","text.stream"],"pricingPolicy":{"currency":"USD"}},
    {"modelKey":"qwen2.5vl","displayName":"Qwen2.5 VL","modality":"multimodal","taskTypes":["text.generate","text.stream"],"inputLimits":{"inputTypes":["text","image"]},"pricingPolicy":{"currency":"USD"}}
  ]$json$::jsonb,
  '["text.generate","text.stream"]'::jsonb,
  $json${
    "defaultConfig":{"runtime":"openai_compatible","modelsEndpoint":"/models","chatCompletionsEndpoint":"/chat/completions"},
    "fields":[
      {"key":"modelsEndpoint","label":"模型列表路径","type":"text","required":false,"defaultValue":"/models"},
      {"key":"chatCompletionsEndpoint","label":"文本生成路径","type":"text","required":false,"defaultValue":"/chat/completions"}
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'google_gemini',
  'google_gemini',
  'Google Gemini',
  'Google Gemini OpenAI-compatible endpoint.',
  'openai_compatible',
  'multimodal',
  'google',
  'https://ai.google.dev/gemini-api/docs/openai',
  'https://generativelanguage.googleapis.com/v1beta/openai',
  'bearer',
  '{}'::jsonb,
  $json$[
    {"modelKey":"gemini-2.5-flash","displayName":"Gemini 2.5 Flash","modality":"multimodal","taskTypes":["text.generate","text.stream"],"inputLimits":{"inputTypes":["text","image"]},"pricingPolicy":{"currency":"USD"}},
    {"modelKey":"gemini-2.5-pro","displayName":"Gemini 2.5 Pro","modality":"multimodal","taskTypes":["text.generate","text.stream"],"inputLimits":{"inputTypes":["text","image"]},"pricingPolicy":{"currency":"USD"}}
  ]$json$::jsonb,
  '["text.generate","text.stream"]'::jsonb,
  $json${
    "defaultConfig":{"runtime":"openai_compatible","modelsEndpoint":"/models","chatCompletionsEndpoint":"/chat/completions"},
    "fields":[
      {"key":"modelsEndpoint","label":"模型列表路径","type":"text","required":false,"defaultValue":"/models"},
      {"key":"chatCompletionsEndpoint","label":"文本生成路径","type":"text","required":false,"defaultValue":"/chat/completions"}
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'alibaba_dashscope',
  'alibaba_dashscope',
  '阿里通义千问',
  'DashScope OpenAI-compatible mode for Qwen models.',
  'openai_compatible',
  'multimodal',
  'alibaba',
  'https://help.aliyun.com/zh/model-studio/openai-compatible',
  'https://dashscope.aliyuncs.com/compatible-mode/v1',
  'bearer',
  '{}'::jsonb,
  $json$[
    {"modelKey":"qwen-plus","displayName":"Qwen Plus","modality":"text","taskTypes":["text.generate","text.stream"],"pricingPolicy":{"currency":"CNY"}},
    {"modelKey":"qwen-vl-plus","displayName":"Qwen VL Plus","modality":"multimodal","taskTypes":["text.generate","text.stream"],"inputLimits":{"inputTypes":["text","image"]},"pricingPolicy":{"currency":"CNY"}}
  ]$json$::jsonb,
  '["text.generate","text.stream"]'::jsonb,
  $json${
    "defaultConfig":{"runtime":"openai_compatible","modelsEndpoint":"/models","chatCompletionsEndpoint":"/chat/completions"},
    "fields":[
      {"key":"modelsEndpoint","label":"模型列表路径","type":"text","required":false,"defaultValue":"/models"},
      {"key":"chatCompletionsEndpoint","label":"文本生成路径","type":"text","required":false,"defaultValue":"/chat/completions"}
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'zhipu_glm',
  'zhipu_glm',
  '智谱 GLM',
  '智谱 BigModel OpenAI-compatible endpoint.',
  'openai_compatible',
  'multimodal',
  'zhipu',
  'https://docs.bigmodel.cn/',
  'https://open.bigmodel.cn/api/paas/v4',
  'bearer',
  '{}'::jsonb,
  $json$[
    {"modelKey":"glm-4.5","displayName":"GLM-4.5","modality":"text","taskTypes":["text.generate","text.stream"],"pricingPolicy":{"currency":"CNY"}},
    {"modelKey":"glm-4.5v","displayName":"GLM-4.5V","modality":"multimodal","taskTypes":["text.generate","text.stream"],"inputLimits":{"inputTypes":["text","image"]},"pricingPolicy":{"currency":"CNY"}}
  ]$json$::jsonb,
  '["text.generate","text.stream"]'::jsonb,
  $json${
    "defaultConfig":{"runtime":"openai_compatible","modelsEndpoint":"/models","chatCompletionsEndpoint":"/chat/completions"},
    "fields":[
      {"key":"modelsEndpoint","label":"模型列表路径","type":"text","required":false,"defaultValue":"/models"},
      {"key":"chatCompletionsEndpoint","label":"文本生成路径","type":"text","required":false,"defaultValue":"/chat/completions"}
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'baidu_qianfan',
  'baidu_qianfan',
  '百度文心千帆',
  '百度智能云千帆渠道。按 OpenAI-compatible 方式配置可用端点。',
  'openai_compatible',
  'multimodal',
  'baidu',
  'https://cloud.baidu.com/doc/WENXINWORKSHOP/index.html',
  NULL,
  'bearer',
  '{}'::jsonb,
  $json$[
    {"modelKey":"ernie-4.5-turbo","displayName":"ERNIE 4.5 Turbo","modality":"text","taskTypes":["text.generate","text.stream"],"pricingPolicy":{"currency":"CNY"}},
    {"modelKey":"ernie-x1-turbo","displayName":"ERNIE X1 Turbo","modality":"text","taskTypes":["text.generate","text.stream"],"pricingPolicy":{"currency":"CNY"}}
  ]$json$::jsonb,
  '["text.generate","text.stream"]'::jsonb,
  $json${
    "defaultConfig":{"runtime":"openai_compatible","modelsEndpoint":"/models","chatCompletionsEndpoint":"/chat/completions"},
    "fields":[
      {"key":"modelsEndpoint","label":"模型列表路径","type":"text","required":false,"defaultValue":"/models"},
      {"key":"chatCompletionsEndpoint","label":"文本生成路径","type":"text","required":false,"defaultValue":"/chat/completions"}
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'xunfei_spark',
  'xunfei_spark',
  '讯飞星火',
  '讯飞星火渠道。按 OpenAI-compatible 方式配置可用端点。',
  'openai_compatible',
  'multimodal',
  'xunfei',
  'https://www.xfyun.cn/doc/spark/',
  NULL,
  'bearer',
  '{}'::jsonb,
  $json$[
    {"modelKey":"spark-x1","displayName":"Spark X1","modality":"text","taskTypes":["text.generate","text.stream"],"pricingPolicy":{"currency":"CNY"}},
    {"modelKey":"spark-max","displayName":"Spark Max","modality":"text","taskTypes":["text.generate","text.stream"],"pricingPolicy":{"currency":"CNY"}}
  ]$json$::jsonb,
  '["text.generate","text.stream"]'::jsonb,
  $json${
    "defaultConfig":{"runtime":"openai_compatible","modelsEndpoint":"/models","chatCompletionsEndpoint":"/chat/completions"},
    "fields":[
      {"key":"modelsEndpoint","label":"模型列表路径","type":"text","required":false,"defaultValue":"/models"},
      {"key":"chatCompletionsEndpoint","label":"文本生成路径","type":"text","required":false,"defaultValue":"/chat/completions"}
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'minimax',
  'minimax',
  'MiniMax',
  'MiniMax 渠道。文本模型可通过兼容端点接入，图片/视频模型可先用自定义模型能力配置。',
  'openai_compatible',
  'multimodal',
  'minimax',
  'https://platform.minimaxi.com/document',
  NULL,
  'bearer',
  '{}'::jsonb,
  $json$[
    {"modelKey":"minimax/minimax-m3","displayName":"MiniMax M3","modality":"multimodal","taskTypes":["text.generate","text.stream"],"inputLimits":{"inputTypes":["text","image","video"]},"pricingPolicy":{"currency":"USD"}},
    {"modelKey":"minimax-video-01","displayName":"MiniMax Video 01","modality":"video","taskTypes":["video.create_task","video.poll_task","video.cancel_task"],"pricingPolicy":{"currency":"CNY"}}
  ]$json$::jsonb,
  '["text.generate","text.stream","video.create_task","video.poll_task","video.cancel_task"]'::jsonb,
  $json${
    "defaultConfig":{"runtime":"openai_compatible","modelsEndpoint":"/models","chatCompletionsEndpoint":"/chat/completions"},
    "fields":[
      {"key":"modelsEndpoint","label":"模型列表路径","type":"text","required":false,"defaultValue":"/models"},
      {"key":"chatCompletionsEndpoint","label":"文本生成路径","type":"text","required":false,"defaultValue":"/chat/completions"}
    ]
  }$json$::jsonb,
  true,
  true
)
ON CONFLICT (provider_key) DO UPDATE SET
  name = EXCLUDED.name,
  display_name = EXCLUDED.display_name,
  description = EXCLUDED.description,
  provider_type = EXCLUDED.provider_type,
  category = EXCLUDED.category,
  logo_key = EXCLUDED.logo_key,
  docs_url = EXCLUDED.docs_url,
  default_base_url = EXCLUDED.default_base_url,
  default_auth_type = EXCLUDED.default_auth_type,
  connector_manifest = EXCLUDED.connector_manifest,
  model_templates = EXCLUDED.model_templates,
  supported_task_types = EXCLUDED.supported_task_types,
  setup_schema = EXCLUDED.setup_schema,
  enabled = EXCLUDED.enabled,
  is_official = EXCLUDED.is_official;

INSERT INTO schema_migrations(version) VALUES ('000028_common_provider_catalog')
ON CONFLICT (version) DO NOTHING;

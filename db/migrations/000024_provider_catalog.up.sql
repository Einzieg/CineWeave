CREATE TABLE IF NOT EXISTS provider_catalog_entries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_key TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  display_name TEXT NOT NULL,
  description TEXT,
  provider_type TEXT NOT NULL CHECK (provider_type IN ('openai_compatible', 'declarative_manifest', 'native')),
  category TEXT NOT NULL CHECK (category IN ('text', 'image', 'video', 'multimodal')),
  logo_key TEXT,
  docs_url TEXT,
  default_base_url TEXT,
  default_auth_type TEXT NOT NULL DEFAULT 'bearer' CHECK (default_auth_type IN ('none', 'bearer', 'api_key', 'basic')),
  connector_manifest JSONB NOT NULL DEFAULT '{}',
  model_templates JSONB NOT NULL DEFAULT '[]',
  supported_task_types JSONB NOT NULL DEFAULT '[]',
  setup_schema JSONB NOT NULL DEFAULT '{}',
  enabled BOOLEAN NOT NULL DEFAULT true,
  is_official BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS provider_catalog_entries_set_updated_at ON provider_catalog_entries;
CREATE TRIGGER provider_catalog_entries_set_updated_at
BEFORE UPDATE ON provider_catalog_entries
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS provider_catalog_entries_enabled_category_idx
  ON provider_catalog_entries(enabled, category, display_name);

INSERT INTO provider_catalog_entries(
  provider_key, name, display_name, description, provider_type, category,
  logo_key, docs_url, default_base_url, default_auth_type,
  connector_manifest, model_templates, supported_task_types, setup_schema,
  enabled, is_official
) VALUES
(
  'deepseek',
  'deepseek',
  'DeepSeek',
  'DeepSeek 文本模型，兼容 OpenAI Chat Completions 格式。',
  'openai_compatible',
  'text',
  'deepseek',
  'https://api-docs.deepseek.com/',
  'https://api.deepseek.com',
  'bearer',
  '{}'::jsonb,
  $json$[
    {
      "modelKey": "deepseek-chat",
      "displayName": "DeepSeek Chat",
      "modality": "text",
      "taskTypes": ["text.generate", "text.stream"],
      "executionMode": "sync",
      "supportsJsonOutput": true,
      "supportsToolCalls": true,
      "supportsReasoning": false,
      "providerOptionsSchema": {
        "type": "object",
        "properties": {
          "deepseek": {
            "type": "object",
            "properties": {
              "thinking": { "type": "object" },
              "reasoning_effort": { "type": "string" }
            }
          },
          "extraBody": { "type": "object" }
        },
        "xCapabilities": {
          "executionMode": "sync",
          "supportsJsonOutput": true,
          "supportsToolCalls": true,
          "supportsReasoning": true
        }
      },
      "pricingPolicy": { "currency": "USD" }
    },
    {
      "modelKey": "deepseek-reasoner",
      "displayName": "DeepSeek Reasoner",
      "modality": "text",
      "taskTypes": ["text.generate", "text.stream"],
      "executionMode": "sync",
      "supportsJsonOutput": true,
      "supportsToolCalls": true,
      "supportsReasoning": true,
      "providerOptionsSchema": {
        "type": "object",
        "properties": {
          "deepseek": {
            "type": "object",
            "properties": {
              "thinking": { "type": "object" },
              "reasoning_effort": { "type": "string" }
            }
          },
          "extraBody": { "type": "object" }
        },
        "xCapabilities": {
          "executionMode": "sync",
          "supportsJsonOutput": true,
          "supportsToolCalls": true,
          "supportsReasoning": true
        }
      },
      "pricingPolicy": { "currency": "USD" }
    },
    {
      "modelKey": "deepseek-v4-flash",
      "displayName": "DeepSeek V4 Flash",
      "modality": "text",
      "taskTypes": ["text.generate", "text.stream"],
      "executionMode": "sync",
      "supportsJsonOutput": true,
      "supportsToolCalls": true,
      "supportsReasoning": true,
      "providerOptionsSchema": { "type": "object", "properties": { "deepseek": { "type": "object" }, "extraBody": { "type": "object" } } },
      "pricingPolicy": { "currency": "USD" }
    },
    {
      "modelKey": "deepseek-v4-pro",
      "displayName": "DeepSeek V4 Pro",
      "modality": "text",
      "taskTypes": ["text.generate", "text.stream"],
      "executionMode": "sync",
      "supportsJsonOutput": true,
      "supportsToolCalls": true,
      "supportsReasoning": true,
      "providerOptionsSchema": { "type": "object", "properties": { "deepseek": { "type": "object" }, "extraBody": { "type": "object" } } },
      "pricingPolicy": { "currency": "USD" }
    }
  ]$json$::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  $json${
    "defaultConfig": {
      "runtime": "openai_compatible",
      "disableV1Prefix": true,
      "modelsEndpoint": "/models",
      "chatCompletionsEndpoint": "/chat/completions"
    },
    "fields": [
      {
        "key": "modelsEndpoint",
        "label": "模型列表路径",
        "type": "text",
        "required": false,
        "defaultValue": "/models"
      },
      {
        "key": "chatCompletionsEndpoint",
        "label": "文本生成路径",
        "type": "text",
        "required": false,
        "defaultValue": "/chat/completions"
      }
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'volcengine_ark_text',
  'volcengine_ark_text',
  '火山方舟文本',
  '火山方舟文本模型。请填写控制台提供的推理接入地址和模型 ID。',
  'openai_compatible',
  'text',
  'volcengine',
  'https://www.volcengine.com/docs',
  NULL,
  'bearer',
  '{}'::jsonb,
  $json$[
    {
      "modelKey": "doubao-seed-1.6",
      "displayName": "Doubao Seed 1.6",
      "modality": "text",
      "taskTypes": ["text.generate", "text.stream"],
      "executionMode": "sync",
      "supportsJsonOutput": true,
      "supportsToolCalls": true,
      "supportsReasoning": false,
      "providerOptionsSchema": { "type": "object", "properties": { "extraBody": { "type": "object" } } },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seed-1.6-thinking",
      "displayName": "Doubao Seed 1.6 Thinking",
      "modality": "text",
      "taskTypes": ["text.generate", "text.stream"],
      "executionMode": "sync",
      "supportsJsonOutput": true,
      "supportsToolCalls": true,
      "supportsReasoning": true,
      "providerOptionsSchema": { "type": "object", "properties": { "extraBody": { "type": "object" } } },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "custom-model-id",
      "displayName": "自定义模型 ID",
      "modality": "text",
      "taskTypes": ["text.generate", "text.stream"],
      "executionMode": "sync",
      "providerOptionsSchema": { "type": "object", "properties": { "extraBody": { "type": "object" } } },
      "pricingPolicy": { "currency": "CNY" }
    }
  ]$json$::jsonb,
  '["text.generate", "text.stream"]'::jsonb,
  $json${
    "defaultConfig": {
      "runtime": "openai_compatible",
      "modelsEndpoint": "/models",
      "chatCompletionsEndpoint": "/chat/completions"
    },
    "fields": [
      {
        "key": "modelsEndpoint",
        "label": "模型列表路径",
        "type": "text",
        "required": false,
        "defaultValue": "/models"
      },
      {
        "key": "chatCompletionsEndpoint",
        "label": "文本生成路径",
        "type": "text",
        "required": false,
        "defaultValue": "/chat/completions"
      }
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'volcengine_seedream_image',
  'volcengine_seedream_image',
  '火山方舟图片',
  '火山方舟图片模型预设。接口路径和模型 ID 均可按控制台配置修改。',
  'declarative_manifest',
  'image',
  'volcengine',
  'https://www.volcengine.com/docs',
  NULL,
  'bearer',
  $json${
    "kind": "ProviderConnector",
    "version": "v1",
    "id": "volcengine-seedream-image",
    "name": "火山方舟图片",
    "transport": "http",
    "baseUrl": "https://example.invalid",
    "auth": { "type": "bearer" },
    "models": [
      {
        "id": "seedream-configured-model",
        "displayName": "Seedream 可配置模型",
        "modality": "image",
        "capabilities": {
          "taskTypes": ["image.generate"],
          "executionMode": "sync"
        }
      }
    ],
    "endpoints": {
      "image_generate": {
        "endpointType": "sync",
        "method": "POST",
        "pathTemplate": "{{ account.config.imageGenerationPath }}",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {
          "model": "{{ model.id }}",
          "prompt": "{{ input.prompt }}",
          "size": "{{ input.size }}",
          "aspect_ratio": "{{ input.aspectRatio }}",
          "quality": "{{ input.quality }}",
          "response_format": "{{ input.responseFormat }}"
        },
        "responseMapping": {
          "imageUrl": "$.data[0].url",
          "b64Json": "$.data[0].b64_json",
          "errorMessage": "$.error.message"
        },
        "timeoutMs": 120000
      }
    }
  }$json$::jsonb,
  $json$[
    {
      "modelKey": "seedream-configured-model",
      "displayName": "Seedream 可配置模型",
      "modality": "image",
      "taskTypes": ["image.generate"],
      "executionMode": "sync",
      "providerOptionsSchema": { "type": "object", "properties": { "imageEndpointKey": { "type": "string" } } },
      "pricingPolicy": { "currency": "CNY" }
    }
  ]$json$::jsonb,
  '["image.generate"]'::jsonb,
  $json${
    "defaultConfig": {
      "runtime": "declarative_manifest",
      "imageEndpointKey": "image_generate"
    },
    "fields": [
      {
        "key": "imageGenerationPath",
        "label": "图片生成路径",
        "type": "text",
        "required": true,
        "defaultValue": "/your/image/generation/path"
      }
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'volcengine_seedance_video',
  'volcengine_seedance_video',
  '火山方舟视频',
  '火山方舟视频模型预设。创建、轮询和取消路径均可配置。',
  'declarative_manifest',
  'video',
  'volcengine',
  'https://www.volcengine.com/docs',
  NULL,
  'bearer',
  $json${
    "kind": "ProviderConnector",
    "version": "v1",
    "id": "volcengine-seedance-video",
    "name": "火山方舟视频",
    "transport": "http",
    "baseUrl": "https://example.invalid",
    "auth": { "type": "bearer" },
    "models": [
      {
        "id": "seedance-configured-model",
        "displayName": "Seedance 可配置模型",
        "modality": "video",
        "capabilities": {
          "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
          "executionMode": "async"
        }
      }
    ],
    "endpoints": {
      "video_create": {
        "endpointType": "async_create",
        "method": "POST",
        "pathTemplate": "{{ account.config.createTaskPath }}",
        "pollEndpointKey": "video_poll",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {
          "model": "{{ model.id }}",
          "prompt": "{{ input.prompt }}",
          "duration": "{{ input.duration }}",
          "aspect_ratio": "{{ input.aspectRatio }}",
          "resolution": "{{ input.resolution }}",
          "negative_prompt": "{{ input.negativePrompt }}",
          "image_url": "{{ references.0.url }}"
        },
        "responseMapping": {
          "externalTaskId": "$.id",
          "status": "$.status",
          "videoUrl": "$.video.url",
          "errorMessage": "$.error.message",
          "progress": "$.progress"
        },
        "timeoutMs": 120000
      },
      "video_poll": {
        "endpointType": "async_poll",
        "method": "POST",
        "pathTemplate": "{{ account.config.pollTaskPath }}",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {
          "task_id": "{{ task.externalTaskId }}"
        },
        "responseMapping": {
          "externalTaskId": "$.id",
          "status": "$.status",
          "videoUrl": "$.video.url",
          "errorMessage": "$.error.message",
          "progress": "$.progress"
        },
        "timeoutMs": 120000
      },
      "video_cancel": {
        "endpointType": "sync",
        "method": "POST",
        "pathTemplate": "{{ account.config.cancelTaskPath }}",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {
          "task_id": "{{ task.externalTaskId }}"
        },
        "responseMapping": {
          "externalTaskId": "$.id",
          "status": "$.status",
          "errorMessage": "$.error.message"
        },
        "timeoutMs": 60000
      }
    }
  }$json$::jsonb,
  $json$[
    {
      "modelKey": "seedance-configured-model",
      "displayName": "Seedance 可配置模型",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "type": "object",
        "properties": {
          "videoCreateEndpointKey": { "type": "string" },
          "videoPollEndpointKey": { "type": "string" },
          "videoCancelEndpointKey": { "type": "string" }
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    }
  ]$json$::jsonb,
  '["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"]'::jsonb,
  $json${
    "defaultConfig": {
      "runtime": "declarative_manifest",
      "videoCreateEndpointKey": "video_create",
      "videoPollEndpointKey": "video_poll",
      "videoCancelEndpointKey": "video_cancel"
    },
    "fields": [
      {
        "key": "createTaskPath",
        "label": "创建任务路径",
        "type": "text",
        "required": true,
        "defaultValue": "/your/video/create/path"
      },
      {
        "key": "pollTaskPath",
        "label": "轮询任务路径",
        "type": "text",
        "required": true,
        "defaultValue": "/your/video/poll/path"
      },
      {
        "key": "cancelTaskPath",
        "label": "取消任务路径",
        "type": "text",
        "required": false,
        "defaultValue": "/your/video/cancel/path"
      }
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'kling_video',
  'kling_video',
  '可灵视频',
  '可灵视频模型预设。请填写开放平台接口地址、模型 ID 和密钥。',
  'declarative_manifest',
  'video',
  'kling',
  'https://app.klingai.com/',
  NULL,
  'bearer',
  $json${
    "kind": "ProviderConnector",
    "version": "v1",
    "id": "kling-video",
    "name": "可灵视频",
    "transport": "http",
    "baseUrl": "https://example.invalid",
    "auth": { "type": "bearer" },
    "models": [
      {
        "id": "kling-configured-model",
        "displayName": "可灵可配置模型",
        "modality": "video",
        "capabilities": {
          "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
          "executionMode": "async"
        }
      }
    ],
    "endpoints": {
      "video_create": {
        "endpointType": "async_create",
        "method": "POST",
        "pathTemplate": "{{ account.config.createTaskPath }}",
        "pollEndpointKey": "video_poll",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {
          "model": "{{ model.id }}",
          "prompt": "{{ input.prompt }}",
          "duration": "{{ input.duration }}",
          "aspect_ratio": "{{ input.aspectRatio }}",
          "resolution": "{{ input.resolution }}",
          "negative_prompt": "{{ input.negativePrompt }}",
          "image_url": "{{ references.0.url }}"
        },
        "responseMapping": {
          "externalTaskId": "$.id",
          "status": "$.status",
          "videoUrl": "$.video.url",
          "errorMessage": "$.error.message",
          "progress": "$.progress"
        },
        "timeoutMs": 120000
      },
      "video_poll": {
        "endpointType": "async_poll",
        "method": "POST",
        "pathTemplate": "{{ account.config.pollTaskPath }}",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {
          "task_id": "{{ task.externalTaskId }}"
        },
        "responseMapping": {
          "externalTaskId": "$.id",
          "status": "$.status",
          "videoUrl": "$.video.url",
          "errorMessage": "$.error.message",
          "progress": "$.progress"
        },
        "timeoutMs": 120000
      },
      "video_cancel": {
        "endpointType": "sync",
        "method": "POST",
        "pathTemplate": "{{ account.config.cancelTaskPath }}",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {
          "task_id": "{{ task.externalTaskId }}"
        },
        "responseMapping": {
          "externalTaskId": "$.id",
          "status": "$.status",
          "errorMessage": "$.error.message"
        },
        "timeoutMs": 60000
      }
    }
  }$json$::jsonb,
  $json$[
    {
      "modelKey": "kling-configured-model",
      "displayName": "可灵可配置模型",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "type": "object",
        "properties": {
          "videoCreateEndpointKey": { "type": "string" },
          "videoPollEndpointKey": { "type": "string" },
          "videoCancelEndpointKey": { "type": "string" }
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    }
  ]$json$::jsonb,
  '["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"]'::jsonb,
  $json${
    "defaultConfig": {
      "runtime": "declarative_manifest",
      "videoCreateEndpointKey": "video_create",
      "videoPollEndpointKey": "video_poll",
      "videoCancelEndpointKey": "video_cancel"
    },
    "fields": [
      {
        "key": "createTaskPath",
        "label": "创建任务路径",
        "type": "text",
        "required": true,
        "defaultValue": "/your/video/create/path"
      },
      {
        "key": "pollTaskPath",
        "label": "轮询任务路径",
        "type": "text",
        "required": true,
        "defaultValue": "/your/video/poll/path"
      },
      {
        "key": "cancelTaskPath",
        "label": "取消任务路径",
        "type": "text",
        "required": false,
        "defaultValue": "/your/video/cancel/path"
      }
    ]
  }$json$::jsonb,
  true,
  true
),
(
  'openai_compatible_custom',
  'openai_compatible_custom',
  '自定义 OpenAI 兼容',
  '接入兼容 OpenAI Chat Completions / Images 的自定义服务。',
  'openai_compatible',
  'multimodal',
  'openai',
  NULL,
  'https://api.openai.com/v1',
  'bearer',
  '{}'::jsonb,
  $json$[
    {
      "modelKey": "custom-model-id",
      "displayName": "自定义模型 ID",
      "modality": "text",
      "taskTypes": ["text.generate", "text.stream"],
      "executionMode": "sync",
      "providerOptionsSchema": { "type": "object", "properties": { "extraBody": { "type": "object" } } },
      "pricingPolicy": { "currency": "USD" }
    }
  ]$json$::jsonb,
  '["text.generate", "text.stream", "image.generate"]'::jsonb,
  $json${
    "defaultConfig": {
      "runtime": "openai_compatible",
      "modelsEndpoint": "/models",
      "chatCompletionsEndpoint": "/chat/completions",
      "imagesGenerationsEndpoint": "/images/generations"
    },
    "fields": [
      {
        "key": "modelsEndpoint",
        "label": "模型列表路径",
        "type": "text",
        "required": false,
        "defaultValue": "/models"
      },
      {
        "key": "chatCompletionsEndpoint",
        "label": "文本生成路径",
        "type": "text",
        "required": false,
        "defaultValue": "/chat/completions"
      },
      {
        "key": "imagesGenerationsEndpoint",
        "label": "图片生成路径",
        "type": "text",
        "required": false,
        "defaultValue": "/images/generations"
      }
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

INSERT INTO schema_migrations(version) VALUES ('000024_provider_catalog')
ON CONFLICT (version) DO NOTHING;

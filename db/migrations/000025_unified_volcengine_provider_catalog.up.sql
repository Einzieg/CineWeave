UPDATE provider_catalog_entries
SET enabled = false
WHERE provider_key IN (
  'volcengine_ark_text',
  'volcengine_seedream_image',
  'volcengine_seedance_video'
);

INSERT INTO provider_catalog_entries(
  provider_key, name, display_name, description, provider_type, category,
  logo_key, docs_url, default_base_url, default_auth_type,
  connector_manifest, model_templates, supported_task_types, setup_schema,
  enabled, is_official
) VALUES (
  'volcengine_ark',
  'volcengine_ark',
  '火山方舟',
  '火山方舟统一渠道。一个账号同时管理豆包文本、Seedream 图片与 Seedance 视频模型。',
  'declarative_manifest',
  'multimodal',
  'volcengine',
  'https://www.volcengine.com/docs',
  'https://ark.cn-beijing.volces.com',
  'bearer',
  $json${
    "kind": "ProviderConnector",
    "version": "v1",
    "id": "volcengine-ark",
    "name": "火山方舟",
    "transport": "http",
    "baseUrl": "https://ark.cn-beijing.volces.com",
    "auth": { "type": "bearer" },
    "models": [
      {
        "id": "doubao-seed-1.6",
        "displayName": "Doubao Seed 1.6",
        "modality": "text",
        "capabilities": {
          "taskTypes": ["text.generate", "text.stream"],
          "executionMode": "sync"
        }
      },
      {
        "id": "doubao-seedream-4-0-250828",
        "displayName": "Seedream 4.0",
        "modality": "image",
        "capabilities": {
          "taskTypes": ["image.generate"],
          "executionMode": "sync"
        }
      },
      {
        "id": "doubao-seedance-2-0-260128",
        "displayName": "Seedance 2.0",
        "modality": "video",
        "capabilities": {
          "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
          "executionMode": "async"
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
          "watermark": false,
          "response_format": "{{ input.responseFormat }}"
        },
        "responseMapping": {
          "imageUrl": "$.data[0].url",
          "b64Json": "$.data[0].b64_json",
          "errorMessage": "$.error.message"
        },
        "timeoutMs": 120000
      },
      "video_create": {
        "endpointType": "async_create",
        "method": "POST",
        "pathTemplate": "{{ account.config.videoCreateTaskPath }}",
        "pollEndpointKey": "video_poll",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {
          "model": "{{ model.id }}",
          "content": [
            { "type": "text", "text": "{{ input.prompt }}" }
          ],
          "ratio": "{{ input.aspectRatio }}",
          "resolution": "{{ input.resolution }}",
          "duration": "{{ input.duration }}",
          "watermark": false,
          "generate_audio": false,
          "camera_fixed": false,
          "return_last_frame": true
        },
        "responseMapping": {
          "externalTaskId": "$.id",
          "status": "$.status",
          "videoUrl": "$.content.video_url",
          "errorMessage": "$.error.message",
          "progress": "$.progress"
        },
        "timeoutMs": 120000
      },
      "video_poll": {
        "endpointType": "async_poll",
        "method": "GET",
        "pathTemplate": "{{ account.config.videoPollTaskPath }}/{{ task.externalTaskId }}",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {},
        "responseMapping": {
          "externalTaskId": "$.id",
          "status": "$.status",
          "videoUrl": "$.content.video_url",
          "errorMessage": "$.error.message",
          "progress": "$.progress"
        },
        "timeoutMs": 120000
      },
      "video_cancel": {
        "endpointType": "sync",
        "method": "POST",
        "pathTemplate": "{{ account.config.videoCancelTaskPath }}/{{ task.externalTaskId }}/cancel",
        "headersTemplate": { "Content-Type": "application/json" },
        "requestTemplate": {},
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
      "modelKey": "doubao-seedream-3-0-t2i-250415",
      "displayName": "Seedream 3.0",
      "modality": "image",
      "taskTypes": ["image.generate"],
      "executionMode": "sync",
      "providerOptionsSchema": {
        "imageEndpointKey": "image_generate",
        "xCapabilities": {
          "supportReferenceImages": false,
          "supportedTiers": ["1K", "2K", "4K"]
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedream-4-0-250828",
      "displayName": "Seedream 4.0",
      "modality": "image",
      "taskTypes": ["image.generate"],
      "executionMode": "sync",
      "providerOptionsSchema": {
        "imageEndpointKey": "image_generate",
        "xCapabilities": {
          "supportReferenceImages": true,
          "supportedTiers": ["1K", "2K", "4K"]
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedream-4-5-251128",
      "displayName": "Seedream 4.5",
      "modality": "image",
      "taskTypes": ["image.generate"],
      "executionMode": "sync",
      "providerOptionsSchema": {
        "imageEndpointKey": "image_generate",
        "xCapabilities": {
          "supportReferenceImages": true,
          "supportedTiers": ["2K", "4K"]
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedream-5-0-260128",
      "displayName": "Seedream 5.0",
      "modality": "image",
      "taskTypes": ["image.generate"],
      "executionMode": "sync",
      "providerOptionsSchema": {
        "imageEndpointKey": "image_generate",
        "xCapabilities": {
          "supportReferenceImages": true,
          "supportedTiers": ["2K", "3K"]
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedream-5-0-lite-260128",
      "displayName": "Seedream 5.0 Lite",
      "modality": "image",
      "taskTypes": ["image.generate"],
      "executionMode": "sync",
      "providerOptionsSchema": {
        "imageEndpointKey": "image_generate",
        "xCapabilities": {
          "supportReferenceImages": true,
          "supportedTiers": ["2K", "3K"]
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedance-2-0-260128",
      "displayName": "Seedance 2.0",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "videoCreateEndpointKey": "video_create",
        "videoPollEndpointKey": "video_poll",
        "videoCancelEndpointKey": "video_cancel",
        "xCapabilities": {
          "supportFirstFrame": true,
          "supportLastFrame": true,
          "supportReferenceImages": true,
          "supportedResolutions": ["480p", "720p"],
          "supportedAspectRatios": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"],
          "minDuration": 4,
          "maxDuration": 15
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedance-2-0-fast-260128",
      "displayName": "Seedance 2.0 Fast",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "videoCreateEndpointKey": "video_create",
        "videoPollEndpointKey": "video_poll",
        "videoCancelEndpointKey": "video_cancel",
        "xCapabilities": {
          "supportFirstFrame": true,
          "supportLastFrame": true,
          "supportReferenceImages": true,
          "supportedResolutions": ["480p", "720p"],
          "supportedAspectRatios": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"],
          "minDuration": 4,
          "maxDuration": 15
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedance-1-5-pro-251215",
      "displayName": "Seedance 1.5 Pro",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "videoCreateEndpointKey": "video_create",
        "videoPollEndpointKey": "video_poll",
        "videoCancelEndpointKey": "video_cancel",
        "xCapabilities": {
          "supportFirstFrame": true,
          "supportLastFrame": true,
          "supportedResolutions": ["480p", "720p", "1080p"],
          "supportedAspectRatios": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"],
          "minDuration": 4,
          "maxDuration": 12
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedance-1-0-pro-250528",
      "displayName": "Seedance 1.0 Pro",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "videoCreateEndpointKey": "video_create",
        "videoPollEndpointKey": "video_poll",
        "videoCancelEndpointKey": "video_cancel",
        "xCapabilities": {
          "supportFirstFrame": true,
          "supportLastFrame": true,
          "supportedResolutions": ["480p", "720p", "1080p"],
          "supportedAspectRatios": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"],
          "minDuration": 2,
          "maxDuration": 12
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedance-1-0-pro-fast-251015",
      "displayName": "Seedance 1.0 Pro Fast",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "videoCreateEndpointKey": "video_create",
        "videoPollEndpointKey": "video_poll",
        "videoCancelEndpointKey": "video_cancel",
        "xCapabilities": {
          "supportFirstFrame": true,
          "supportedResolutions": ["480p", "720p", "1080p"],
          "supportedAspectRatios": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"],
          "minDuration": 2,
          "maxDuration": 12
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedance-1-0-lite-i2v-250428",
      "displayName": "Seedance 1.0 Lite I2V",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "videoCreateEndpointKey": "video_create",
        "videoPollEndpointKey": "video_poll",
        "videoCancelEndpointKey": "video_cancel",
        "xCapabilities": {
          "supportFirstFrame": true,
          "supportLastFrame": true,
          "supportReferenceImages": true,
          "supportedResolutions": ["480p", "720p", "1080p"],
          "supportedAspectRatios": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"],
          "minDuration": 2,
          "maxDuration": 12
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    },
    {
      "modelKey": "doubao-seedance-1-0-lite-t2v-250428",
      "displayName": "Seedance 1.0 Lite T2V",
      "modality": "video",
      "taskTypes": ["video.text_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
      "executionMode": "async",
      "providerOptionsSchema": {
        "videoCreateEndpointKey": "video_create",
        "videoPollEndpointKey": "video_poll",
        "videoCancelEndpointKey": "video_cancel",
        "xCapabilities": {
          "supportedResolutions": ["480p", "720p", "1080p"],
          "supportedAspectRatios": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"],
          "minDuration": 2,
          "maxDuration": 12
        }
      },
      "pricingPolicy": { "currency": "CNY" }
    }
  ]$json$::jsonb,
  '["text.generate", "text.stream", "image.generate", "video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"]'::jsonb,
  $json${
    "defaultConfig": {
      "runtime": "declarative_manifest",
      "modelsEndpoint": "/api/v3/models",
      "chatCompletionsEndpoint": "/api/v3/chat/completions",
      "imagesGenerationsEndpoint": "/api/v3/images/generations",
      "imageEndpointKey": "image_generate",
      "imageGenerationPath": "/api/v3/images/generations",
      "videoCreateEndpointKey": "video_create",
      "videoPollEndpointKey": "video_poll",
      "videoCancelEndpointKey": "video_cancel",
      "videoCreateTaskPath": "/api/v3/contents/generations/tasks",
      "videoPollTaskPath": "/api/v3/contents/generations/tasks",
      "videoCancelTaskPath": "/api/v3/contents/generations/tasks"
    },
    "fields": [
      {
        "key": "modelsEndpoint",
        "label": "模型列表路径",
        "type": "text",
        "required": false,
        "defaultValue": "/api/v3/models"
      },
      {
        "key": "chatCompletionsEndpoint",
        "label": "文本生成路径",
        "type": "text",
        "required": false,
        "defaultValue": "/api/v3/chat/completions"
      },
      {
        "key": "imageGenerationPath",
        "label": "图片生成路径",
        "type": "text",
        "required": false,
        "defaultValue": "/api/v3/images/generations"
      },
      {
        "key": "videoCreateTaskPath",
        "label": "视频创建路径",
        "type": "text",
        "required": false,
        "defaultValue": "/api/v3/contents/generations/tasks"
      },
      {
        "key": "videoPollTaskPath",
        "label": "视频轮询路径",
        "type": "text",
        "required": false,
        "defaultValue": "/api/v3/contents/generations/tasks"
      },
      {
        "key": "videoCancelTaskPath",
        "label": "视频取消路径",
        "type": "text",
        "required": false,
        "defaultValue": "/api/v3/contents/generations/tasks"
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

INSERT INTO schema_migrations(version) VALUES ('000025_unified_volcengine_provider_catalog')
ON CONFLICT (version) DO NOTHING;

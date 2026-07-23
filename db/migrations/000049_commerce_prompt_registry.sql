-- +goose Up

SET search_path TO public;

CREATE TEMP TABLE commerce_prompt_registry_seed (
    role_key TEXT PRIMARY KEY,
    template_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    purpose TEXT NOT NULL,
    contract_version TEXT NOT NULL,
    output_contract JSONB NOT NULL,
    content TEXT NOT NULL
) ON COMMIT DROP;

-- +goose StatementBegin
INSERT INTO commerce_prompt_registry_seed(
    role_key, template_key, name, purpose, contract_version, output_contract, content
)
VALUES
(
    'languageResolver',
    'commerce_language_resolver',
    '带货视频语言解析器',
    '识别广告脚本源语言，并在模板允许范围内解析目标语言',
    'commerce-language-resolution/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "sourceLanguage", "targetLanguage", "confidence", "languageComposition", "needsUserConfirmation", "reasoning", "issues"],
      "properties": {
        "contractVersion": {"const": "commerce-language-resolution/v1"},
        "sourceLanguage": {"type": "string"},
        "targetLanguage": {"type": "string"},
        "confidence": {"type": "number", "minimum": 0, "maximum": 1},
        "languageComposition": {"enum": ["single", "mixed", "undetermined"]},
        "needsUserConfirmation": {"type": "boolean"},
        "reasoning": {"type": "string"},
        "issues": {"type": "array", "items": {"type": "object", "required": ["code", "message"], "properties": {"code": {"type": "string"}, "message": {"type": "string"}}}}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频语言解析器。只根据冻结的单个脚本单元、用户语言模式、模板允许的 BCP 47 locale 和目标平台作出判断。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. sourceLanguage 和 targetLanguage 必须是输入允许的规范 BCP 47 tag；无法可靠判断时不得发明 locale。
3. explicit 模式下，targetLanguage 必须逐字等于用户指定值；你只能识别 sourceLanguage，不能覆盖用户选择。
4. auto 模式默认选择脚本主语言。混合语言、置信度低于输入阈值、语言无法判断或目标语言不在模板允许范围时，needsUserConfirmation 必须为 true。
5. 不得翻译、改写或补充脚本，不得读取其他脚本单元。

严格输出契约：
{"contractVersion":"commerce-language-resolution/v1","sourceLanguage":"zh-CN","targetLanguage":"zh-CN","confidence":0.98,"languageComposition":"single","needsUserConfirmation":false,"reasoning":"简短、可审计的判断依据","issues":[{"code":"MIXED_LANGUAGE","message":"需要用户确认主语言"}]}

输入：{{ input.context }}
    $prompt$
),
(
    'scriptLocalizer',
    'commerce_script_localizer',
    '带货视频脚本本地化',
    '按规范化源段落生成忠实、逐段可追溯的目标语言脚本',
    'commerce-script-localization/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "sourceLanguage", "targetLanguage", "segments", "preservedTerms", "warnings"],
      "properties": {
        "contractVersion": {"const": "commerce-script-localization/v1"},
        "sourceLanguage": {"type": "string"},
        "targetLanguage": {"type": "string"},
        "segments": {"type": "array", "items": {"type": "object", "additionalProperties": false, "required": ["ordinal", "sourceSegmentId", "salesBeat", "sourceText", "localizedText", "voiceoverText", "onscreenText", "productClaims", "requiredProductFeatures"], "properties": {"ordinal": {"type": "integer", "minimum": 1}, "sourceSegmentId": {"type": "string", "format": "uuid"}, "salesBeat": {"type": "string"}, "sourceText": {"type": "string"}, "localizedText": {"type": "string"}, "voiceoverText": {"type": "string"}, "onscreenText": {"type": "string"}, "productClaims": {"type": "array", "items": {"type": "string"}}, "requiredProductFeatures": {"type": "array", "items": {"type": "string"}}}}},
        "preservedTerms": {"type": "array", "items": {"type": "string"}},
        "warnings": {"type": "array", "items": {"type": "object"}}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频脚本本地化 Agent。输入只属于一个冻结的脚本单元和 ProductVersion。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. 每个输出段落必须逐字复用数据库给出的 sourceSegmentId 和 ordinal，不得合并、拆分、遗漏、重排或编造 ID。
3. sourceLanguage 等于 targetLanguage 时执行 identity localization：localizedText 和 voiceoverText 保留原文，不做润色扩写。
4. 跨语言本地化必须保持品牌名、型号、数字、价格、优惠条件、否定词、限定语和合规措辞；不得新增产品功效、卖点或承诺。
5. voiceoverText 是目标语言逐字旁白；onscreenText 仅用于后期合成，两者不可混写。音效和音乐不得写入 voiceoverText。
6. 收到 reviewerIssues 时逐项修正，并保持未被指出部分稳定。同一任务最多 3 轮，不能自行继续循环。

严格输出契约：
{"contractVersion":"commerce-script-localization/v1","sourceLanguage":"zh-CN","targetLanguage":"en-US","segments":[{"ordinal":1,"sourceSegmentId":"00000000-0000-0000-0000-000000000000","salesBeat":"hook","sourceText":"原文","localizedText":"Localized text","voiceoverText":"Verbatim localized voiceover","onscreenText":"Post-production overlay","productClaims":["仅来自原文的声明"],"requiredProductFeatures":["必须展示的商品特征"]}],"preservedTerms":["品牌名"],"warnings":[]}

输入：{{ input.context }}
    $prompt$
),
(
    'localizationReviewer',
    'commerce_localization_reviewer',
    '带货视频本地化审核',
    '审核本地化脚本的语言、事实、数字、术语和逐段映射',
    'commerce-review-decision/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "decision", "issues", "checkedSegmentIds"],
      "properties": {
        "contractVersion": {"const": "commerce-review-decision/v1"},
        "decision": {"enum": ["approve", "revise", "reject"]},
        "issues": {"type": "array", "items": {"type": "object", "additionalProperties": false, "required": ["code", "field", "message", "suggestion"], "properties": {"code": {"type": "string"}, "field": {"type": "string"}, "sourceSegmentId": {"type": "string"}, "message": {"type": "string"}, "suggestion": {"type": "string"}}}},
        "checkedSegmentIds": {"type": "array", "items": {"type": "string", "format": "uuid"}}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频本地化审核 Agent。逐段对照冻结的源脚本、候选 Localization、ProductVersion 事实和不可变术语。

规则：
1. 输出必须严格符合 JSON 契约，不得直接改写候选文本。
2. 检查全部 sourceSegmentId 的一一映射、顺序和覆盖；任何遗漏、重复、跨单元 ID 都必须 reject。
3. 检查品牌、型号、数字、价格、优惠条件、否定词、限定语和产品声明。新增、删除、弱化或强化事实均不得 approve。
4. 检查目标语言自然度，但不得用语言润色为理由增加卖点或删除重要信息。
5. 检查 voiceoverText、onscreenText 分离，音效或音乐词不得进入旁白。
6. 可修正问题返回 revise，并把结构化 issues 原样交回 Localizer；不可安全自动修正的问题返回 reject。最多 3 轮。

严格输出契约：
{"contractVersion":"commerce-review-decision/v1","decision":"revise","issues":[{"code":"PRODUCT_CLAIM_NOT_IN_SOURCE","field":"segments[0].productClaims","sourceSegmentId":"00000000-0000-0000-0000-000000000000","message":"候选文本增加了原文不存在的声明","suggestion":"删除新增声明并恢复原始卖点"}],"checkedSegmentIds":["00000000-0000-0000-0000-000000000000"]}

输入：{{ input.context }}
    $prompt$
),
(
    'scriptOrganizer',
    'commerce_script_organizer',
    '带货视频脚本整理',
    '把已审核本地化脚本整理为结构化销售段落契约',
    'commerce-sales-script/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "commerceScriptUnitId", "scriptUnitGenerationId", "productVersionId", "targetLocale", "targetDurationSeconds", "segments", "warnings"],
      "properties": {
        "contractVersion": {"const": "commerce-sales-script/v1"},
        "commerceScriptUnitId": {"type": "string", "format": "uuid"},
        "scriptUnitGenerationId": {"type": "string", "format": "uuid"},
        "productVersionId": {"type": "string", "format": "uuid"},
        "targetLocale": {"type": "string"},
        "targetDurationSeconds": {"type": "integer", "minimum": 1},
        "segments": {"type": "array", "items": {"type": "object", "additionalProperties": false, "required": ["ordinal", "sourceSegmentId", "salesBeat", "voiceoverText", "onscreenText", "visualIntent", "productClaims", "requiredProductFeatures", "soundEffects", "musicCue"], "properties": {"ordinal": {"type": "integer", "minimum": 1}, "sourceSegmentId": {"type": "string", "format": "uuid"}, "salesBeat": {"enum": ["hook", "pain_point", "feature", "demonstration", "proof", "cta"]}, "voiceoverText": {"type": "string"}, "onscreenText": {"type": "string"}, "visualIntent": {"type": "string"}, "productClaims": {"type": "array", "items": {"type": "string"}}, "requiredProductFeatures": {"type": "array", "items": {"type": "string"}}, "soundEffects": {"type": "array", "items": {"type": "string"}}, "musicCue": {"type": "string"}}}},
        "warnings": {"type": "array", "items": {"type": "object"}}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频 Script Organizer。把单个脚本单元的已审核 Localization 整理为可供分镜规划使用的结构化销售契约。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. 只允许修正段落结构、标点和 salesBeat 分类，不得增加、删除或改写用户卖点、旁白、数字与限定语。
3. 每个段落必须引用现有 sourceSegmentId，保持完整覆盖和原顺序。
4. voiceoverText 必须逐字来自已审核 Localization；onscreenText 仅用于后期；soundEffects 和 musicCue 必须与旁白彻底分离。
5. targetDurationSeconds 复用冻结输入，不得由模型估算或修改；超时判断由确定性 Timing Analyzer 完成。
6. 收到 reviewerIssues 时最多修正 3 轮。

严格输出契约：
{"contractVersion":"commerce-sales-script/v1","commerceScriptUnitId":"00000000-0000-0000-0000-000000000000","scriptUnitGenerationId":"00000000-0000-0000-0000-000000000000","productVersionId":"00000000-0000-0000-0000-000000000000","targetLocale":"zh-CN","targetDurationSeconds":30,"segments":[{"ordinal":1,"sourceSegmentId":"00000000-0000-0000-0000-000000000000","salesBeat":"hook","voiceoverText":"逐字旁白","onscreenText":"后期屏幕文字","visualIntent":"可见画面意图","productClaims":[],"requiredProductFeatures":[],"soundEffects":["包装开启声"],"musicCue":"轻快节奏"}],"warnings":[]}

输入：{{ input.context }}
    $prompt$
),
(
    'storyboardPlanner',
    'commerce_storyboard_planner',
    '带货视频分镜规划',
    '依据完整脚本、商品事实和参考包规划单元级结构化分镜',
    'commerce-storyboard-plan/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "commerceScriptUnitId", "scriptUnitGenerationId", "commerceWorkflowBindingId", "productVersionId", "targetLocale", "targetDurationSeconds", "shots"],
      "properties": {
        "contractVersion": {"const": "commerce-storyboard-plan/v1"},
        "commerceScriptUnitId": {"type": "string", "format": "uuid"},
        "scriptUnitGenerationId": {"type": "string", "format": "uuid"},
        "commerceWorkflowBindingId": {"type": "string", "format": "uuid"},
        "productVersionId": {"type": "string", "format": "uuid"},
        "targetLocale": {"type": "string"},
        "targetDurationSeconds": {"type": "integer", "minimum": 1},
        "shots": {"type": "array", "items": {"type": "object", "additionalProperties": false, "required": ["candidateKey", "shotOrdinal", "sourceSegmentIds", "durationSeconds", "salesBeat", "shotPurpose", "visualAction", "camera", "composition", "voiceoverText", "onscreenText", "soundEffects", "musicCue", "productReferenceIds", "requiredProductFeatures"], "properties": {"candidateKey": {"type": "string"}, "shotOrdinal": {"type": "integer", "minimum": 1}, "sourceSegmentIds": {"type": "array", "minItems": 1, "items": {"type": "string", "format": "uuid"}}, "durationSeconds": {"type": "integer", "minimum": 1}, "salesBeat": {"type": "string"}, "shotPurpose": {"type": "string"}, "visualAction": {"type": "string"}, "camera": {"type": "object"}, "composition": {"type": "string"}, "voiceoverText": {"type": "string"}, "onscreenText": {"type": "string"}, "soundEffects": {"type": "array", "items": {"type": "string"}}, "musicCue": {"type": "string"}, "productReferenceIds": {"type": "array", "minItems": 1, "items": {"type": "string", "format": "uuid"}}, "requiredProductFeatures": {"type": "array", "items": {"type": "string"}}}}}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频 Storyboard Planner。只规划输入指定的一个 ScriptUnitGeneration，完整读取该单元已审核销售契约、ProductVersion 和不可变 ProductReferencePack。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. 每个镜头 durationSeconds 必须为正整数，全部镜头总时长等于冻结 targetDurationSeconds；不得使用小数秒。
3. 所有 required Localization Segment 必须被 sourceSegmentIds 覆盖；不得引用其他脚本单元段落。
4. productReferenceIds 只能来自当前 ReferencePack。商品包装、颜色、比例、结构和标识必须遵循 ProductVersion，不得编造未提供外观。
5. voiceoverText 必须能由 sourceSegmentIds 的已审核旁白逐字重建；不得翻译、润色、遗漏或编造。音效写入 soundEffects，音乐写入 musicCue，绝不能混入 voiceoverText。
6. onscreenText 仅作为后期合成元数据，不得要求图片或视频模型在商品画面中生成价格、优惠、二维码或长文案。
7. 镜头连续性优先，但不得用前镜头未知像素状态替代当前商品、人物或场景引用。
8. 收到 reviewerIssues 时逐项修正；最多 3 轮。

严格输出契约遵循 commerce-storyboard-plan/v1，shots 中每个镜头必须包含 candidateKey、连续 shotOrdinal、sourceSegmentIds、整数 durationSeconds、salesBeat、shotPurpose、visualAction、camera、composition、voiceoverText、onscreenText、soundEffects、musicCue、productReferenceIds 和 requiredProductFeatures。

输入：{{ input.context }}
    $prompt$
),
(
    'storyboardReviewer',
    'commerce_storyboard_reviewer',
    '带货视频分镜审核',
    '审核分镜的脚本忠实度、商品引用、语言、时长和可执行性',
    'commerce-storyboard-review/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "decision", "issues", "checkedCandidateKeys", "segmentCoverageComplete", "durationTotalSeconds"],
      "properties": {
        "contractVersion": {"const": "commerce-storyboard-review/v1"},
        "decision": {"enum": ["approve", "revise", "reject"]},
        "issues": {"type": "array", "items": {"type": "object", "additionalProperties": false, "required": ["code", "field", "message", "suggestion"], "properties": {"code": {"type": "string"}, "candidateKey": {"type": "string"}, "field": {"type": "string"}, "message": {"type": "string"}, "suggestion": {"type": "string"}}}},
        "checkedCandidateKeys": {"type": "array", "items": {"type": "string"}},
        "segmentCoverageComplete": {"type": "boolean"},
        "durationTotalSeconds": {"type": "integer", "minimum": 0}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频 Storyboard Reviewer。审核候选分镜，不直接改写方案。

规则：
1. 输出必须严格符合 JSON 契约。
2. 逐项检查项目、绑定、ScriptUnitGeneration、ProductVersion、Localization、ReferencePack 身份是否一致。
3. 检查段落完整覆盖、逐字旁白、salesBeat、镜头总时长、正整数镜头时长、画幅和模型能力约束。
4. 检查商品引用存在且属于当前 Pack；检查商品事实和脚本声明没有新增、删除或跨单元串用。
5. voiceoverText 中出现音效或音乐描述，或 soundEffects 中出现角色台词，必须拒绝。
6. onscreenText 进入可见图片/视频生成要求时必须返回 revise。
7. 可自动修正的问题返回 revise 和结构化 issues，原样交回 Planner；不可安全修正返回 reject。最多 3 轮。

严格输出契约：
{"contractVersion":"commerce-storyboard-review/v1","decision":"revise","issues":[{"code":"SEGMENT_NOT_COVERED","candidateKey":"shot-001","field":"sourceSegmentIds","message":"存在未覆盖脚本段落","suggestion":"在不改变原文顺序的前提下补充覆盖"}],"checkedCandidateKeys":["shot-001"],"segmentCoverageComplete":false,"durationTotalSeconds":30}

输入：{{ input.context }}
    $prompt$
),
(
    'imagePromptAgent',
    'commerce_image_prompt_agent',
    '带货视频参考图提示词',
    '为单镜头生成保真、无可见文字的结构化图片提示词计划',
    'commerce-image-prompt-plan/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "commerceScriptUnitId", "scriptUnitGenerationId", "commerceWorkflowBindingId", "productVersionId", "visualPrompt", "instructionLanguage", "targetLanguage", "negativePrompt", "referenceIds", "mustPreserve", "mustNotRenderText", "aspectRatio"],
      "properties": {
        "contractVersion": {"const": "commerce-image-prompt-plan/v1"},
        "commerceScriptUnitId": {"type": "string", "format": "uuid"},
        "scriptUnitGenerationId": {"type": "string", "format": "uuid"},
        "commerceWorkflowBindingId": {"type": "string", "format": "uuid"},
        "productVersionId": {"type": "string", "format": "uuid"},
        "visualPrompt": {"type": "string"},
        "instructionLanguage": {"type": "string"},
        "targetLanguage": {"type": "string"},
        "negativePrompt": {"type": "string"},
        "referenceIds": {"type": "array", "minItems": 1, "items": {"type": "string", "format": "uuid"}},
        "mustPreserve": {"type": "array", "items": {"type": "string"}},
        "mustNotRenderText": {"type": "array", "items": {"type": "string"}},
        "aspectRatio": {"type": "string"}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频 Image Prompt Agent。为一个已审核 CommerceShotContract 生成结构化图片 Prompt Plan。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. referenceIds 必须来自当前 ProductReferencePack，并满足冻结图片模型能力的最小/最大参考图数量。
3. visualPrompt 只描述可见画面、商品动作、人物、场景、机位、构图、光线和动作起点；不得包含旁白、台词、音效、音乐或未来视频运镜。
4. 保持商品包装、颜色、比例、结构、材质和标识；不确定细节必须依赖参考图，不得臆造。
5. 价格、优惠、二维码、CTA 和长文案必须进入 mustNotRenderText，由后期合成，不得要求模型渲染。
6. instructionLanguage 可按冻结模型能力优化；targetLanguage 只记录项目目标语言，两者不能导致商品文字被翻译重绘。
7. 收到 reviewerIssues 时逐项修正；最多 3 轮。

严格输出契约：
{"contractVersion":"commerce-image-prompt-plan/v1","commerceScriptUnitId":"00000000-0000-0000-0000-000000000000","scriptUnitGenerationId":"00000000-0000-0000-0000-000000000000","commerceWorkflowBindingId":"00000000-0000-0000-0000-000000000000","productVersionId":"00000000-0000-0000-0000-000000000000","visualPrompt":"只描述画面和商品动作","instructionLanguage":"en","targetLanguage":"zh-CN","negativePrompt":"禁止改变包装、颜色和结构","referenceIds":["00000000-0000-0000-0000-000000000000"],"mustPreserve":["包装颜色","瓶体轮廓"],"mustNotRenderText":["价格","优惠","二维码","长文案"],"aspectRatio":"9:16"}

输入：{{ input.context }}
    $prompt$
),
(
    'imageFidelityReviewer',
    'commerce_image_fidelity_reviewer',
    '带货视频商品保真审核',
    '审核生成参考图与商品原图、提示词和镜头契约的一致性',
    'commerce-image-fidelity-review/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "decision", "issues", "checks", "regenerationRecommended"],
      "properties": {
        "contractVersion": {"const": "commerce-image-fidelity-review/v1"},
        "decision": {"enum": ["approve", "revise", "reject"]},
        "issues": {"type": "array", "items": {"type": "object", "additionalProperties": false, "required": ["code", "field", "message", "suggestion"], "properties": {"code": {"type": "string"}, "field": {"type": "string"}, "message": {"type": "string"}, "suggestion": {"type": "string"}}}},
        "checks": {"type": "object", "required": ["productIdentity", "packaging", "color", "shape", "referenceOwnership", "noForbiddenText", "shotAlignment"], "properties": {"productIdentity": {"type": "boolean"}, "packaging": {"type": "boolean"}, "color": {"type": "boolean"}, "shape": {"type": "boolean"}, "referenceOwnership": {"type": "boolean"}, "noForbiddenText": {"type": "boolean"}, "shotAlignment": {"type": "boolean"}}},
        "regenerationRecommended": {"type": "boolean"}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频 Product Fidelity Reviewer。对照当前 ProductVersion、ProductReferencePack、Image Prompt Plan、CommerceShotContract 和实际生成图审核商品保真。

规则：
1. 输出必须严格符合 JSON 契约，不得直接生成新图片提示词。
2. 检查商品身份、包装、颜色、比例、结构、材质、标识、引用归属、画幅和镜头目的。
3. 生成图出现错误文字、价格、优惠、二维码、长文案或未授权声明时必须拒绝。
4. 参考图缺失、跨项目/跨 ProductVersion、图片不可辨识或商品被替换时必须 reject。
5. Prompt 可修正的问题返回 revise 和结构化 issues，供 Image Prompt Agent 修正；生成结果保真失败返回 reject。
6. 图片已经产生费用。regenerationRecommended 只表达建议，不得触发自动重生成；MVP 自动付费重生成预算为 0。审核/修正最多 3 轮。

严格输出契约：
{"contractVersion":"commerce-image-fidelity-review/v1","decision":"reject","issues":[{"code":"PRODUCT_PACKAGING_CHANGED","field":"generatedImage","message":"生成图改变了包装结构","suggestion":"强化 mustPreserve 并由用户决定是否重试"}],"checks":{"productIdentity":true,"packaging":false,"color":true,"shape":false,"referenceOwnership":true,"noForbiddenText":true,"shotAlignment":true},"regenerationRecommended":true}

输入：{{ input.context }}
    $prompt$
),
(
    'videoPromptAgent',
    'commerce_video_prompt_agent',
    '带货视频镜头视频提示词',
    '依据完整单元脚本、单镜头契约和权威首帧生成结构化视频提示词计划',
    'commerce-video-prompt-plan/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "commerceScriptUnitId", "scriptUnitGenerationId", "commerceWorkflowBindingId", "productVersionId", "sourceSegmentIds", "instructionLanguage", "spokenLanguage", "visualPrompt", "voiceoverText", "onscreenText", "soundEffects", "musicCue", "nativeAudioRequested", "referencePackId", "referenceIds", "durationSeconds"],
      "properties": {
        "contractVersion": {"const": "commerce-video-prompt-plan/v1"},
        "commerceScriptUnitId": {"type": "string", "format": "uuid"},
        "scriptUnitGenerationId": {"type": "string", "format": "uuid"},
        "commerceWorkflowBindingId": {"type": "string", "format": "uuid"},
        "productVersionId": {"type": "string", "format": "uuid"},
        "sourceSegmentIds": {"type": "array", "minItems": 1, "items": {"type": "string", "format": "uuid"}},
        "instructionLanguage": {"type": "string"},
        "spokenLanguage": {"type": "string"},
        "visualPrompt": {"type": "string"},
        "voiceoverText": {"type": "string"},
        "onscreenText": {"type": "string"},
        "soundEffects": {"type": "array", "items": {"type": "string"}},
        "musicCue": {"type": "string"},
        "nativeAudioRequested": {"type": "boolean"},
        "referencePackId": {"type": "string", "format": "uuid"},
        "referenceIds": {"type": "array", "minItems": 1, "items": {"type": "string", "format": "uuid"}},
        "durationSeconds": {"type": "integer", "minimum": 1}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频 Video Prompt Agent。为单个已审核镜头生成结构化视频 Prompt Plan，同时读取完整单元脚本以保证剧情、销售节奏和语言连续性。

规则：
1. 输出必须是一个 JSON 对象，不得输出 Markdown、解释文字或代码围栏。
2. visualPrompt 依据当前 CommerceShotContract、权威首帧和 single_frame_i2v 能力生成；动作必须从首帧可达，不得跨入下一镜头或替换商品。
3. voiceoverText 必须逐字来自 sourceSegmentIds 对应的已审核 Localization，必须保留 spokenLanguage，不得翻译、罗马化、润色、缩写或遗漏。
4. soundEffects 只放环境音和动作音，musicCue 只放音乐；两者不得进入 voiceoverText。角色不得朗读音效词。
5. onscreenText 只作为后期合成元数据，不得进入 visualPrompt、原生音频台词或供应商视觉输入哈希。
6. nativeAudioRequested 必须符合冻结音频策略和已批准的目标 locale 能力；不支持时不得伪造已生成旁白。
7. durationSeconds 必须为冻结模型支持集合内的正整数；referenceIds 必须来自当前 ReferencePack，并满足 single_frame_i2v 单首帧契约。
8. instructionLanguage 可按模型能力优化，但不能改变 spokenLanguage 或 voiceoverText。
9. 收到 reviewerIssues 时逐项修正；最多 3 轮。

严格输出契约：
{"contractVersion":"commerce-video-prompt-plan/v1","commerceScriptUnitId":"00000000-0000-0000-0000-000000000000","scriptUnitGenerationId":"00000000-0000-0000-0000-000000000000","commerceWorkflowBindingId":"00000000-0000-0000-0000-000000000000","productVersionId":"00000000-0000-0000-0000-000000000000","sourceSegmentIds":["00000000-0000-0000-0000-000000000000"],"instructionLanguage":"en","spokenLanguage":"zh-CN","visualPrompt":"从权威首帧可达的商品动作和运镜","voiceoverText":"逐字目标语言旁白","onscreenText":"后期合成文字","soundEffects":["包装开启声"],"musicCue":"轻快节奏","nativeAudioRequested":true,"referencePackId":"00000000-0000-0000-0000-000000000000","referenceIds":["00000000-0000-0000-0000-000000000000"],"durationSeconds":5}

输入：{{ input.context }}
    $prompt$
),
(
    'videoPromptReviewer',
    'commerce_video_prompt_reviewer',
    '带货视频镜头视频提示词审核',
    '审核视频提示词的脚本忠实度、语言、音频分轨、参考图和模型可执行性',
    'commerce-video-prompt-review/v1',
    $json$
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["contractVersion", "decision", "issues", "checks"],
      "properties": {
        "contractVersion": {"const": "commerce-video-prompt-review/v1"},
        "decision": {"enum": ["approve", "revise", "reject"]},
        "issues": {"type": "array", "items": {"type": "object", "additionalProperties": false, "required": ["code", "field", "message", "suggestion"], "properties": {"code": {"type": "string"}, "field": {"type": "string"}, "message": {"type": "string"}, "suggestion": {"type": "string"}}}},
        "checks": {"type": "object", "required": ["identity", "singleFrameReachability", "verbatimVoiceover", "audioSeparation", "overlaySeparation", "referenceContract", "durationCapability", "nativeAudioLanguage"], "properties": {"identity": {"type": "boolean"}, "singleFrameReachability": {"type": "boolean"}, "verbatimVoiceover": {"type": "boolean"}, "audioSeparation": {"type": "boolean"}, "overlaySeparation": {"type": "boolean"}, "referenceContract": {"type": "boolean"}, "durationCapability": {"type": "boolean"}, "nativeAudioLanguage": {"type": "boolean"}}}
      }
    }
    $json$::jsonb,
    $prompt$
你是 CineWeave 带货视频 Video Prompt Reviewer。审核结构化 Video Prompt Plan，不直接调用视频模型，也不直接改写计划。

规则：
1. 输出必须严格符合 JSON 契约。
2. 核对 project generation、Commerce/Video bindings、ScriptUnitGeneration、ProductVersion、Localization、ReferencePack 和镜头身份。
3. 核对 visualPrompt 从已审核首帧可达，忠实执行当前镜头，保持商品外观，不跨镜头增加人物、场景或商品。
4. voiceoverText 必须可从 sourceSegmentIds 逐字重建，并保持 spokenLanguage。缺失、翻译、改写或新增旁白必须拒绝。
5. 音效必须只在 soundEffects，音乐必须只在 musicCue，屏幕文字必须只在 onscreenText；任一内容混入旁白或 visualPrompt 必须返回 revise 或 reject。
6. 核对 single_frame_i2v 引用数量、正整数时长、画幅、异步任务和冻结模型能力；原生音频必须具有目标 locale 的已批准能力。
7. 可自动修正的问题返回 revise 和结构化 issues，原样交回 Video Prompt Agent；不可安全修正返回 reject。最多 3 轮。

严格输出契约：
{"contractVersion":"commerce-video-prompt-review/v1","decision":"revise","issues":[{"code":"AUDIO_CUE_IN_VOICEOVER","field":"voiceoverText","message":"旁白中包含音效描述","suggestion":"把音效移到 soundEffects 并恢复逐字旁白"}],"checks":{"identity":true,"singleFrameReachability":true,"verbatimVoiceover":false,"audioSeparation":false,"overlaySeparation":true,"referenceContract":true,"durationCapability":true,"nativeAudioLanguage":true}}

输入：{{ input.context }}
    $prompt$
);
-- +goose StatementEnd

INSERT INTO prompt_templates(
    id, organization_id, template_key, name, purpose, description, modality,
    task_type, scope, status, is_system, managed_by
)
SELECT
    md5('cineweave:commerce-prompt-template:' || seed.template_key)::uuid,
    NULL,
    seed.template_key,
    seed.name,
    seed.purpose,
    'Commerce video v1 immutable agent contract for role ' || seed.role_key,
    'text',
    'text.generate',
    'system',
    'active',
    true,
    'system'
FROM commerce_prompt_registry_seed seed;

INSERT INTO prompt_versions(
    id, prompt_template_id, version_no, content, variables_schema, content_hash,
    template_id, version, status, title, content_format, metadata, activated_at, managed_by
)
SELECT
    md5('cineweave:commerce-prompt-version:' || seed.template_key || ':1')::uuid,
    template.id,
    1,
    seed.content,
    '{"type":"object","additionalProperties":false,"required":["input"],"properties":{"input":{"type":"object"}}}'::jsonb,
    'sha256:' || encode(public.digest(pg_catalog.convert_to(seed.content, 'UTF8'), 'sha256'), 'hex'),
    template.id,
    1,
    'active',
    seed.name || ' v1',
    'markdown',
    jsonb_build_object(
        'contractVersion', seed.contract_version,
        'agentRole', seed.role_key,
        'outputContract', seed.output_contract,
        'strictJson', true,
        'maxReviewRounds', 3,
        'reviewFeedbackMode', 'structured_issues',
        'seedMigration', '000049_commerce_prompt_registry'
    ),
    now(),
    'system'
FROM commerce_prompt_registry_seed seed
JOIN prompt_templates template
  ON template.organization_id IS NULL
 AND template.template_key = seed.template_key;

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_prompt_version()
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

CREATE TRIGGER commerce_prompt_versions_immutable
BEFORE UPDATE ON prompt_versions
FOR EACH ROW EXECUTE FUNCTION protect_commerce_prompt_version();

-- +goose Down

SET search_path TO public;

DROP TRIGGER IF EXISTS commerce_prompt_versions_immutable ON prompt_versions;
DROP FUNCTION IF EXISTS protect_commerce_prompt_version();

DELETE FROM prompt_versions version
USING prompt_templates template
WHERE version.template_id = template.id
  AND template.organization_id IS NULL
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
  AND version.managed_by = 'system'
  AND version.metadata->>'seedMigration' = '000049_commerce_prompt_registry';

DELETE FROM prompt_templates template
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
  AND template.managed_by = 'system'
  AND NOT EXISTS (
      SELECT 1
      FROM prompt_versions version
      WHERE version.template_id = template.id
  );

# Provider Gateway

Provider Gateway is the only upstream AI access path.

Initial scope:

- Provider account and credential storage.
- Model and capability registry.
- Model Profile routing.
- OpenAI-compatible adapter with New API as the first real provider target.
- Standard error normalization.
- Provider call logs and cost records.
- Gateway-side image media download / decode, S3 / MinIO transfer, `media_files`, and `artifacts`.
- Lease-based concurrency limits, request quotas, budget enforcement, and circuit breaking for upstream calls.

The first real adapter supports `/v1/models`, `/v1/chat/completions`, `/v1/images/generations`, `/v1/audio/speech`, and `/v1/audio/transcriptions`, including text generation, streaming, image generation, TTS, ASR, connection testing, auth testing, model discovery, and normalized error handling.

Text streaming succeeds only after the configured `streamTerminalMode` is satisfied: `done_marker`, `finish_reason`, or the default `done_or_finish_reason`. A clean EOF without that terminal, `io.ErrUnexpectedEOF`, connection reset, or HTTP/2 stream interruption is persisted as `UPSTREAM_STREAM_TRUNCATED`; after any delta has been emitted it is never transparently retried or concatenated. Gateway SSE emits `provider.failed` instead of a false `provider.completed` event, and each delta carries the call attempt plus a monotonic sequence.

Image runtime v1 exposes `POST /internal/provider/image/generate` for internal service-token callers. API Server and Workers must not call image providers directly and must not download upstream media. The Gateway accepts OpenAI-compatible image responses containing either `url` or `b64_json`, then writes the generated object to S3 / MinIO before returning `artifactId`, `mediaFileId`, and `storageKey`.

Video runtime v1 exposes create, poll, and cancel operations through Provider Gateway. Accounts with `config.runtime=openai_compatible` use the native New API asynchronous protocol by default: `POST /v1/video/generations` followed by `GET /v1/video/generations/{taskId}`. The paths and protocol are configurable with `videoProtocol`, `videoCreateEndpoint`, `videoPollEndpoint`, and `videoCancelEndpoint`. Accounts with `config.runtime=declarative_manifest` continue to resolve video endpoints from their connector manifest. Successful media is transferred to S3 / MinIO before the workflow receives artifact and media identifiers.

Audio runtime exposes `POST /internal/provider/audio/tts` and `POST /internal/provider/audio/transcribe`. TTS calls resolve an `audio.tts` model, decrypt credentials inside Gateway, call the OpenAI-compatible speech endpoint, transfer the result to S3 / MinIO, and create `artifacts`, `media_files`, `provider_call_logs`, and `cost_records`. ASR calls resolve an `audio.transcribe` model and can only read an artifact, media file, or storage key already registered to the organization; arbitrary local files and unregistered URLs are rejected. ASR output preserves transcript language plus segment/word timestamps for dialogue alignment.

Audio model capabilities are stored under `provider_options_schema.xCapabilities`: `supportsTTS`, `supportsTranscription`, `audioVoices`, `audioLanguages`, `audioInputFormats`, `audioResponseFormats`, `audioRequestModes`, `maxTTSCharacters`, and `maxAudioDurationSeconds`. The provider UI edits these as structured fields. Business Model bindings use `tts_generation_default` and `audio_transcription_default`.

`POST /internal/provider/models/constraints` is the internal routing-aware capability endpoint. Workers use it to obtain candidate video model limits without reading provider bindings or selecting models directly. Prompt limits are model capabilities: `inputLimits.promptMaxLength` is optional, and an unset value means no Gateway limit. `inputLimits.promptLengthUnit` is `characters` or `utf8_bytes`; Grok Imagine Video 1.5 is seeded as `4096` UTF-8 bytes while the generic Grok video preset remains unlimited.

Video Prompt production is separate from video execution. The Script Worker deterministically compiles a persisted `PromptContextPlan` from the complete episode authority, current scene and shot, adjacent summaries, manuals, ShotState, transition, typed ReferencePack, Profile Prompt Contract, and selected model limits. Prompt generation and independent review use the same context hash and persist an immutable approved `video_prompt_plans` revision. The complete episode provides plot continuity but is not an audio source: only the current shot's timing-assigned dialogue cues may define dialogue, narration, voiceover, or system audio. Chinese dialogue is preserved verbatim. Image prompts reject dialogue content.

Video create never runs a Prompt Agent. The Workflow submits the IDs and hashes of an approved Prompt Plan and Render Plan; Gateway reloads the authoritative generation, binding revision, Profile version, capability attestation, ShotState, ReferencePack, PromptContextPlan, dialogue/audio contract, and canonical input roles from PostgreSQL. Any stale hash, non-active generation, unapproved capability snapshot, incompatible Input Contract, or changed Prompt returns a normalized replan/stale error before the upstream call. Gateway performs the final prompt-length, duration, aspect-ratio, native-audio, and reference-role checks after model selection.

## Video Production Profiles And Input Contracts

Provider model video capabilities use structured `videoGenerationVariants`. Each variant declares the request mode, duration/aspect/quality limits, native audio support, and a canonical initial/continuation `inputContract`. Runtime adapters map canonical roles to provider-specific fields only after Gateway validation; Workers never send provider field names.

The four built-in Profile families use these runtime contracts:

- `single_frame_i2v`: approved `first_frame` for the initial segment; a later segment may use explicit `video_extension` or a fresh same-shot `observed_tail_frame` contract.
- `first_last_frame`: ordered `first_frame` and `last_frame`; the selected model must natively support both and the shot must fit one model request.
- `multimodal_reference`: typed image/video/audio references retain canonical role and semantics through routing and adapter mapping.
- `storyboard_sheet`: the approved `storyboard_sheet` and `PanelManifest` remain a semantic multi-timepoint reference and must never be mapped as a first frame.

Capability declarations and approvals are separate. An `inferred` capability snapshot can enter automatic production only after an organization administrator approves its exact hash in `provider_model_capability_attestations`; changing the capability snapshot invalidates the approval without rewriting Provider account, model, capability, or binding configuration.

Stored image references are signed with `S3_PUBLIC_ENDPOINT` before they are sent to an external video provider. For server deployments this must be an object-storage URL that the upstream provider can reach; Docker-only names such as `minio` and local addresses such as `localhost` are rejected before task creation. `CINEWEAVE_ALLOW_PRIVATE_PROVIDER_REFERENCE_URLS=true` is available only for controlled providers that share the same private network.

Provider-returned image and video URLs use the shared `MediaFetcher`. It resolves and validates every A/AAAA address, pins the validated address set into the connection dialer, revalidates redirects, forbids HTTPS downgrade, ignores generic proxy environment variables, enforces MIME/byte/time limits, and streams output through a temporary file into object storage. Private targets require account config containing both an exact host and matching CIDR:

```json
{
  "mediaEgress": {
    "allowedPrivateHosts": ["media.internal.example"],
    "allowedPrivateCidrs": ["10.20.30.0/24"]
  }
}
```

There is no process-wide switch that disables private-network protection.

## Logical Requests And Idempotency

Every text, image, audio, video create/poll/cancel, and model-discovery execution is represented by a durable `provider_requests` row. An idempotency key is scoped by organization and task type and is bound to a canonical request hash.

- The first request creates one logical request and one or more numbered provider-call attempts.
- Replaying the same key and request hash returns the persisted successful result without another upstream call or cost record.
- Reusing a key with a different request hash returns `PROVIDER_IDEMPOTENCY_CONFLICT`.
- A duplicate received while the first execution is active returns `PROVIDER_REQUEST_IN_PROGRESS` and never starts a second upstream call.
- A terminal failed, cancelled, or `unknown_outcome` logical request is not restarted implicitly. Callers must set `options.retry=true`, which increments `attemptGeneration` while preserving the logical request ID. This is separate from bounded transient upstream attempts performed while one logical request is still executing.
- Gateway writes the `provider_call_logs` row with `running` before it calls the guard or upstream provider. Stale running requests and calls are reconciled to `unknown_outcome`; they are never relabeled as ordinary failures.
- `cost_records.provider_call_id` is unique, so replay, process recovery, and duplicate terminal writes cannot charge the same provider call twice.

Runtime responses expose `providerRequestId` and `attemptGeneration`; call logs additionally expose `attemptSequence`. These identifiers are the supported correlation path for retry, audit, and incident reconciliation.

## Provider Catalog

Provider Catalog is the onboarding layer for upstream AI platforms. Catalog APIs are:

- `GET /api/provider-catalog`
- `GET /api/provider-catalog/{providerKey}`
- `POST /api/provider-catalog/{providerKey}/install`

Catalog installation requires `provider.manage` and only creates local resources: provider connector, account, encrypted credential, models, capabilities, and optional Model Profile bindings. It must not call upstream services. Runtime execution, credential decryption, rate limits, logs, cost records, media transfer, and fallback remain inside Provider Gateway.

Provider model deletion physically removes the live model configuration after confirming there are no active async tasks or unexpired leases. Configuration children such as profile bindings, capability declarations, limits, and credential availability mappings are removed with it. Historical provenance is never cascaded away: call logs, cost records, async-task history, Render Plans, render segments, and capability attestations remain, while their `provider_model_id` becomes `NULL`. A Render Plan keeps its `capability_attestation_id`, and the deletion tombstone preserves the deleted model identity. New schema references to `provider_models` must follow this live-configuration versus historical-provenance split.

Seeded presets are `deepseek`, `volcengine_ark_text`, `volcengine_seedream_image`, `volcengine_seedance_video`, `kling_video`, and `openai_compatible_custom`. DeepSeek and Volcengine text use the OpenAI-compatible runtime. Volcengine image/video and Kling video use Declarative Manifest presets with editable base URL and endpoint paths. Example manifests live under `examples/providers/`.

## Provider Guard

Provider Guard runs inside Provider Gateway before text, image, TTS, ASR, video create-task, video poll-task, and video cancel-task calls. API Server and Workers do not enforce provider rate limits and must not call upstream providers directly.

- `provider_limit_policies` configures limits by organization, provider account, provider model, and task type. Matching priority is model+task, model+any, account+task, account+any, organization+task, organization+any.
- `provider_leases` records active call leases and is checked transactionally with an advisory lock before a new upstream call starts.
- Requests per minute/day are counted from `provider_call_logs`.
- Daily and monthly budgets are counted from `cost_records`.
- `provider_circuit_states` opens after configured failures, transitions to `half_open` after cooldown, and closes on a successful half-open call.
- Guard-blocked calls are persisted to `provider_call_logs` with `status=blocked` and a normalized error such as `PROVIDER_CONCURRENCY_LIMITED`, `PROVIDER_RATE_LIMITED`, `PROVIDER_DAILY_QUOTA_EXCEEDED`, `PROVIDER_MONTHLY_BUDGET_EXCEEDED`, or `PROVIDER_CIRCUIT_OPEN`. Blocked calls never write `cost_records`.

## Model Profile Routing

Routing and fallback are owned by Provider Gateway. API Server, Workers, and Activities pass either `providerModelId` for an explicit one-model call or `modelProfileKey` for profile routing.

- Supported profile strategies are `priority`, `priority_with_fallback`, `weighted`, `cost_optimized`, and `latency_optimized`.
- `fallback_strategy` controls `enabled`, `maxAttempts`, `fallbackOn`, and `stopOn`. Empty strategy defaults to three attempts and fallback for guard/rate/timeout/internal failures.
- `text.generate`, `text.stream`, `image.generate`, `audio.tts`, `audio.transcribe`, and `video.create_task` can route across profile candidates. `video.poll_task` and `video.cancel_task` are pinned to the `provider_async_tasks` model/account.
- Every attempt writes `provider_call_logs`. Failed image/video-create candidates write logs only; artifacts, media files, async tasks, and cost records are created by the successful candidate.
- `text.generate` and `text.stream` retry the same selected model up to three additional times for HTTP `408` and streams that close before their required completion marker. Each retry has a new `attemptSequence`, call log, guard evaluation, and cost record; exhausted retries may then enter the configured model fallback policy.
- Text stream retry and fallback are allowed only before the first non-empty delta is sent. Once content has been emitted, later stream errors are returned directly because the gateway cannot retract or safely duplicate visible output.
- Gateway responses include `attempts` with provider call, model, account, binding, status, error, retryable flag, and latency.
- `model_profile_bindings.runtime_options.reasoningLevel` stores the default reasoning level for that business-model binding. The value must match the selected model's `provider_options_schema.xCapabilities.reasoningLevels`; an empty value delegates to the provider default.
- A text request may set `input.reasoningLevel` to override the binding default for that logical request. Gateway validates the effective value after each routing or fallback selection, maps it to OpenAI-compatible `reasoning_effort`, and persists the effective upstream request in `provider_call_logs.request_snapshot`.
- Model capability declarations and binding defaults are intentionally separate: capabilities describe what a model accepts, while binding runtime options describe what a specific business workflow should use.

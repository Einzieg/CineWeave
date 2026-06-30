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

The first real adapter must support `/v1/models`, `/v1/chat/completions`, `/v1/images/generations`, text generation, streaming, image generation, connection testing, auth testing, model discovery, and normalized error handling.

Image runtime v1 exposes `POST /internal/provider/image/generate` for internal service-token callers. API Server and Workers must not call image providers directly and must not download upstream media. The Gateway accepts OpenAI-compatible image responses containing either `url` or `b64_json`, then writes the generated object to S3 / MinIO before returning `artifactId`, `mediaFileId`, and `storageKey`.

`CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS=false` is the default. Set it to `true` only for controlled development providers whose media URLs resolve to localhost or private networks.

## Provider Guard

Provider Guard runs inside Provider Gateway before text, image, video create-task, video poll-task, and video cancel-task calls. API Server and Workers do not enforce provider rate limits and must not call upstream providers directly.

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
- `text.generate`, `text.stream`, `image.generate`, and `video.create_task` can route across profile candidates. `video.poll_task` and `video.cancel_task` are pinned to the `provider_async_tasks` model/account.
- Every attempt writes `provider_call_logs`. Failed image/video-create candidates write logs only; artifacts, media files, async tasks, and cost records are created by the successful candidate.
- Stream fallback is allowed only before the first delta is sent. Once content has been emitted, later stream errors are returned directly.
- Gateway responses include `attempts` with provider call, model, account, binding, status, error, retryable flag, and latency.

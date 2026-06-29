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
- Lease-based concurrency limits for expensive tasks.

The first real adapter must support `/v1/models`, `/v1/chat/completions`, `/v1/images/generations`, text generation, streaming, image generation, connection testing, auth testing, model discovery, and normalized error handling.

Image runtime v1 exposes `POST /internal/provider/image/generate` for internal service-token callers. API Server and Workers must not call image providers directly and must not download upstream media. The Gateway accepts OpenAI-compatible image responses containing either `url` or `b64_json`, then writes the generated object to S3 / MinIO before returning `artifactId`, `mediaFileId`, and `storageKey`.

`CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS=false` is the default. Set it to `true` only for local mock providers whose image URLs resolve to localhost or private networks.

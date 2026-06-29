# Provider Gateway

Provider Gateway is the only upstream AI access path.

Initial scope:

- Provider account and credential storage.
- Model and capability registry.
- Model Profile routing.
- OpenAI-compatible adapter with New API as the first real provider target.
- Standard error normalization.
- Provider call logs and cost records.
- Lease-based concurrency limits for expensive tasks.

The first real adapter must support `/v1/models`, `/v1/chat/completions`, text generation, streaming, connection testing, auth testing, model discovery, and normalized error handling.


# CineWeave Agent Guide

This file is the project-level operating guide for coding agents working in this repository. Follow it unless the user explicitly gives a newer instruction in the current thread.

## Repository Facts

- Repository root is `D:\Code\CineWeave`.
- Do not create a nested `cineweave/` or `CineWeave/` project directory.
- The project is still in active development. Do not preserve old demo behavior, old demo data, old TypeScript provider scripts, or compatibility migrations unless the user explicitly asks.
- The preferred runtime is Docker Compose for server deployment. Do not prioritize `.exe` packaging.
- Main execution documents:
  - `docs/codex-execution-plan.md`: current status, priority order, validation commands.
  - `docs/follow-up-development-plan.md`: detailed remaining development tasks.
  - `docs/provider-gateway.md`: Provider Gateway behavior and boundaries.
  - `packages/openapi/openapi.yaml`: public API contract.

## Architecture Boundaries

- API Server and Workers must not call upstream AI providers directly.
- API Server and Workers must not decrypt provider credentials.
- Provider Gateway owns:
  - credential decryption
  - model selection
  - model profile routing and fallback
  - upstream provider calls
  - normalized provider errors
  - `provider_call_logs`
  - `cost_records`
  - `provider_async_tasks`
  - leases, quotas, budget checks, and circuit breaking
- Provider Gateway runtime paths currently cover text, streaming text, image generation, async video create/poll/cancel, media transfer, call logs, and cost records.
- Media Worker may use storage and FFmpeg, but must not call Provider Gateway or provider credentials directly.
- Provider Gateway is internal on the Docker network; do not expose it to the host unless the user explicitly asks.

## Docker Compose Runtime

Use this deployment path unless the task is explicitly local-only:

```powershell
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

Current host ports:

- Web: `http://localhost:19285`
- API: `http://localhost:19288`
- Realtime: `http://localhost:19281`
- MinIO API: `http://localhost:19290`

Services that should normally stay on the Docker network only:

- PostgreSQL
- Redis
- NATS
- Temporal
- Provider Gateway
- Workers
- Event Publisher
- MinIO Console

Avoid introducing common development ports such as `3000`, `8080`, `8081`, `8082`, `5432`, `6379`, `7233`, or `9001` as host mappings unless the user approves.

## Validation Commands

Use the root test entry when broad validation is appropriate:

```powershell
pnpm run test
```

The root test script is expected to cover:

```powershell
go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
python -c "import pathlib, yaml; yaml.safe_load(pathlib.Path('packages/openapi/openapi.yaml').read_text(encoding='utf-8'))"
python scripts/check-openapi-routes.py
docker compose -f compose.yml config --quiet
```

For targeted work, run the narrowest relevant tests first, then broaden before finishing if the change touches shared behavior, API contracts, workflows, Provider Gateway, auth, or frontend routing.

After changes that affect runtime services, rebuild the app profile and verify service health:

```powershell
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

## Workflow And Agent Runtime Rules

- A Project Agent `workflow.start` step being `succeeded` means the workflow launch action succeeded. It does not mean the child workflow is complete.
- Treat a child workflow as still active if:
  - `workflow_runs.status` is not terminal, or
  - it has `workflow_node_runs` in `queued` or `running`, or
  - it has `provider_async_tasks` in `queued`, `running`, or `cancelling`.
- Do not let an Agent execute later production/read steps that depend on a previous child workflow until the previous child workflow is effectively complete by the rule above.
- Agent permission modes are part of product behavior:
  - `require_approval`: ask before risky/write/provider/workflow actions.
  - `auto_approve`: proceed for allowed actions under supervision rules.
  - `full_access`: minimize approval friction but still enforce backend permissions and state gates.
- Do not bypass RBAC or agent supervision to make a workflow "just run".
- If a workflow is stuck in `cancelling`, prefer fixing the state reconciliation path. Do not silently edit database rows unless the user explicitly asks for a one-off repair.
- For novel/event workflows, always preserve exact source chapter IDs and ordinal fields. Do not infer chapter order from display title text.

## Provider And Model Rules

- OpenAI-compatible providers are first-class and should remain the first/default channel option. This covers New API, One API, LiteLLM, OpenAI official, and other `/v1` compatible gateways.
- Supported provider expansion targets include Ollama, Baidu Qianfan, Alibaba Tongyi/DashScope, iFlytek Spark, Zhipu GLM, Google Gemini, OpenRouter, and MiniMax.
- Volcengine Ark-style channels should be modeled as a unified provider channel when appropriate; do not split image and video into fake unrelated channels if the upstream account/channel is shared.
- Provider account/model deletion is soft delete from the user point of view:
  - disabled accounts/models are hidden from default lists
  - disabled models must not appear in default model lists, routing candidates, or model tests
  - deleting a provider must disable its models and related `model_profile_bindings`
- Model discovery behavior:
  - insert newly discovered models as active
  - update display name/modality for existing non-disabled models
  - do not resurrect disabled models automatically
  - fill missing task types and default capability records by modality
- Model capability data must be structured. Do not expose raw JSON as the default UI for normal users.
- Text model capabilities should cover streaming, reasoning/thinking levels, multimodal input, token limits, supported input file types, and request mode.
- Image model capabilities should cover reference images, image edit support, reference image count, prompt limit, request mode, aspect ratios, sizes/resolutions, quality tiers, and formats.
- Video model capabilities should cover async task support, prompt limit, duration limits/options, reference image support, first/last frame support, video reference support, request mode, polling/webhook behavior, aspect ratios, quality tiers, and output formats.
- Runtime behavior, Gateway validation, OpenAPI schema, API client types, and frontend labels must stay aligned with stored model capabilities.

## Frontend Rules

- The web app lives in `apps/web`.
- Build real product screens, not placeholder landing pages.
- All visible UI labels for statuses, roles, permissions, modalities, source types, file formats, provider types, workflow states, and production steps should be Chinese user-facing text.
- Use centralized label mapping such as `apps/web/src/lib/labels.ts`. Do not scatter enum-to-label mappings across pages.
- Do not display raw internal English enum keys to normal users.
- Do not add visible notes such as "开发中", "此功能将用于...", implementation comments, debug remarks, or explanatory placeholder text.
- The main sidebar and top navigation should remain fixed/stable while the page content scrolls.
- AI Assistant should behave as a persistent workspace panel: toggling visibility must not destroy the session context unless the user explicitly starts a new conversation.
- Slash-command style assistant tools should replace always-visible quick action clutter when the user requests Codex-like command entry.
- Provider and model dialogs must not close just because a select/dropdown loses focus after opening without selection.
- Use existing design conventions, shadcn/Radix patterns, React Query, Zustand stores, and lucide icons where appropriate.

## Source, Novel, And Script Rules

- Original source content must be editable and deletable.
- Novel import must split into durable volume/chapter/section rows at ingestion time, not only at display time.
- Preserve ordinal fields such as volume index, section index, and chapter index. Event extraction must use selected chapter IDs and stored ordinals, not title matching.
- Million-character novels must be manageable by episode/section. Do not design flows that require extracting events from an entire huge source as one unit.
- Script import should initialize script/version/scene data instead of treating every script as a novel chapter split.

## OpenAPI, API Client, And Database

- When adding or changing public API routes, update:
  - `packages/openapi/openapi.yaml`
  - frontend API client/types in `apps/web/src/lib`
  - route consistency checks if an internal allowlist is needed
- API routes and OpenAPI paths must not drift.
- Add focused backend tests for behavior changes in `internal/api`, `internal/provider`, or `internal/workflows`.
- Add both `.up.sql` and `.down.sql` migrations where the repository pattern expects both.
- No old-data compatibility is required during development, but migrations should still leave a clear schema state.

## Git And Working Tree

- The worktree may be dirty. Do not revert, delete, or overwrite unrelated user changes.
- Use `rg` / `rg --files` for searching.
- Use `apply_patch` for manual file edits.
- Do not use destructive git commands such as `git reset --hard` or `git checkout --` unless explicitly requested.
- Do not commit or push unless the user asks.
- Keep local secrets and personal notes out of commits, including `.env`, credential dumps, login notes, and ad-hoc local reports.

## Implementation Standard

- Prefer coherent, product-correct implementations over tiny patches that preserve a broken model.
- Still keep scope disciplined: do not refactor unrelated modules just because they are nearby.
- If a visible user workflow is affected, verify with runtime state, HTTP/API calls, database checks, or browser checks when practical.
- Final reports should state what changed, what was verified, and any remaining risk or manual follow-up.

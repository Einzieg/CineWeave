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

## Production Update, Repair, And Deployment Checklist

Treat a production update as one immutable release unit. Editing source, building an image, replacing one container, applying a migration, or seeing a healthy process is not by itself a completed deployment. The release is complete only when source, image tags, database schema/seed, API contract, Web assets, Temporal Worker routing, and browser behavior have all been verified as the same release.

The current server deployment lives under `/soft/CineWeave`. The public application and storage entry points are `cineweave.einzieg.site` and `cineweave-s3.einzieg.site`; do not modify or take over the main domain. Follow `docs/runtime-foundation-hardening-runbook.md` for the detailed release procedure.

### Release authorization and scope

- [ ] Obtain explicit user authorization before migrating or rebuilding the main production environment. Authorization to call paid provider APIs does not automatically authorize a database migration or Compose rebuild, and vice versa.
- [ ] Record an immutable release ID before building. Use a full commit SHA or a release identifier tied to exact source and image digests. Never use `latest`, `main`, `local-dev`, or another mutable value in production.
- [ ] Record the source commit, dirty patch/untracked-file inventory, expected migration head, expected seed versions, affected services, affected Temporal Worker Deployments, and whether a paid smoke test is authorized.
- [ ] Do not overwrite a dirty production checkout. Assemble a clean release worktree/directory from a known commit plus an explicit patch and explicit untracked files. Keep the previous release directory and images until validation and Worker drainage finish.
- [ ] Never copy `.env`, secrets, credential exports, provider protection snapshots, database dumps, or login notes into Git or a release archive. Do not print fully resolved Compose configuration when it may contain secrets.
- [ ] A production-only hotfix must also be applied to the repository source, tested, and tracked for commit/push. Otherwise the next deployment will silently remove it.

### Local release gate

- [ ] Run `git status --short` and review every staged, unstaged, and untracked path. Preserve unrelated changes from other tasks.
- [ ] Run `git diff --check`.
- [ ] Run `pnpm run test`; do not deploy while the root test entry, migration/seed validation, OpenAPI route check, Web typecheck/lint, or Compose validation is failing.
- [ ] Confirm all changed public routes exist in both `packages/openapi/openapi.yaml` and the running API implementation.
- [ ] Confirm migrations and seeds are embedded in the binaries/images being deployed, and that the expected schema head comes from the same release source.
- [ ] Build every service affected by a shared contract. If a feature spans Web, API, migration, Provider Gateway, or Worker code, deploying only one of those services is prohibited.

### Production preflight and protection

- [ ] Inspect the currently running image tags/digests, `CINEWEAVE_RELEASE_ID`, database schema version, seed versions, and current/ramping Temporal Build IDs. Any mismatch is version drift and must be resolved before diagnosing product behavior.
- [ ] Validate the exact production Compose stack, including `compose.yml`, `.compose.server.yml`, and the release image override when present. Use `docker compose ... config --quiet` and `docker compose ... config --images`; avoid dumping secret-bearing resolved configuration.
- [ ] Drain or safely pause active work before replacing runtime services. The gate includes active `workflow_runs`, `workflow_node_runs`, Commerce setup/production runs, `provider_requests`, `provider_async_tasks`, and video/asset execution checkpoints in queued, running, cancelling, or waiting states.
- [ ] If active work cannot be drained, keep the compatible old Worker release running and prepare a Temporal versioned rollout. Do not force-recreate a Pinned Worker and abandon workflows assigned to its old Build ID.
- [ ] Freeze Provider configuration writes and run the current `scripts/provider-data-guard.ps1` DrainCheck/Snapshot flow when the release touches Provider, model, workflow, video, or migration behavior. Store the snapshot outside Git.
- [ ] Create a non-empty PostgreSQL custom-format backup before applying migrations. Record its absolute server path, size, timestamp, schema version, and release ID.
- [ ] Build all release images while the old containers are still serving. Do not switch traffic to a partially built release.

### Migration and service rollout

- [ ] Set `CINEWEAVE_ENV=production` and the immutable `CINEWEAVE_RELEASE_ID` for migration, seed, application, and Worker containers.
- [ ] For a backward-incompatible migration, stop or freeze every writer before migration. Do not allow old API/Worker containers to write against an incompatible new schema.
- [ ] Run the release-specific `migrate` image, then migration `verify`. Run `seed apply` and `seed verify` when the release changes system templates, prompts, profiles, permissions, or other managed seed data.
- [ ] Confirm the database reached the exact expected migration head. Never edit the migration ledger or database rows to make a failed release appear successful.
- [ ] Recreate the complete affected service set as one release. A Web/API/Worker contract change must not leave any of those components on an older image.
- [ ] Wait for Provider Gateway readiness before starting callers that depend on it. Then verify API, Realtime, Event Publisher, Workers, Web, storage, and supporting services.
- [ ] Treat successful one-shot exits for `migrate`, `seed`, `temporal-schema`, `temporal-namespace`, and `minio-create-bucket` as expected only after their exit status and logs have been checked.

### Temporal Worker release gate

- [ ] Verify each Worker startup log reports the intended immutable Build ID and expected Worker Deployment name.
- [ ] Run the repository `temporal-release check/ramp/promote` flow for every changed Worker Deployment. A healthy Worker container does not mean new workflows are routed to it.
- [ ] Confirm the deployment current Build ID equals the release ID after promotion. For canary rollout, also verify the ramping Build ID and percentage.
- [ ] Start a zero-cost canary workflow when available and verify its workflow/build assignment. Do not use a real paid provider merely to prove Worker routing unless the current task explicitly authorizes that spend.
- [ ] Keep the prior Pinned Worker version available until `temporal-release drain` reports it safe to decommission. Never delete Temporal data to clear a release mismatch.

### Post-deployment verification

- [ ] Run `docker compose ... ps`; every long-running required service must be `Up`, and every service with a healthcheck must be `healthy`.
- [ ] Verify API `/healthz` and `/readyz`, Realtime health, Web HTTP status, MinIO health, and Provider Gateway health/readiness from inside the Docker network.
- [ ] Verify the schema head, seed hashes, running image tags/digests, container release labels/environment, and Temporal current Build IDs all resolve to the same release.
- [ ] Smoke each changed API route. An unauthenticated request may return `401` or `403`, but it must not return `404` when the route should exist.
- [ ] For Web changes, verify the new production asset/chunk is being served, then perform a hard reload or reopen the browser tab and exercise the exact changed control. Static bundle text alone is not sufficient browser acceptance.
- [ ] Verify changed dialogs, mutations, realtime invalidation, task progress, and terminal states in the browser. Confirm the UI is not an old cached build before reopening a backend investigation.
- [ ] Inspect recent logs for panic, fatal errors, migration failures, repeated retries, Temporal nondeterminism, provider polling loops, and authorization failures.
- [ ] Recheck active workflow/provider/task counts after smoke. A task card marked completed must agree with its item/checkpoint/provider terminal states.
- [ ] If a paid smoke test is authorized, record the exact project, workflow, provider request/call/task IDs, outcome, and spend boundary. Otherwise use preflight-only or local contract tests.
- [ ] Run Provider protection `Verify` before unfreezing Provider writes. Any unexpected configuration/history difference blocks reopening production traffic.

### Failure and rollback rules

- [ ] If image build, migration, seed, readiness, Worker promotion, or browser acceptance fails, stop the release and preserve logs/state. Do not continue through later checklist items hoping the system will self-correct.
- [ ] Before any paid task starts, roll back application image tags and Temporal routing to the recorded prior release when runtime verification fails.
- [ ] Database rollback requires an explicitly reviewed down migration or backup restore plan and user authorization. Never improvise destructive SQL in production.
- [ ] Do not mark stuck workflow/provider rows successful or delete them manually as a normal repair. Fix reconciliation/finalization, then use an explicitly approved one-off repair only when necessary.
- [ ] Keep the previous release worktree, image tags/digests, backup, Provider snapshot, and prior Worker Build IDs until the new release has passed smoke and old workflows have drained.

### Required completion report

Every production deployment report must include:

- release ID and source commit/patch identity
- schema and seed versions before and after
- backup path and verification
- services rebuilt and exact image tags/digests
- Temporal Deployment current/ramping/previous Build IDs
- tests, readiness checks, API smoke, and browser smoke performed
- active workflow/provider task counts before and after
- whether a real paid provider call was made
- remaining risk and exact rollback target

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

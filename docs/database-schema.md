# Database Schema

The source of truth for the first schema pass is `dev_docs/cineweave-technical-spec-codex-reviewed-v4.md`.

Migration order:

1. organizations / users / auth_sessions
2. roles / permissions / role_permissions
3. organization_members / teams / team_members / workspaces
4. projects / project_members / role_bindings
5. provider_connectors / provider_accounts / provider_credentials / provider_models / provider_model_capabilities
6. model_profiles / model_profile_bindings / provider_endpoints / provider_test_runs
7. workflow_templates / workflow_template_nodes
8. workflow_runs / workflow_node_runs / artifacts
9. prompt_templates / prompt_versions
10. provider_call_logs / provider_async_tasks / cost_records / provider_leases / event_outbox
11. novels / scripts / storyboards / storyboard_shots / assets / media_files
12. indexes / constraints / seeds

Provider logs must be created after prompt versions, model profiles, and provider credentials because they reference those tables.

Migration notes:

- `000002_provider_core` creates `idempotency_keys`, `prompt_templates`, `prompt_versions`, provider call logs, async provider task tracking, cost records, leases, and `event_outbox`.
- `000003_creative_domain` adds the v4 creative domain tables: `novels`, `novel_chapters`, `novel_events`, `scripts`, `script_versions`, `storyboards`, `storyboard_shots`, `assets`, `asset_relations`, `media_files`, and `media_variants`.
- `scripts.current_version_id` is added after `script_versions` through an idempotent constraint block to avoid the circular table dependency.
- Asset API writes user-managed project assets to `assets`. Uploaded/derived media is represented through an `artifacts` row, a `media_files` row, and a `media_variants` row; `assets.current_artifact_id` points at the latest artifact.
- `000008_provider_limits` adds `provider_limit_policies` and `provider_circuit_states`, extends `provider_leases` with `lease_token`, and allows `provider_call_logs.status=blocked` for Gateway guard rejections.
- Provider budget enforcement reads `cost_records`; blocked provider calls are logged but intentionally do not write cost rows.
- `000009_model_profile_routing` expands `model_profiles.routing_strategy` to `priority`, `priority_with_fallback`, `weighted`, `cost_optimized`, and `latency_optimized`, and changes the default to `priority_with_fallback`.
- `000010_prompt_registry` upgrades the early prompt table shape into Prompt Registry: `prompt_templates` gains purpose/modality/task/scope/status fields, `prompt_versions` gains active/draft versioning fields, `prompt_bindings` adds project/organization overrides, default storyboard prompts are seeded, and `prompt.read` / `prompt.manage` permissions are granted.
- `000011_storyboard_shots` upgrades `storyboard_shots` for workflow production by adding `workflow_run_id`, storyboard artifact linkage, normalized prompt fields, per-shot image/video artifact/media/storage fields, provider async task fields, status indexes, and a `(workflow_run_id, shot_index)` unique key.

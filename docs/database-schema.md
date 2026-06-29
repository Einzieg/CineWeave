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

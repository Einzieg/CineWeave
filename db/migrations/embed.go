package migrations

import "embed"

// FS contains only the post-reset migration history. The pre-baseline SQL
// files can coexist in a dirty worktree without becoming executable.
//
//go:embed 000001_current_schema.sql 000002_runtime_hardening.sql 000003_provider_requests_and_workflow_outbox.sql 000004_asset_batch_runtime.sql 000005_runtime_contract_remediation.sql 000006_workflow_execution_fencing.sql 000007_project_event_contract.sql 000008_model_profile_binding_runtime_options.sql
var FS embed.FS

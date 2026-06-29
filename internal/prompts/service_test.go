package prompts

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServiceResolvePriority(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run prompt service integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for prompt service integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	orgID, userID, projectID := seedPromptResolveOrg(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	systemVersionID := promptVersionIDByKey(t, ctx, pool, "storyboard_planner")
	orgVersionID := insertPromptTemplateVersion(t, ctx, pool, orgID, userID, "storyboard_planner", "Organization {{ input.prompt }}")
	projectVersionID := insertPromptTemplateVersion(t, ctx, pool, orgID, userID, "storyboard_planner_project", "Project {{ input.prompt }}")
	insertPromptBinding(t, ctx, pool, orgID, "", "storyboard_planner", orgVersionID, userID)

	service := NewService(pool)
	resolved, err := service.Resolve(ctx, ResolveRequest{OrganizationID: orgID, ProjectID: projectID, TemplateKey: "storyboard_planner"})
	if err != nil {
		t.Fatalf("Resolve organization binding: %v", err)
	}
	if resolved.VersionID != orgVersionID || resolved.Source != "organization_binding" || resolved.VersionID == systemVersionID {
		t.Fatalf("resolved organization binding = %+v", resolved)
	}

	insertPromptBinding(t, ctx, pool, orgID, projectID, "storyboard_planner", projectVersionID, userID)
	resolved, err = service.Resolve(ctx, ResolveRequest{OrganizationID: orgID, ProjectID: projectID, TemplateKey: "storyboard_planner"})
	if err != nil {
		t.Fatalf("Resolve project binding: %v", err)
	}
	if resolved.VersionID != projectVersionID || resolved.Source != "project_binding" {
		t.Fatalf("resolved project binding = %+v", resolved)
	}
}

func seedPromptResolveOrg(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	suffix := uuid.NewString()
	var orgID, userID, workspaceID, projectID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, "Prompt Resolve", "prompt-resolve-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, 'Prompt User') RETURNING id`, "prompt-resolve-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id) VALUES ($1, $2)`, orgID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Prompt Workspace') RETURNING id`, orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(organization_id, workspace_id, name, created_by) VALUES ($1, $2, 'Prompt Project', $3) RETURNING id`, orgID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return orgID, userID, projectID
}

func promptVersionIDByKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, templateKey string) string {
	t.Helper()
	var versionID string
	if err := pool.QueryRow(ctx, `
		SELECT pv.id::text
		FROM prompt_templates pt
		JOIN prompt_versions pv ON pv.template_id = pt.id
		WHERE pt.organization_id IS NULL AND pt.template_key = $1 AND pv.status = 'active'
		LIMIT 1
	`, templateKey).Scan(&versionID); err != nil {
		t.Fatalf("select prompt version: %v", err)
	}
	return versionID
}

func insertPromptTemplateVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, userID, templateKey, content string) string {
	t.Helper()
	var templateID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO prompt_templates(organization_id, template_key, name, purpose, modality, task_type, scope, status, is_system, created_by)
		VALUES ($1, $2, $2, 'test', 'text', 'text.generate', 'organization', 'active', false, $3)
		RETURNING id
	`, orgID, templateKey, userID).Scan(&templateID); err != nil {
		t.Fatalf("insert prompt template: %v", err)
	}
	var versionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO prompt_versions(prompt_template_id, template_id, version_no, version, status, content, content_format, content_hash, activated_at, created_by)
		VALUES ($1, $1, 1, 1, 'active', $2, 'text', 'sha256:test', now(), $3)
		RETURNING id::text
	`, templateID, content, userID).Scan(&versionID); err != nil {
		t.Fatalf("insert prompt version: %v", err)
	}
	return versionID
}

func insertPromptBinding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, templateKey, versionID, userID string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO prompt_bindings(organization_id, project_id, template_key, prompt_version_id, created_by)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5)
	`, orgID, projectID, templateKey, versionID, userID)
	if err != nil {
		t.Fatalf("insert prompt binding: %v", err)
	}
}

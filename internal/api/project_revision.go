package api

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// lockProjectConfigurationTx is the single serialization point shared by
// project configuration writers and workflow snapshot creators.
func lockProjectConfigurationTx(ctx context.Context, tx pgx.Tx, projectID, organizationID string) (int64, error) {
	var actualOrganizationID string
	var revision int64
	if err := tx.QueryRow(ctx, `
		SELECT organization_id::text, revision
		FROM projects
		WHERE id = $1
		FOR UPDATE
	`, projectID).Scan(&actualOrganizationID, &revision); err != nil {
		return 0, err
	}
	if organizationID != "" && actualOrganizationID != organizationID {
		return 0, newAPIError(http.StatusNotFound, "NOT_FOUND", "project was not found")
	}
	return revision, nil
}

func bumpProjectRevisionTx(ctx context.Context, tx pgx.Tx, projectID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE projects
		SET revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, projectID)
	return err
}

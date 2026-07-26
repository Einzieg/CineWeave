package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const CodeProjectDeletionInProgress = "PROJECT_DELETION_IN_PROGRESS"

func (s *Service) assertProviderProjectWritable(ctx context.Context, organizationID, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	// Internal media-layout tests call the pure storage preparation helpers
	// without constructing the Provider runtime. Every real Gateway entry point
	// requires a database-backed Service and performs this check before upstream.
	if s == nil || s.db == nil {
		return nil
	}
	var lifecycleStatus string
	if err := s.db.QueryRow(ctx, `
		SELECT lifecycle_status
		FROM projects
		WHERE id = $1 AND organization_id = $2
	`, projectID, strings.TrimSpace(organizationID)).Scan(&lifecycleStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &StandardErrorError{Standard: StandardError{
				Code:      CodeProjectDeletionInProgress,
				Message:   "项目已删除或正在删除，供应商请求已停止",
				Retryable: false,
			}}
		}
		return err
	}
	if lifecycleStatus == "deleting" {
		return &StandardErrorError{Standard: StandardError{
			Code:      CodeProjectDeletionInProgress,
			Message:   "项目正在删除，供应商请求已停止",
			Retryable: false,
		}}
	}
	return nil
}

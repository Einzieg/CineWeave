package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var billingSnapshotHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type GatewayBillingIdentityResolutionRequest struct {
	OrganizationID string                 `json:"organizationId"`
	ProjectID      string                 `json:"projectId,omitempty"`
	WorkflowRunID  string                 `json:"workflowRunId,omitempty"`
	TaskType       string                 `json:"taskType"`
	IdempotencyKey string                 `json:"idempotencyKey"`
	Identity       GatewayBillingIdentity `json:"identity"`
}

type GatewayBillingIdentityResolver interface {
	ResolveGatewayBillingIdentity(
		context.Context,
		GatewayBillingIdentityResolutionRequest,
	) (GatewayBillingIdentity, error)
}

type passthroughGatewayBillingIdentityResolver struct{}

func (passthroughGatewayBillingIdentityResolver) ResolveGatewayBillingIdentity(
	_ context.Context,
	request GatewayBillingIdentityResolutionRequest,
) (GatewayBillingIdentity, error) {
	return request.Identity, nil
}

func (s *Service) resolveGatewayBillingIdentity(
	ctx context.Context,
	organizationID string,
	projectID string,
	workflowRunID string,
	taskType string,
	idempotencyKey string,
	identity GatewayBillingIdentity,
) (GatewayBillingIdentity, error) {
	resolver := s.billingIdentity
	if resolver == nil {
		resolver = passthroughGatewayBillingIdentityResolver{}
	}
	resolved, err := resolver.ResolveGatewayBillingIdentity(
		ctx,
		GatewayBillingIdentityResolutionRequest{
			OrganizationID: strings.TrimSpace(organizationID),
			ProjectID:      strings.TrimSpace(projectID),
			WorkflowRunID:  strings.TrimSpace(workflowRunID),
			TaskType:       strings.TrimSpace(taskType),
			IdempotencyKey: strings.TrimSpace(idempotencyKey),
			Identity:       identity,
		},
	)
	if err != nil {
		return GatewayBillingIdentity{}, normalizeBillingAuthorizationError(err)
	}
	return normalizeGatewayBillingIdentity(
		organizationID,
		projectID,
		resolved,
	)
}

func normalizeGatewayBillingIdentity(
	organizationID string,
	projectID string,
	identity GatewayBillingIdentity,
) (GatewayBillingIdentity, error) {
	identity.RequestedByUserID = strings.TrimSpace(identity.RequestedByUserID)
	identity.BillingContextID = strings.TrimSpace(identity.BillingContextID)
	identity.BillingContextSnapshotHash = strings.ToLower(
		strings.TrimSpace(identity.BillingContextSnapshotHash),
	)
	identity.BillingOperationPermission = strings.TrimSpace(
		identity.BillingOperationPermission,
	)
	identity.BillingContextReason = strings.TrimSpace(
		identity.BillingContextReason,
	)
	contextPresent := identity.BillingContextID != "" ||
		identity.BillingContextRevision != 0 ||
		identity.BillingContextSnapshotHash != ""
	if !contextPresent {
		if identity.RequestedByUserID != "" {
			parsed, err := uuid.Parse(identity.RequestedByUserID)
			if err != nil {
				return GatewayBillingIdentity{}, fmt.Errorf(
					"%w: requestedByUserId is invalid",
					ErrValidation,
				)
			}
			identity.RequestedByUserID = parsed.String()
		}
		return identity, nil
	}
	if strings.TrimSpace(organizationID) == "" ||
		strings.TrimSpace(projectID) == "" ||
		identity.RequestedByUserID == "" ||
		identity.BillingContextID == "" ||
		identity.BillingContextRevision < 1 ||
		!billingSnapshotHashPattern.MatchString(
			identity.BillingContextSnapshotHash,
		) {
		return GatewayBillingIdentity{}, fmt.Errorf(
			"%w: commercial billing context identity is incomplete",
			ErrValidation,
		)
	}
	requestedBy, err := uuid.Parse(identity.RequestedByUserID)
	if err != nil {
		return GatewayBillingIdentity{}, fmt.Errorf(
			"%w: requestedByUserId is invalid",
			ErrValidation,
		)
	}
	contextID, err := uuid.Parse(identity.BillingContextID)
	if err != nil {
		return GatewayBillingIdentity{}, fmt.Errorf(
			"%w: billingContextId is invalid",
			ErrValidation,
		)
	}
	identity.RequestedByUserID = requestedBy.String()
	identity.BillingContextID = contextID.String()
	return identity, nil
}

func (identity GatewayBillingIdentity) reference() *editionpkg.BillingContextReference {
	if identity.BillingContextID == "" {
		return nil
	}
	return &editionpkg.BillingContextReference{
		ID:           identity.BillingContextID,
		Revision:     identity.BillingContextRevision,
		SnapshotHash: identity.BillingContextSnapshotHash,
	}
}

func applyBillingIdentityToProviderRequest(
	input *providerRequestStartInput,
	identity GatewayBillingIdentity,
) {
	input.RequestedByUserID = identity.RequestedByUserID
	input.BillingContextID = identity.BillingContextID
	input.BillingContextRevision = identity.BillingContextRevision
	input.BillingContextSnapshotHash = identity.BillingContextSnapshotHash
}

func (s *Service) authorizeCredentialForModel(
	ctx context.Context,
	organizationID string,
	projectID string,
	identity GatewayBillingIdentity,
	account Account,
	model Model,
) (string, error) {
	candidates, err := s.billingCredentialCandidates(
		ctx,
		organizationID,
		account.ID,
		model.ID,
	)
	if err != nil {
		var denial editionpkg.AuthorizationError
		if errors.As(err, &denial) {
			return "", &StandardErrorError{Standard: StandardError{
				Code:      string(denial.Code),
				Message:   denial.Message,
				Retryable: denial.Retryable,
			}}
		}
		return "", err
	}
	decision, err := s.billingRouting.Authorize(
		ctx,
		editionpkg.BillingRoutingRequest{
			OrganizationID:    organizationID,
			ProjectID:         strings.TrimSpace(projectID),
			RequestedByUserID: identity.RequestedByUserID,
			ProviderModelID:   model.ID,
			BillingContext:    identity.reference(),
			Candidates:        candidates,
		},
	)
	if err != nil {
		return "", normalizeBillingAuthorizationError(err)
	}
	available := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		available[candidate.CredentialID] = true
	}
	for _, credentialID := range decision.AllowedCredentialIDs {
		credentialID = strings.TrimSpace(credentialID)
		if available[credentialID] {
			return credentialID, nil
		}
	}
	return "", &StandardErrorError{Standard: StandardError{
		Code:      string(editionpkg.DenialBillingRoutingCandidateMissing),
		Message:   "no provider credential satisfies the billing routing constraints",
		Retryable: false,
	}}
}

func normalizeBillingAuthorizationError(err error) error {
	var denial editionpkg.AuthorizationError
	if !errors.As(err, &denial) {
		return err
	}
	return &StandardErrorError{Standard: StandardError{
		Code:      string(denial.Code),
		Message:   denial.Message,
		Retryable: denial.Retryable,
	}}
}

func (s *Service) billingCredentialCandidates(
	ctx context.Context,
	organizationID string,
	accountID string,
	modelID string,
) ([]editionpkg.BillingRoutingCandidate, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			credential.id::text,
			credential.provider_account_id::text,
			credential.organization_id::text,
			COALESCE(managed.management_scope, 'tenant_managed'),
			COALESCE(managed.management_reference, '')
		FROM provider_credentials credential
		JOIN provider_models model
		  ON model.id = $3
		 AND model.provider_account_id = credential.provider_account_id
		LEFT JOIN provider_credential_models availability
		  ON availability.provider_credential_id = credential.id
		 AND availability.provider_model_id = model.id
		LEFT JOIN provider_managed_credentials managed
		  ON managed.provider_credential_id = credential.id
		WHERE credential.organization_id = $1
		  AND credential.provider_account_id = $2
		  AND credential.status = 'active'
		  AND credential.is_active
		  AND (
		      availability.is_available
		      OR NOT EXISTS (
		          SELECT 1
		          FROM provider_credential_models any_mapping
		          JOIN provider_credentials mapped_credential
		            ON mapped_credential.id = any_mapping.provider_credential_id
		          WHERE mapped_credential.provider_account_id = $2
		            AND any_mapping.provider_model_id = $3
		      )
		  )
		ORDER BY credential.credential_key, credential.created_at DESC
	`, organizationID, accountID, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]editionpkg.BillingRoutingCandidate, 0)
	for rows.Next() {
		var candidate editionpkg.BillingRoutingCandidate
		if err := rows.Scan(
			&candidate.CredentialID,
			&candidate.ProviderAccountID,
			&candidate.OrganizationID,
			&candidate.ManagementScope,
			&candidate.ConstraintRef,
		); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, pgx.ErrNoRows
	}
	return candidates, nil
}

func (s *Service) listLogicalModelEquivalents(
	ctx context.Context,
	organizationID string,
	model Model,
) ([]Model, error) {
	rows, err := s.db.Query(ctx, `
		SELECT candidate.id::text
		FROM provider_models candidate
		JOIN provider_accounts account
		  ON account.id = candidate.provider_account_id
		WHERE account.organization_id = $1
		  AND account.status = 'active'
		  AND candidate.status = 'active'
		  AND lower(btrim(candidate.model_key)) = lower(btrim($2))
		ORDER BY (candidate.id = $3::uuid) DESC, candidate.id
	`, organizationID, model.ModelKey, model.ID)
	if err != nil {
		return nil, err
	}
	modelIDs := make([]string, 0)
	for rows.Next() {
		var modelID string
		if err := rows.Scan(&modelID); err != nil {
			rows.Close()
			return nil, err
		}
		modelIDs = append(modelIDs, modelID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	result := make([]Model, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		candidate, err := s.GetModel(ctx, organizationID, modelID)
		if err != nil {
			return nil, err
		}
		if candidate.Modality != model.Modality {
			continue
		}
		result = append(result, candidate)
	}
	return result, nil
}

func billingRoutingCandidateUnavailable(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	standard, ok := StandardErrorFromError(err)
	if !ok {
		return false
	}
	switch standard.Code {
	case string(editionpkg.DenialBillingCredentialUnavailable),
		string(editionpkg.DenialBillingModelForbidden),
		string(editionpkg.DenialBillingRoutingCandidateMissing):
		return true
	default:
		return false
	}
}

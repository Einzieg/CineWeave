package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Service) resolveVideoCapabilityAttestation(
	ctx context.Context,
	organizationID string,
	providerModelID string,
	variant VideoGenerationVariant,
	capabilitySnapshotHash string,
) (string, error) {
	status := normalizeVideoCapabilityVerification(variant.VerificationStatus, variant.Source)
	if status == VideoCapabilityVerificationOfficial || status == VideoCapabilityVerificationTested {
		evidenceType := "official_documentation"
		if status == VideoCapabilityVerificationTested {
			evidenceType = "adapter_contract_test"
		}
		evidence, err := json.Marshal(map[string]string{
			"source": variant.Source, "sourceUrl": variant.SourceURL,
			"verifiedAt": variant.VerifiedAt, "capabilityVersion": variant.CapabilityVersion,
		})
		if err != nil {
			return "", err
		}
		if _, err := s.db.Exec(ctx, `
			INSERT INTO provider_model_capability_attestations(
				id, organization_id, provider_model_id, variant_key, capability_snapshot_hash,
				verification_status, evidence_type, evidence, decision, reason
			)
			VALUES (
				md5('cineweave:video-capability-attestation:auto:' || $2::uuid::text || ':' || $3 || ':' || $4)::uuid,
				$1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb, 'approved', 'system verified capability evidence'
			)
			ON CONFLICT DO NOTHING
		`, organizationID, providerModelID, variant.VariantKey, capabilitySnapshotHash, status, evidenceType, evidence); err != nil {
			return "", err
		}
	}

	var id, decision, attestedVerificationStatus string
	err := s.db.QueryRow(ctx, `
		SELECT id::text, decision, verification_status
		FROM provider_model_capability_attestations
		WHERE organization_id = $1
		  AND provider_model_id = $2
		  AND variant_key = $3
		  AND capability_snapshot_hash = $4
		  AND revoked_at IS NULL
		ORDER BY decided_at DESC, id DESC
		LIMIT 1
	`, organizationID, providerModelID, variant.VariantKey, capabilitySnapshotHash).Scan(&id, &decision, &attestedVerificationStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		if status == VideoCapabilityVerificationUnknown {
			return "", videoCapabilityApprovalRequired("视频模型能力来源未知，必须先完成 Adapter 验证或受控探测")
		}
		return "", videoCapabilityApprovalRequired("视频模型能力为推断结果，需要组织管理员批准当前能力快照")
	}
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(decision, "approved") {
		return "", videoCapabilityApprovalRequired("当前视频模型能力快照已被组织管理员拒绝")
	}
	if status == VideoCapabilityVerificationUnknown && attestedVerificationStatus != VideoCapabilityVerificationTested && attestedVerificationStatus != VideoCapabilityVerificationOfficial {
		return "", videoCapabilityApprovalRequired("未知来源能力只能通过 Adapter 验证或受控探测启用")
	}
	return id, nil
}

func videoCapabilityApprovalRequired(message string) error {
	return &StandardErrorError{Standard: StandardError{
		Code: CodeModelCapabilityApprovalRequired, Message: message, Retryable: false,
	}}
}

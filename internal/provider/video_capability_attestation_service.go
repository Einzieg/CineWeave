package provider

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/jackc/pgx/v5"
)

func (s *Service) ListVideoCapabilityAttestations(ctx context.Context, organizationID, modelID string) (VideoCapabilityAttestationList, error) {
	model, variants, hashes, err := s.currentVideoCapabilityVariants(ctx, organizationID, modelID)
	if err != nil {
		return VideoCapabilityAttestationList{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, organization_id::text, provider_model_id::text, variant_key,
		       capability_snapshot_hash, verification_status, evidence_type, evidence,
		       decision, reason, decided_by::text, decided_at, supersedes_attestation_id::text,
		       revoked_by::text, revoked_at, created_at
		FROM provider_model_capability_attestations
		WHERE organization_id = $1 AND provider_model_id = $2
		ORDER BY decided_at DESC, id DESC
	`, organizationID, model.ID)
	if err != nil {
		return VideoCapabilityAttestationList{}, err
	}
	defer rows.Close()
	result := VideoCapabilityAttestationList{
		Variants:     make([]VideoCapabilityVariantStatus, 0, len(variants)),
		Attestations: make([]VideoCapabilityAttestation, 0),
	}
	activeBySnapshot := make(map[string]*VideoCapabilityAttestation)
	for rows.Next() {
		item, err := scanVideoCapabilityAttestation(rows)
		if err != nil {
			return VideoCapabilityAttestationList{}, err
		}
		item.CurrentSnapshot = hashes[item.VariantKey] == item.CapabilitySnapshotHash
		item.Active = item.RevokedAt == nil
		result.Attestations = append(result.Attestations, item)
		if item.Active && item.CurrentSnapshot {
			copy := item
			activeBySnapshot[item.VariantKey+":"+item.CapabilitySnapshotHash] = &copy
		}
	}
	if err := rows.Err(); err != nil {
		return VideoCapabilityAttestationList{}, err
	}
	for _, variant := range variants {
		hash := hashes[variant.VariantKey]
		result.Variants = append(result.Variants, VideoCapabilityVariantStatus{
			VariantKey: variant.VariantKey, CapabilitySnapshotHash: hash,
			VerificationStatus: variant.VerificationStatus, Source: variant.Source,
			SourceURL: variant.SourceURL, VerifiedAt: variant.VerifiedAt,
			InitialInputContract:  variant.InputContract,
			ContinuationContracts: append([]VideoInputContract(nil), variant.ContinuationInputContracts...),
			NativeAudio:           variant.NativeAudio, Duration: variant.Duration,
			CurrentAttestation: activeBySnapshot[variant.VariantKey+":"+hash],
		})
	}
	return result, nil
}

func (s *Service) CreateVideoCapabilityAttestation(
	ctx context.Context,
	organizationID, userID, modelID string,
	req CreateVideoCapabilityAttestationRequest,
) (VideoCapabilityAttestation, error) {
	decision := strings.ToLower(strings.TrimSpace(req.Decision))
	if decision != "approved" && decision != "rejected" {
		return VideoCapabilityAttestation{}, fmt.Errorf("%w: decision must be approved or rejected", ErrValidation)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return VideoCapabilityAttestation{}, fmt.Errorf("%w: reason is required", ErrValidation)
	}
	model, variant, snapshotHash, err := s.currentVideoCapabilityVariant(ctx, organizationID, modelID, req.VariantKey, req.CapabilitySnapshotHash)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	verificationStatus := normalizeVideoCapabilityVerification(variant.VerificationStatus, variant.Source)
	if decision == "approved" && verificationStatus == VideoCapabilityVerificationUnknown {
		return VideoCapabilityAttestation{}, videoCapabilityApprovalRequired("来源未知的能力必须先完成 Adapter 验证或受控探测")
	}
	evidence, err := normalizeVideoAttestationEvidence(req.Evidence, map[string]any{
		"source": "organization_administrator", "modelKey": model.ModelKey,
	})
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	return s.insertVideoCapabilityAttestation(ctx, videoCapabilityAttestationInsert{
		OrganizationID: organizationID, UserID: userID, Model: model, Variant: variant,
		SnapshotHash: snapshotHash, VerificationStatus: verificationStatus,
		EvidenceType: "administrator_review", Evidence: evidence, Decision: decision, Reason: reason,
	})
}

func (s *Service) VerifyVideoCapability(
	ctx context.Context,
	organizationID, userID, modelID string,
	req VerifyVideoCapabilityRequest,
) (VideoCapabilityAttestation, error) {
	model, variant, snapshotHash, err := s.currentVideoCapabilityVariant(ctx, organizationID, modelID, req.VariantKey, req.CapabilitySnapshotHash)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	account, err := s.GetAccount(ctx, organizationID, model.ProviderAccountID)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	fixtureEvidence, err := s.validateVideoVariantAdapterFixture(ctx, account, model, variant)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(req.VerificationMode))
	if mode == "" {
		mode = "adapter_contract_test"
	}
	evidenceType := "adapter_contract_test"
	evidence := map[string]any{
		"verificationMode": mode,
		"adapterFixture":   fixtureEvidence,
		"verifiedAt":       time.Now().UTC().Format(time.RFC3339Nano),
	}
	switch mode {
	case "adapter_contract_test":
	case "controlled_probe":
		testRunID := strings.TrimSpace(req.ProviderTestRunID)
		if testRunID == "" {
			return VideoCapabilityAttestation{}, fmt.Errorf("%w: providerTestRunId is required for controlled_probe", ErrValidation)
		}
		var status, testType string
		var completedAt time.Time
		if err := s.db.QueryRow(ctx, `
			SELECT status, test_type, created_at
			FROM provider_test_runs
			WHERE id = $1 AND organization_id = $2 AND provider_model_id = $3
		`, testRunID, organizationID, model.ID).Scan(&status, &testType, &completedAt); err != nil {
			return VideoCapabilityAttestation{}, err
		}
		if status != "succeeded" || testType != "video_generation_test" {
			return VideoCapabilityAttestation{}, fmt.Errorf("%w: controlled probe must reference a succeeded video_generation_test", ErrValidation)
		}
		evidenceType = "controlled_probe"
		evidence["providerTestRunId"] = testRunID
		evidence["providerTestRunCreatedAt"] = completedAt.UTC().Format(time.RFC3339Nano)
	default:
		return VideoCapabilityAttestation{}, fmt.Errorf("%w: verificationMode must be adapter_contract_test or controlled_probe", ErrValidation)
	}
	if reason := strings.TrimSpace(req.Reason); reason != "" {
		evidence["reason"] = reason
	}
	return s.insertVideoCapabilityAttestation(ctx, videoCapabilityAttestationInsert{
		OrganizationID: organizationID, UserID: userID, Model: model, Variant: variant,
		SnapshotHash: snapshotHash, VerificationStatus: VideoCapabilityVerificationTested,
		EvidenceType: evidenceType, Evidence: mustJSON(evidence), Decision: "approved",
		Reason: firstNonEmpty(strings.TrimSpace(req.Reason), "video adapter capability verification passed"),
	})
}

func (s *Service) RevokeVideoCapabilityAttestation(
	ctx context.Context,
	organizationID, userID, modelID, attestationID string,
	req RevokeVideoCapabilityAttestationRequest,
) (VideoCapabilityAttestation, error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return VideoCapabilityAttestation{}, fmt.Errorf("%w: reason is required", ErrValidation)
	}
	model, _, currentHash, err := s.currentVideoCapabilityVariant(ctx, organizationID, modelID, "", req.CapabilitySnapshotHash)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	defer tx.Rollback(ctx)
	item, err := loadVideoCapabilityAttestationForUpdate(ctx, tx, organizationID, model.ID, attestationID)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	if item.CapabilitySnapshotHash != currentHash {
		return VideoCapabilityAttestation{}, videoCapabilityApprovalRequired("能力快照已变化，不能用旧审批修改当前模型能力")
	}
	if item.RevokedAt != nil {
		return item, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_model_capability_attestations
		SET revoked_by = NULLIF($2, '')::uuid, revoked_at = now(),
		    evidence = evidence || jsonb_build_object('revocationReason', $3::text)
		WHERE id = $1
	`, item.ID, userID, reason); err != nil {
		return VideoCapabilityAttestation{}, err
	}
	item, err = loadVideoCapabilityAttestationForUpdate(ctx, tx, organizationID, model.ID, item.ID)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	if err := appendVideoCapabilityAttestationEvent(ctx, tx, "provider.model_capability.revoked", item); err != nil {
		return VideoCapabilityAttestation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VideoCapabilityAttestation{}, err
	}
	return item, nil
}

type videoCapabilityAttestationInsert struct {
	OrganizationID     string
	UserID             string
	Model              Model
	Variant            VideoGenerationVariant
	SnapshotHash       string
	VerificationStatus string
	EvidenceType       string
	Evidence           json.RawMessage
	Decision           string
	Reason             string
}

func (s *Service) insertVideoCapabilityAttestation(ctx context.Context, input videoCapabilityAttestationInsert) (VideoCapabilityAttestation, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	defer tx.Rollback(ctx)
	var lockedModelID string
	if err := tx.QueryRow(ctx, `
		SELECT model.id::text
		FROM provider_models model
		JOIN provider_accounts account ON account.id = model.provider_account_id
		WHERE model.id = $1 AND account.organization_id = $2
		FOR UPDATE OF model
	`, input.Model.ID, input.OrganizationID).Scan(&lockedModelID); err != nil {
		return VideoCapabilityAttestation{}, err
	}
	var existingID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM provider_model_capability_attestations
		WHERE organization_id = $1 AND provider_model_id = $2 AND variant_key = $3
		  AND capability_snapshot_hash = $4 AND revoked_at IS NULL
		FOR UPDATE
	`, input.OrganizationID, input.Model.ID, input.Variant.VariantKey, input.SnapshotHash).Scan(&existingID)
	if err == nil {
		return VideoCapabilityAttestation{}, fmt.Errorf("%w: current capability snapshot already has an active decision; revoke it before replacing the decision", ErrConflict)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return VideoCapabilityAttestation{}, err
	}
	var supersedesID sql.NullString
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM provider_model_capability_attestations
		WHERE organization_id = $1 AND provider_model_id = $2 AND variant_key = $3
		  AND capability_snapshot_hash = $4
		ORDER BY decided_at DESC, id DESC LIMIT 1
	`, input.OrganizationID, input.Model.ID, input.Variant.VariantKey, input.SnapshotHash).Scan(&supersedesID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return VideoCapabilityAttestation{}, err
	}
	var attestationID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_model_capability_attestations(
			organization_id, provider_model_id, variant_key, capability_snapshot_hash,
			verification_status, evidence_type, evidence, decision, reason, decided_by,
			supersedes_attestation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, NULLIF($10, '')::uuid, $11)
		RETURNING id::text
	`, input.OrganizationID, input.Model.ID, input.Variant.VariantKey, input.SnapshotHash,
		input.VerificationStatus, input.EvidenceType, input.Evidence, input.Decision, input.Reason,
		input.UserID, nullableSQLString(supersedesID)).Scan(&attestationID); err != nil {
		return VideoCapabilityAttestation{}, err
	}
	item, err := loadVideoCapabilityAttestationForUpdate(ctx, tx, input.OrganizationID, input.Model.ID, attestationID)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	if err := appendVideoCapabilityAttestationEvent(ctx, tx, "provider.model_capability.attested", item); err != nil {
		return VideoCapabilityAttestation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VideoCapabilityAttestation{}, err
	}
	return item, nil
}

func (s *Service) currentVideoCapabilityVariants(ctx context.Context, organizationID, modelID string) (Model, []VideoGenerationVariant, map[string]string, error) {
	model, err := s.GetModel(ctx, organizationID, strings.TrimSpace(modelID))
	if err != nil {
		return Model{}, nil, nil, err
	}
	if model.Modality != "video" && model.Modality != "multimodal" {
		return Model{}, nil, nil, fmt.Errorf("%w: provider model does not support video", ErrValidation)
	}
	variants, err := videoGenerationVariants(model.Capabilities, model)
	if err != nil {
		return Model{}, nil, nil, err
	}
	hashes := make(map[string]string, len(variants))
	for _, variant := range variants {
		hash, err := capabilitySnapshotHash(variant)
		if err != nil {
			return Model{}, nil, nil, err
		}
		hashes[variant.VariantKey] = hash
	}
	return model, variants, hashes, nil
}

func (s *Service) currentVideoCapabilityVariant(ctx context.Context, organizationID, modelID, variantKey, requestedHash string) (Model, VideoGenerationVariant, string, error) {
	model, variants, hashes, err := s.currentVideoCapabilityVariants(ctx, organizationID, modelID)
	if err != nil {
		return Model{}, VideoGenerationVariant{}, "", err
	}
	variantKey = strings.TrimSpace(variantKey)
	requestedHash = strings.TrimSpace(requestedHash)
	if requestedHash != "" {
		requestedHash = "sha256:" + cleanVideoContractHash(requestedHash)
	}
	if variantKey == "" && len(variants) == 1 {
		variantKey = variants[0].VariantKey
	}
	if variantKey == "" && requestedHash != "" {
		for _, variant := range variants {
			if hashes[variant.VariantKey] == requestedHash {
				variantKey = variant.VariantKey
				break
			}
		}
	}
	for _, variant := range variants {
		if variant.VariantKey != variantKey {
			continue
		}
		hash := hashes[variant.VariantKey]
		if requestedHash == "" || requestedHash != hash {
			return Model{}, VideoGenerationVariant{}, "", videoCapabilityApprovalRequired("能力快照已变化，请刷新后再审批")
		}
		return model, variant, hash, nil
	}
	return Model{}, VideoGenerationVariant{}, "", fmt.Errorf("%w: video capability variant was not found", ErrValidation)
}

func (s *Service) validateVideoVariantAdapterFixture(ctx context.Context, account Account, model Model, variant VideoGenerationVariant) (map[string]any, error) {
	contracts := make([]VideoInputContract, 0, 1+len(variant.ContinuationInputContracts))
	contracts = append(contracts, variant.InputContract)
	contracts = append(contracts, variant.ContinuationInputContracts...)
	return s.validateVideoInputContractsAdapterFixture(ctx, account, model, contracts)
}

func (s *Service) validateVideoInputContractsAdapterFixture(ctx context.Context, account Account, model Model, contracts []VideoInputContract) (map[string]any, error) {
	verifiedContracts := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		references := videoInputContractFixtureReferences(contract)
		if usesNativeOpenAICompatibleRuntime(account) {
			cfg := parseOpenAICompatibleConfig(account.Config)
			body, err := buildOpenAICompatibleVideoRequest(model.ModelKey, mustJSON(map[string]any{
				"prompt": "adapter contract fixture", "duration": 5,
				"aspectRatio": "16:9", "resolution": "720p", "mode": "image_to_video",
			}), references, cfg)
			if err != nil {
				return nil, err
			}
			encoded := mustJSON(body)
			if err := ensureFixtureReferencesRendered(encoded, references, contract.ContractKey); err != nil {
				return nil, err
			}
		} else {
			manifest, err := s.manifestForAccount(ctx, account)
			if err != nil {
				return nil, err
			}
			endpointKey, endpoint, err := selectVideoCreateEndpoint(gatewayModelSelection{Account: account, Model: model}, manifest)
			if err != nil {
				return nil, err
			}
			extra := videoManifestContext(gatewayModelSelection{Account: account, Model: model}, references, nil)
			contextValue := map[string]any{
				"input":      map[string]any{"prompt": "adapter contract fixture", "duration": 5, "aspectRatio": "16:9", "resolution": "720p"},
				"references": extra.References, "credential": map[string]any{"apiKey": "fixture"},
				"endpoint": map[string]any{"key": endpointKey}, "model": extra.Model,
				"account": extra.Account, "task": extra.Task,
			}
			rendered, err := renderTemplateJSON(endpoint.RequestTemplate, contextValue)
			if err != nil {
				return nil, err
			}
			if err := ensureFixtureReferencesRendered(rendered, references, contract.ContractKey); err != nil {
				return nil, err
			}
		}
		verifiedContracts = append(verifiedContracts, contract.ContractKey)
	}
	return map[string]any{
		"runtime":   firstNonEmpty(accountConfigString(account.Config, "runtime"), account.ConnectorKey),
		"contracts": verifiedContracts,
	}, nil
}

func videoInputContractFixtureReferences(contract VideoInputContract) []GatewayVideoReference {
	references := make([]GatewayVideoReference, 0, len(contract.Slots))
	for _, slot := range contract.Slots {
		mediaType := strings.ToLower(strings.TrimSpace(slot.MediaType))
		extension := ".png"
		mimeType := "image/png"
		referenceType := "image_reference"
		if mediaType == "video" {
			extension = ".mp4"
			mimeType = "video/mp4"
			referenceType = "video_reference"
		}
		references = append(references, GatewayVideoReference{
			Role: slot.Role, Type: referenceType,
			URL: "https://fixtures.invalid/" + slot.Role + extension, MimeType: mimeType,
		})
	}
	return references
}

func ensureFixtureReferencesRendered(rendered json.RawMessage, references []GatewayVideoReference, contractKey string) error {
	for _, reference := range references {
		if !bytes.Contains(rendered, []byte(reference.URL)) {
			return &StandardErrorError{Standard: StandardError{
				Code:      CodeModelInputContractUnsupported,
				Message:   fmt.Sprintf("视频 Adapter 没有映射输入契约 %s 的角色 %s", contractKey, reference.Role),
				Retryable: false,
			}}
		}
	}
	return nil
}

func normalizeVideoAttestationEvidence(raw json.RawMessage, defaults map[string]any) (json.RawMessage, error) {
	values := make(map[string]any, len(defaults))
	for key, value := range defaults {
		values[key] = value
	}
	if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		var provided map[string]any
		if err := json.Unmarshal(raw, &provided); err != nil {
			return nil, fmt.Errorf("%w: evidence must be a JSON object", ErrValidation)
		}
		for key, value := range provided {
			values[key] = value
		}
	}
	return mustJSON(values), nil
}

func scanVideoCapabilityAttestation(row rowScanner) (VideoCapabilityAttestation, error) {
	var item VideoCapabilityAttestation
	var evidence []byte
	var decidedBy, supersedesID, revokedBy sql.NullString
	var revokedAt sql.NullTime
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProviderModelID, &item.VariantKey,
		&item.CapabilitySnapshotHash, &item.VerificationStatus, &item.EvidenceType, &evidence,
		&item.Decision, &item.Reason, &decidedBy, &item.DecidedAt, &supersedesID,
		&revokedBy, &revokedAt, &item.CreatedAt,
	)
	if err != nil {
		return VideoCapabilityAttestation{}, err
	}
	item.Evidence = rawOrDefault(evidence, "{}")
	item.DecidedBy = nullStringPointer(decidedBy)
	item.SupersedesAttestationID = nullStringPointer(supersedesID)
	item.RevokedBy = nullStringPointer(revokedBy)
	if revokedAt.Valid {
		value := revokedAt.Time
		item.RevokedAt = &value
	}
	item.Active = item.RevokedAt == nil
	return item, nil
}

func loadVideoCapabilityAttestationForUpdate(ctx context.Context, tx pgx.Tx, organizationID, modelID, attestationID string) (VideoCapabilityAttestation, error) {
	return scanVideoCapabilityAttestation(tx.QueryRow(ctx, `
		SELECT id::text, organization_id::text, provider_model_id::text, variant_key,
		       capability_snapshot_hash, verification_status, evidence_type, evidence,
		       decision, reason, decided_by::text, decided_at, supersedes_attestation_id::text,
		       revoked_by::text, revoked_at, created_at
		FROM provider_model_capability_attestations
		WHERE id = $1 AND organization_id = $2 AND provider_model_id = $3
		FOR UPDATE
	`, strings.TrimSpace(attestationID), organizationID, modelID))
}

func appendVideoCapabilityAttestationEvent(ctx context.Context, tx pgx.Tx, eventType string, item VideoCapabilityAttestation) error {
	payload := mustJSON(map[string]any{
		"providerModelId": item.ProviderModelID, "variantKey": item.VariantKey,
		"capabilitySnapshotHash": item.CapabilitySnapshotHash,
		"attestationId":          item.ID, "verificationStatus": item.VerificationStatus,
		"decision": item.Decision, "status": map[bool]string{true: "active", false: "revoked"}[item.RevokedAt == nil],
	})
	return events.AppendTx(ctx, tx, item.OrganizationID, "", eventType, "provider_model", item.ProviderModelID, payload)
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullableSQLString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

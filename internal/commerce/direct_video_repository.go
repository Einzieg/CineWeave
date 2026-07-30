package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type DirectVideoService struct {
	repository *Repository
	catalog    *CatalogService
}

type PrepareDirectVideoJobParams struct {
	JobID          string
	OrganizationID string
	ProjectID      string
	ScriptUnitID   string
	WorkflowRunID  string
	CreatedBy      string
	IdempotencyKey string
	Input          CreateDirectVideoJobInput
}

type CreateScriptReferenceParams struct {
	OrganizationID   string
	ProjectID        string
	ProductID        string
	ScriptUnitID     string
	StorageKey       string
	OriginalFileName string
	MimeType         string
	Width            int
	Height           int
	ByteSize         int64
	ContentHash      string
	CreatedBy        string
}

func NewDirectVideoService(repository *Repository) *DirectVideoService {
	if repository == nil {
		repository = NewRepository()
	}
	return &DirectVideoService{repository: repository, catalog: NewCatalogService(repository)}
}

func (s *DirectVideoService) Options(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
) (DirectVideoOptions, error) {
	production, err := s.repository.LoadActiveProductionContext(ctx, db, organizationID, projectID)
	if err != nil {
		return DirectVideoOptions{}, err
	}
	return BuildDirectVideoOptions(production)
}

func (s *DirectVideoService) ClaimScriptReferenceUpload(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	productID string,
	scriptUnitID string,
	storageKey string,
	mimeType string,
	fileName string,
	idempotencyKey string,
	createdBy string,
	expiresAt time.Time,
) (ScriptReferenceUpload, bool, error) {
	var item ScriptReferenceUpload
	err := scanScriptReferenceUpload(tx.QueryRow(ctx, `
		INSERT INTO commerce_script_reference_uploads(
			organization_id, project_id, product_id, script_unit_id,
			storage_key, requested_mime_type, original_file_name,
			idempotency_key, created_by, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (organization_id, idempotency_key) DO NOTHING
		RETURNING id::text, organization_id::text, project_id::text, product_id::text,
		          script_unit_id::text, storage_key, requested_mime_type,
		          original_file_name, status, idempotency_key,
		          reference_image_id::text, created_at, expires_at, completed_at, abandoned_at
	`, organizationID, projectID, productID, scriptUnitID, storageKey, mimeType,
		fileName, idempotencyKey, createdBy, expiresAt), &item)
	if err == nil {
		return item, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ScriptReferenceUpload{}, false, err
	}
	err = scanScriptReferenceUpload(tx.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text, product_id::text,
		       script_unit_id::text, storage_key, requested_mime_type,
		       original_file_name, status, idempotency_key,
		       reference_image_id::text, created_at, expires_at, completed_at, abandoned_at
		FROM commerce_script_reference_uploads
		WHERE organization_id = $1 AND idempotency_key = $2
		FOR UPDATE
	`, organizationID, idempotencyKey), &item)
	if err != nil {
		return ScriptReferenceUpload{}, false, err
	}
	if item.ProjectID != projectID || item.ProductID != productID || item.ScriptUnitID != scriptUnitID ||
		item.RequestedMimeType != mimeType || item.OriginalFileName != fileName {
		return ScriptReferenceUpload{}, false, Error{Code: CodeIdempotencyKeyReused, Message: "该上传请求标识已用于其他图片"}
	}
	return item, true, nil
}

func (s *DirectVideoService) GetScriptReferenceUpload(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	scriptUnitID string,
	uploadID string,
	lock bool,
) (ScriptReferenceUpload, error) {
	query := `
		SELECT id::text, organization_id::text, project_id::text, product_id::text,
		       script_unit_id::text, storage_key, requested_mime_type,
		       original_file_name, status, idempotency_key,
		       reference_image_id::text, created_at, expires_at, completed_at, abandoned_at
		FROM commerce_script_reference_uploads
		WHERE id = $1 AND organization_id = $2 AND project_id = $3 AND script_unit_id = $4`
	if lock {
		query += " FOR UPDATE"
	}
	var item ScriptReferenceUpload
	if err := scanScriptReferenceUpload(db.QueryRow(ctx, query, uploadID, organizationID, projectID, scriptUnitID), &item); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScriptReferenceUpload{}, Error{Code: CodeDirectVideoNotFound, Message: "自定义参考图上传记录不存在", Cause: err}
		}
		return ScriptReferenceUpload{}, err
	}
	return item, nil
}

func (s *DirectVideoService) FindScriptReferenceByHash(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	scriptUnitID string,
	contentHash string,
) (ScriptReferenceImage, bool, error) {
	item, err := scanScriptReferenceImage(db.QueryRow(ctx, scriptReferenceImageSelectSQL+`
		WHERE reference.organization_id = $1 AND reference.project_id = $2
		  AND reference.script_unit_id = $3 AND reference.content_hash = $4
		  AND reference.status = 'active'
	`, organizationID, projectID, scriptUnitID, contentHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return ScriptReferenceImage{}, false, nil
	}
	return item, err == nil, err
}

func (s *DirectVideoService) CreateScriptReference(
	ctx context.Context,
	tx pgx.Tx,
	params CreateScriptReferenceParams,
) (ScriptReferenceImage, error) {
	metadata := mustJSON(map[string]any{
		"source": "commerce_script_reference", "scriptUnitId": params.ScriptUnitID,
		"fileName": params.OriginalFileName, "contentHash": params.ContentHash,
		"width": params.Width, "height": params.Height, "byteSize": params.ByteSize,
	})
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, type, storage_key, mime_type,
			content_hash, metadata, created_by
		)
		VALUES ($1, $2, 'commerce_script_reference', $3, $4, $5, $6, $7)
		RETURNING id::text
	`, params.OrganizationID, params.ProjectID, params.StorageKey, params.MimeType,
		params.ContentHash, metadata, params.CreatedBy).Scan(&artifactID); err != nil {
		return ScriptReferenceImage{}, err
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id::text
	`, params.OrganizationID, params.ProjectID, artifactID, params.StorageKey, params.MimeType,
		params.ByteSize, params.Width, params.Height, params.ContentHash, metadata, params.CreatedBy).Scan(&mediaFileID); err != nil {
		return ScriptReferenceImage{}, err
	}
	var referenceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_reference_images(
			organization_id, project_id, product_id, script_unit_id,
			artifact_id, media_file_id, original_file_name, mime_type,
			width, height, byte_size, content_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id::text
	`, params.OrganizationID, params.ProjectID, params.ProductID, params.ScriptUnitID,
		artifactID, mediaFileID, params.OriginalFileName, params.MimeType,
		params.Width, params.Height, params.ByteSize, params.ContentHash, params.CreatedBy).Scan(&referenceID); err != nil {
		return ScriptReferenceImage{}, err
	}
	return scanScriptReferenceImage(tx.QueryRow(ctx, scriptReferenceImageSelectSQL+`
		WHERE reference.id = $1
	`, referenceID))
}

func (s *DirectVideoService) CompleteScriptReferenceUpload(
	ctx context.Context,
	tx pgx.Tx,
	upload ScriptReferenceUpload,
	referenceID string,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_reference_uploads
		SET status = 'completed', reference_image_id = $2,
		    completed_at = now()
		WHERE id = $1 AND status = 'pending'
	`, upload.ID, referenceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Error{Code: CodeDirectVideoStateConflict, Message: "自定义参考图上传状态已变化"}
	}
	return nil
}

func (s *DirectVideoService) AbandonScriptReferenceUpload(
	ctx context.Context,
	tx pgx.Tx,
	upload ScriptReferenceUpload,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_reference_uploads
		SET status = 'abandoned', abandoned_at = now()
		WHERE id = $1 AND status = 'pending'
	`, upload.ID)
	return err
}

func (s *DirectVideoService) ListScriptReferences(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	scriptUnitID string,
	status string,
) ([]ScriptReferenceImage, error) {
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "archived" && status != "all" {
		return nil, Error{Code: CodeDirectVideoInvalid, Message: "自定义参考图状态筛选无效"}
	}
	query := scriptReferenceImageSelectSQL + `
		WHERE reference.organization_id = $1 AND reference.project_id = $2
		  AND reference.script_unit_id = $3`
	args := []any{organizationID, projectID, scriptUnitID}
	if status != "all" {
		query += ` AND reference.status = $4`
		args = append(args, status)
	}
	query += ` ORDER BY reference.status, reference.created_at, reference.id`
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptReferenceImage, 0)
	for rows.Next() {
		item, err := scanScriptReferenceImage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *DirectVideoService) ArchiveScriptReference(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	scriptUnitID string,
	referenceID string,
	expectedRevision int64,
) (ScriptReferenceImage, error) {
	var revision int64
	err := tx.QueryRow(ctx, `
		UPDATE commerce_script_reference_images
		SET status = 'archived', archived_at = now(), revision = revision + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND script_unit_id = $4 AND status = 'active' AND revision = $5
		RETURNING revision
	`, referenceID, organizationID, projectID, scriptUnitID, expectedRevision).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScriptReferenceImage{}, Error{Code: CodeRevisionConflict, Message: "自定义参考图已变化，请刷新后重试", Cause: err}
	}
	if err != nil {
		return ScriptReferenceImage{}, err
	}
	return scanScriptReferenceImage(tx.QueryRow(ctx, scriptReferenceImageSelectSQL+` WHERE reference.id = $1`, referenceID))
}

func (s *DirectVideoService) PrepareJob(
	ctx context.Context,
	tx pgx.Tx,
	params PrepareDirectVideoJobParams,
) (PreparedDirectVideoJob, error) {
	if err := validateDirectVideoJobInput(params.Input); err != nil {
		return PreparedDirectVideoJob{}, Error{Code: CodeDirectVideoInvalid, Message: "视频生成参数无效", Cause: err}
	}
	production, err := s.repository.LockActiveProductionContext(ctx, tx, params.OrganizationID, params.ProjectID)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	if production.ProjectLocked || production.LifecycleStatus == "deleting" {
		return PreparedDirectVideoJob{}, Error{Code: CodeProjectLocked, Message: "项目当前不能启动新的视频生成任务", Retryable: true}
	}
	product, err := s.catalog.GetProduct(ctx, tx, params.OrganizationID, params.ProjectID)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	if product.CurrentVersion == nil {
		return PreparedDirectVideoJob{}, Error{Code: CodeProductRequired, Message: "请先完成商品配置"}
	}
	unit, err := s.catalog.GetScriptUnit(ctx, tx, params.OrganizationID, params.ProjectID, params.ScriptUnitID)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	if unit.Status == "archived" {
		return PreparedDirectVideoJob{}, Error{Code: CodeScriptRequired, Message: "请先保存广告脚本"}
	}
	currentContent, err := s.catalog.ResolveCurrentScriptContent(
		ctx, tx, params.OrganizationID, params.ProjectID, params.ScriptUnitID,
	)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	options, err := BuildDirectVideoOptions(production)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	if params.Input.DurationSeconds == 0 {
		params.Input.DurationSeconds = options.DefaultDurationSeconds
	}
	if strings.TrimSpace(params.Input.Resolution) == "" {
		params.Input.Resolution = options.DefaultResolution
	}
	if strings.TrimSpace(params.Input.AspectRatio) == "" {
		params.Input.AspectRatio = options.DefaultAspectRatio
	}
	route, err := SelectDirectVideoRoute(options, params.Input.DurationSeconds, params.Input.Resolution)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	if err := ValidateDirectVideoScript(currentContent.Content, route.PromptConstraint); err != nil {
		return PreparedDirectVideoJob{}, err
	}
	references, err := s.loadSelectedReferences(ctx, tx, product, unit, params.Input.References)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	prioritizeDirectVideoReferences(references)
	roles, err := AssignDirectVideoReferenceRoles(route.InputContract, len(references))
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	if len(params.Input.References) > 0 && len(references) > len(roles) {
		return PreparedDirectVideoJob{}, Error{
			Code:    CodeDirectVideoInvalid,
			Message: "所选参考图数量超过当前视频模型可接收的上限",
			Details: map[string]any{"selectedCount": len(references), "maximumCount": len(roles)},
		}
	}
	references = references[:len(roles)]
	for index := range references {
		references[index].ID = uuid.NewString()
		references[index].ReferenceRole = roles[index]
		references[index].Ordinal = index
	}
	referenceHash, err := DirectVideoReferenceSetHash(references)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	productSnapshot, err := json.Marshal(product.CurrentVersion)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	productSnapshotHash, err := DirectVideoHash(product.CurrentVersion)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	inputContractHash, err := DirectVideoHash(route.InputContract)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	generateAudio := true
	if params.Input.GenerateAudio != nil {
		generateAudio = *params.Input.GenerateAudio
	}
	aspectRatio := strings.TrimSpace(params.Input.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = options.DefaultAspectRatio
	}
	executionContract := mustJSON(map[string]any{
		"contractVersion": CommerceDirectVideoContractV1,
		"route":           route, "inputContractHash": inputContractHash,
		"durationSeconds": params.Input.DurationSeconds,
		"resolution":      normalizeDirectVideoString(params.Input.Resolution),
		"aspectRatio":     aspectRatio, "generateAudio": generateAudio,
	})
	executionContractHash, err := DirectVideoHash(executionContract)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	script := currentContent.Content
	scriptHash := currentContent.ContentHash
	payload := map[string]any{
		"projectId": params.ProjectID, "scriptUnitId": unit.ID, "scriptUnitRevision": unit.Revision,
		"generationId": production.Generation.ID, "bindingId": production.VideoBinding.ID,
		"bindingRevision": production.VideoBinding.Revision, "executionContractHash": executionContractHash,
		"scriptHash": scriptHash, "productSnapshotHash": productSnapshotHash, "referenceSetHash": referenceHash,
	}
	payloadHash, err := DirectVideoHash(payload)
	if err != nil {
		return PreparedDirectVideoJob{}, err
	}
	jobID := strings.TrimSpace(params.JobID)
	if jobID == "" {
		jobID = uuid.NewString()
	}
	job := DirectVideoJob{
		ID: jobID, OrganizationID: params.OrganizationID, ProjectID: params.ProjectID,
		ProductID: product.ID, ProductVersionID: product.CurrentVersion.ID,
		ScriptUnitID: unit.ID, ScriptUnitRevision: unit.Revision,
		ProjectProductionGenerationID:  production.Generation.ID,
		VideoProductionBindingID:       production.VideoBinding.ID,
		VideoProductionBindingRevision: production.VideoBinding.Revision,
		VideoProfileVersionID:          production.VideoBinding.ProfileVersionID,
		VideoProfileSnapshotHash:       production.VideoBinding.ProfileSnapshotHash,
		ModelProfileKey:                route.ModelProfileKey, ModelProfileID: directStringPointer(route.ModelProfileID),
		ModelProfileBindingID: directStringPointer(route.ModelProfileBindingID),
		ProviderModelID:       directStringPointer(route.ProviderModelID), ProviderAccountID: directStringPointer(route.ProviderAccountID),
		ProviderModelKey: route.ProviderModelKey, RouteKey: route.RouteKey, VariantKey: route.VariantKey,
		CapabilitySnapshotHash:   route.CapabilitySnapshotHash,
		RequestedDurationSeconds: params.Input.DurationSeconds, AspectRatio: aspectRatio,
		Resolution: normalizeDirectVideoString(params.Input.Resolution), GenerateAudio: generateAudio,
		ScriptSnapshot: script, ScriptHash: scriptHash,
		ProductSnapshot: productSnapshot, ProductSnapshotHash: productSnapshotHash,
		ExecutionContract: executionContract, ExecutionContractHash: executionContractHash,
		ReferenceSetHash: referenceHash, PromptHash: scriptHash,
		Status: "queued", AttemptGeneration: 1, WorkflowRunID: directStringPointer(params.WorkflowRunID),
		CreatedBy: directStringPointer(params.CreatedBy), References: references,
	}
	return PreparedDirectVideoJob{
		Job: job, Route: route, References: references, Production: production, PayloadHash: payloadHash,
	}, nil
}

func (s *DirectVideoService) InsertPreparedJob(
	ctx context.Context,
	tx pgx.Tx,
	prepared PreparedDirectVideoJob,
	idempotencyKey string,
) (DirectVideoJob, error) {
	job := prepared.Job
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_direct_video_jobs(
			id, organization_id, project_id, product_id, product_version_id,
			script_unit_id, script_unit_revision, project_production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			video_profile_version_id, video_profile_snapshot_hash,
			model_profile_key, model_profile_id, model_profile_binding_id,
			provider_model_id, provider_account_id, provider_model_key,
			route_key, variant_key, capability_snapshot_hash,
			requested_duration_seconds, aspect_ratio, resolution, generate_audio,
			script_snapshot, script_hash, product_snapshot, product_snapshot_hash,
			execution_contract, execution_contract_hash, reference_set_hash, prompt_hash,
			status, attempt_generation, idempotency_key, payload_hash,
			workflow_run_id, created_by
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
			$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,'queued',1,$34,$35,$36,$37
		)
	`, job.ID, job.OrganizationID, job.ProjectID, job.ProductID, job.ProductVersionID,
		job.ScriptUnitID, job.ScriptUnitRevision, job.ProjectProductionGenerationID,
		job.VideoProductionBindingID, job.VideoProductionBindingRevision,
		job.VideoProfileVersionID, job.VideoProfileSnapshotHash,
		job.ModelProfileKey, pointerValue(job.ModelProfileID), pointerValue(job.ModelProfileBindingID),
		pointerValue(job.ProviderModelID), pointerValue(job.ProviderAccountID), job.ProviderModelKey,
		job.RouteKey, job.VariantKey, job.CapabilitySnapshotHash,
		job.RequestedDurationSeconds, job.AspectRatio, job.Resolution, job.GenerateAudio,
		job.ScriptSnapshot, job.ScriptHash, job.ProductSnapshot, job.ProductSnapshotHash,
		job.ExecutionContract, job.ExecutionContractHash, job.ReferenceSetHash, job.PromptHash,
		idempotencyKey, prepared.PayloadHash, pointerValue(job.WorkflowRunID), pointerValue(job.CreatedBy))
	if err != nil {
		return DirectVideoJob{}, err
	}
	for _, reference := range prepared.References {
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_direct_video_job_references(
				id, organization_id, project_id, product_id, script_unit_id, job_id,
				source_type, source_id, product_reference_id, script_reference_image_id,
				artifact_id, media_file_id, reference_role, ordinal,
				content_hash, source_revision, snapshot
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid,NULLIF($10,'')::uuid,
			        $11,$12,$13,$14,$15,$16,$17)
		`, reference.ID, job.OrganizationID, job.ProjectID, job.ProductID, job.ScriptUnitID, job.ID,
			reference.SourceType, reference.SourceID, pointerText(reference.ProductReferenceID),
			pointerText(reference.ScriptReferenceImageID), reference.ArtifactID, reference.MediaFileID,
			reference.ReferenceRole, reference.Ordinal, reference.ContentHash, reference.SourceRevision,
			reference.Snapshot); err != nil {
			return DirectVideoJob{}, err
		}
	}
	return s.GetJob(ctx, tx, job.OrganizationID, job.ProjectID, job.ID)
}

func (s *DirectVideoService) GetJob(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	jobID string,
) (DirectVideoJob, error) {
	job, err := scanDirectVideoJob(db.QueryRow(ctx, directVideoJobSelectSQL+`
		WHERE job.id = $1 AND job.organization_id = $2 AND job.project_id = $3
	`, jobID, organizationID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return DirectVideoJob{}, Error{Code: CodeDirectVideoNotFound, Message: "视频生成任务不存在", Cause: err}
	}
	if err != nil {
		return DirectVideoJob{}, err
	}
	if rowsDB, ok := db.(rowsQuerier); ok {
		job.References, err = s.listJobReferences(ctx, rowsDB, organizationID, projectID, job.ID)
	}
	return job, err
}

func (s *DirectVideoService) ListJobs(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	scriptUnitID string,
) ([]DirectVideoJob, error) {
	query := directVideoJobSelectSQL + `
		WHERE job.organization_id = $1 AND job.project_id = $2`
	args := []any{organizationID, projectID}
	if strings.TrimSpace(scriptUnitID) != "" {
		query += ` AND job.script_unit_id = $3`
		args = append(args, scriptUnitID)
	}
	query += ` ORDER BY job.created_at DESC, job.id DESC`
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DirectVideoJob, 0)
	for rows.Next() {
		item, err := scanDirectVideoJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		items[index].References, err = s.listJobReferences(ctx, db, organizationID, projectID, items[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *DirectVideoService) listJobReferences(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	jobID string,
) ([]DirectVideoReferenceSnapshot, error) {
	rows, err := db.Query(ctx, `
		SELECT reference.id::text, reference.source_type, reference.source_id::text,
		       reference.product_reference_id::text, reference.script_reference_image_id::text,
		       reference.artifact_id::text, reference.media_file_id::text,
		       artifact.storage_key, artifact.mime_type, reference.reference_role,
		       reference.ordinal, reference.content_hash, reference.source_revision, reference.snapshot
		FROM commerce_direct_video_job_references reference
		JOIN artifacts artifact ON artifact.id = reference.artifact_id
		WHERE reference.job_id = $1 AND reference.organization_id = $2 AND reference.project_id = $3
		ORDER BY reference.ordinal
	`, jobID, organizationID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DirectVideoReferenceSnapshot, 0)
	for rows.Next() {
		var item DirectVideoReferenceSnapshot
		var productReferenceID, scriptReferenceID pgtype.Text
		if err := rows.Scan(&item.ID, &item.SourceType, &item.SourceID,
			&productReferenceID, &scriptReferenceID, &item.ArtifactID, &item.MediaFileID,
			&item.StorageKey, &item.MimeType, &item.ReferenceRole, &item.Ordinal,
			&item.ContentHash, &item.SourceRevision, &item.Snapshot); err != nil {
			return nil, err
		}
		item.ProductReferenceID = pgTextPointer(productReferenceID)
		item.ScriptReferenceImageID = pgTextPointer(scriptReferenceID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *DirectVideoService) loadSelectedReferences(
	ctx context.Context,
	tx pgx.Tx,
	product Product,
	unit ScriptUnit,
	selected []DirectVideoReferenceSelection,
) ([]DirectVideoReferenceSnapshot, error) {
	if len(selected) == 0 {
		rows, err := tx.Query(ctx, `
			SELECT reference.id::text, reference.artifact_id::text, reference.media_file_id::text,
			       artifact.storage_key, reference.mime_type, reference.content_hash,
			       reference.revision, reference.reference_role, reference.is_primary,
			       reference.ordinal, reference.width, reference.height
			FROM commerce_product_references reference
			JOIN artifacts artifact ON artifact.id = reference.artifact_id
			WHERE reference.organization_id = $1 AND reference.project_id = $2
			  AND reference.product_id = $3 AND reference.status = 'active'
			ORDER BY reference.is_primary DESC, reference.ordinal, reference.created_at, reference.id
		`, product.OrganizationID, product.ProjectID, product.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]DirectVideoReferenceSnapshot, 0)
		for rows.Next() {
			item, err := scanProductDirectVideoReference(rows)
			if err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(result) == 0 {
			return nil, Error{Code: CodeProductPrimaryImage, Message: "请先上传商品参考图"}
		}
		return result, nil
	}
	seen := map[string]bool{}
	result := make([]DirectVideoReferenceSnapshot, 0, len(selected))
	for _, selection := range selected {
		key := selection.SourceType + ":" + selection.SourceID
		if seen[key] {
			continue
		}
		seen[key] = true
		var item DirectVideoReferenceSnapshot
		var err error
		switch selection.SourceType {
		case "product":
			item, err = scanProductDirectVideoReference(tx.QueryRow(ctx, `
				SELECT reference.id::text, reference.artifact_id::text, reference.media_file_id::text,
				       artifact.storage_key, reference.mime_type, reference.content_hash,
				       reference.revision, reference.reference_role, reference.is_primary,
				       reference.ordinal, reference.width, reference.height
				FROM commerce_product_references reference
				JOIN artifacts artifact ON artifact.id = reference.artifact_id
				WHERE reference.id = $1 AND reference.organization_id = $2
				  AND reference.project_id = $3 AND reference.product_id = $4
				  AND reference.status = 'active'
			`, selection.SourceID, product.OrganizationID, product.ProjectID, product.ID))
		case "custom":
			custom, customErr := scanScriptReferenceImage(tx.QueryRow(ctx, scriptReferenceImageSelectSQL+`
				WHERE reference.id = $1 AND reference.organization_id = $2
				  AND reference.project_id = $3 AND reference.script_unit_id = $4
				  AND reference.status = 'active'
			`, selection.SourceID, product.OrganizationID, product.ProjectID, unit.ID))
			err = customErr
			if err == nil {
				item = directVideoReferenceFromCustom(custom)
			}
		default:
			err = errors.New("unsupported direct video reference source")
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, Error{Code: CodeDirectVideoInvalid, Message: "所选参考图不存在或已归档", Cause: err}
		}
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, Error{Code: CodeProductPrimaryImage, Message: "请至少选择一张参考图"}
	}
	return result, nil
}

const scriptReferenceImageSelectSQL = `
	SELECT reference.id::text, reference.organization_id::text, reference.project_id::text,
	       reference.product_id::text, reference.script_unit_id::text,
	       reference.artifact_id::text, reference.media_file_id::text,
	       artifact.storage_key, reference.original_file_name, reference.mime_type, reference.width,
	       reference.height, reference.byte_size, reference.content_hash,
	       reference.status, reference.revision, reference.created_at,
	       reference.updated_at, reference.archived_at
	FROM commerce_script_reference_images reference
	JOIN artifacts artifact ON artifact.id = reference.artifact_id
`

func scanScriptReferenceImage(row interface{ Scan(...any) error }) (ScriptReferenceImage, error) {
	var item ScriptReferenceImage
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID, &item.ScriptUnitID,
		&item.ArtifactID, &item.MediaFileID, &item.StorageKey, &item.OriginalFileName, &item.MimeType,
		&item.Width, &item.Height, &item.ByteSize, &item.ContentHash, &item.Status,
		&item.Revision, &item.CreatedAt, &item.UpdatedAt, &item.ArchivedAt,
	)
	return item, err
}

func scanScriptReferenceUpload(row interface{ Scan(...any) error }, item *ScriptReferenceUpload) error {
	var referenceID pgtype.Text
	if err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID, &item.ScriptUnitID,
		&item.StorageKey, &item.RequestedMimeType, &item.OriginalFileName, &item.Status,
		&item.IdempotencyKey, &referenceID, &item.CreatedAt, &item.ExpiresAt,
		&item.CompletedAt, &item.AbandonedAt,
	); err != nil {
		return err
	}
	item.ReferenceImageID = pgTextPointer(referenceID)
	return nil
}

const directVideoJobSelectSQL = `
	SELECT job.id::text, job.organization_id::text, job.project_id::text,
	       job.product_id::text, job.product_version_id::text,
	       job.script_unit_id::text, job.script_unit_revision,
	       job.project_production_generation_id::text,
	       job.video_production_binding_id::text, job.video_production_binding_revision,
	       job.video_profile_version_id::text, job.video_profile_snapshot_hash,
	       job.model_profile_key, job.model_profile_id::text, job.model_profile_binding_id::text,
	       job.provider_model_id::text, job.provider_account_id::text, job.provider_model_key,
	       job.route_key, job.variant_key, job.capability_snapshot_hash,
	       job.requested_duration_seconds, job.aspect_ratio, job.resolution, job.generate_audio,
	       job.script_snapshot, job.script_hash, job.product_snapshot, job.product_snapshot_hash,
	       job.execution_contract, job.execution_contract_hash, job.reference_set_hash, job.prompt_hash,
	       job.status, job.attempt_generation, job.workflow_run_id::text, job.node_run_id::text,
	       job.provider_request_id::text, job.provider_call_id::text,
	       job.provider_async_task_id::text, job.external_task_id,
	       job.output_artifact_id::text, job.output_media_file_id::text,
	       job.output_storage_key, job.output_mime_type,
	       COALESCE(output_artifact.metadata->'warnings', '[]'::jsonb),
	       job.error_code, job.error_message, job.created_by::text,
	       job.created_at, job.started_at, job.completed_at, job.cancelled_at, job.updated_at
	FROM commerce_direct_video_jobs job
	LEFT JOIN artifacts output_artifact
	  ON output_artifact.id = job.output_artifact_id
	 AND output_artifact.organization_id = job.organization_id
`

func scanDirectVideoJob(row interface{ Scan(...any) error }) (DirectVideoJob, error) {
	var item DirectVideoJob
	var modelProfileID, modelBindingID, providerModelID, providerAccountID pgtype.Text
	var workflowRunID, nodeRunID, providerRequestID, providerCallID, providerTaskID pgtype.Text
	var externalTaskID, outputArtifactID, outputMediaID, outputStorageKey, outputMimeType pgtype.Text
	var errorCode, errorMessage, createdBy pgtype.Text
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID, &item.ProductVersionID,
		&item.ScriptUnitID, &item.ScriptUnitRevision, &item.ProjectProductionGenerationID,
		&item.VideoProductionBindingID, &item.VideoProductionBindingRevision,
		&item.VideoProfileVersionID, &item.VideoProfileSnapshotHash,
		&item.ModelProfileKey, &modelProfileID, &modelBindingID,
		&providerModelID, &providerAccountID, &item.ProviderModelKey,
		&item.RouteKey, &item.VariantKey, &item.CapabilitySnapshotHash,
		&item.RequestedDurationSeconds, &item.AspectRatio, &item.Resolution, &item.GenerateAudio,
		&item.ScriptSnapshot, &item.ScriptHash, &item.ProductSnapshot, &item.ProductSnapshotHash,
		&item.ExecutionContract, &item.ExecutionContractHash, &item.ReferenceSetHash, &item.PromptHash,
		&item.Status, &item.AttemptGeneration, &workflowRunID, &nodeRunID,
		&providerRequestID, &providerCallID, &providerTaskID, &externalTaskID,
		&outputArtifactID, &outputMediaID, &outputStorageKey, &outputMimeType,
		&item.OutputWarnings,
		&errorCode, &errorMessage, &createdBy, &item.CreatedAt, &item.StartedAt,
		&item.CompletedAt, &item.CancelledAt, &item.UpdatedAt,
	)
	item.ModelProfileID = pgTextPointer(modelProfileID)
	item.ModelProfileBindingID = pgTextPointer(modelBindingID)
	item.ProviderModelID = pgTextPointer(providerModelID)
	item.ProviderAccountID = pgTextPointer(providerAccountID)
	item.WorkflowRunID = pgTextPointer(workflowRunID)
	item.NodeRunID = pgTextPointer(nodeRunID)
	item.ProviderRequestID = pgTextPointer(providerRequestID)
	item.ProviderCallID = pgTextPointer(providerCallID)
	item.ProviderAsyncTaskID = pgTextPointer(providerTaskID)
	item.ExternalTaskID = pgTextPointer(externalTaskID)
	item.OutputArtifactID = pgTextPointer(outputArtifactID)
	item.OutputMediaFileID = pgTextPointer(outputMediaID)
	item.OutputStorageKey = pgTextPointer(outputStorageKey)
	item.OutputMimeType = pgTextPointer(outputMimeType)
	item.ErrorCode = pgTextPointer(errorCode)
	item.ErrorMessage = pgTextPointer(errorMessage)
	item.CreatedBy = pgTextPointer(createdBy)
	return item, err
}

func scanProductDirectVideoReference(row interface{ Scan(...any) error }) (DirectVideoReferenceSnapshot, error) {
	var item DirectVideoReferenceSnapshot
	var role string
	var primary bool
	var productOrdinal, width, height int
	err := row.Scan(
		&item.SourceID, &item.ArtifactID, &item.MediaFileID, &item.StorageKey,
		&item.MimeType, &item.ContentHash, &item.SourceRevision, &role, &primary,
		&productOrdinal, &width, &height,
	)
	if err != nil {
		return DirectVideoReferenceSnapshot{}, err
	}
	item.SourceType = "product"
	item.ProductReferenceID = directStringPointer(item.SourceID)
	item.Snapshot = mustJSON(map[string]any{
		"sourceType": "product", "sourceId": item.SourceID,
		"referenceRole": role, "isPrimary": primary, "productOrdinal": productOrdinal,
		"width": width, "height": height, "mimeType": item.MimeType,
		"contentHash": item.ContentHash, "sourceRevision": item.SourceRevision,
	})
	return item, nil
}

func directVideoReferenceFromCustom(custom ScriptReferenceImage) DirectVideoReferenceSnapshot {
	return DirectVideoReferenceSnapshot{
		SourceType: "custom", SourceID: custom.ID, ScriptReferenceImageID: directStringPointer(custom.ID),
		ArtifactID: custom.ArtifactID, MediaFileID: custom.MediaFileID,
		StorageKey: custom.StorageKey, MimeType: custom.MimeType,
		ContentHash: custom.ContentHash, SourceRevision: custom.Revision,
		Snapshot: mustJSON(map[string]any{
			"sourceType": "custom", "sourceId": custom.ID, "fileName": custom.OriginalFileName,
			"width": custom.Width, "height": custom.Height, "mimeType": custom.MimeType,
			"contentHash": custom.ContentHash, "sourceRevision": custom.Revision,
		}),
	}
}

func pgTextPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func directStringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func pointerValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

package commerce

import (
	"encoding/json"
	"time"
)

const ScriptDerivationMaxVariations = 20

type ScriptDerivationVariation struct {
	Ordinal int    `json:"ordinal"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	Brief   string `json:"brief"`
}

type CreateScriptDerivationInput struct {
	Dimension   string                      `json:"dimension"`
	Instruction string                      `json:"instruction"`
	Preserve    []string                    `json:"preserve"`
	Variations  []ScriptDerivationVariation `json:"variations"`
}

type ScriptDerivationPreviewInput struct {
	SourceScriptUnitID string   `json:"sourceScriptUnitId"`
	Count              int      `json:"count"`
	Dimension          string   `json:"dimension"`
	Instruction        string   `json:"instruction"`
	CandidateValues    []string `json:"candidateValues,omitempty"`
	Preserve           []string `json:"preserve,omitempty"`
}

type ScriptDerivationPreview struct {
	ContractVersion    string                      `json:"contractVersion"`
	SourceScriptUnitID string                      `json:"sourceScriptUnitId"`
	SourceScriptTitle  string                      `json:"sourceScriptTitle"`
	SourceContentHash  string                      `json:"sourceContentHash"`
	Dimension          string                      `json:"dimension"`
	Instruction        string                      `json:"instruction"`
	Preserve           []string                    `json:"preserve"`
	Variations         []ScriptDerivationVariation `json:"variations"`
	ProviderRequestID  string                      `json:"providerRequestId,omitempty"`
	ProviderCallID     string                      `json:"providerCallId,omitempty"`
	ProviderModelID    string                      `json:"providerModelId,omitempty"`
	PromptTemplateKey  string                      `json:"promptTemplateKey"`
	PromptVersionID    string                      `json:"promptVersionId"`
	PromptHash         string                      `json:"promptHash"`
}

type ScriptDerivationPromptBinding struct {
	TemplateKey     string          `json:"templateKey"`
	PromptVersionID string          `json:"promptVersionId"`
	ContentHash     string          `json:"contentHash"`
	Metadata        json.RawMessage `json:"metadata"`
}

type ScriptDerivationPromptContract struct {
	CandidatePlanner ScriptDerivationPromptBinding `json:"candidatePlanner"`
	Generator        ScriptDerivationPromptBinding `json:"generator"`
	Reviewer         ScriptDerivationPromptBinding `json:"reviewer"`
	Reviser          ScriptDerivationPromptBinding `json:"reviser"`
}

type ScriptDerivationRoutingSnapshot struct {
	ModelProfileID        string `json:"modelProfileId"`
	ModelProfileKey       string `json:"modelProfileKey"`
	ModelProfileBindingID string `json:"modelProfileBindingId"`
	BindingRevision       int64  `json:"bindingRevision"`
	ProviderModelID       string `json:"providerModelId"`
	ProviderModelKey      string `json:"providerModelKey"`
	Priority              int    `json:"priority"`
	Weight                int    `json:"weight"`
}

type ScriptDerivationBatch struct {
	ID                             string                          `json:"id"`
	OrganizationID                 string                          `json:"organizationId"`
	ProjectID                      string                          `json:"projectId"`
	ProductID                      string                          `json:"productId"`
	SourceScriptUnitID             string                          `json:"sourceScriptUnitId"`
	SourceContentSnapshot          string                          `json:"sourceContentSnapshot,omitempty"`
	SourceContentHash              string                          `json:"sourceContentHash"`
	ProductVersionID               string                          `json:"productVersionId"`
	ProductSnapshotHash            string                          `json:"productSnapshotHash"`
	ProductionGenerationID         string                          `json:"productionGenerationId"`
	VideoProductionBindingID       string                          `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64                           `json:"videoProductionBindingRevision"`
	ProductionConfigurationHash    string                          `json:"productionConfigurationHash"`
	ScriptModelProfileKey          string                          `json:"scriptModelProfileKey"`
	ModelProfileBindingID          *string                         `json:"modelProfileBindingId,omitempty"`
	ModelProfileBindingRevision    int64                           `json:"modelProfileBindingRevision"`
	ProviderModelID                *string                         `json:"providerModelId,omitempty"`
	RoutingSnapshotHash            string                          `json:"routingSnapshotHash"`
	PromptContract                 ScriptDerivationPromptContract  `json:"promptContract"`
	Dimension                      string                          `json:"dimension"`
	Instruction                    string                          `json:"instruction"`
	Preserve                       []string                        `json:"preserve"`
	Variations                     []ScriptDerivationVariation     `json:"variations"`
	RequestedCount                 int                             `json:"requestedCount"`
	RootBatchID                    *string                         `json:"rootBatchId,omitempty"`
	RetryOfBatchID                 *string                         `json:"retryOfBatchId,omitempty"`
	RetryDepth                     int                             `json:"retryDepth"`
	WorkflowRunID                  *string                         `json:"workflowRunId,omitempty"`
	Status                         string                          `json:"status"`
	QueuedCount                    int                             `json:"queuedCount"`
	RunningCount                   int                             `json:"runningCount"`
	SucceededCount                 int                             `json:"succeededCount"`
	FailedRetryableCount           int                             `json:"failedRetryableCount"`
	FailedTerminalCount            int                             `json:"failedTerminalCount"`
	CancelledCount                 int                             `json:"cancelledCount"`
	Revision                       int64                           `json:"revision"`
	CreatedBy                      *string                         `json:"createdBy,omitempty"`
	CreatedAt                      time.Time                       `json:"createdAt"`
	StartedAt                      *time.Time                      `json:"startedAt,omitempty"`
	CompletedAt                    *time.Time                      `json:"completedAt,omitempty"`
	CancelledAt                    *time.Time                      `json:"cancelledAt,omitempty"`
	UpdatedAt                      time.Time                       `json:"updatedAt"`
	Items                          []ScriptDerivationItem          `json:"items,omitempty"`
	Lineage                        []ScriptDerivationBatchSummary  `json:"lineage,omitempty"`
	LineageResults                 []ScriptDerivationLineageResult `json:"lineageResults,omitempty"`
}

type ScriptDerivationBatchSummary struct {
	ID                   string  `json:"id"`
	RootBatchID          *string `json:"rootBatchId,omitempty"`
	RetryOfBatchID       *string `json:"retryOfBatchId,omitempty"`
	RetryDepth           int     `json:"retryDepth"`
	Status               string  `json:"status"`
	SucceededCount       int     `json:"succeededCount"`
	FailedRetryableCount int     `json:"failedRetryableCount"`
	FailedTerminalCount  int     `json:"failedTerminalCount"`
	CancelledCount       int     `json:"cancelledCount"`
}

type ScriptDerivationLineageResult struct {
	VariationKey   string                 `json:"variationKey"`
	VariationLabel string                 `json:"variationLabel"`
	RootItemID     string                 `json:"rootItemId"`
	LatestResult   ScriptDerivationItem   `json:"latestResult"`
	Items          []ScriptDerivationItem `json:"items"`
}

type ScriptDerivationItem struct {
	ID                    string                    `json:"id"`
	BatchID               string                    `json:"batchId"`
	OrganizationID        string                    `json:"organizationId"`
	ProjectID             string                    `json:"projectId"`
	ProductID             string                    `json:"productId"`
	InputOrdinal          int                       `json:"inputOrdinal"`
	RootItemID            *string                   `json:"rootItemId,omitempty"`
	RetryOfItemID         *string                   `json:"retryOfItemId,omitempty"`
	VariationKey          string                    `json:"variationKey"`
	VariationLabel        string                    `json:"variationLabel"`
	VariationBrief        string                    `json:"variationBrief"`
	InputSnapshot         json.RawMessage           `json:"inputSnapshot"`
	InputHash             string                    `json:"inputHash"`
	ReservedUnitNo        int64                     `json:"reservedUnitNo"`
	ReservedSortOrder     int64                     `json:"reservedSortOrder"`
	Status                string                    `json:"status"`
	CurrentAttemptID      *string                   `json:"currentAttemptId,omitempty"`
	OutputScriptUnitID    *string                   `json:"outputScriptUnitId,omitempty"`
	OutputScriptVersionID *string                   `json:"outputScriptVersionId,omitempty"`
	ErrorCode             *string                   `json:"errorCode,omitempty"`
	ErrorMessage          *string                   `json:"errorMessage,omitempty"`
	Revision              int64                     `json:"revision"`
	CreatedAt             time.Time                 `json:"createdAt"`
	StartedAt             *time.Time                `json:"startedAt,omitempty"`
	CompletedAt           *time.Time                `json:"completedAt,omitempty"`
	UpdatedAt             time.Time                 `json:"updatedAt"`
	Attempts              []ScriptDerivationAttempt `json:"attempts,omitempty"`
}

type ScriptDerivationAttempt struct {
	ID                     string                        `json:"id"`
	BatchID                string                        `json:"batchId"`
	ItemID                 string                        `json:"itemId"`
	AttemptNo              int                           `json:"attemptNo"`
	RootAttemptID          *string                       `json:"rootAttemptId,omitempty"`
	RetryOfAttemptID       *string                       `json:"retryOfAttemptId,omitempty"`
	Status                 string                        `json:"status"`
	FinalOutputContentHash *string                       `json:"finalOutputContentHash,omitempty"`
	ReviewRound            int                           `json:"reviewRound"`
	ReviewResult           json.RawMessage               `json:"reviewResult"`
	ReviewFeedback         *string                       `json:"reviewFeedback,omitempty"`
	ErrorCode              *string                       `json:"errorCode,omitempty"`
	ErrorMessage           *string                       `json:"errorMessage,omitempty"`
	StartedAt              *time.Time                    `json:"startedAt,omitempty"`
	CompletedAt            *time.Time                    `json:"completedAt,omitempty"`
	CreatedAt              time.Time                     `json:"createdAt"`
	UpdatedAt              time.Time                     `json:"updatedAt"`
	Calls                  []ScriptDerivationAttemptCall `json:"calls,omitempty"`
}

type ScriptDerivationAttemptCall struct {
	ID                    string     `json:"id"`
	BatchID               string     `json:"batchId"`
	ItemID                string     `json:"itemId"`
	AttemptID             string     `json:"attemptId"`
	OrganizationID        string     `json:"organizationId"`
	ProjectID             string     `json:"projectId"`
	ProductID             string     `json:"productId"`
	RoundNo               int        `json:"roundNo"`
	Phase                 string     `json:"phase"`
	ProviderRequestID     *string    `json:"providerRequestId,omitempty"`
	ProviderCallID        *string    `json:"providerCallId,omitempty"`
	ModelProfileKey       string     `json:"modelProfileKey"`
	ModelProfileBindingID *string    `json:"modelProfileBindingId,omitempty"`
	ProviderModelID       *string    `json:"providerModelId,omitempty"`
	PromptTemplateKey     string     `json:"promptTemplateKey"`
	PromptVersionID       string     `json:"promptVersionId"`
	PromptHash            string     `json:"promptHash"`
	OutputContentHash     *string    `json:"outputContentHash,omitempty"`
	Status                string     `json:"status"`
	ErrorCode             *string    `json:"errorCode,omitempty"`
	ErrorMessage          *string    `json:"errorMessage,omitempty"`
	StartedAt             time.Time  `json:"startedAt"`
	CompletedAt           *time.Time `json:"completedAt,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
}

type ScriptDerivationBatchList struct {
	Items      []ScriptDerivationBatch `json:"items"`
	NextCursor string                  `json:"nextCursor,omitempty"`
	HasMore    bool                    `json:"hasMore"`
}

type PrepareScriptDerivationParams struct {
	BatchID        string
	WorkflowRunID  string
	OrganizationID string
	ProjectID      string
	ScriptUnitID   string
	CreatedBy      string
	IdempotencyKey string
	RequestHash    string
	Input          CreateScriptDerivationInput
}

type PreparedScriptDerivation struct {
	Batch      ScriptDerivationBatch
	Product    Product
	Production ProductionContext
	Positions  []ScriptUnitPosition
}

package edition

import (
	"context"
	"net/http"
	"time"
)

const ContractVersionV2 = "edition.v2"

type Edition string

const (
	EditionCommunity Edition = "community"
	EditionCloud     Edition = "cloud"
)

type OperationalMode string

const (
	OperationalModeNormal               OperationalMode = "normal"
	OperationalModeCommercialRestricted OperationalMode = "commercial_restricted"
)

type RestrictionReason string

const (
	RestrictionInternalReleaseMismatch RestrictionReason = "internal_release_mismatch"
	RestrictionCommercialWritesFrozen  RestrictionReason = "commercial_writes_frozen"
)

type FeatureKey string

const (
	FeatureCoreWorkflow        FeatureKey = "core.workflow"
	FeatureCoreProviderGateway FeatureKey = "core.provider_gateway"
	FeatureCoreSelfHosting     FeatureKey = "core.self_hosting"

	FeatureBillingShadowAccount      FeatureKey = "billing.shadow_account"
	FeatureBillingBalance            FeatureKey = "billing.balance"
	FeatureBillingTopUp              FeatureKey = "billing.top_up"
	FeatureBillingSubscription       FeatureKey = "billing.subscription"
	FeatureBillingOrganizationWallet FeatureKey = "billing.organization_wallet"
	FeatureBillingReconciliation     FeatureKey = "billing.reconciliation"
	FeatureBillingInvoice            FeatureKey = "billing.invoice"
	FeatureGovernanceSSO             FeatureKey = "governance.sso"
	FeatureGovernanceSCIM            FeatureKey = "governance.scim"
	FeatureGovernanceAuditExport     FeatureKey = "governance.audit_export"
	FeatureOperationsSupportedHA     FeatureKey = "operations.supported_ha_tooling"
	FeatureOperationsManagedDR       FeatureKey = "operations.managed_disaster_recovery"
)

type DenialCode string

const (
	DenialFeatureUnknown                 DenialCode = "feature_unknown"
	DenialFeatureNotCompiled             DenialCode = "feature_not_compiled"
	DenialInternalReleaseMismatch        DenialCode = "internal_release_mismatch"
	DenialCommercialWritesFrozen         DenialCode = "commercial_writes_frozen"
	DenialPlanEntitlementRequired        DenialCode = "plan_entitlement_required"
	DenialBillingAccountSuspended        DenialCode = "billing_account_suspended"
	DenialPermission                     DenialCode = "permission_denied"
	DenialBillingBindingInvalid          DenialCode = "billing_binding_invalid"
	DenialBillingAccountScopeMismatch    DenialCode = "billing_account_scope_mismatch"
	DenialBillingAuthorityMismatch       DenialCode = "billing_authority_mismatch"
	DenialBillingSponsorshipRequired     DenialCode = "billing_sponsorship_required"
	DenialBillingRoutingCandidateMissing DenialCode = "billing_routing_candidate_missing"
	DenialBillingInsufficientBalance     DenialCode = "billing_insufficient_balance"
	DenialBillingCredentialUnavailable   DenialCode = "billing_credential_unavailable"
	DenialBillingModelForbidden          DenialCode = "billing_model_forbidden"
	DenialBillingUpstreamUnavailable     DenialCode = "billing_upstream_unavailable"
)

type CommercialOperation string

const (
	CommercialOperationReadOrExport       CommercialOperation = "read_or_export"
	CommercialOperationCore               CommercialOperation = "core_operation"
	CommercialOperationWrite              CommercialOperation = "commercial_write"
	CommercialOperationPaidProviderCreate CommercialOperation = "paid_provider_create"
	CommercialOperationAsyncPollOrCancel  CommercialOperation = "async_poll_or_cancel"
	CommercialOperationFinalization       CommercialOperation = "finalization"
	CommercialOperationIdempotentRecovery CommercialOperation = "idempotent_recovery"
)

type FeatureDescriptor struct {
	Key                       FeatureKey `json:"key"`
	MinimumContractVersion    string     `json:"minimumContractVersion"`
	RequiresTenantEntitlement bool       `json:"requiresTenantEntitlement"`
	RequiredPermissions       []string   `json:"requiredPermissions"`
	BackendEnforcementPoint   string     `json:"backendEnforcementPoint"`
	FrontendEntry             string     `json:"frontendEntry"`
	DenialCode                DenialCode `json:"denialCode"`
	DegradationBehavior       string     `json:"degradationBehavior"`
	AffectsInFlightWorkflow   bool       `json:"affectsInFlightWorkflow"`
	AuditEvent                string     `json:"auditEvent"`
	MetricName                string     `json:"metricName"`
}

type FeatureRegistry interface {
	All() []FeatureDescriptor
	Lookup(FeatureKey) (FeatureDescriptor, bool)
}

type CompiledModule struct {
	Key         string     `json:"key"`
	FeatureKey  FeatureKey `json:"featureKey"`
	ContentHash string     `json:"contentHash"`
}

type Manifest struct {
	DeploymentEdition   Edition          `json:"deploymentEdition"`
	DistributionID      string           `json:"distributionId"`
	CoreReleaseID       string           `json:"coreReleaseId"`
	CommercialReleaseID *string          `json:"commercialReleaseId,omitempty"`
	ContractVersion     string           `json:"contractVersion"`
	ContractHash        string           `json:"contractHash"`
	CompiledFeatures    []FeatureKey     `json:"compiledFeatures"`
	CompiledModules     []CompiledModule `json:"compiledModules"`
}

type OperationalState struct {
	Mode              OperationalMode   `json:"operationalMode"`
	RestrictionReason RestrictionReason `json:"restrictionReason,omitempty"`
}

type SystemEdition struct {
	Manifest
	OperationalState
}

type EditionProvider interface {
	Manifest(context.Context) (Manifest, error)
	OperationalState(context.Context) (OperationalState, error)
}

type EntitlementSubject struct {
	UserID           string `json:"userId"`
	OrganizationID   string `json:"organizationId"`
	BillingAccountID string `json:"billingAccountId,omitempty"`
}

type EntitlementRequest struct {
	Subject                          EntitlementSubject
	FeatureKeys                      []FeatureKey
	Operation                        CommercialOperation
	ProvesNoAdditionalProviderCharge bool
}

type EntitlementDecision struct {
	FeatureKey        FeatureKey `json:"featureKey"`
	Compiled          bool       `json:"compiled"`
	DeploymentEnabled bool       `json:"deploymentEnabled"`
	TenantEntitled    bool       `json:"tenantEntitled"`
	Allowed           bool       `json:"allowed"`
	Reason            DenialCode `json:"reason,omitempty"`
}

type EntitlementSnapshot struct {
	ContractVersion string                `json:"contractVersion"`
	Edition         Edition               `json:"edition"`
	Subject         EntitlementSubject    `json:"subject"`
	Decisions       []EntitlementDecision `json:"decisions"`
	EvaluatedAt     time.Time             `json:"evaluatedAt"`
}

type EntitlementService interface {
	Evaluate(context.Context, EntitlementRequest) (EntitlementSnapshot, error)
}

type ProviderManagementScope string

const (
	ProviderManagementScopeTenant ProviderManagementScope = "tenant_managed"
	ProviderManagementScopeSystem ProviderManagementScope = "system_managed"
)

type BillingContextReference struct {
	ID           string `json:"id"`
	Revision     int64  `json:"revision"`
	SnapshotHash string `json:"snapshotHash"`
}

type BillingRoutingCandidate struct {
	CredentialID      string                  `json:"credentialId"`
	ProviderAccountID string                  `json:"providerAccountId"`
	OrganizationID    string                  `json:"organizationId"`
	ManagementScope   ProviderManagementScope `json:"managementScope"`
	ConstraintRef     string                  `json:"constraintRef,omitempty"`
}

type BillingRoutingRequest struct {
	OrganizationID    string
	ProjectID         string
	RequestedByUserID string
	ProviderModelID   string
	BillingContext    *BillingContextReference
	Candidates        []BillingRoutingCandidate
}

type BillingRoutingAuditSnapshot struct {
	Edition               Edition `json:"edition"`
	BillingContextPresent bool    `json:"billingContextPresent"`
	CandidateCount        int     `json:"candidateCount"`
	AllowedCandidateCount int     `json:"allowedCandidateCount"`
	CandidateSetHash      string  `json:"candidateSetHash"`
}

type BillingRoutingDecision struct {
	AllowedCredentialIDs []string                    `json:"allowedCredentialIds"`
	AuditSnapshot        BillingRoutingAuditSnapshot `json:"auditSnapshot"`
}

type BillingRoutingAuthorizer interface {
	Authorize(context.Context, BillingRoutingRequest) (BillingRoutingDecision, error)
}

type BillingAccountScope string

const (
	BillingAccountScopeOrganization BillingAccountScope = "organization"
	BillingAccountScopePersonal     BillingAccountScope = "personal"
)

type EffectiveSpendAuthorizationFacts struct {
	ProjectOperationAllowedByRBAC          bool
	BillingSpendAllowedByRBACForProject    bool
	ActiveBindingMatchesContextRevision    bool
	AccountScope                           BillingAccountScope
	AccountAndProjectSameOrganization      bool
	AccountOwnerMatchesSponsorshipSponsor  bool
	SponsorshipActiveForProjectAndRevision bool
}

type BillingAuthorityIsolationFacts struct {
	ContextAuthorityRef        string
	AccountAuthorityRef        string
	CredentialAuthorityRef     string
	ContextOrganizationID      string
	AccountOrganizationID      string
	CredentialOrganizationID   string
	ContextBillingAccountID    string
	CredentialBillingAccountID string
}

type APIResourceScope string

const (
	APIResourceScopeOrganization APIResourceScope = "organization"
	APIResourceScopeWorkspace    APIResourceScope = "workspace"
	APIResourceScopeProject      APIResourceScope = "project"
)

type APIPrincipal struct {
	UserID           string
	OrganizationID   string
	BillingAccountID string
}

type APIModuleHandler func(http.ResponseWriter, *http.Request, APIPrincipal)

type APIModuleRegistration struct {
	ModuleKey             string
	FeatureKey            FeatureKey
	Method                string
	Pattern               string
	OperationID           string
	Operation             CommercialOperation
	RequiredPermissions   []string
	ResourceScope         APIResourceScope
	ResourcePathParameter string
	Handler               APIModuleHandler
}

type EventMessage struct {
	Key            string
	OrganizationID string
	AggregateType  string
	AggregateID    string
	Payload        []byte
}

type EventConsumerRegistration struct {
	ModuleKey   string
	FeatureKey  FeatureKey
	EventKey    string
	ConsumerKey string
	Consume     func(context.Context, EventMessage) error
}

type BackgroundTaskRegistration struct {
	ModuleKey  string
	FeatureKey FeatureKey
	TaskKey    string
	Run        func(context.Context) error
}

type CommercialModuleRegistry interface {
	APIModules(context.Context) ([]APIModuleRegistration, error)
	EventConsumers(context.Context) ([]EventConsumerRegistration, error)
	BackgroundTasks(context.Context) ([]BackgroundTaskRegistration, error)
}

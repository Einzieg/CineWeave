package edition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const CommunityDistributionID = "cineweave-ce"

var communityCompiledFeatures = []FeatureKey{
	FeatureCoreWorkflow,
	FeatureCoreProviderGateway,
	FeatureCoreSelfHosting,
}

type CommunityOptions struct {
	CoreReleaseID    string
	RequestedEdition string
	ContractHash     string
	Now              func() time.Time
}

type Runtime struct {
	EditionProvider          EditionProvider
	Entitlements             EntitlementService
	BillingRoutingAuthorizer BillingRoutingAuthorizer
	CommercialModules        CommercialModuleRegistry
	Features                 FeatureRegistry
}

func NewCommunityRuntime(options CommunityOptions) (*Runtime, error) {
	requestedEdition := Edition(strings.ToLower(strings.TrimSpace(options.RequestedEdition)))
	if requestedEdition == "" {
		requestedEdition = EditionCommunity
	}
	if requestedEdition != EditionCommunity {
		return nil, fmt.Errorf("community build cannot run as %q: %w", requestedEdition, newAuthorizationError(DenialFeatureNotCompiled, "commercial edition is not compiled"))
	}
	coreReleaseID := strings.TrimSpace(options.CoreReleaseID)
	if coreReleaseID == "" {
		coreReleaseID = "local-dev"
	}
	registry := DefaultFeatureRegistry()
	contractHash := strings.TrimSpace(options.ContractHash)
	if contractHash == "" {
		contractHash = communityContractHash(registry)
	}
	manifest := Manifest{
		DeploymentEdition: EditionCommunity,
		DistributionID:    CommunityDistributionID,
		CoreReleaseID:     coreReleaseID,
		ContractVersion:   ContractVersionV1,
		ContractHash:      contractHash,
		CompiledFeatures:  append([]FeatureKey(nil), communityCompiledFeatures...),
		CompiledModules:   []CompiledModule{},
	}
	provider := staticEditionProvider{
		manifest: manifest,
		state: OperationalState{
			Mode: OperationalModeNormal,
		},
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	runtime := &Runtime{
		EditionProvider:          provider,
		Entitlements:             communityEntitlementService{manifest: manifest, registry: registry, now: now},
		BillingRoutingAuthorizer: communityBillingRoutingAuthorizer{},
		CommercialModules:        emptyCommercialModuleRegistry{},
		Features:                 registry,
	}
	if err := runtime.Validate(context.Background()); err != nil {
		return nil, err
	}
	return runtime, nil
}

func MustCommunityRuntime() *Runtime {
	runtime, err := NewCommunityRuntime(CommunityOptions{})
	if err != nil {
		panic(err)
	}
	return runtime
}

func (r *Runtime) Validate(ctx context.Context) error {
	if r == nil || r.EditionProvider == nil || r.Entitlements == nil || r.BillingRoutingAuthorizer == nil || r.CommercialModules == nil || r.Features == nil {
		return fmt.Errorf("edition runtime is incomplete")
	}
	manifest, err := r.EditionProvider.Manifest(ctx)
	if err != nil {
		return fmt.Errorf("load edition manifest: %w", err)
	}
	if err := validateManifest(manifest, r.Features); err != nil {
		return err
	}
	state, err := r.EditionProvider.OperationalState(ctx)
	if err != nil {
		return fmt.Errorf("load edition operational state: %w", err)
	}
	if state.Mode != OperationalModeNormal && state.Mode != OperationalModeCommercialRestricted {
		return fmt.Errorf("edition operational mode %q is invalid", state.Mode)
	}
	if state.Mode == OperationalModeNormal && state.RestrictionReason != "" {
		return fmt.Errorf("normal edition state cannot contain a restriction reason")
	}
	if state.Mode == OperationalModeCommercialRestricted && !validRestrictionReason(state.RestrictionReason) {
		return fmt.Errorf("commercial restricted state requires a public restriction reason")
	}
	return validateModuleRegistry(ctx, manifest, r.CommercialModules)
}

func (r *Runtime) SystemEdition(ctx context.Context) (SystemEdition, error) {
	manifest, err := r.EditionProvider.Manifest(ctx)
	if err != nil {
		return SystemEdition{}, err
	}
	state, err := r.EditionProvider.OperationalState(ctx)
	if err != nil {
		return SystemEdition{}, err
	}
	return SystemEdition{Manifest: manifest, OperationalState: state}, nil
}

type staticEditionProvider struct {
	manifest Manifest
	state    OperationalState
}

func (p staticEditionProvider) Manifest(context.Context) (Manifest, error) {
	return cloneManifest(p.manifest), nil
}

func (p staticEditionProvider) OperationalState(context.Context) (OperationalState, error) {
	return p.state, nil
}

type communityEntitlementService struct {
	manifest Manifest
	registry FeatureRegistry
	now      func() time.Time
}

func (s communityEntitlementService) Evaluate(_ context.Context, request EntitlementRequest) (EntitlementSnapshot, error) {
	if strings.TrimSpace(request.Subject.UserID) == "" || strings.TrimSpace(request.Subject.OrganizationID) == "" {
		return EntitlementSnapshot{}, newAuthorizationError(DenialPermission, "user and organization are required")
	}
	compiled := make(map[FeatureKey]struct{}, len(s.manifest.CompiledFeatures))
	for _, key := range s.manifest.CompiledFeatures {
		compiled[key] = struct{}{}
	}
	keys := append([]FeatureKey(nil), request.FeatureKeys...)
	if len(keys) == 0 {
		descriptors := s.registry.All()
		keys = make([]FeatureKey, 0, len(descriptors))
		for _, descriptor := range descriptors {
			keys = append(keys, descriptor.Key)
		}
	}
	decisions := make([]EntitlementDecision, 0, len(keys))
	seen := make(map[FeatureKey]struct{}, len(keys))
	for _, key := range keys {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if _, known := s.registry.Lookup(key); !known {
			decisions = append(decisions, EntitlementDecision{FeatureKey: key, Reason: DenialFeatureUnknown})
			continue
		}
		_, isCompiled := compiled[key]
		decision := EntitlementDecision{
			FeatureKey:         key,
			Compiled:           isCompiled,
			DeploymentLicensed: isCompiled,
			TenantEntitled:     isCompiled,
			Allowed:            isCompiled,
		}
		if !isCompiled {
			decision.Reason = DenialFeatureNotCompiled
		}
		decisions = append(decisions, decision)
	}
	return EntitlementSnapshot{
		ContractVersion: s.manifest.ContractVersion,
		Edition:         s.manifest.DeploymentEdition,
		Subject:         request.Subject,
		Decisions:       decisions,
		EvaluatedAt:     s.now().UTC(),
	}, nil
}

type communityBillingRoutingAuthorizer struct{}

func (communityBillingRoutingAuthorizer) Authorize(_ context.Context, request BillingRoutingRequest) (BillingRoutingDecision, error) {
	if request.BillingContext != nil {
		return BillingRoutingDecision{}, newAuthorizationError(DenialFeatureNotCompiled, "commercial billing context is not available in the community edition")
	}
	allowed := make([]string, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		if candidate.ManagementScope != ProviderManagementScopeTenant {
			continue
		}
		if strings.TrimSpace(candidate.CredentialID) == "" ||
			strings.TrimSpace(request.OrganizationID) == "" ||
			candidate.OrganizationID != request.OrganizationID {
			continue
		}
		allowed = append(allowed, candidate.CredentialID)
	}
	sort.Strings(allowed)
	if len(allowed) == 0 {
		return BillingRoutingDecision{}, newAuthorizationError(DenialBillingRoutingCandidateMissing, "no tenant-managed provider credential is eligible")
	}
	return BillingRoutingDecision{
		AllowedCredentialIDs: allowed,
		AuditSnapshot: BillingRoutingAuditSnapshot{
			Edition:               EditionCommunity,
			BillingContextPresent: false,
			CandidateCount:        len(request.Candidates),
			AllowedCandidateCount: len(allowed),
			CandidateSetHash:      candidateSetHash(allowed),
		},
	}, nil
}

type emptyCommercialModuleRegistry struct{}

func (emptyCommercialModuleRegistry) APIModules(context.Context) ([]APIModuleRegistration, error) {
	return []APIModuleRegistration{}, nil
}

func (emptyCommercialModuleRegistry) EventConsumers(context.Context) ([]EventConsumerRegistration, error) {
	return []EventConsumerRegistration{}, nil
}

func (emptyCommercialModuleRegistry) BackgroundTasks(context.Context) ([]BackgroundTaskRegistration, error) {
	return []BackgroundTaskRegistration{}, nil
}

func EvaluateEffectiveSpendAuthorization(facts EffectiveSpendAuthorizationFacts) error {
	if !facts.ProjectOperationAllowedByRBAC || !facts.BillingSpendAllowedByRBACForProject {
		return newAuthorizationError(DenialPermission, "project operation and billing.spend must both be granted by RBAC")
	}
	if !facts.ActiveBindingMatchesContextRevision {
		return newAuthorizationError(DenialBillingBindingInvalid, "active project billing binding does not match the billing context revision")
	}
	switch facts.AccountScope {
	case BillingAccountScopeOrganization:
		if !facts.AccountAndProjectSameOrganization {
			return newAuthorizationError(DenialBillingAccountScopeMismatch, "organization wallet and project must belong to the same organization")
		}
	case BillingAccountScopePersonal:
		if !facts.AccountOwnerMatchesSponsorshipSponsor || !facts.SponsorshipActiveForProjectAndRevision {
			return newAuthorizationError(DenialBillingSponsorshipRequired, "an active owner sponsorship for the project and binding revision is required")
		}
	default:
		return newAuthorizationError(DenialBillingAccountScopeMismatch, "billing account scope is invalid")
	}
	return nil
}

func validateManifest(manifest Manifest, registry FeatureRegistry) error {
	if manifest.DeploymentEdition != EditionCommunity && manifest.DeploymentEdition != EditionCloud && manifest.DeploymentEdition != EditionEnterprise {
		return fmt.Errorf("deployment edition %q is invalid", manifest.DeploymentEdition)
	}
	if strings.TrimSpace(manifest.DistributionID) == "" ||
		strings.TrimSpace(manifest.CoreReleaseID) == "" ||
		strings.TrimSpace(manifest.ContractVersion) == "" ||
		strings.TrimSpace(manifest.ContractHash) == "" {
		return fmt.Errorf("edition manifest identity is incomplete")
	}
	seen := make(map[FeatureKey]struct{}, len(manifest.CompiledFeatures))
	for _, key := range manifest.CompiledFeatures {
		if _, exists := seen[key]; exists {
			return fmt.Errorf("compiled feature %q is duplicated", key)
		}
		if _, known := registry.Lookup(key); !known {
			return fmt.Errorf("compiled feature %q is not declared in the feature registry", key)
		}
		seen[key] = struct{}{}
	}
	if manifest.DeploymentEdition == EditionCommunity {
		if manifest.CommercialReleaseID != nil || len(manifest.CompiledModules) > 0 {
			return fmt.Errorf("community manifest cannot contain commercial release identity or modules")
		}
	}
	return nil
}

func validateModuleRegistry(ctx context.Context, manifest Manifest, registry CommercialModuleRegistry) error {
	compiled := make(map[FeatureKey]struct{}, len(manifest.CompiledFeatures))
	for _, feature := range manifest.CompiledFeatures {
		compiled[feature] = struct{}{}
	}
	apiModules, err := registry.APIModules(ctx)
	if err != nil {
		return fmt.Errorf("load commercial API modules: %w", err)
	}
	eventConsumers, err := registry.EventConsumers(ctx)
	if err != nil {
		return fmt.Errorf("load commercial event consumers: %w", err)
	}
	backgroundTasks, err := registry.BackgroundTasks(ctx)
	if err != nil {
		return fmt.Errorf("load commercial background tasks: %w", err)
	}
	for _, registration := range apiModules {
		if registration.ModuleKey == "" || registration.Method == "" || registration.Pattern == "" || registration.OperationID == "" || registration.Handler == nil {
			return fmt.Errorf("commercial API module registration is incomplete")
		}
		if _, ok := compiled[registration.FeatureKey]; !ok {
			return fmt.Errorf("commercial API module %q uses uncompiled feature %q", registration.ModuleKey, registration.FeatureKey)
		}
	}
	for _, registration := range eventConsumers {
		if registration.ModuleKey == "" || registration.EventKey == "" || registration.ConsumerKey == "" || registration.Consume == nil {
			return fmt.Errorf("commercial event consumer registration is incomplete")
		}
		if _, ok := compiled[registration.FeatureKey]; !ok {
			return fmt.Errorf("commercial event consumer %q uses uncompiled feature %q", registration.ConsumerKey, registration.FeatureKey)
		}
	}
	for _, registration := range backgroundTasks {
		if registration.ModuleKey == "" || registration.TaskKey == "" || registration.Run == nil {
			return fmt.Errorf("commercial background task registration is incomplete")
		}
		if _, ok := compiled[registration.FeatureKey]; !ok {
			return fmt.Errorf("commercial background task %q uses uncompiled feature %q", registration.TaskKey, registration.FeatureKey)
		}
	}
	return nil
}

func validRestrictionReason(reason RestrictionReason) bool {
	switch reason {
	case RestrictionLicenseInvalid, RestrictionLicenseNotYetValid, RestrictionLicenseExpired, RestrictionLicenseRevoked, RestrictionClockRollback, RestrictionDeploymentMismatch:
		return true
	default:
		return false
	}
}

func cloneManifest(manifest Manifest) Manifest {
	manifest.CompiledFeatures = append([]FeatureKey(nil), manifest.CompiledFeatures...)
	manifest.CompiledModules = append([]CompiledModule(nil), manifest.CompiledModules...)
	if manifest.CommercialReleaseID != nil {
		value := *manifest.CommercialReleaseID
		manifest.CommercialReleaseID = &value
	}
	return manifest
}

func communityContractHash(registry FeatureRegistry) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(ContractVersionV1))
	for _, descriptor := range registry.All() {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(descriptor.Key))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(descriptor.MinimumContractVersion))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func candidateSetHash(credentialIDs []string) string {
	hash := sha256.New()
	for _, credentialID := range credentialIDs {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(credentialID))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

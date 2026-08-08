package edition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/projectcontrol"
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
	TenantLifecycle          TenantLifecycle
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
		ContractVersion:   ContractVersionV2,
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
		TenantLifecycle:          noopTenantLifecycle{},
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
	if r == nil || r.EditionProvider == nil || r.Entitlements == nil || r.BillingRoutingAuthorizer == nil || r.CommercialModules == nil || r.TenantLifecycle == nil || r.Features == nil {
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
	return validateModuleRegistry(ctx, manifest, r.CommercialModules, r.Features)
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

func (r *Runtime) ValidatedAPIModules(ctx context.Context) ([]APIModuleRegistration, error) {
	if err := r.Validate(ctx); err != nil {
		return nil, err
	}
	registrations, err := r.CommercialModules.APIModules(ctx)
	if err != nil {
		return nil, fmt.Errorf("load commercial API modules: %w", err)
	}
	cloned := make([]APIModuleRegistration, 0, len(registrations))
	for _, registration := range registrations {
		registration.RequiredPermissions = append([]string(nil), registration.RequiredPermissions...)
		cloned = append(cloned, registration)
	}
	return cloned, nil
}

func (r *Runtime) ValidatedProjectControlActions(ctx context.Context) ([]ProjectControlActionRegistration, error) {
	if err := r.Validate(ctx); err != nil {
		return nil, err
	}
	registry, ok := r.CommercialModules.(ProjectControlModuleRegistry)
	if !ok {
		return []ProjectControlActionRegistration{}, nil
	}
	registrations, err := registry.ProjectControlActions(ctx)
	if err != nil {
		return nil, fmt.Errorf("load commercial project-control actions: %w", err)
	}
	cloned := make([]ProjectControlActionRegistration, 0, len(registrations))
	for _, registration := range registrations {
		registration.QueryParameters = append([]string(nil), registration.QueryParameters...)
		registration.Descriptor = registration.Descriptor.Clone()
		cloned = append(cloned, registration)
	}
	return cloned, nil
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
			FeatureKey:        key,
			Compiled:          isCompiled,
			DeploymentEnabled: isCompiled,
			TenantEntitled:    isCompiled,
			Allowed:           isCompiled,
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

type noopTenantLifecycle struct{}

func (noopTenantLifecycle) OrganizationCreated(context.Context, OrganizationCreated) {}

func (noopTenantLifecycle) ProjectCreated(context.Context, ProjectCreated) {}

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
	if manifest.DeploymentEdition != EditionCommunity && manifest.DeploymentEdition != EditionCloud {
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
	moduleKeys := make(map[string]struct{}, len(manifest.CompiledModules))
	for _, module := range manifest.CompiledModules {
		moduleKey := strings.TrimSpace(module.Key)
		if moduleKey == "" || strings.TrimSpace(module.ContentHash) == "" {
			return fmt.Errorf("compiled module identity is incomplete")
		}
		if _, exists := moduleKeys[moduleKey]; exists {
			return fmt.Errorf("compiled module %q is duplicated", moduleKey)
		}
		if _, ok := seen[module.FeatureKey]; !ok {
			return fmt.Errorf("compiled module %q uses uncompiled feature %q", moduleKey, module.FeatureKey)
		}
		moduleKeys[moduleKey] = struct{}{}
	}
	return nil
}

func validateModuleRegistry(ctx context.Context, manifest Manifest, registry CommercialModuleRegistry, features FeatureRegistry) error {
	compiled := make(map[FeatureKey]struct{}, len(manifest.CompiledFeatures))
	for _, feature := range manifest.CompiledFeatures {
		compiled[feature] = struct{}{}
	}
	compiledModules := make(map[string]CompiledModule, len(manifest.CompiledModules))
	for _, module := range manifest.CompiledModules {
		compiledModules[module.Key] = module
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
	apiRoutes := make(map[string]struct{}, len(apiModules))
	operationIDs := make(map[string]struct{}, len(apiModules))
	for _, registration := range apiModules {
		if err := validateAPIModuleRegistration(registration, compiled, compiledModules, features); err != nil {
			return err
		}
		routeKey := registration.Method + " " + registration.Pattern
		if _, exists := apiRoutes[routeKey]; exists {
			return fmt.Errorf("commercial API route %q is duplicated", routeKey)
		}
		apiRoutes[routeKey] = struct{}{}
		if _, exists := operationIDs[registration.OperationID]; exists {
			return fmt.Errorf("commercial API operationId %q is duplicated", registration.OperationID)
		}
		operationIDs[registration.OperationID] = struct{}{}
	}
	for _, registration := range eventConsumers {
		if registration.ModuleKey == "" || registration.EventKey == "" || registration.ConsumerKey == "" || registration.Consume == nil {
			return fmt.Errorf("commercial event consumer registration is incomplete")
		}
		if err := validateCommercialModuleIdentity(registration.ModuleKey, registration.FeatureKey, compiled, compiledModules, features); err != nil {
			return fmt.Errorf("commercial event consumer %q: %w", registration.ConsumerKey, err)
		}
	}
	for _, registration := range backgroundTasks {
		if registration.ModuleKey == "" || registration.TaskKey == "" || registration.Run == nil {
			return fmt.Errorf("commercial background task registration is incomplete")
		}
		if err := validateCommercialModuleIdentity(registration.ModuleKey, registration.FeatureKey, compiled, compiledModules, features); err != nil {
			return fmt.Errorf("commercial background task %q: %w", registration.TaskKey, err)
		}
	}
	if projectControlRegistry, ok := registry.(ProjectControlModuleRegistry); ok {
		projectControlActions, actionErr := projectControlRegistry.ProjectControlActions(ctx)
		if actionErr != nil {
			return fmt.Errorf("load commercial project-control actions: %w", actionErr)
		}
		if err := validateProjectControlActionRegistrations(projectControlActions, apiModules); err != nil {
			return err
		}
	}
	return nil
}

func validateProjectControlActionRegistrations(
	registrations []ProjectControlActionRegistration,
	apiModules []APIModuleRegistration,
) error {
	apiByOperation := make(map[string]APIModuleRegistration, len(apiModules))
	for _, registration := range apiModules {
		apiByOperation[registration.OperationID] = registration
	}
	actionNames := make(map[string]struct{}, len(registrations))
	operationIDs := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		descriptor := registration.Descriptor
		if registration.Handler == nil {
			return fmt.Errorf("commercial project-control action %q has no shared domain handler", descriptor.Name)
		}
		if err := descriptor.Validate(); err != nil {
			return fmt.Errorf("commercial project-control action %q: %w", descriptor.Name, err)
		}
		if descriptor.Scope != projectcontrol.ScopeProject {
			return fmt.Errorf("commercial project-control action %q must use project scope", descriptor.Name)
		}
		if !descriptor.ExportToMCP {
			return fmt.Errorf("commercial project-control action %q must be exported to MCP", descriptor.Name)
		}
		if _, exists := actionNames[descriptor.Name]; exists {
			return fmt.Errorf("commercial project-control action %q is duplicated", descriptor.Name)
		}
		actionNames[descriptor.Name] = struct{}{}

		operationID := strings.TrimSpace(registration.APIOperationID)
		apiRegistration, exists := apiByOperation[operationID]
		if !exists {
			return fmt.Errorf("commercial project-control action %q references unknown API operation %q", descriptor.Name, operationID)
		}
		if _, exists := operationIDs[operationID]; exists {
			return fmt.Errorf("commercial API operation %q is linked to multiple project-control actions", operationID)
		}
		operationIDs[operationID] = struct{}{}
		if registration.ModuleKey != apiRegistration.ModuleKey || registration.FeatureKey != apiRegistration.FeatureKey {
			return fmt.Errorf("commercial project-control action %q module identity disagrees with API operation %q", descriptor.Name, operationID)
		}
		if apiRegistration.ResourceScope != APIResourceScopeProject || apiRegistration.ResourcePathParameter != "projectId" {
			return fmt.Errorf("commercial project-control action %q must bind a project-scoped API operation", descriptor.Name)
		}
		apiReadOnly := apiRegistration.Operation == CommercialOperationReadOrExport
		if descriptor.ReadOnly != apiReadOnly {
			return fmt.Errorf("commercial project-control action %q read-only policy disagrees with API operation %q", descriptor.Name, operationID)
		}
		if strings.Join(sortedDistinctStrings(descriptor.Permissions), "\x00") != strings.Join(sortedDistinctStrings(apiRegistration.RequiredPermissions), "\x00") {
			return fmt.Errorf("commercial project-control action %q permissions disagree with API operation %q", descriptor.Name, operationID)
		}
		queryNames := make(map[string]struct{}, len(registration.QueryParameters))
		for _, queryName := range registration.QueryParameters {
			queryName = strings.TrimSpace(queryName)
			if queryName == "" || strings.ContainsAny(queryName, "{}[] \t\r\n") {
				return fmt.Errorf("commercial project-control action %q has invalid query parameter %q", descriptor.Name, queryName)
			}
			if _, exists := queryNames[queryName]; exists {
				return fmt.Errorf("commercial project-control action %q duplicates query parameter %q", descriptor.Name, queryName)
			}
			queryNames[queryName] = struct{}{}
		}
	}
	return nil
}

func sortedDistinctStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validateAPIModuleRegistration(
	registration APIModuleRegistration,
	compiled map[FeatureKey]struct{},
	compiledModules map[string]CompiledModule,
	features FeatureRegistry,
) error {
	if strings.TrimSpace(registration.ModuleKey) == "" ||
		strings.TrimSpace(registration.Method) == "" ||
		strings.TrimSpace(registration.Pattern) == "" ||
		strings.TrimSpace(registration.OperationID) == "" ||
		registration.Handler == nil {
		return fmt.Errorf("commercial API module registration is incomplete")
	}
	if err := validateCommercialModuleIdentity(registration.ModuleKey, registration.FeatureKey, compiled, compiledModules, features); err != nil {
		return fmt.Errorf("commercial API operation %q: %w", registration.OperationID, err)
	}
	if registration.Method != strings.ToUpper(registration.Method) || !validAPIMethod(registration.Method) {
		return fmt.Errorf("commercial API operation %q has invalid method %q", registration.OperationID, registration.Method)
	}
	if !strings.HasPrefix(registration.Pattern, "/api/") || strings.ContainsAny(registration.Pattern, " \t\r\n") {
		return fmt.Errorf("commercial API operation %q has invalid API pattern %q", registration.OperationID, registration.Pattern)
	}
	if !validCommercialOperation(registration.Operation) {
		return fmt.Errorf("commercial API operation %q has invalid operation policy %q", registration.OperationID, registration.Operation)
	}
	if len(registration.RequiredPermissions) == 0 {
		return fmt.Errorf("commercial API operation %q must require RBAC permission", registration.OperationID)
	}
	descriptor, ok := features.Lookup(registration.FeatureKey)
	if !ok || !descriptor.RequiresTenantEntitlement {
		return fmt.Errorf("commercial API operation %q must use a tenant-entitled commercial feature", registration.OperationID)
	}
	allowedPermissions := make(map[string]struct{}, len(descriptor.RequiredPermissions))
	for _, permission := range descriptor.RequiredPermissions {
		allowedPermissions[permission] = struct{}{}
	}
	permissions := make(map[string]struct{}, len(registration.RequiredPermissions))
	for _, permission := range registration.RequiredPermissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			return fmt.Errorf("commercial API operation %q contains an empty permission", registration.OperationID)
		}
		if _, exists := permissions[permission]; exists {
			return fmt.Errorf("commercial API operation %q duplicates permission %q", registration.OperationID, permission)
		}
		if _, allowed := allowedPermissions[permission]; !allowed {
			return fmt.Errorf("commercial API operation %q uses permission %q outside feature %q", registration.OperationID, permission, registration.FeatureKey)
		}
		permissions[permission] = struct{}{}
	}
	switch registration.ResourceScope {
	case APIResourceScopeOrganization:
		if registration.ResourcePathParameter != "" &&
			!patternContainsPathParameter(registration.Pattern, registration.ResourcePathParameter) {
			return fmt.Errorf("commercial API operation %q does not declare resource path parameter %q", registration.OperationID, registration.ResourcePathParameter)
		}
	case APIResourceScopeWorkspace, APIResourceScopeProject:
		if strings.TrimSpace(registration.ResourcePathParameter) == "" ||
			!patternContainsPathParameter(registration.Pattern, registration.ResourcePathParameter) {
			return fmt.Errorf("commercial API operation %q requires a declared %s path parameter", registration.OperationID, registration.ResourceScope)
		}
	default:
		return fmt.Errorf("commercial API operation %q has invalid resource scope %q", registration.OperationID, registration.ResourceScope)
	}
	return nil
}

func validateCommercialModuleIdentity(
	moduleKey string,
	featureKey FeatureKey,
	compiled map[FeatureKey]struct{},
	compiledModules map[string]CompiledModule,
	features FeatureRegistry,
) error {
	if _, ok := compiled[featureKey]; !ok {
		return fmt.Errorf("module %q uses uncompiled feature %q", moduleKey, featureKey)
	}
	descriptor, ok := features.Lookup(featureKey)
	if !ok || !descriptor.RequiresTenantEntitlement {
		return fmt.Errorf("module %q must use a tenant-entitled commercial feature", moduleKey)
	}
	module, ok := compiledModules[moduleKey]
	if !ok {
		return fmt.Errorf("module %q is not declared in the Edition Manifest", moduleKey)
	}
	if module.FeatureKey != featureKey {
		return fmt.Errorf("module %q feature %q does not match manifest feature %q", moduleKey, featureKey, module.FeatureKey)
	}
	return nil
}

func validAPIMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func validCommercialOperation(operation CommercialOperation) bool {
	switch operation {
	case CommercialOperationReadOrExport,
		CommercialOperationCore,
		CommercialOperationWrite,
		CommercialOperationPaidProviderCreate,
		CommercialOperationAsyncPollOrCancel,
		CommercialOperationFinalization,
		CommercialOperationIdempotentRecovery:
		return true
	default:
		return false
	}
}

func patternContainsPathParameter(pattern, parameter string) bool {
	return strings.Contains(pattern, "{"+strings.TrimSpace(parameter)+"}")
}

func validRestrictionReason(reason RestrictionReason) bool {
	switch reason {
	case RestrictionInternalReleaseMismatch, RestrictionCommercialWritesFrozen:
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
	_, _ = hash.Write([]byte(ContractVersionV2))
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

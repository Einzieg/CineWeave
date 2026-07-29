package edition

type staticFeatureRegistry struct {
	ordered []FeatureDescriptor
	byKey   map[FeatureKey]FeatureDescriptor
}

func NewFeatureRegistry(descriptors []FeatureDescriptor) (FeatureRegistry, error) {
	registry := &staticFeatureRegistry{
		ordered: make([]FeatureDescriptor, 0, len(descriptors)),
		byKey:   make(map[FeatureKey]FeatureDescriptor, len(descriptors)),
	}
	for _, descriptor := range descriptors {
		if descriptor.Key == "" {
			return nil, newAuthorizationError(DenialFeatureUnknown, "feature key is required")
		}
		if _, exists := registry.byKey[descriptor.Key]; exists {
			return nil, newAuthorizationError(DenialFeatureUnknown, "feature key is duplicated")
		}
		cloned := cloneFeatureDescriptor(descriptor)
		registry.ordered = append(registry.ordered, cloned)
		registry.byKey[descriptor.Key] = cloned
	}
	return registry, nil
}

func DefaultFeatureRegistry() FeatureRegistry {
	registry, err := NewFeatureRegistry(defaultFeatureDescriptors())
	if err != nil {
		panic(err)
	}
	return registry
}

func (r *staticFeatureRegistry) All() []FeatureDescriptor {
	result := make([]FeatureDescriptor, 0, len(r.ordered))
	for _, descriptor := range r.ordered {
		result = append(result, cloneFeatureDescriptor(descriptor))
	}
	return result
}

func (r *staticFeatureRegistry) Lookup(key FeatureKey) (FeatureDescriptor, bool) {
	descriptor, ok := r.byKey[key]
	return cloneFeatureDescriptor(descriptor), ok
}

func cloneFeatureDescriptor(descriptor FeatureDescriptor) FeatureDescriptor {
	descriptor.RequiredPermissions = append([]string(nil), descriptor.RequiredPermissions...)
	return descriptor
}

func defaultFeatureDescriptors() []FeatureDescriptor {
	return []FeatureDescriptor{
		coreFeature(FeatureCoreWorkflow, "workflow create boundaries", "projects"),
		coreFeature(FeatureCoreProviderGateway, "provider gateway routing", "provider-settings"),
		coreFeature(FeatureCoreSelfHosting, "deployment composition root", "system-settings"),
		commercialFeature(FeatureBillingShadowAccount, []string{"organization.read", "billing.manage"}, "billing account provisioning", "billing-center", true),
		commercialFeature(FeatureBillingBalance, []string{"organization.read", "billing.read"}, "billing balance query", "billing-balance", false),
		commercialFeature(FeatureBillingTopUp, []string{"billing.topup"}, "billing top-up order creation", "billing-top-up", false),
		commercialFeature(FeatureBillingSubscription, []string{"billing.subscription.manage"}, "billing subscription mutation", "billing-subscription", false),
		commercialFeature(
			FeatureBillingOrganizationWallet,
			[]string{
				"organization.read",
				"project.read",
				"project.write",
				"billing.manage",
				"billing.spend",
				"billing.sponsor",
			},
			"project billing binding and provider create",
			"project-billing-settings",
			true,
		),
		commercialFeature(FeatureBillingReconciliation, []string{"billing.reconcile"}, "billing reconciliation command", "billing-reconciliation", false),
		commercialFeature(FeatureBillingInvoice, []string{"billing.read"}, "billing invoice query", "billing-invoices", false),
		commercialFeature(FeatureGovernanceSSO, []string{"admin.manage"}, "authentication composition root", "organization-security", false),
		commercialFeature(FeatureGovernanceSCIM, []string{"admin.manage"}, "identity provisioning boundary", "organization-security", false),
		commercialFeature(FeatureGovernanceAuditExport, []string{"billing.audit"}, "audit export command", "audit-export", false),
		commercialFeature(FeatureOperationsSupportedHA, []string{"admin.manage"}, "supported operations tooling", "system-operations", false),
		commercialFeature(FeatureOperationsManagedDR, []string{"admin.manage"}, "managed disaster recovery tooling", "system-operations", false),
	}
}

func coreFeature(key FeatureKey, enforcementPoint, frontendEntry string) FeatureDescriptor {
	return FeatureDescriptor{
		Key:                       key,
		MinimumContractVersion:    ContractVersionV2,
		RequiresTenantEntitlement: false,
		RequiredPermissions:       []string{},
		BackendEnforcementPoint:   enforcementPoint,
		FrontendEntry:             frontendEntry,
		DenialCode:                DenialPermission,
		DegradationBehavior:       "core behavior remains available",
		AffectsInFlightWorkflow:   false,
		AuditEvent:                "edition.entitlement.evaluated",
		MetricName:                "cineweave_entitlement_decisions_total",
	}
}

func commercialFeature(key FeatureKey, permissions []string, enforcementPoint, frontendEntry string, affectsInFlight bool) FeatureDescriptor {
	return FeatureDescriptor{
		Key:                       key,
		MinimumContractVersion:    ContractVersionV2,
		RequiresTenantEntitlement: true,
		RequiredPermissions:       permissions,
		BackendEnforcementPoint:   enforcementPoint,
		FrontendEntry:             frontendEntry,
		DenialCode:                DenialPlanEntitlementRequired,
		DegradationBehavior:       "hide commercial entry and preserve readable existing data",
		AffectsInFlightWorkflow:   affectsInFlight,
		AuditEvent:                "edition.entitlement.evaluated",
		MetricName:                "cineweave_entitlement_decisions_total",
	}
}

package api

import (
	"net/http"
	"strconv"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
)

func (s *Server) listProviderConnectors(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	items, err := s.providers.ListConnectors(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) importProviderConnector(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.ImportConnectorRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.ImportConnector(r.Context(), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) listProviderAccounts(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	items, err := s.providers.ListAccounts(r.Context(), orgID, r.URL.Query().Get("filter[status]"), queryInt(r, "limit", 20))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, map[string]any{"limit": queryInt(r, "limit", 20)})
}

func (s *Server) createProviderAccount(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.CreateAccountRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = organizationID(r, principal)
	}
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.CreateAccount(r.Context(), orgID, principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getProviderAccount(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.GetAccount(r.Context(), orgID, r.PathValue("accountId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateProviderAccount(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.UpdateAccountRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.UpdateAccount(r.Context(), orgID, r.PathValue("accountId"), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteProviderAccount(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	if err := s.providers.DeleteAccount(r.Context(), orgID, r.PathValue("accountId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) rotateProviderCredential(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.RotateCredentialRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.RotateCredential(r.Context(), orgID, r.PathValue("accountId"), principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) discoverProviderModels(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.DiscoverModels(r.Context(), orgID, r.PathValue("accountId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listProviderModels(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	items, err := s.providers.ListModels(r.Context(), orgID, r.PathValue("accountId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createProviderModel(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.CreateModelRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.CreateModel(r.Context(), orgID, r.PathValue("accountId"), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) updateProviderModel(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.UpdateModelRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.UpdateModel(r.Context(), orgID, r.PathValue("modelId"), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) testProviderModel(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.TestProviderModelRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	req.IdempotencyKey = idempotencyKey(r, req.IdempotencyKey)
	requestHash := idempotencyRequestHash(map[string]any{
		"modelId":  r.PathValue("modelId"),
		"testType": req.TestType,
		"input":    req.Input,
	})
	idempotencyState, ok := s.prepareIdempotency(w, r, orgID, "providers:model-test", req.IdempotencyKey, requestHash)
	if !ok {
		return
	}
	item, err := s.providers.RecordProviderModelTest(r.Context(), orgID, principal.UserID, r.PathValue("modelId"), req)
	if err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	if err := s.completeIdempotency(r.Context(), idempotencyState, item); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) validateProviderManifest(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.ValidateManifestRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.ValidateManifest(req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) runProviderManifestTest(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.ManifestTestRunRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	req.IdempotencyKey = idempotencyKey(r, req.IdempotencyKey)
	requestHash := idempotencyRequestHash(map[string]any{
		"accountId":       req.AccountID,
		"endpointKey":     req.EndpointKey,
		"pollEndpointKey": req.PollEndpointKey,
		"input":           req.Input,
		"manifest":        req.Manifest,
		"manifestText":    req.ManifestText,
		"maxPolls":        req.MaxPolls,
	})
	idempotencyState, ok := s.prepareIdempotency(w, r, orgID, "providers:manifest-test", req.IdempotencyKey, requestHash)
	if !ok {
		return
	}
	item, err := s.providers.RunManifestTest(r.Context(), orgID, principal.UserID, req)
	if err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	if err := s.completeIdempotency(r.Context(), idempotencyState, item); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) listModelProfiles(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	items, err := s.providers.ListModelProfiles(r.Context(), orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createModelProfile(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.CreateModelProfileRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.CreateModelProfile(r.Context(), orgID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) updateModelProfile(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.UpdateModelProfileRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.UpdateModelProfile(r.Context(), orgID, r.PathValue("profileId"), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) createModelProfileBinding(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.CreateModelProfileBindingRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.CreateModelProfileBinding(r.Context(), orgID, r.PathValue("profileId"), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) deleteModelProfileBinding(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	if err := s.providers.DeleteModelProfileBinding(r.Context(), orgID, r.PathValue("profileId"), r.PathValue("bindingId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) listProviderCallLogs(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	items, err := s.providers.ListCallLogs(r.Context(), orgID, provider.CallLogFilters{
		ProjectID: r.URL.Query().Get("filter[projectId]"),
		Status:    r.URL.Query().Get("filter[status]"),
		Limit:     queryInt(r, "limit", 20),
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getProviderUsageSummary(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.UsageSummary(r.Context(), orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listProviderLimitPolicies(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	items, err := s.providers.ListProviderLimitPolicies(r.Context(), orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createProviderLimitPolicy(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.CreateProviderLimitPolicyRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if req.OrganizationID == "" {
		req.OrganizationID = orgID
	}
	if req.OrganizationID != orgID {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "organizationId must match request context", nil, false)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.CreateProviderLimitPolicy(r.Context(), orgID, principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getProviderLimitPolicy(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.GetProviderLimitPolicy(r.Context(), orgID, r.PathValue("policyId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateProviderLimitPolicy(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req provider.UpdateProviderLimitPolicyRequest
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.UpdateProviderLimitPolicy(r.Context(), orgID, r.PathValue("policyId"), req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteProviderLimitPolicy(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	if err := s.providers.DeleteProviderLimitPolicy(r.Context(), orgID, r.PathValue("policyId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) listProviderCircuitStates(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	items, err := s.providers.ListProviderCircuitStates(r.Context(), orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) resetProviderCircuitState(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionProviderManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.providers.ResetProviderCircuitState(r.Context(), orgID, r.PathValue("stateId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) requireOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal, orgID string) bool {
	if orgID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "ORGANIZATION_REQUIRED", "organization context is required", nil, false)
		return false
	}
	if err := s.ensureOrganizationMember(r, principal.UserID, orgID); err != nil {
		s.writeError(w, r, err)
		return false
	}
	return true
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

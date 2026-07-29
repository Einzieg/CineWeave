package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/provider"
)

type apiProviderIdentityContext struct {
	principal  auth.Principal
	invocation string
}

type apiProviderIdentityContextKey struct{}

func withAPIProviderIdentity(
	ctx context.Context,
	principal auth.Principal,
	invocation string,
) context.Context {
	return context.WithValue(ctx, apiProviderIdentityContextKey{}, apiProviderIdentityContext{
		principal:  principal,
		invocation: strings.TrimSpace(invocation),
	})
}

func gatewayBillingIdentityFromContext(
	ctx context.Context,
	operationPermission string,
	reason string,
) provider.GatewayBillingIdentity {
	identity, _ := ctx.Value(apiProviderIdentityContextKey{}).(apiProviderIdentityContext)
	return provider.GatewayBillingIdentity{
		RequestedByUserID:          strings.TrimSpace(identity.principal.UserID),
		BillingOperationPermission: strings.TrimSpace(operationPermission),
		BillingContextReason:       strings.TrimSpace(reason),
	}
}

func gatewayProviderIdempotencyKey(
	ctx context.Context,
	taskType string,
	parts ...string,
) string {
	identity, _ := ctx.Value(apiProviderIdentityContextKey{}).(apiProviderIdentityContext)
	values := []string{
		"cineweave.api-provider.v1",
		strings.TrimSpace(identity.invocation),
		strings.TrimSpace(identity.principal.UserID),
		strings.TrimSpace(taskType),
	}
	for _, part := range parts {
		values = append(values, strings.TrimSpace(part))
	}
	hash := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return "api-provider:" + hex.EncodeToString(hash[:])
}

func (s *Server) firstAuthorizedProjectPermission(
	ctx context.Context,
	principal auth.Principal,
	projectID string,
	permissions ...string,
) string {
	for _, permission := range permissions {
		if err := s.authorizer.Authorize(
			ctx,
			principal,
			permission,
			authz.Resource{ProjectID: projectID},
		); err == nil {
			return permission
		}
	}
	return ""
}

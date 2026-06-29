package provider

import "context"

func (g *ProviderGuard) Release(ctx context.Context, lease GuardLease, providerCallID string) error {
	if g == nil || g.DB == nil || lease.LeaseToken == "" {
		return nil
	}
	_, err := g.DB.Exec(ctx, `
		UPDATE provider_leases
		SET status = 'released',
		    released_at = now(),
		    provider_call_id = COALESCE(NULLIF($2, '')::uuid, provider_call_id)
		WHERE lease_token = $1
		  AND status = 'active'
	`, lease.LeaseToken, providerCallID)
	return err
}

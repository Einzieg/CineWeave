//go:build !commercial

package main

import (
	"context"

	"github.com/Einzieg/cineweave/internal/agentworkerapp"
	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/edition"
)

func buildEditionRuntime(
	_ context.Context,
	_ agentworkerapp.RuntimeDependencies,
) (*edition.Runtime, error) {
	return edition.NewCommunityRuntime(edition.CommunityOptions{
		CoreReleaseID:    config.Get("CINEWEAVE_RELEASE_ID", "local-dev"),
		RequestedEdition: config.Get("CINEWEAVE_EDITION", string(edition.EditionCommunity)),
		ContractHash:     config.Get("CINEWEAVE_CONTRACT_HASH", ""),
	})
}

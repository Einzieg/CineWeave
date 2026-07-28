//go:build !commercial

package main

import (
	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/edition"
)

func buildEditionRuntime() (*edition.Runtime, error) {
	return edition.NewCommunityRuntime(edition.CommunityOptions{
		CoreReleaseID:    config.Get("CINEWEAVE_RELEASE_ID", "local-dev"),
		RequestedEdition: config.Get("CINEWEAVE_EDITION", string(edition.EditionCommunity)),
		ContractHash:     config.Get("CINEWEAVE_CONTRACT_HASH", ""),
	})
}

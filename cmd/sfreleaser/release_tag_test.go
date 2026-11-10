package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_releaseTagsOnly(t *testing.T) {
	assert.True(t, releaseTagsOnly(&GlobalModel{Variant: VariantChain}), "chain")
	assert.False(t, releaseTagsOnly(&GlobalModel{Variant: VariantLibrary}), "library")
	assert.False(t, releaseTagsOnly(&GlobalModel{Variant: VariantApplication}), "application")
	assert.False(t, releaseTagsOnly(&GlobalModel{Variant: VariantSubstreams}), "Substreams package")
}

func Test_validVersion_releaseLinePrefix(t *testing.T) {
	for _, version := range []string{"v1.0.0", "v1.0.0-rc.1", "v0.1.4-fh3.1", "v0.1.3-fh3.1-3", "reth-v2.9.0-fh3.1-1", "world-chain-v2.6.0-fh3.1", "op-reth-v2.6.1-fh3.1", "base-v2.5.2.8-fh3.1-2"} {
		assert.NoError(t, validVersion(version), version)
	}

	for _, version := range []string{"1.0.0", "reth-2.9.0", "Reth-v2.9.0", "-v1.0.0", "reth_v2.9.0", "reth-v2.9"} {
		assert.Error(t, validVersion(version), version)
	}
}

func Test_tagLookupNotFound(t *testing.T) {
	jsonOnly := `{"message":"No commit found for SHA: refs/tags/reth-v2.9.0-fh3.1-2-rc.1","documentation_url":"https://docs.github.com/rest/commits/commits#get-a-commit","status":"422"}`

	assert.True(t, tagLookupNotFound(jsonOnly), "JSON body only, as in GitHub Actions")
	assert.True(t, tagLookupNotFound(jsonOnly+"\ngh: No commit found for SHA: refs/tags/x (HTTP 422)"), "with gh's stderr line")
	assert.True(t, tagLookupNotFound(`{"message":"Not Found","status":"404"}`), "unknown ref")
	assert.False(t, tagLookupNotFound(`{"message":"Bad credentials","status":"401"}`), "authentication error")
	assert.False(t, tagLookupNotFound("gh: Bad credentials (HTTP 401)"), "authentication error from gh")
}

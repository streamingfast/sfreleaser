package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_releaseTagsOnly(t *testing.T) {
	library := &GlobalModel{Variant: VariantLibrary}
	application := &GlobalModel{Variant: VariantApplication}
	substreams := &GlobalModel{Variant: VariantSubstreams}

	assert.True(t, releaseTagsOnly(library, &ReleaseModel{}), "Go library")
	assert.True(t, releaseTagsOnly(library, &ReleaseModel{Rust: &RustReleaseModel{}}), "Rust library without crates")
	assert.False(t, releaseTagsOnly(library, &ReleaseModel{Rust: &RustReleaseModel{Crates: []string{"substreams"}}}), "Rust library with crates")
	assert.False(t, releaseTagsOnly(application, &ReleaseModel{}), "application")
	assert.False(t, releaseTagsOnly(application, &ReleaseModel{Rust: &RustReleaseModel{}}), "Rust application")
	assert.False(t, releaseTagsOnly(substreams, &ReleaseModel{Substreams: &SubstreamsReleaseModel{}}), "Substreams package")
}

func Test_validVersion_releaseLinePrefix(t *testing.T) {
	for _, version := range []string{"v1.0.0", "v1.0.0-rc.1", "v0.1.4-fh3.1", "v0.1.3-fh3.1-3", "reth-v2.9.0-fh3.1-1", "world-chain-v2.6.0-fh3.1", "op-reth-v2.6.1-fh3.1", "base-v2.5.2.8-fh3.1-2"} {
		assert.NoError(t, validVersion(version), version)
	}

	for _, version := range []string{"1.0.0", "reth-2.9.0", "Reth-v2.9.0", "-v1.0.0", "reth_v2.9.0", "reth-v2.9"} {
		assert.Error(t, validVersion(version), version)
	}
}

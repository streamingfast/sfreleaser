package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_resolveKindVariant(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		variant    Variant
		wantKind   string
		wantVar    Variant
		deprecated bool
		wantErr    bool
	}{
		{name: "nothing", variant: VariantUnset},
		{name: "legacy application", variant: VariantApplication, wantKind: "application", wantVar: VariantApplication, deprecated: true},
		{name: "legacy library", variant: VariantLibrary, wantKind: "library", wantVar: VariantLibrary, deprecated: true},
		{name: "kind application", kind: "application", wantKind: "application", wantVar: VariantApplication},
		{name: "kind library", kind: "Library", wantKind: "library", wantVar: VariantLibrary},
		{name: "kind and same legacy variant", kind: "library", variant: VariantLibrary, wantKind: "library", wantVar: VariantLibrary, deprecated: true},
		{name: "kind and conflicting legacy variant", kind: "application", variant: VariantLibrary, wantVar: VariantLibrary, wantErr: true},
		{name: "substreams infers library", variant: VariantSubstreams, wantKind: "library", wantVar: VariantSubstreams},
		{name: "substreams library", kind: "library", variant: VariantSubstreams, wantKind: "library", wantVar: VariantSubstreams},
		{name: "substreams application rejected", kind: "application", variant: VariantSubstreams, wantVar: VariantSubstreams, wantErr: true},
		{name: "chain infers library", variant: VariantChain, wantKind: "library", wantVar: VariantChain},
		{name: "chain library", kind: "library", variant: VariantChain, wantKind: "library", wantVar: VariantChain},
		{name: "chain application", kind: "application", variant: VariantChain, wantKind: "application", wantVar: VariantChain},
		{name: "invalid kind", kind: "other", variant: VariantChain, wantVar: VariantChain, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, variant, deprecated, err := resolveKindVariant(tt.kind, tt.variant)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantVar, variant)
			assert.Equal(t, tt.deprecated, deprecated)
		})
	}
}

func Test_releaseTagsOnly_resolved(t *testing.T) {
	for _, tt := range []struct {
		kind    string
		variant Variant
		want    bool
	}{
		{"", VariantChain, true},
		{"library", VariantChain, true},
		{"application", VariantChain, false},
		{"library", VariantLibrary, false},
	} {
		kind, variant, _, err := resolveKindVariant(tt.kind, tt.variant)
		assert.NoError(t, err)
		assert.Equal(t, tt.want, releaseTagsOnly(&GlobalModel{Kind: kind, Variant: variant}), "%q %s", tt.kind, tt.variant)
	}
}

func Test_kindOf(t *testing.T) {
	assert.Equal(t, "application", kindOf(VariantApplication))
	assert.Equal(t, "library", kindOf(VariantLibrary))
	assert.Equal(t, "library", kindOf(VariantChain))
	assert.Equal(t, "", kindOf(VariantUnset))
}

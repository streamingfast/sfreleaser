package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_resolveKindVariant(t *testing.T) {
	tests := []struct {
		name       string
		kind       Kind
		variant    Variant
		wantKind   Kind
		wantVar    Variant
		deprecated bool
		wantErr    bool
	}{
		{name: "nothing", variant: VariantUnset},
		{name: "legacy application", variant: VariantApplication, wantKind: KindApplication, wantVar: VariantApplication, deprecated: true},
		{name: "legacy library", variant: VariantLibrary, wantKind: KindLibrary, wantVar: VariantLibrary, deprecated: true},
		{name: "kind application", kind: KindApplication, wantKind: KindApplication, wantVar: VariantApplication},
		{name: "kind library", kind: KindLibrary, wantKind: KindLibrary, wantVar: VariantLibrary},
		{name: "kind and same legacy variant", kind: KindLibrary, variant: VariantLibrary, wantKind: KindLibrary, wantVar: VariantLibrary, deprecated: true},
		{name: "kind and conflicting legacy variant", kind: KindApplication, variant: VariantLibrary, wantVar: VariantLibrary, wantErr: true},
		{name: "substreams infers library", variant: VariantSubstreams, wantKind: KindLibrary, wantVar: VariantSubstreams},
		{name: "substreams library", kind: KindLibrary, variant: VariantSubstreams, wantKind: KindLibrary, wantVar: VariantSubstreams},
		{name: "substreams application rejected", kind: KindApplication, variant: VariantSubstreams, wantVar: VariantSubstreams, wantErr: true},
		{name: "chain infers library", variant: VariantChain, wantKind: KindLibrary, wantVar: VariantChain},
		{name: "chain library", kind: KindLibrary, variant: VariantChain, wantKind: KindLibrary, wantVar: VariantChain},
		{name: "chain application", kind: KindApplication, variant: VariantChain, wantKind: KindApplication, wantVar: VariantChain},
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
		kind    Kind
		variant Variant
		want    bool
	}{
		{KindUnset, VariantChain, true},
		{KindLibrary, VariantChain, true},
		{KindApplication, VariantChain, false},
		{KindLibrary, VariantLibrary, false},
	} {
		kind, variant, _, err := resolveKindVariant(tt.kind, tt.variant)
		assert.NoError(t, err)
		assert.Equal(t, tt.want, releaseTagsOnly(&GlobalModel{Kind: kind, Variant: variant}), "%s %s", tt.kind, tt.variant)
	}
}

func Test_kindOf(t *testing.T) {
	assert.Equal(t, KindApplication, kindOf(VariantApplication))
	assert.Equal(t, KindLibrary, kindOf(VariantLibrary))
	assert.Equal(t, KindLibrary, kindOf(VariantChain))
	assert.Equal(t, KindUnset, kindOf(VariantUnset))
}

func Test_ParseKind(t *testing.T) {
	kind, err := ParseKind("Application")
	assert.NoError(t, err)
	assert.Equal(t, KindApplication, kind)

	_, err = ParseKind("other")
	assert.Error(t, err)
}

package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

const (
	// KindApplication is a project releasing an application, with a 'chain' variant it gets
	// a full GitHub release whose binaries are uploaded by the repository's own workflows.
	KindApplication = "application"

	// KindLibrary is a project releasing a library, with a 'chain' variant it is only its tag.
	KindLibrary = "library"
)

// resolveKindVariant resolves the 'kind' (application or library) and the 'variant' of a project
// from the configured values. The 'kind' is the main key, the 'application' and 'library'
// variants being its deprecated spelling, 'deprecated' reports that one was used. A kind without
// variant is its own variant, 'substreams' and 'chain' variants without kind are libraries.
func resolveKindVariant(rawKind string, variant Variant) (kind string, resolved Variant, deprecated bool, err error) {
	kind = strings.ToLower(rawKind)
	if kind != "" && kind != KindApplication && kind != KindLibrary {
		return "", variant, false, fmt.Errorf("the kind %q is invalid, use %q or %q", rawKind, KindApplication, KindLibrary)
	}

	switch variant {
	case VariantUnset:
		switch kind {
		case KindApplication:
			return kind, VariantApplication, false, nil
		case KindLibrary:
			return kind, VariantLibrary, false, nil
		}
		return "", VariantUnset, false, nil

	case VariantApplication, VariantLibrary:
		if kind != "" && kind != variant.Lower() {
			return "", variant, false, fmt.Errorf("the kind %q conflicts with the variant %q", kind, variant.Lower())
		}
		return variant.Lower(), variant, true, nil

	case VariantSubstreams:
		if kind == KindApplication {
			return "", variant, false, fmt.Errorf("the kind %q is not supported yet for the %q variant, only %q is", kind, variant.Lower(), KindLibrary)
		}
		return KindLibrary, variant, false, nil

	default:
		if kind == "" {
			kind = KindLibrary
		}
		return kind, variant, false, nil
	}
}

var warnDeprecatedVariantOnce sync.Once

// mustGetKindVariant reads the 'kind' and 'variant' flags and resolves them, quitting when
// they are invalid, see [resolveKindVariant].
func mustGetKindVariant(cmd *cobra.Command) (string, Variant) {
	kind, variant, deprecated, err := resolveKindVariant(sflags.MustGetString(cmd, "kind"), mustGetVariant(cmd))
	cli.NoError(err, "Invalid kind and variant")

	if deprecated {
		warnDeprecatedVariantOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "Warning: the variant %q is deprecated, use \"kind: %s\" (\"global.kind\" in the \".sfreleaser\" file or the \"--kind\" flag) instead\n", variant.Lower(), kind)
		})
	}

	return kind, variant
}

// kindOf is the kind of a project of the given variant when the kind is not configured.
func kindOf(variant Variant) string {
	switch variant {
	case VariantApplication:
		return KindApplication
	case VariantUnset:
		return ""
	default:
		return KindLibrary
	}
}

// NeedsVariant reports whether the variant must be configured next to the kind, which is the
// case when it is not the kind's own.
func (g *GlobalModel) NeedsVariant() bool {
	return g.Variant == VariantChain || g.Variant == VariantSubstreams
}

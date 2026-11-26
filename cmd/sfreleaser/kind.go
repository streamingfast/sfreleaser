package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"go.uber.org/zap"
)

//go:generate go-enum -f=$GOFILE --marshal --names --nocase

// ENUM(
//
//	Unset
//	Application
//	Library
//
// )
type Kind uint

func (k Kind) Lower() string {
	return strings.ToLower(k.String())
}

func mustGetKind(cmd *cobra.Command) Kind {
	raw := sflags.MustGetString(cmd, "kind")
	zlog.Debug("raw read kind", zap.String("raw", raw))

	if raw == "" {
		return KindUnset
	}

	kind, err := ParseKind(raw)
	cli.NoError(err, "Invalid")

	return kind
}

func isSupportedKind(x string) bool {
	return x != KindUnset.String()
}

// resolveKindVariant resolves the kind and the variant of a project from the configured
// values. The kind is the main key, the 'application' and 'library' variants being its
// deprecated spelling, 'deprecated' reports that one was used. A kind without variant is its
// own variant, 'substreams' and 'chain' variants without kind are libraries.
func resolveKindVariant(kind Kind, variant Variant) (resolvedKind Kind, resolvedVariant Variant, deprecated bool, err error) {
	switch variant {
	case VariantUnset:
		switch kind {
		case KindApplication:
			return kind, VariantApplication, false, nil
		case KindLibrary:
			return kind, VariantLibrary, false, nil
		}
		return KindUnset, VariantUnset, false, nil

	case VariantApplication, VariantLibrary:
		if kind != KindUnset && kind.Lower() != variant.Lower() {
			return kind, variant, false, fmt.Errorf("the kind %q conflicts with the variant %q", kind.Lower(), variant.Lower())
		}
		return kindOf(variant), variant, true, nil

	case VariantSubstreams:
		if kind == KindApplication {
			return kind, variant, false, fmt.Errorf("the kind %q is not supported yet for the %q variant, only %q is", kind.Lower(), variant.Lower(), KindLibrary.Lower())
		}
		return KindLibrary, variant, false, nil

	default:
		if kind == KindUnset {
			kind = KindLibrary
		}
		return kind, variant, false, nil
	}
}

var warnDeprecatedVariantOnce sync.Once

// mustGetKindVariant reads the 'kind' and 'variant' flags and resolves them, quitting when
// they are invalid, see [resolveKindVariant].
func mustGetKindVariant(cmd *cobra.Command) (Kind, Variant) {
	kind, variant, deprecated, err := resolveKindVariant(mustGetKind(cmd), mustGetVariant(cmd))
	cli.NoError(err, "Invalid kind and variant")

	if deprecated {
		warnDeprecatedVariantOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "Warning: the variant %q is deprecated, use \"kind: %s\" (\"global.kind\" in the \".sfreleaser\" file or the \"--kind\" flag) instead\n", variant.Lower(), kind.Lower())
		})
	}

	return kind, variant
}

// kindOf is the kind of a project of the given variant when the kind is not configured.
func kindOf(variant Variant) Kind {
	switch variant {
	case VariantApplication:
		return KindApplication
	case VariantUnset:
		return KindUnset
	default:
		return KindLibrary
	}
}

// NeedsVariant reports whether the variant must be configured next to the kind, which is the
// case when it is not the kind's own.
func (g *GlobalModel) NeedsVariant() bool {
	return g.Variant == VariantChain || g.Variant == VariantSubstreams
}

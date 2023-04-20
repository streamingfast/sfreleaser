package main

import (
	"strings"

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
type Variant uint

func (v Variant) Lower() string {
	return strings.ToLower(v.String())
}

func VariantResolveAlias(in string) string {
	lowered := strings.ToLower(in)
	switch lowered {
	case "app":
		return "application"
	case "lib":
		return "library"
	}

	return in
}

func mustGetVariant(cmd *cobra.Command) Variant {
	raw := sflags.MustGetString(cmd, "variant")
	zlog.Debug("raw read variant", zap.String("raw", raw))

	if raw == "" {
		return VariantUnset
	}

	variant, err := ParseVariant(VariantResolveAlias(raw))
	cli.NoError(err, "Invalid")

	return variant
}

func isSupportedVariant(x string) bool {
	return x != VariantUnset.String()
}

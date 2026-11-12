package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

//go:generate go-enum -f=$GOFILE --marshal --names --nocase

// Builder is what builds the binaries of a Go application.
//
// ENUM(
//
//	goreleaser-docker
//	native
//
// )
type Builder uint

// mustGetBuilder reads the 'builder' flag, else the 'release.builder' config value, so that a
// '.sfreleaser' file only needs to set it once, under its 'release' section. Unset, Go projects
// use the native builder and others Goreleaser.
func mustGetBuilder(cmd *cobra.Command, global *GlobalModel) Builder {
	raw := sflags.MustGetString(cmd, "builder")
	if raw == "" {
		raw = viper.GetString("release.builder")
	}
	if raw == "" {
		if global.Language == LanguageGolang {
			return BuilderNative
		}
		return BuilderGoreleaserDocker
	}

	builder, err := ParseBuilder(raw)
	cli.NoError(err, "Invalid 'builder' value")

	return builder
}

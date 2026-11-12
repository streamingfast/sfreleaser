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

// mustGetBuilder reads the 'builder' flag. On 'build', where it defaults to empty, the value
// configured on the 'release' command is used instead, so that a '.sfreleaser' file only needs
// to set it once, under its 'release' section.
func mustGetBuilder(cmd *cobra.Command) Builder {
	raw := sflags.MustGetString(cmd, "builder")
	if raw == "" {
		raw = viper.GetString("release.builder")
	}
	if raw == "" {
		return BuilderGoreleaserDocker
	}

	builder, err := ParseBuilder(raw)
	cli.NoError(err, "Invalid 'builder' value")

	return builder
}

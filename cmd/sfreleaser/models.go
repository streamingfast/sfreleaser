package main

import (
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"go.uber.org/zap/zapcore"
)

type GlobalModel struct {
	Project  string
	Binary   string
	Language Language
	Variant  Variant
	Root     string

	WorkingDirectory string
}

func (g *GlobalModel) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("project", g.Project)
	encoder.AddString("language", g.Language.String())
	encoder.AddString("variant", g.Variant.String())
	encoder.AddString("working_directory", g.WorkingDirectory)
	return nil
}

func mustGetGlobal(cmd *cobra.Command) *GlobalModel {
	global := &GlobalModel{
		Project:  sflags.MustGetString(cmd, "project"),
		Binary:   sflags.MustGetString(cmd, "binary"),
		Language: mustGetLanguage(cmd),
		Variant:  mustGetVariant(cmd),
		Root:     sflags.MustGetString(cmd, "root"),
	}

	global.WorkingDirectory = cli.WorkingDirectory()
	if global.Root != "" {
		global.WorkingDirectory = cli.AbsolutePath(global.Root)
	}

	if global.Project == "" {
		global.Project = filepath.Base(global.WorkingDirectory)
	}

	if global.Binary == "" {
		global.Binary = global.Project
	}

	return global
}

func (g *GlobalModel) ensureValidForRelease() {
	var errors []string
	if g.Language == LanguageUnset {
		errors = append(errors, `You must specify for which language you are building via flag ("--language"), config file ("global.language" in ".sfreleaser" file) or environment variable ("SFRELEASER_GLOBAL_LANGUAGE")`)
	}

	if g.Variant == VariantUnset {
		errors = append(errors, `You must specify for which variant you are building for via flag ("--variant"), config file ("global.variant" in ".sfreleaser" file) or environment variable ("SFRELEASER_GLOBAL_VARIANT")`)
	}

	if len(errors) != 0 {
		cli.Quit(strings.Join(errors, "\n"))
	}
}

type ReleaseModel struct {
	Version string

	// Rust is populated only if config if of type Rust
	Rust *RustReleaseModel
}

func (m *ReleaseModel) populateLanguageSpecificModel(cmd *cobra.Command, language Language) {
	switch language {
	case LanguageGolang:
		// Nothing

	case LanguageRust:
		m.Rust = &RustReleaseModel{}

		m.Rust.CargoPublishArgs = unquotedFlatten(sflags.MustGetString(cmd, "rust-cargo-publish-args"))
		m.Rust.Crates = sflags.MustGetStringArray(cmd, "rust-crates")

	default:
		cli.Quit("unhandled language %q", language)
	}
}

type RustReleaseModel struct {
	CargoPublishArgs []string
	Crates           []string
}

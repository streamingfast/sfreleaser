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
	Owner      string
	Project    string
	Binary     string
	Language   Language
	License    string
	Variant    Variant
	Root       string
	ConfigRoot string

	WorkingDirectory string
}

func (g *GlobalModel) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("owner", g.Owner)
	encoder.AddString("project", g.Project)
	encoder.AddString("binary", g.Binary)
	encoder.AddString("language", g.Language.String())
	encoder.AddString("license", g.License)
	encoder.AddString("variant", g.Variant.String())
	encoder.AddString("config_root", g.ConfigRoot)
	encoder.AddString("working_directory", g.WorkingDirectory)

	return nil
}

func mustGetGlobal(cmd *cobra.Command) *GlobalModel {
	global := &GlobalModel{
		Owner:    sflags.MustGetString(cmd, "owner"),
		Project:  sflags.MustGetString(cmd, "project"),
		Binary:   sflags.MustGetString(cmd, "binary"),
		Language: mustGetLanguage(cmd),
		License:  sflags.MustGetString(cmd, "license"),
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

	global.ConfigRoot = findSfreleaserDir(global.WorkingDirectory)

	return global
}

func (g *GlobalModel) ResolveFile(in string) string {
	if filepath.IsAbs(in) {
		return in
	}

	return filepath.Join(g.ConfigRoot, in)
}

func (g *GlobalModel) ensureValidForBuild() {
	g.ensureValidForRelease()

	if g.Language != LanguageGolang {
		cli.Quit(`'sfreleaser build' only works for Go projects at the moment, sorry!`)
	}
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

type BuildModel struct {
	Version string

	All       bool
	Platforms []string
}

func (m *BuildModel) populate(cmd *cobra.Command) {
	m.All = sflags.MustGetBool(cmd, "all")
	m.Platforms = sflags.MustGetStringArray(cmd, "platform")

	for i, platform := range m.Platforms {
		m.Platforms[i] = strings.Replace(strings.ToLower(platform), "/", "-", 1)
	}
}

type ReleaseModel struct {
	Version string

	Brew *BrewReleaseModel

	// Rust is populated only if config if of type Rust
	Rust *RustReleaseModel
}

func (m *ReleaseModel) populate(cmd *cobra.Command, language Language) {
	m.Brew = &BrewReleaseModel{
		Disabled: sflags.MustGetBool(cmd, "brew-disabled"),
		TapRepo:  sflags.MustGetString(cmd, "brew-tap-repo"),
	}

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

type GitHubReleaseModel struct {
	AllowDirty          bool
	EnvFilePath         string
	GoreleaseConfigPath string
	GoreleaserImageID   string
	ReleaseNotesPath    string
}

type BrewReleaseModel struct {
	Disabled bool
	TapRepo  string
}

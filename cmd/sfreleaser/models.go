package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type GlobalModel struct {
	Owner    string
	Project  string
	Binary   string
	Language Language
	License  string
	Variant  Variant
	// The root of the project as provided by the user, never modified,
	// most usage should use `WorkingDirectory` instead which is computed
	// from this value when set.
	Root string

	// ConfigRoot is the absolute path to the directory containing the ".sfreleaser"
	// file. It is computed from [WorkingDirectory] (which itself can be overriden by [Root]).
	//
	// The [Root]/[WorkingDirectory] could be different then [ConfigRoot] if the user
	// executes in a subdirectory of the project and the ".sfreleaser" file is in the
	// root of the project.
	ConfigRoot string

	GitRemote string

	// WorkingDirectory is the absolute path to directory all command should use
	// when execution and is computed based on other configuration values found
	// in this model.
	//
	// The value is first initialized to `os.Getwd()`, if `Root` is set, [WorkingDirectory]
	// is set to `cli.AbsolutePath(Root)`.
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
	encoder.AddString("git_remote", g.GitRemote)

	return nil
}

func mustGetGlobal(cmd *cobra.Command) *GlobalModel {
	global := &GlobalModel{
		Owner:     sflags.MustGetString(cmd, "owner"),
		Project:   sflags.MustGetString(cmd, "project"),
		Binary:    sflags.MustGetString(cmd, "binary"),
		Language:  mustGetLanguage(cmd),
		License:   sflags.MustGetString(cmd, "license"),
		Variant:   mustGetVariant(cmd),
		Root:      sflags.MustGetString(cmd, "root"),
		GitRemote: sflags.MustGetString(cmd, "git-remote"),
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

	// The relative path (aginst the root of the project) to the file containing
	// the README of the project. The file for now is inferred when populating the
	// model by trying to find the files "README.md" and "README" in the root of the
	// project (only root folder is explored, not recursive).
	ReadmeRelativePath *string

	// The relative path (aginst the root of the project) to the file containing
	// the LICENSE of the project. The file for now is inferred when populating the
	// model by trying to find the files "LICENSE.md" and "LICENSE" in the root of the
	// project (only root folder is explored, not recursive).
	LicenseRelativePath *string

	Brew *BrewReleaseModel

	// Rust is populated only if config if of type Rust
	Rust *RustReleaseModel
}

func (m *ReleaseModel) populate(cmd *cobra.Command, global *GlobalModel) {
	m.ReadmeRelativePath = findFile(global.WorkingDirectory, orMatcher(
		caseInsensitiveMatcher("README.md"),
		caseInsensitiveMatcher("README"),
	))

	m.LicenseRelativePath = findFile(global.WorkingDirectory, orMatcher(
		caseInsensitiveMatcher("LICENSE.md"),
		caseInsensitiveMatcher("LICENSE"),
	))

	m.Brew = &BrewReleaseModel{
		Disabled: sflags.MustGetBool(cmd, "brew-disabled"),
		TapRepo:  sflags.MustGetString(cmd, "brew-tap-repo"),
	}

	switch global.Language {
	case LanguageGolang:
		// Nothing

	case LanguageRust:
		m.Rust = &RustReleaseModel{}

		m.Rust.CargoPublishArgs = unquotedFlatten(sflags.MustGetString(cmd, "rust-cargo-publish-args"))
		m.Rust.Crates = sflags.MustGetStringArray(cmd, "rust-crates")

	default:
		cli.Quit("unhandled language %q", global.Language)
	}
}

func findFile(root string, matcher func(in string) bool) *string {
	entries, err := os.ReadDir(root)
	if err != nil {
		zlog.Warn("unable to walk config root directory", zap.String("root", root), zap.Error(err))
		return nil
	}

	for _, entry := range entries {
		if matcher(entry.Name()) {
			name := entry.Name()
			return &name
		}
	}

	return nil
}

func orMatcher(matchers ...func(string) bool) func(string) bool {
	return func(in string) bool {
		return slices.ContainsFunc(matchers, func(matcher func(string) bool) bool {
			return matcher(in)
		})
	}
}

func caseInsensitiveMatcher(in string) func(string) bool {
	return func(name string) bool {
		return strings.EqualFold(name, in)
	}
}

type RustReleaseModel struct {
	CargoPublishArgs []string
	Crates           []string
}

type GitHubReleaseModel struct {
	AllowDirty           bool
	EnvFilePath          string
	GoreleaserConfigPath string
	GoreleaserImageID    string
	ReleaseNotesPath     string
}

type BrewReleaseModel struct {
	Disabled bool
	TapRepo  string
}

package main

import (
	"os"
	"path/filepath"
	"regexp"
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

// parseRepository parses a repository string in various formats and returns owner and project.
// Accepted formats:
//   - owner/project
//   - github.com/owner/project
//   - https://github.com/owner/project
//   - https://github.com/owner/project.git
func parseRepository(repository string) (owner, project string) {
	// Remove common prefixes
	repository = strings.TrimPrefix(repository, "https://github.com/")
	repository = strings.TrimPrefix(repository, "http://github.com/")
	repository = strings.TrimPrefix(repository, "github.com/")

	// Remove .git suffix if present
	repository = strings.TrimSuffix(repository, ".git")

	// Split by / to get owner and project
	parts := strings.Split(repository, "/")

	if len(parts) < 2 {
		cli.Quit("Invalid repository format %q, expected format: <owner>/<project> (e.g., streamingfast/firehose-core)", repository)
	}

	if len(parts) > 2 {
		cli.Quit("Invalid repository format %q, expected format: <owner>/<project>, got too many path segments", repository)
	}

	owner = strings.TrimSpace(parts[0])
	project = strings.TrimSpace(parts[1])

	if owner == "" || project == "" {
		cli.Quit("Invalid repository format %q, both owner and project must be non-empty", repository)
	}

	return owner, project
}

func mustGetGlobal(cmd *cobra.Command) *GlobalModel {
	// Check for conflicting flags
	repository, repositoryProvided := sflags.MustGetStringProvided(cmd, "repository")
	owner, ownerProvided := sflags.MustGetStringProvided(cmd, "owner")
	project, projectProvided := sflags.MustGetStringProvided(cmd, "project")

	if repositoryProvided && (ownerProvided || projectProvided) {
		cli.Quit("Cannot use --repository flag together with --owner or --project flags")
	}

	// Parse repository if provided
	if repositoryProvided {
		owner, project = parseRepository(repository)
	}

	global := &GlobalModel{
		Owner:     owner,
		Project:   project,
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

	if g.Language != LanguageGolang && !(g.Language == LanguageRust && g.Variant == VariantSubstreams) {
		cli.Quit(`'sfreleaser build' only works for Go projects and Rust substreams projects at the moment, sorry!`)
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
		cli.Quit("%s", strings.Join(errors, "\n"))
	}
}

func (m *ReleaseModel) ensureValidForRelease(global *GlobalModel) {
	var errors []string

	if m.NoBinaries && global.Variant == VariantLibrary {
		errors = append(errors, `The "noBinaries" flag cannot be used with library variant as libraries already skip binary builds by default`)
	}

	if len(errors) != 0 {
		cli.Quit("%s", strings.Join(errors, "\n"))
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

	// NoBinaries when set to true will completely skip building binaries during release.
	// This is useful for making releases without providing final binaries on the GitHub release,
	// such as for library-only releases or when binaries are built through other means.
	// Note: This flag cannot be used with library variant as libraries already skip binary builds.
	NoBinaries bool

	Brew *BrewReleaseModel

	// Rust is populated only if config if of type Rust
	Rust *RustReleaseModel

	// Substreams is populated only if variant is Substreams
	Substreams *SubstreamsReleaseModel
}

var tapRepoOwnerPrefix = regexp.MustCompile(`^[^/]+/`)

func (m *ReleaseModel) populate(cmd *cobra.Command, global *GlobalModel) {
	m.ReadmeRelativePath = findFile(global.WorkingDirectory, orMatcher(
		caseInsensitiveMatcher("README.md"),
		caseInsensitiveMatcher("README"),
	))

	m.LicenseRelativePath = findFile(global.WorkingDirectory, orMatcher(
		caseInsensitiveMatcher("LICENSE.md"),
		caseInsensitiveMatcher("LICENSE"),
	))

	m.NoBinaries = sflags.MustGetBool(cmd, "no-binaries")

	tapRepo := sflags.MustGetString(cmd, "brew-tap-repo")

	tapRepoOwner := global.Owner
	tapRepoName := tapRepo

	if tapRepoOwnerPrefix.MatchString(tapRepo) {
		tapRepoOwner = strings.TrimSuffix(tapRepoOwnerPrefix.FindString(tapRepo), "/")
		tapRepoName = strings.TrimPrefix(tapRepo, tapRepoOwner+"/")
	}

	m.Brew = &BrewReleaseModel{
		Disabled:     sflags.MustGetBool(cmd, "brew-disabled"),
		TapRepoOwner: tapRepoOwner,
		TapRepoName:  tapRepoName,
	}

	switch global.Language {
	case LanguageGolang:
		// Nothing

	case LanguageRust:
		if global.Variant == VariantSubstreams {
			m.Substreams = &SubstreamsReleaseModel{}
			m.Substreams.RegistryURL = sflags.MustGetString(cmd, "substreams-registry-url")
			m.Substreams.TeamSlug = sflags.MustGetString(cmd, "substreams-publish-team-slug")
		} else {
			m.Rust = &RustReleaseModel{}
			m.Rust.CargoPublishArgs = unquotedFlatten(sflags.MustGetString(cmd, "rust-cargo-publish-args"))
			m.Rust.Crates = sflags.MustGetStringArray(cmd, "rust-crates")
		}

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

type SubstreamsReleaseModel struct {
	RegistryURL string
	TeamSlug    string
}

type GitHubReleaseModel struct {
	AllowDirty           bool
	EnvFilePath          string
	GoreleaserConfigPath string
	GoreleaserImageID    string
	ReleaseNotesPath     string
}

type BrewReleaseModel struct {
	Disabled     bool
	TapRepoOwner string
	TapRepoName  string
}

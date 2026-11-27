package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

// containerGlobalGitIgnorePath is the in-container mount target for the host
// user's global Git excludes file. It is intentionally namespaced under
// `/etc/sfreleaser/` so it cannot collide with anything shipped in the
// goreleaser-cross image and does not depend on the in-container HOME.
const containerGlobalGitIgnorePath = "/etc/sfreleaser/gitignore_global"

// gitConfigOverride is a single `key=value` Git config entry that we want to
// force inside the goreleaser-cross container, regardless of what `~/.gitconfig`
// might ship in the image. The collected set is materialized into the
// `GIT_CONFIG_COUNT` / `GIT_CONFIG_KEY_<n>` / `GIT_CONFIG_VALUE_<n>` env-var
// family that Git honors since 2.31.
type gitConfigOverride struct {
	key   string
	value string
}

func buildArtifacts(global *GlobalModel, build *BuildModel, githubRelease *GitHubReleaseModel) {
	releaseModel := &ReleaseModel{
		Version:     build.Version,
		CgoDisabled: build.CgoDisabled,
		Brew:        &BrewReleaseModel{Disabled: true},
	}

	if global.Language == LanguageRust {
		if global.Variant == VariantSubstreams {
			releaseModel.Substreams = &SubstreamsReleaseModel{}
		} else {
			releaseModel.Rust = &RustReleaseModel{}
		}
	}

	renderGoreleaserFile(global, releaseModel, githubRelease)

	var goreleaserArguments []string
	if build.All {
		// Nothing, default build all
	} else if len(build.Platforms) > 0 {
		for _, platform := range build.Platforms {
			goreleaserArguments = append(goreleaserArguments, "--id", platform)
		}
	} else {
		goreleaserArguments = []string{"--id", runtime.GOOS + "-" + runtime.GOARCH}
	}

	if build.Version == "" {
		goreleaserArguments = append(goreleaserArguments, "--snapshot")
	}

	run(goreleaseDockerCommand(global, githubRelease, "build", nil, goreleaserArguments)...)
}

// usesGoreleaser reports whether the release goes through Goreleaser, which is only the case
// for a Go application building binaries. Every other project only needs a draft GitHub release,
// created directly with 'gh' without Docker.
func usesGoreleaser(global *GlobalModel, release *ReleaseModel) bool {
	return global.Language == LanguageGolang && global.Variant == VariantApplication && !release.NoBinaries
}

func releaseGithub(global *GlobalModel, release *ReleaseModel, githubRelease *GitHubReleaseModel) {
	if devSkipGoreleaser {
		return
	}

	if !usesGoreleaser(global, release) {
		releaseGithubDraft(global, release, githubRelease)
		return
	}

	if release.Builder == BuilderNative {
		releaseGithubNative(global, release, githubRelease)
		return
	}

	renderGoreleaserFile(global, release, githubRelease)

	fmt.Println()
	run(goreleaseDockerCommand(global, githubRelease, "release", nil, []string{
		"--release-notes=" + githubRelease.ReleaseNotesPath,
	})...)
}

func goreleaseDockerCommand(global *GlobalModel, githubRelease *GitHubReleaseModel, command string, dockerExtraArguments []string, goReleaserExtraArguments []string) []string {
	platform := "linux/amd64"
	if runtime.GOARCH == "arm64" {
		platform = "linux/arm64"
	}

	arguments := []string{
		"docker",

		"run",
		"--platform", platform,
		"--rm",
		"-e CGO_ENABLED=1",
		"--env-file", githubRelease.EnvFilePath,
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"-v", cli.WorkingDirectory() + ":/go/src/work",
		"-w /go/src/work",
	}

	arguments = appendGitConfigOverrides(arguments)

	if global.Language == LanguageGolang {
		if output, _, err := maybeResultOf("go env GOCACHE"); err == nil && output != "" {
			arguments = append(arguments, "-e GOCACHE=/go/cache")
			arguments = append(arguments, "-v", strings.TrimSpace(output)+":/go/cache")
		}
	}

	arguments = append(arguments, dockerExtraArguments...)

	arguments = append(arguments, []string{
		githubRelease.GoreleaserImageID,

		// goreleaser arguments
		command,
		"-f", githubRelease.GoreleaserConfigPath,
		"--timeout=60m",
		"--clean",
	}...)

	if githubRelease.AllowDirty {
		arguments = append(arguments, "--skip=validate")
	}

	arguments = append(arguments, goReleaserExtraArguments...)

	return arguments
}

// appendGitConfigOverrides collects every Git config entry that sfreleaser
// wants to force inside the goreleaser-cross container (alongside any related
// bind mounts), then materializes the collected set as the
// `GIT_CONFIG_COUNT` / `GIT_CONFIG_KEY_<n>` / `GIT_CONFIG_VALUE_<n>` env-var
// family that Git honors since 2.31.
//
// Adding a new override in the future is intentionally a one-liner: write a
// small `collect…` helper that appends to `overrides` (and optionally adds the
// bind mount it needs), call it from here, and the env-var wiring updates
// automatically.
//
// When no overrides are collected, this is a silent no-op so the build is
// never blocked for an optional convenience.
func appendGitConfigOverrides(arguments []string) []string {
	var overrides []gitConfigOverride

	arguments, overrides = collectGlobalGitIgnoreOverride(arguments, overrides)

	if len(overrides) == 0 {
		return arguments
	}

	arguments = append(arguments, "-e GIT_CONFIG_COUNT="+strconv.Itoa(len(overrides)))
	for i, override := range overrides {
		idx := strconv.Itoa(i)
		arguments = append(arguments,
			"-e GIT_CONFIG_KEY_"+idx+"="+override.key,
			"-e GIT_CONFIG_VALUE_"+idx+"="+override.value,
		)
	}
	return arguments
}

// collectGlobalGitIgnoreOverride detects the host user's global Git excludes
// file, mounts it read-only into the container, and registers the matching
// `core.excludesFile` override. It is a silent no-op when nothing is found.
func collectGlobalGitIgnoreOverride(arguments []string, overrides []gitConfigOverride) ([]string, []gitConfigOverride) {
	hostPath, ok := resolveHostGlobalGitIgnore()
	if !ok {
		return arguments, overrides
	}

	zlog.Debug("mounting host global git ignore into goreleaser container",
		zap.String("host_path", hostPath),
		zap.String("container_path", containerGlobalGitIgnorePath),
	)

	arguments = append(arguments, "-v", hostPath+":"+containerGlobalGitIgnorePath+":ro")
	overrides = append(overrides, gitConfigOverride{
		key:   "core.excludesFile",
		value: containerGlobalGitIgnorePath,
	})
	return arguments, overrides
}

// releaseGithubDraft creates the draft GitHub release with 'gh', the same release Goreleaser
// creates: titled after the version, pointing at the current commit, marked as pre-release for
// a pre-release version, with the release notes, the given assets and, for a Substreams
// package, its '.spkg' file(s) attached.
func releaseGithubDraft(global *GlobalModel, release *ReleaseModel, githubRelease *GitHubReleaseModel, assets ...string) {
	commit := strings.TrimSpace(run("git rev-parse HEAD"))

	args := []string{
		"gh release create", release.Version,
		"--repo", global.Owner + "/" + global.Project,
		"--draft",
		"--title", release.Version,
		"--target", commit,
		"--notes-file", "'" + githubRelease.ReleaseNotesPath + "'",
	}

	prerelease := isPrereleaseVersion(release.Version)
	if prerelease {
		args = append(args, "--prerelease")
	}

	for _, asset := range assets {
		args = append(args, "'"+asset+"'")
	}

	if global.Variant == VariantSubstreams {
		packages, err := findSubstreamsPackages(".")
		cli.NoError(err, "Unable to list Substreams packages to attach to the release")

		for _, pkg := range packages {
			args = append(args, "'"+pkg+"'")
		}
	}

	fmt.Println()
	if prerelease {
		fmt.Printf("Creating draft release %s with gh, marked as pre-release\n", release.Version)
	} else {
		fmt.Printf("Creating draft release %s with gh\n", release.Version)
	}
	run(args...)
}

// findSubstreamsPackages lists the '.spkg' files under root, the files Goreleaser attaches for
// a Substreams release through its '**/*.spkg' extra files glob.
func findSubstreamsPackages(root string) ([]string, error) {
	var packages []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}

		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".spkg") {
			packages = append(packages, path)
		}

		return nil
	})

	return packages, err
}

// releaseGithubNative builds the binaries with the native builder, packages them as Goreleaser
// did and creates the draft release with them, then updates the Homebrew formula unless
// disabled.
func releaseGithubNative(global *GlobalModel, release *ReleaseModel, githubRelease *GitHubReleaseModel) {
	outputDir := filepath.Join("build", "dist")

	fmt.Println()
	fmt.Printf("Building %d target(s) with the native builder\n", len(nativeTargets))
	var binaries map[string]string
	err := runInterruptible(func(ctx context.Context) (err error) {
		binaries, err = nativeBuild(ctx, nativeBuildOptions{
			Binary: global.Binary,
			// Goreleaser stamped the version without its 'v'.
			Version:     strings.TrimPrefix(release.Version, "v"),
			CgoDisabled: release.CgoDisabled,
			Targets:     nativeTargets,
			OutputDir:   outputDir,
			ZigPath:     release.ZigPath,
		})
		return err
	})
	cli.NoError(err, "Unable to build the binaries")

	var files []string
	for _, file := range []*string{release.ReadmeRelativePath, release.LicenseRelativePath} {
		if file != nil {
			files = append(files, *file)
		}
	}
	if len(files) == 0 {
		files = defaultArchiveFiles(".")
	}

	archives, err := packageNativeArchives(nativePackageOptions{
		Project:   global.Project,
		Binary:    global.Binary,
		Binaries:  binaries,
		Files:     files,
		ModTime:   nativeArchiveModTime(),
		OutputDir: outputDir,
	})
	cli.NoError(err, "Unable to package the binaries")

	var assets []string
	for _, archive := range archives {
		assets = append(assets, archive.Path)
	}

	// The packages are release assets and in the checksums, but not in the Homebrew formula.
	released := archives
	if release.Deb {
		debConfig, err := debConfigFor(global, release)
		cli.NoError(err, "Unable to describe the Debian packages")

		var packages []debPackage
		err = runInterruptible(func(ctx context.Context) (err error) {
			packages, err = buildDebPackages(ctx, debOptions{
				Name:       global.Binary,
				Version:    release.Version,
				ConfigPath: debConfigPath(global, release),
				Config:     debConfig,
				Binaries:   binaries,
				OutputDir:  outputDir,
				SmokeTest:  release.DebSmokeTest,
			})
			return err
		})
		cli.NoError(err, "Unable to build the Debian packages")

		released = append(slices.Clone(archives), packages...)
		for _, pkg := range packages {
			assets = append(assets, pkg.Path)
		}
	}

	if !release.ChecksumsDisabled {
		checksums := filepath.Join(outputDir, "checksums.txt")
		cli.NoError(writeChecksums(checksums, released), "Unable to write %q", checksums)
		assets = append(assets, checksums)
	}

	releaseGithubDraft(global, release, githubRelease, assets...)
	release.Brew.Archives = archives

	if !release.Brew.Disabled && !release.Brew.Deferred {
		_, err := publishBrewFormula(global, release, archives)
		cli.NoError(err, "Unable to update the Homebrew formula")
	}
}

// prereleaseVersionRegex splits a version in its release line prefix, like 'reth-', its
// numbers and its suffix.
var prereleaseVersionRegex = regexp.MustCompile(`^(?:[a-z][a-z0-9-]*?-)?v?[0-9]+\.[0-9]+\.[0-9]+(?:\.[0-9]+)?(.*)$`)

// prereleaseIdentifierRegex matches an identifier of the suffix marking a pre-release, like
// 'rc' in 'v1.2.0-rc.1' or 'alpha1'.
var prereleaseIdentifierRegex = regexp.MustCompile(`^(alpha|beta|rc|pre|preview|dev|nightly|snapshot|test)[0-9]*$`)

// isPrereleaseVersion reports whether version is a pre-release: an identifier of its suffix,
// split on '-' and '.', is a pre-release keyword, like 'v1.2.0-rc.1' or
// 'reth-v3.0.0-beta.1-fh3.1'. The release line prefix and a suffix like the Firehose one of
// 'reth-v2.11.0-fh3.1-2' do not make a pre-release.
func isPrereleaseVersion(version string) bool {
	version, _, _ = strings.Cut(strings.ToLower(version), "+")

	matches := prereleaseVersionRegex.FindStringSubmatch(version)
	if matches == nil {
		return false
	}

	return slices.ContainsFunc(strings.FieldsFunc(matches[1], func(r rune) bool { return r == '-' || r == '.' }), prereleaseIdentifierRegex.MatchString)
}

// defaultArchiveFiles are the files of root Goreleaser archived next to the binary when the
// project had neither README nor LICENSE: its license, readme and changelog files, whatever
// their case.
func defaultArchiveFiles(root string) []string {
	found := map[string]bool{}
	for _, pattern := range []string{"license*", "LICENSE*", "readme*", "README*", "changelog*", "CHANGELOG*"} {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil && info.Mode().IsRegular() {
				found[filepath.Base(match)] = true
			}
		}
	}

	files := make([]string, 0, len(found))
	for file := range found {
		files = append(files, file)
	}
	sort.Strings(files)

	return files
}

// debConfigPath is the nfpm configuration file to use as is, empty when it is generated.
func debConfigPath(global *GlobalModel, release *ReleaseModel) string {
	if release.DebConfig == "" {
		return ""
	}

	return global.ResolveFile(release.DebConfig)
}

// repositoryMetadata is what GitHub knows of the repository.
type repositoryMetadata struct {
	Description string `json:"description"`
	LicenseInfo struct {
		SPDXID string `json:"spdxId"`
	} `json:"licenseInfo"`
}

// parseRepositoryMetadata decodes 'gh repo view --json description,homepageUrl,licenseInfo'.
func parseRepositoryMetadata(output string) (repositoryMetadata, error) {
	var metadata repositoryMetadata
	if err := json.Unmarshal([]byte(output), &metadata); err != nil {
		return metadata, err
	}

	// GitHub's placeholders for a license it could not identify.
	if metadata.LicenseInfo.SPDXID == "NOASSERTION" {
		metadata.LicenseInfo.SPDXID = ""
	}

	return metadata, nil
}

// debMaintainer is the git identity, 'Name <email>'.
func debMaintainer() string {
	name, _, _ := maybeResultOf("git config user.name")
	email, _, _ := maybeResultOf("git config user.email")
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" || email == "" {
		return ""
	}

	return fmt.Sprintf("%s <%s>", name, email)
}

// debConfigFor is the package description the nfpm configuration is generated from. What is not
// configured is inferred from the repository files first (the license from the LICENSE file, the
// description from the README, the homepage from the GitHub repository) and only the description
// and license still missing are read from the GitHub repository. The maintainer is the git
// identity.
func debConfigFor(global *GlobalModel, release *ReleaseModel) (debConfigOptions, error) {
	options := debConfigOptions{
		Name:        global.Binary,
		Maintainer:  cmp.Or(release.DebMaintainer, debMaintainer()),
		Description: release.DebDescription,
		Homepage:    cmp.Or(release.DebHomepage, fmt.Sprintf("https://github.com/%s/%s", global.Owner, global.Project)),
		License:     release.DebLicense,
		Depends:     release.DebDepends,
		Recommends:  release.DebRecommends,
		Dir:         global.ResolveFile(release.DebDir),
	}

	if options.License == "" && release.LicenseRelativePath != nil {
		options.License = detectLicense(global.ResolveFile(*release.LicenseRelativePath))
	}

	if options.Description == "" && release.ReadmeRelativePath != nil {
		options.Description = readmeDescription(global.ResolveFile(*release.ReadmeRelativePath))
	}

	if options.Description == "" || options.License == "" {
		var metadata repositoryMetadata
		if output, _, err := maybeResultOf("gh repo view", global.Owner+"/"+global.Project, "--json description,licenseInfo"); err != nil {
			zlog.Debug("unable to read the repository metadata", zap.Error(err))
		} else if metadata, err = parseRepositoryMetadata(output); err != nil {
			zlog.Debug("unable to decode the repository metadata", zap.Error(err))
		}

		options.Description = cmp.Or(options.Description, metadata.Description)
		options.License = cmp.Or(options.License, metadata.LicenseInfo.SPDXID)
	}

	options.Description = cmp.Or(options.Description, global.Binary)

	if options.Maintainer == "" {
		return options, errors.New(`no maintainer for the Debian packages, set "deb-maintainer" ('Name <email>') or the git "user.name" and "user.email"`)
	}

	return options, nil
}

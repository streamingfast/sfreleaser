package main

import (
	"fmt"
	"runtime"
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
		Version: build.Version,
		Brew:    &BrewReleaseModel{Disabled: true},
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

func releaseGithub(global *GlobalModel, release *ReleaseModel, githubRelease *GitHubReleaseModel) {
	if devSkipGoreleaser {
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

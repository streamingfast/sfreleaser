package main

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/streamingfast/cli"
)

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

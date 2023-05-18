package main

import (
	"fmt"

	"github.com/streamingfast/cli"
)

func releaseGithub(global *GlobalModel, release *ReleaseModel, githubRelease *GitHubReleaseModel) {
	if devSkipGoreleaser {
		return
	}

	renderGoreleaserFile(global, release, githubRelease)

	arguments := []string{
		"docker",

		"run",
		"--rm",
		"-e CGO_ENABLED=1",
		"--env-file", githubRelease.EnvFilePath,
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"-v", cli.WorkingDirectory() + ":/go/src/work",
		"-w /go/src/work",
		githubRelease.GoreleaserImageID,

		// goreleaser arguments
		"-f", githubRelease.GoreleaseConfigPath,
		"--timeout=60m",
		"--rm-dist",
		"--release-notes=" + githubRelease.ReleaseNotesPath,
	}

	if githubRelease.AllowDirty {
		arguments = append(arguments, "--skip-validate")
	}

	fmt.Println()
	run(arguments...)
}

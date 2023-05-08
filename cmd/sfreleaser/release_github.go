package main

import (
	"fmt"

	"github.com/streamingfast/cli"
)

func releaseGithub(model *GitHubReleaseModel) {
	if devSkipGoreleaser {
		return
	}

	arguments := []string{
		"docker",

		// docker arguments
		"run",
		"--rm",
		"-e CGO_ENABLED=1",
		"--env-file", model.EnvFilePath,
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"-v", cli.WorkingDirectory() + ":/go/src/work",
		"-w /go/src/work",
		model.GoreleaserImageID,

		// goreleaser arguments
		"-f", model.GoreleaseConfigPath,
		"--timeout=60m",
		"--rm-dist",
		"--release-notes=" + model.ReleaseNotesPath,
	}

	if model.AllowDirty {
		arguments = append(arguments, "--skip-validate")
	}

	fmt.Println()
	run(arguments...)
}

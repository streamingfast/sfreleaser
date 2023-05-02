package main

import (
	"fmt"

	"github.com/streamingfast/cli"
)

func releaseGithub(goreleaseConfigPath string, allowDirty bool, envFilePath string, releaseNotesPath string) {
	if devSkipGoreleaser {
		return
	}

	golangCrossVersion := "v1.20.2"
	arguments := []string{
		"docker",

		// docker arguments
		"run",
		"--rm",
		"-e CGO_ENABLED=1",
		"--env-file", envFilePath,
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"-v", cli.WorkingDirectory() + ":/go/src/work",
		"-w /go/src/work",
		"goreleaser/goreleaser-cross:" + golangCrossVersion,

		// goreleaser arguments
		"-f", goreleaseConfigPath,
		"--timeout=60m",
		"--rm-dist",
		"--release-notes=" + releaseNotesPath,
	}

	if allowDirty {
		arguments = append(arguments, "--skip-validate")
	}

	fmt.Println()
	run(arguments...)
}

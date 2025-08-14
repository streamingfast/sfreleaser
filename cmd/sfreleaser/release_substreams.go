package main

import (
	"fmt"
	"strings"

	"github.com/streamingfast/cli"
)

func printSubstreamsRegistryNotPublishedMessage(substreams *SubstreamsReleaseModel) {
	cli.Ensure(substreams != nil, "Substreams model should have been populated by now but it's currently nil")

	fmt.Println(dedent(`
		Since release is not published yet, we have not performed Substreams package publishing to the
		registry. Once the release is published, you will need afterward to publish the package
		manually.

		Here is the command you need to perform to publish your package:
	`))

	fmt.Println()
	fmt.Println("  ", publishSubstreamsPackageCommand(substreams.RegistryURL, substreams.TeamSlug))

	fmt.Println()
	fmt.Println(dedent(`
		Ensure that you are on the published tag before doing the 'substreams registry publish' command, to
		be 100%% sure you are releasing the package from the correct commit.
	`))
}

func releaseSubstreamsPublishPackage(substreams *SubstreamsReleaseModel) {
	cli.Ensure(substreams != nil, "Substreams model should have been populated by now but it's currently nil")

	if devSkipSubstreamsRegistryPublish {
		return
	}

	run(publishSubstreamsPackageArgs(substreams.RegistryURL, substreams.TeamSlug)...)
}

func publishSubstreamsPackageArgs(registryURL string, teamSlug string) []string {
	args := []string{"substreams", "registry", "publish", "--yes"}
	if registryURL != "" {
		args = append(args, "--registry-url", registryURL)
	}
	if teamSlug != "" {
		args = append(args, "--teamSlug", teamSlug)
	}

	return args
}

func publishSubstreamsPackageCommand(registryURL string, teamSlug string) string {
	args := publishSubstreamsPackageArgs(registryURL, teamSlug)

	return strings.Join(args, " ")
}

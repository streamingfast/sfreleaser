package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/streamingfast/cli"
)

func releaseRustGitHub(global *GlobalModel, gitHubRelease *GitHubReleaseModel) {
	buildDirectory := "build"
	gitHubRelease.GoreleaseConfigPath = filepath.Join(buildDirectory, "goreleaser.yaml")

	cli.NoError(os.MkdirAll(buildDirectory, os.ModePerm), `Unable to create %q directory`, buildDirectory)

	goreleaserTemplate := goreleaserAppTmpl
	if global.Variant == VariantLibrary {
		goreleaserTemplate = goreleaserLibTmpl
	}

	renderTemplate(gitHubRelease.GoreleaseConfigPath, true, goreleaserTemplate, getInstallTemplateModel(global))

	releaseGithub(gitHubRelease)
}

func printRustCratesNotPublishedMessage(rust *RustReleaseModel) {
	cli.Ensure(rust != nil, "Rust model should have been populated by now but it's currently nil")

	fmt.Println(dedent(`
		Since release is not published yet, we have not perform crates publishing to crates.io
		repository. Once the release is published, you will need afterward to publish the crates
		manually.

		Here the command you need to perform to publish your crate(s):
	`))

	fmt.Println()
	for _, crate := range rust.Crates {
		fmt.Println("  ", publishRustCrateCommand(crate, rust.CargoPublishArgs))
	}

	fmt.Println()
	fmt.Println(dedent(`
		It's important to run them strictly in the order printed above, otherwise publishing will fail.

		Also, ensure that you are on the published tag before doing the 'cargo publish' commands, to
		be 100%% your are releasing the crates from the correct commit.
	`))
}

func releaseRustPublishCrates(rust *RustReleaseModel) {
	cli.Ensure(rust != nil, "Rust model should have been populated by now but it's currently nil")

	if devSkipRustCargoPublish {
		return
	}

	for _, crate := range rust.Crates {
		run(publishRustCrateArgs(crate, rust.CargoPublishArgs)...)
	}
}

func publishRustCrateArgs(crate string, publishArgs []string) []string {
	args := []string{"cargo publish"}
	args = append(args, publishArgs...)
	args = append(args, "-p", crate)

	return args
}

func publishRustCrateCommand(crate string, publishArgs []string) string {
	args := publishRustCrateArgs(crate, publishArgs)

	return strings.Join(unquotedFlatten(args...), " ")

}

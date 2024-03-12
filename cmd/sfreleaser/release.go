package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"go.uber.org/zap"
)

const deleteTagExitHandlerID = "delete-tag"

var ReleaseCmd = Command(release,
	"release [<version>]",
	"Perform the actual release",
	Description(`
		Based on the type of project your are building, perform the necessary step to perform
		a GitHub release of your project.

		How the build is performed and what build artifacts are published depends on the choosen
		language and variant.

		The config value:

			release:
		    	pre-build-hooks:
				- bash -c '{{ .release.Version }}'
				- echo "{{ .global.Project }}"

		Can be used to run a script before the actual build process so that you can perform
		so preparation.

		The upload a Substreams project to your release, define a pre-hook that build the
		Substreams project and upload it using upload extra assets:

			release:
				pre-build-hooks:
				- substreams pack -o substreams-near-{{ .release.Version }}.spkg

		Can be used to append a Substreams '.spkg' file to your release. If the received <manifest>
		ends with a '.spkg' extension, it's appended as is. Otherwise, it's assume to be a Substreams
		project in which case we build the '.spkg' for you.

		## Pre-build hooks template

		When using the 'pre-build-hooks' config value, you can use the following template variables:
		- {{ .global }}: The global model containing project information (see https://github.com/streamingfast/sfreleaser/blob/master/cmd/sfreleaser/models.go#L13)
		- {{ .release }}: The release model containing release specific information (see https://github.com/streamingfast/sfreleaser/blob/master/cmd/sfreleaser/models.go#L115)
		- {{ .build_dir }}: The final build directory used for the build

	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("allow-dirty", false, "Perform release step even if Git is not clean, tries to configured used tool(s) to also allow dirty Git state")
		flags.String("changelog-path", "CHANGELOG.md", "Path where to find the changelog file used to extract the release notes")
		flags.StringArray("pre-build-hooks", nil, "Set of pre build hooks to run before run the actual building steps, template your pre-hook with various injected variables, see long description of command for more details")
		flags.StringArray("upload-extra-assets", nil, "If provided, add this extra asset file to the release, use a 'pre-build-hooks' to generate the file if needed")
		flags.Bool("publish-now", false, "By default, publish the release to GitHub in draft mode, if the flag is used, the release is published as latest")
		flags.String("goreleaser-docker-image", "goreleaser/goreleaser-cross:v1.22", "Full Docker image used to run Goreleaser tool (which perform Go builds and GitHub releases (in all languages))")

		// Brew Flags
		flags.Bool("brew-disabled", false, "[Brew only] Disable Brew tap release completely, only applies for 'Golang'/'Application' types")
		flags.String("brew-tap-repo", "homebrew-tap", "[Brew only] The GitHub project name of the tap, the repo owner is defined by 'owner' config value")

		// Rust Flags
		flags.String("rust-cargo-publish-args", "", "[Rust only] The extra arguments to pass to 'cargo publish' when publishing, the tool might provide some default on its own, Bash rules are used to split the arguments from the string")
		flags.StringArray("rust-crates", nil, "[Rust only] The list of crates we should publish, the project is expected to be a workspace if this is used")

		// Deprecated Flags
		flags.String("upload-substreams-spkg", "", "If provided, add this Substreams package file to the release, if manifest is a 'substreams.yaml' file, the package is first built")
		flags.Lookup("upload-substreams-spkg").Deprecated = "use a --pre-build-hooks to build your '.spkg' and --upload-extra-assets to upload it, see command long description for more details"
	}),
	Execute(func(cmd *cobra.Command, args []string) error {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigs
			cli.Exit(1)
		}()

		if err := release(cmd, args); err != nil {
			// Return error normally, will hit OnCommandError right after
			return err
		}

		// Forces our exit handler (if any) to run
		cli.Exit(0)
		return nil
	}),
	OnCommandError(func(err error) {
		fmt.Println("The release failed with the following error:")
		fmt.Println()
		fmt.Println(err.Error())
		fmt.Println()

		fmt.Println("If the error is not super clear, you can use 'sfreleaser doctor' which")
		fmt.Println("list common errors and how to fix them.")

		cli.Exit(1)
	}),
)

func release(cmd *cobra.Command, args []string) error {
	global := mustGetGlobal(cmd)
	release := &ReleaseModel{Version: ""}
	if len(args) > 0 {
		release.Version = args[0]
		cli.NoError(validVersion(release.Version), "invalid version")
	}

	allowDirty := sflags.MustGetBool(cmd, "allow-dirty")
	changelogPath := global.ResolveFile(sflags.MustGetString(cmd, "changelog-path"))
	goreleaserDockerImage := sflags.MustGetString(cmd, "goreleaser-docker-image")
	publishNow := sflags.MustGetBool(cmd, "publish-now")
	preBuildHooks := sflags.MustGetStringArray(cmd, "pre-build-hooks")
	uploadExtraAssets := sflags.MustGetStringArray(cmd, "upload-extra-assets")

	// Deprecated, use uploadExtraAsset instead with a custom pre build hook for packaging
	uploadSubstreamsSPKG := sflags.MustGetString(cmd, "upload-substreams-spkg")

	release.populate(cmd, global.Language)

	zlog.Debug("starting 'sfreleaser release'",
		zap.Inline(global),
		zap.Bool("allow_dirty", allowDirty),
		zap.String("changelog_path", changelogPath),
		zap.String("goreleaser_docker_image", goreleaserDockerImage),
		zap.Bool("publish_now", publishNow),
		zap.Strings("pre_build_hooks", preBuildHooks),
		zap.String("upload", uploadSubstreamsSPKG),
		zap.String("upload_substreams_spkg (deprecated)", uploadSubstreamsSPKG),
		zap.Strings("upload_extra_assets", uploadExtraAssets),
		zap.Reflect("release_model", release),
	)

	global.ensureValidForRelease()

	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	verifyTools()

	if release.Version == "" {
		release.Version = promptVersion(changelogPath)
	}

	// For simplicity in the code below
	version := release.Version

	ensureGitHubReleaseValid(version)

	delay := 3 * time.Second
	fmt.Printf("Releasing %q (Draft: %t, Publish Now: %t) in %s...\n", version, !publishNow, publishNow, delay)
	time.Sleep(delay)

	ensureGitSync()

	buildDirectory := "build"
	envFilePath := filepath.Join(buildDirectory, ".env.release")
	releaseNotesPath := filepath.Join(buildDirectory, ".release_notes.md")

	cli.NoError(os.MkdirAll(buildDirectory, os.ModePerm), "Unable to create build directory")
	configureGitHubTokenEnvFile(envFilePath)
	cli.WriteFile(releaseNotesPath, readReleaseNotes(changelogPath))

	// By doing this after creating the build directory and release notes, we ensure
	// that those are ignored, the user will need to ignore them to process (or --allow-dirty).
	if !allowDirty {
		ensureGitNotDirty()
	}

	if uploadSubstreamsSPKG != "" {
		if !strings.HasSuffix(uploadSubstreamsSPKG, ".spkg") {
			manifestFile := uploadSubstreamsSPKG
			uploadSubstreamsSPKG = filepath.Join("{{ .buildDir }}", global.Project+"-"+version+".spkg")

			zlog.Warn(fmt.Sprintf(`the 'upload-substreams-spkg' flag is deprecated, use a custom "pre-build-hooks: ['substreams pack -o "{{ .buildDir }}/substreams-{{ .release.Version }}.spkg" %s'] to package it and 'upload-extra-assets: ['{{ .buildDir }}/substreams-{{ .version }}.spkg'] to attach it to the release`, manifestFile))
			preBuildHooks = append(preBuildHooks, fmt.Sprintf("substreams pack -o '%s' '%s'", global.ResolveFile(uploadSubstreamsSPKG), global.ResolveFile(manifestFile)))
		} else {
			zlog.Warn(fmt.Sprintf("the 'upload-substreams-spkg' flag is deprecated, use a custom 'upload-extra-assets: [%s]' (under 'release' section) to attach it to the release", uploadSubstreamsSPKG))
		}

		uploadExtraAssets = append(uploadExtraAssets, uploadSubstreamsSPKG)
	}

	if len(preBuildHooks) > 0 {
		fmt.Println()
		fmt.Printf("Executing %d pre-build hook(s)\n", len(preBuildHooks))
		executeHooks(preBuildHooks, buildDirectory, global, release)
	}

	if len(uploadExtraAssets) > 0 {
		fmt.Println()
		fmt.Printf("Uploading %d extra asset(s)\n", len(uploadExtraAssets))

		model := map[string]any{
			"global":   global,
			"release":  release,
			"buildDir": buildDirectory,
		}

		for i, extraAsset := range uploadExtraAssets {
			uploadExtraAssets[i] = resolveAsset(extraAsset, global, model)
		}
	}

	fmt.Println()
	fmt.Println("Creating temporary tag so that goreleaser can work properly")
	run("git tag", version)

	cli.ExitHandler(deleteTagExitHandlerID, func(_ int) {
		zlog.Debug("Deleting local temporary tag")
		runSilent("git tag -d", version)
	})

	gitHubRelease := &GitHubReleaseModel{
		AllowDirty:          allowDirty,
		EnvFilePath:         envFilePath,
		GoreleaseConfigPath: filepath.Join(buildDirectory, "goreleaser.yaml"),
		GoreleaserImageID:   goreleaserDockerImage,
		ReleaseNotesPath:    releaseNotesPath,
	}

	releaseGithub(global, release, gitHubRelease)

	for _, extraAsset := range uploadExtraAssets {
		fmt.Printf("Uploading asset file %q to release\n", filepath.Base(extraAsset))
		run("gh release upload", version, "'"+extraAsset+"'")
	}

	releaseURL := releaseURL(version)

	if publishNow {
		publishReleaseNow(global, release)
	} else {
		fmt.Println()
		fmt.Println(dedent(`
			Published release in **draft** mode

			View release at %s

			You can now publish it from the GitHub UI directly, on the release
			page, press the small pencil button in the right corner to edit the release
			and then press the 'Publish release' green button (scroll down to the bottom
			of the page.

			You can also publish from the GitHub CLI directly:

			  gh release edit %s --draft=false

			If something is wrong, you can delete the release from GitHub and try again by
			doing 'gh release delete %s'.
		`, releaseURL, version, version))

		fmt.Println()
		if yes, _ := cli.PromptConfirm("View release right now?"); yes {
			reviewRelease(releaseURL)
		}

		fmt.Println()
		if yes, _ := cli.PromptConfirm("Publish release right now?"); yes {
			publishReleaseNow(global, release)
		} else {
			if global.Language == LanguageRust && global.Variant == VariantLibrary {
				printRustCratesNotPublishedMessage(release.Rust)
			}
		}

		fmt.Println("Completed")
	}

	return nil
}

func executeHooks(hooks []string, buildDir string, global *GlobalModel, release *ReleaseModel) {
	model := map[string]any{
		"global":   global,
		"release":  release,
		"buildDir": buildDir,
	}

	for _, hook := range hooks {
		executeHook(hook, model)
	}
}

func executeHook(command string, model map[string]any) {
	parsed, err := template.New("hook").Parse(command)
	cli.NoError(err, "Parse hook template %q", command)

	// Hook length + 10% as the initial buffer size
	out := bytes.NewBuffer(make([]byte, 0, int(float64(len(command))*1.10)))
	cli.NoError(parsed.Execute(out, model), "Unable to execute template")

	zlog.Debug("hook templated", zap.Stringer("hook", out))

	run(out.String())
}

func resolveAsset(asset string, global *GlobalModel, model map[string]any) string {
	parsed, err := template.New("asset").Parse(asset)
	cli.NoError(err, "Parse asset template %q", asset)

	// Hook length + 10% as the initial buffer size
	out := bytes.NewBuffer(make([]byte, 0, int(float64(len(asset))*1.10)))
	cli.NoError(parsed.Execute(out, model), "Unable to execute template")

	zlog.Debug("asset templated", zap.Stringer("hook", out))

	return global.ResolveFile(out.String())
}

func verifyTools() {
	ensureCommandExist("docker", cli.Dedent(`
		The 'docker' utility (https://docs.docker.com/get-docker/) is perform the
		release.

		Install it via https://docs.docker.com/get-docker/. Ensure you have it enough
		resources allocated to it. You should use the fastest available options for your
		system. You should also allocate minimally 4 CPU and 8GiB of RAM.
	`))

	ensureCommandRunSuccesfully("docker info", cli.Dedent(`
		Ensure that your Docker Engine is currently running, it seems it's not running
		right now because the command 'docker info' failed.

		Try running the command 'docker info' locally to see the output. Ensure that it
		execute successuflly and exits with a 0 exit code (run 'echo $?' right after
		execution of the 'docker info' command to get its exit code).
	`))

	ensureCommandExist("gh", cli.Dedent(`
		The GitHub CLI utility (https://cli.github.com/) is required to obtain
		information about the current draft release.

		Install via brew with 'brew install gh' or refer https://github.com/cli/cli#installation
		otherwise.

		Don't forget to activate link with GitHub by doing 'gh auth login'.
	`))
}

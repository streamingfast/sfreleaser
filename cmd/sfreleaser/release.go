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
					- <command {{ .version }}>
					- <command {{ .project }}>

		Can be used to run a script before the actual build process so that you can perform
		so preparation.

		The config value:

			release:
				upload-substreams-spkg: <manifest>

		Can be used to append a Substreams '.spkg' file to your release. If the received <manifest>
		ends with a '.spkg' extension, it's appended as is. Otherwise, it's assume to be a Substreams
		project in which case we build the '.spkg' for you.
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("allow-dirty", false, "Perform release step even if Git is not clean, tries to configured used tool(s) to also allow dirty Git state")
		flags.String("changelog-path", "CHANGELOG.md", "Path where to find the changelog file used to extract the release notes")
		flags.StringArray("pre-build-hooks", nil, "Set of pre build hooks to run before run the actual building steps")
		flags.String("upload-substreams-spkg", "", "If provided, add this Substreams package file to the release, if manifest is a 'substreams.yaml' file, the package is first built")
		flags.Bool("publish-now", false, "By default, publish the release to GitHub in draft mode, if the flag is used, the release is published as latest")
		flags.String("goreleaser-docker-image", "goreleaser/goreleaser-cross:v1.20.3", "Full Docker image used to run Goreleaser tool (which perform Go builds and GitHub releases (in all languages))")

		// Rust Flags
		flags.String("rust-cargo-publish-args", "", "[Rust only] The extra arguments to pass to 'cargo publish' when publishing, the tool might provide some default on its own, Bash rules are used to split the arguments from the string")
		flags.StringArray("rust-crates", nil, "[Rust only] The list of crates we should publish, the project is expected to be a workspace if this is used")
	}),
	Execute(func(cmd *cobra.Command, args []string) error {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigs
			cli.Exit(1)
		}()

		if err := release(cmd, args); err != nil {
			// Let the normal flow happen, we trap error and exit properly
			return err
		}

		// Forces our exit handler (if any) to run
		cli.Exit(0)
		return nil
	}),
	OnCommandError(func(err error) {
		fmt.Println()
		fmt.Println("Error:", err.Error())
		cli.Exit(1)
	}),
)

func release(cmd *cobra.Command, args []string) error {
	global := mustGetGlobal(cmd)
	release := &ReleaseModel{Version: ""}
	if len(args) > 0 {
		release.Version = args[0]
	}

	allowDirty := sflags.MustGetBool(cmd, "allow-dirty")
	changelogPath := global.ResolveFile(sflags.MustGetString(cmd, "changelog-path"))
	goreleaserDockerImage := sflags.MustGetString(cmd, "goreleaser-docker-image")
	publishNow := sflags.MustGetBool(cmd, "publish-now")
	preBuildHooks := sflags.MustGetStringArray(cmd, "pre-build-hooks")
	uploadSubstreamsSPKG := sflags.MustGetString(cmd, "upload-substreams-spkg")

	release.populateLanguageSpecificModel(cmd, global.Language)

	zlog.Debug("starting 'sfreleaser release'",
		zap.Inline(global),
		zap.Bool("allow_dirty", allowDirty),
		zap.String("changelog_path", changelogPath),
		zap.String("goreleaser_docker_image", goreleaserDockerImage),
		zap.Bool("publish_now", publishNow),
		zap.Strings("pre_build_hooks", preBuildHooks),
		zap.String("upload_substreams_spkg", uploadSubstreamsSPKG),
		zap.Reflect("release_model", release),
	)

	global.ensureValidForRelease()

	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	verifyTools()

	if release.Version == "" {
		release.Version = promptVersion()
	}

	// For simplicity in the code below
	version := release.Version

	ensureGitHubReleaseValid(version)

	delay := 3 * time.Second
	fmt.Printf("Releasing %q (Draft: %t, Publish Now: %t) in %s...\n", version, !publishNow, publishNow, delay)
	time.Sleep(delay)

	ensureGitSync()

	cli.NoError(os.MkdirAll("build", os.ModePerm), "Unable to create build directory")

	configureGitHubTokenEnvFile("build/.env.release")
	cli.WriteFile("build/.release_notes.md", readReleaseNotes(changelogPath))

	// By doing this after creating the build directory and release notes, we ensure
	// that those are ignored, the user will need to ignore them to process (or --allow-dirty).
	if !allowDirty {
		ensureGitNotDirty()
	}

	if len(preBuildHooks) > 0 {
		fmt.Println()
		fmt.Printf("Executing %d pre-build hook(s)\n", len(preBuildHooks))
		executeHooks(preBuildHooks, global, release)
	}

	uploadSpkgPath := prepareSubstreamsSpkg(uploadSubstreamsSPKG, global, release)

	fmt.Println()
	fmt.Println("Creating temporary tag so that goreleaser can work properly")
	run("git tag", version)

	cli.ExitHandler(deleteTagExitHandlerID, func(_ int) {
		zlog.Debug("Deleting local temporary tag")
		runSilent("git tag -d", version)
	})

	gitHubRelease := &GitHubReleaseModel{
		AllowDirty:          allowDirty,
		EnvFilePath:         "build/.env.release",
		GoreleaseConfigPath: ".goreleaser.yaml",
		GoreleaserImageID:   goreleaserDockerImage,
		ReleaseNotesPath:    "build/.release_notes.md",
	}

	switch global.Language {
	case LanguageGolang:
		releaseGolangGitHub(gitHubRelease)

	case LanguageRust:
		releaseRustGitHub(global, gitHubRelease)

	default:
		cli.Quit("unhandled language %q", global.Language)
	}

	if uploadSpkgPath != "" {
		fmt.Printf("Uploading Substreams package file %q to release\n", filepath.Base(uploadSpkgPath))
		run("gh release upload", version, "'"+uploadSpkgPath+"'")
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

			If something is wrong, you can delete the release from GitHub
			and try again by doing 'gh release delete %s'.
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

func executeHooks(hooks []string, global *GlobalModel, release *ReleaseModel) {
	model := map[string]any{
		"global":  global,
		"release": release,
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

func prepareSubstreamsSpkg(spkgPath string, global *GlobalModel, release *ReleaseModel) string {
	if spkgPath == "" {
		return ""
	}

	if !strings.HasSuffix(spkgPath, ".spkg") {
		spkgPath = filepath.Join(global.WorkingDirectory, "build", global.Project+"-"+release.Version+".spkg")

		fmt.Printf("Packing your Substreams file at %q\n", spkgPath)
		run("substreams pack -o", "'"+spkgPath+"'")
	}

	return spkgPath
}

func verifyTools() {
	verifyCommand("docker", cli.Dedent(`
		The 'docker' utility (https://docs.docker.com/get-docker/) is perform the
		release.

		Install it via https://docs.docker.com/get-docker/. Ensure you have it enough
		resources allocated to it. You should use the fastest available options for your
		system. You should also allocate minimally 4 CPU and 8GiB of RAM.
	`))

	verifyCommandRunSuccesfully("docker info", cli.Dedent(`
		Ensure that your Docker Engine is currently running, it seems it's not running
		right now because the command 'docker info' failed.

		Try running the command 'docker info' locally to see the output. Ensure that it
		execute successuflly and exits with a 0 exit code (run 'echo $?' right after
		execution of the 'docker info' command to get its exit code).
	`))

	verifyCommand("gh", cli.Dedent(`
		The GitHub CLI utility (https://cli.github.com/) is required to obtain
		information about the current draft release.

		Install via brew with 'brew install gh' or refer https://github.com/cli/cli#installation
		otherwise.

		Don't forget to activate link with GitHub by doing 'gh auth login'.
	`))
}

package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"go.uber.org/zap"
)

var BuildCmd = Command(nil,
	"build [<version>]",
	"Build the actual artifacts this project would produce on release",
	Description(`
		Based on the type of project your are building, perform the necessary step to perform
		a build of the artifacts of your project.

		How the build is performed and what build artifacts are produced depends on the choosen
		language and variant.

		Refer to 'sfreleaser releaser --help' for more information on the available options.
	`),
	ExamplePrefixed("sfreleaser build", `
		# Build for the current platform when no argument

		# Build for all platforms
		--all

		# Build for specific platform(s)
		--platform linux/arm64 --platform linux/amd64

		# Build for specific platform(s) (alternative syntax)
		-P linux/arm64 -P linux/amd64
	`),
	Flags(func(flags *pflag.FlagSet) {
		// Those are all provided by 'release' now! This means duplication at the config level for
		// build vs release, what a mess. How to deal with this? I don't want to break compatibility.
		flags.Bool("allow-dirty", false, "Perform release step even if Git is not clean, tries to configured used tool(s) to also allow dirty Git state")
		flags.StringArray("pre-build-hooks", nil, "Set of pre build hooks to run before run the actual building steps")
		flags.String("goreleaser-docker-image", "goreleaser/goreleaser-cross:v1.25", "Full Docker image used to run Goreleaser tool (which perform Go builds and GitHub releases (in all languages))")

		// Flag specific to build
		flags.Bool("all", false, "Build for all platforms and not your current machine")
		flags.StringArrayP("platform", "P", nil, "Run only for those platform (repeat --platform <value> for multiple platforms), platform are defined as 'os/arch' (e.g. 'linux/amd64', dash separator also accepted), use 'darwin' to build for macOS and 'windows' for Windows (if activated)")
	}),
	Execute(func(cmd *cobra.Command, args []string) error {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigs
			cli.Exit(1)
		}()

		if err := build(cmd, args); err != nil {
			// Return error normally, will hit OnCommandError right after
			return err
		}

		// Forces our exit handler (if any) to run
		cli.Exit(0)
		return nil
	}),
	OnCommandError(func(err error) {
		fmt.Println("The build failed with the following error:")
		fmt.Println()
		fmt.Println(err.Error())
		fmt.Println()

		fmt.Println("If the error is not super clear, you can use 'sfreleaser doctor' which")
		fmt.Println("list common errors and how to fix them.")

		cli.Exit(1)
	}),
)

func build(cmd *cobra.Command, args []string) error {
	global := mustGetGlobal(cmd)
	build := &BuildModel{Version: ""}
	if len(args) > 0 {
		build.Version = args[0]
		cli.NoError(validVersion(build.Version), "invalid version")
	}

	allowDirty := sflags.MustGetBool(cmd, "allow-dirty")
	goreleaserDockerImage := sflags.MustGetString(cmd, "goreleaser-docker-image")
	preBuildHooks := sflags.MustGetStringArray(cmd, "pre-build-hooks")

	build.populate(cmd)

	zlog.Debug("starting 'sfreleaser build'",
		zap.Inline(global),
		zap.Bool("allow_dirty", allowDirty),
		zap.String("goreleaser_docker_image", goreleaserDockerImage),
		zap.Strings("pre_build_hooks", preBuildHooks),
		zap.Reflect("build_model", build),
	)

	global.ensureValidForBuild()

	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	verifyTools()

	// For simplicity in the code below
	version := build.Version
	fmt.Printf("Building %q ...\n", version)

	buildDirectory := "build"
	envFilePath := filepath.Join(buildDirectory, ".env.release")

	cli.NoError(os.MkdirAll(buildDirectory, os.ModePerm), "Unable to create build directory")
	configureGitHubTokenEnvFile(envFilePath)

	// By doing this after creating the build directory and release notes, we ensure
	// that those are ignored, the user will need to ignore them to process (or --allow-dirty).
	if !allowDirty {
		ensureGitNotDirty()
	}

	if len(preBuildHooks) > 0 {
		fmt.Println()
		fmt.Printf("Executing %d pre-build hook(s)\n", len(preBuildHooks))
		executeHooks(preBuildHooks, buildDirectory, global, &ReleaseModel{Version: version})
	}

	if version != "" {
		fmt.Println()
		fmt.Println("Creating temporary tag so that goreleaser can work properly")
		run("git tag", version)

		cli.ExitHandler(deleteTagExitHandlerID, func(_ int) {
			zlog.Debug("Deleting local temporary tag")
			runSilent("git tag -d", version)
		})
	}

	gitHubRelease := &GitHubReleaseModel{
		AllowDirty:           allowDirty,
		EnvFilePath:          envFilePath,
		GoreleaserConfigPath: filepath.Join(buildDirectory, "goreleaser.yaml"),
		GoreleaserImageID:    goreleaserDockerImage,
	}

	if global.Language == LanguageRust && global.Variant == VariantSubstreams {
		fmt.Println()
		fmt.Println("Building Substreams package (.spkg)")
		buildSubstreamsPackage(global)
	}

	fmt.Println()
	fmt.Printf("Building artifacts using image %q\n", goreleaserDockerImage)
	buildArtifacts(global, build, gitHubRelease)

	return nil
}

func buildSubstreamsPackage(global *GlobalModel) {
	// Run substreams build to generate the .spkg file
	fmt.Println("Running 'substreams build' to generate .spkg file...")
	run("substreams", "build")

	fmt.Println("✓ Substreams package (.spkg) built successfully")
}

func selectSubstreamsPackageForRelease(version string) (string, error) {
	// List all .spkg files in current directory
	spkgFiles, err := filepath.Glob("*.spkg")
	if err != nil {
		return "", fmt.Errorf("failed to list .spkg files: %w", err)
	}

	if len(spkgFiles) == 0 {
		return "", fmt.Errorf("no .spkg files found in current directory")
	}

	// Try to match by version
	var matchedFiles []string
	for _, file := range spkgFiles {
		if strings.Contains(file, version) {
			matchedFiles = append(matchedFiles, file)
		}
	}

	if len(matchedFiles) == 1 {
		fmt.Printf("Found .spkg file matching version %s: %s\n", version, matchedFiles[0])
		return matchedFiles[0], nil
	}

	if len(matchedFiles) == 0 {
		return "", fmt.Errorf("no .spkg files match version %s, available files: %v", version, spkgFiles)
	}

	return "", fmt.Errorf("multiple .spkg files match version %s, cannot determine which to use: %v", version, matchedFiles)
}

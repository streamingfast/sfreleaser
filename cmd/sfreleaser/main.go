package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"

	versioning "github.com/hashicorp/go-version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"go.uber.org/zap"
)

// Version value, injected via go build `ldflags` at build time
var version = "dev"

var (
	devSkipGoreleaser       = os.Getenv("SFRELEASER_DEV_SKIP_GORELEASER") == "true"
	devSkipRustCargoPublish = os.Getenv("SFRELEASER_DEV_SKIP_RUST_CARGO_PUBLISH") == "true"
)

var (
	ptyDisabled = os.Getenv("SFRELEASER_DISABLE_PTY") == "true"
)

func main() {
	Run(
		"sfreleaser",
		"StreamingFast specific releaser tool for easier maintaining release process",

		ConfigureViper("SFRELEASER"),
		ConfigureReleaserConfigFile(),
		ConfigureVersion(version),
		ConfigureCheckMinVersion(),

		DoctorCmd,
		BuildCmd,
		ReleaseCmd,
		InstallCmd,

		Description(`
			**Important** This tool is meant for StreamingFast usage and is not a generic release tool. If
			you like it, feel free to use it but your are not our main target.

			Perform the necessary commands to perform a release of the project.
			The <version> is optional, if not provided, you'll be asked the question.

			The release is performed against GitHub, you need a valid GitHub API token
			with the necessary rights to upload release and push to repositories. It needs to
			be provided in file ~/.config/goreleaser/github_token or through an environment
			variable GITHUB_TOKEN.
		`),
		PersistentFlags(func(flags *pflag.FlagSet) {
			flags.StringP("owner", "o", "streamingfast", "The owner/organization owning the project, used to compute the GitHub repository name")
			flags.StringP("binary", "b", "", "The binary name of the project, defaults to <project> if empty (Golang compiles 'cmd/<binary>')")
			flags.StringP("language", "l", "", "The language this release is for")
			flags.String("license", "Apache-2.0", "The license used for the project")
			flags.StringP("variant", "v", "", "Defines the variant of the project")
			flags.StringP("project", "p", "", "Override default computed project name which is directory of root/working directory folder")
			flags.String("root", "", "If defined, change the working directory of the process before proceeding with the release")
			flags.String("sfreleaser-min-version", "", "If sets, will check that the version of sfreleaser is at least this version before attempting the build")
			flags.String("git-remote", "origin", "The git remote to use for pushing the release and commits")
		}),
	)
}

func ConfigureCheckMinVersion() cli.CommandOption {
	return cli.CommandOptionFunc(func(cmd *cobra.Command) {
		root := cmd.Root()

		hook := checkMinVersion
		if actual := root.PersistentPreRun; actual != nil {
			hook = func(cmd *cobra.Command, args []string) {
				actual(cmd, args)

				// We do the checker after the actual hook, so that we can be sure that
				// configuration is loaded properly from file
				checkMinVersion(cmd, args)
			}
		}

		root.PersistentPreRun = hook
	})
}

var goRuntimeFileTaggedVersionRegex = regexp.MustCompile(`sfreleaser@(v[0-9]+\.[0-9]+\.[0-9]+[\.|-]?((alpha|beta|rc)[\.|-][0-9]+)?)/`)

func checkMinVersion(cmd *cobra.Command, _ []string) {
	minVersionRaw := viper.GetString("global.sfreleaser-min-version")
	if minVersionRaw == "" {
		return
	}

	actualVersionRaw := version
	if actualVersionRaw == "dev" {
		// When the version is dev, it can be because the user is running the binary installed
		// through `go install` in which case we extract the version to check against from the
		// go module full path which embeds the version.
		//
		// If you are a developer, use the `devel/sfreleaser` binary instead of `go install` to
		// build your version to avoid this problem (use `direnv` to automatically fix your PATH).
		_, file, _, ok := runtime.Caller(0)
		if fileVersion := extractVersionFromRuntimeCallerFile(file); ok && fileVersion != "" {
			actualVersionRaw = fileVersion
		}
	}

	if actualVersionRaw == "dev" {
		cli.Quit(`You are running a development version of 'sfreleaser', please use a released version to proceed.`)
	}

	minVersion, err := versioning.NewVersion(minVersionRaw)
	cli.NoError(err, "the 'sfreleaser-min-version' flag value %q is invalid", minVersionRaw)

	actualVersion, err := versioning.NewVersion(actualVersionRaw)
	cli.NoError(err, "the actual version %q is invalid", actualVersionRaw)

	if actualVersion.LessThan(minVersion) {
		onMinVersionCheckFailed(actualVersionRaw, minVersionRaw)
	}
}

func extractVersionFromRuntimeCallerFile(file string) string {
	if groups := goRuntimeFileTaggedVersionRegex.FindStringSubmatch(file); len(groups) > 0 {
		return groups[1]
	}

	return ""
}

func onMinVersionCheckFailed(actualVersion string, acceptedMinVersion string) {
	cli.Quit(cli.Dedent(`
			You current version of 'sfreleaser' %q is outdated, please upgrade to
			at least %q.

			On Linux and MacOS, you can upgrade with:

				brew upgrade streamingfast/tap/sfreleaser

			You can upgrade by downloading the latest release from:

				https://github.com/streamingfast/sfreleaser/releases/latest

			You can also install from Go directly if you are a Golang developer:

				go install github.com/streamingfast/sfreleaser/cmd/sfreleaser@latest
		`), actualVersion, acceptedMinVersion)
}

func ensureCommandExist(command string, onErrorText string) {
	zlog.Debug("verifying command", zap.String("command", command))

	_, err := exec.LookPath(command)
	if err != nil {
		zlog.Debug("lookup path failed", zap.Error(err), zap.String("command", command), zap.String("PATH", os.Getenv("PATH")))

		fmt.Printf("Unable to find command %q\n", command)
		fmt.Println()
		fmt.Println(onErrorText)

		cli.Exit(1)
	}
}

func ensureCommandRunSuccesfully(command string, onErrorText string) {
	ensureCommandExist("docker", onErrorText)

	output, _, err := maybeResultOf(command)
	if err != nil {
		zlog.Debug("command check failed", zap.String("command", command), zap.String("output", output))

		fmt.Printf("Command %q did not execute succesfully, error with %q\n", command, err.Error())
		fmt.Println()
		fmt.Println(onErrorText)

		cli.Exit(1)
	}
}

func findSfreleaserDir(workingDirectory string) string {
	zlog.Debug("trying to find .sfreleaser directory", zap.String("working_directory", workingDirectory))
	current := workingDirectory
	volumeName := filepath.VolumeName(current)

	for {
		if current == volumeName || current == "/" {
			return ""
		}

		if _, err := os.Stat(filepath.Join(current, ".sfreleaser")); err == nil {
			return current
		}

		current = filepath.Dir(current)
	}
}
